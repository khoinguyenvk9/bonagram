package main

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

// Theme colours (the design canvas the user approved: graphite with one amber accent).
const (
	cBg      = "#121416" // the app and the message view
	cSurface = "#121416"
	cPanel   = "#16191c" // the chat list, the pinned row
	cFg      = "#e6e3dc"
	cMuted   = "#8d939a"
	cAccent  = "#e5a55d"
	cErr     = "#e58a8a"
	cBoost   = "#1d2125" // fields, menus, chips, cards
	cSel     = "#20252a" // the selected chat or message
	cWarm    = "#2a241c" // amber-tinted: highlighted menu row, your reactions, @bạn
	cOwn     = "#262119" // the background of your own messages
	cKey     = "#2a2f35" // key caps
	cKeyFg   = "#c9cdd2"
	cDivider = "#2a2f35"
	cBorder  = "#3a4047"
)

var palette = []string{"#c3a6d8", "#9fc5a8", "#e0b48a", "#8fb8d8", "#d8a0a0", "#c9c98f", "#a0c8c8", "#b8b8e0"}

const (
	followGap = 5 * time.Minute
	itemRows  = 3 // a chat in the list: name, preview, blank
	listY0    = 7 // blank, Bonagram, unread count, the 3-row search field, blank
	headRows  = 3 // the header beside it: blank, chat name and what it is, blank
)

// keyName writes a key the way the platform does: ⌃K ⌥⏎ ⏎ on macOS, Ctrl+K Alt+Enter Enter elsewhere.
func keyName(k string) string {
	if runtime.GOOS == "darwin" {
		r := strings.NewReplacer("ctrl+", "⌃", "alt+", "⌥", "shift+", "⇧", "enter", "⏎", "tab", "⇥")
		return strings.ToUpper(r.Replace(k))
	}
	parts := strings.Split(k, "+")
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "+")
}

// keyCap is a key drawn as a small cap: the name on a lighter background, a space on each side.
func keyCap(k string) string {
	return chip(cKeyFg, cKey, " "+keyName(k)+" ", false)
}

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

func rgb(h string) (int, int, int) {
	v, _ := strconv.ParseUint(strings.TrimPrefix(h, "#"), 16, 32)
	return int(v >> 16 & 255), int(v >> 8 & 255), int(v & 255)
}

func sgr(foreground, background string) string {
	r, g, b := rgb(foreground)
	r2, g2, b2 := rgb(background)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm", r, g, b, r2, g2, b2)
}

// paint fills a line to width w with a background: the colours are re-applied after every reset inside
// s, so styled pieces never punch holes in it.
func paint(s string, w int, foreground, background string) string {
	wd := ansi.StringWidth(s) // measured once: this runs for every row of every frame
	if wd > w {
		s = ansi.Truncate(s, w, "")
		wd = ansi.StringWidth(s)
	}
	base := seq('f', foreground) + seq('b', background)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+base)
	return base + s + strings.Repeat(" ", max(w-wd, 0)) + "\x1b[0m"
}

// setContent gives the message view its rows; View reads the visible ones from m.vlines directly.
func (m *model) setContent(rows []string) {
	m.vlines = rows
	m.vp.SetContent(strings.Join(rows, "\n")) // the viewport still keeps the offset and the scrolling
}

func nameStyle(name string) lipgloss.Style { return fg(nameColour(name)).Bold(true) }

// followUp: same author within a few minutes of the previous message, shown without a name/time line.
// ticks is the read mark of the run of own messages starting at ls[i] (only its first row has a time):
// ✓✓ once the other side read all of them, ✓ before. None in Saved Messages and channel posts.
func (m *model) ticks(ls []line, i int) string {
	if m.cur == m.selfKey {
		return ""
	}
	read, ro := true, m.readOut()
	for j := i; j < len(ls) && (j == i || (ls[j].out && followUp(ls[j], &ls[j-1]))); j++ {
		if ls[j].msg != nil && ls[j].msg.Post {
			return ""
		}
		read = read && ls[j].id != 0 && ls[j].id <= ro
	}
	if read {
		return " " + ink(cAccent, "✓✓")
	}
	return " " + ink(cMuted, "✓")
}

func followUp(l line, prev *line) bool {
	if prev == nil || l.event || prev.event || l.out != prev.out || l.sender != prev.sender {
		return false
	}
	d := l.at.Sub(prev.at)
	return d >= 0 && d < followGap
}

func cut(s string, n int) string {
	if rs := []rune(s); len(rs) > 4*n+8 { // only the start can be shown: don't measure the rest
		s = string(rs[:4*n+8])
	}
	s = flat(s)
	if ansi.StringWidth(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

func fit(s string, w int) string {
	w = max(w, 0)
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// render rebuilds the message view and remembers where each message starts.
func (m *model) render() {
	if m.cur == "" {
		m.lineAt, m.hits, m.nLines, m.quoteAt = m.lineAt[:0], m.hits[:0], 0, m.quoteAt[:0]
		m.setContent(strings.Split(strings.Repeat("\n", max(m.vp.Height/2-1, 0))+lipgloss.PlaceHorizontal(m.vp.Width, lipgloss.Center,
			ink(cMuted, "Chọn một chat ở bên trái · Ctrl+K để tìm · Ctrl+N / Ctrl+P để đổi chat · F1 hướng dẫn")), "\n"))
		return
	}
	if m.ms != nil && len(m.ms.res) > 0 {
		m.renderResults()
		return
	}
	ls := m.lines()
	byID := make(map[int]*line, len(ls))
	for i := range ls {
		if ls[i].id != 0 {
			byID[ls[i].id] = &ls[i]
		}
	}
	cw := max(m.vp.Width-4, 10) // 2 columns of gutter on each side
	bw := min(cw, max(cw*7/10, 24))
	var out []string
	var outRow []bool
	cache := make(map[*tg.Message]bodyCache, len(ls)) // what this render drew, for the next one
	defer func() { m.bodies = cache }()
	m.lineAt = m.lineAt[:0]
	m.hits = m.hits[:0]
	m.quoteAt = m.quoteAt[:0]
	now, runStart := time.Now(), 0
	for i, l := range ls {
		var prev *line
		if i > 0 {
			prev = &ls[i-1]
		}
		if prev == nil || !sameDay(prev.at, l.at) { // "──── Hôm nay ────" when the day changes
			out, outRow = append(out, ""), append(outRow, false)
			out, outRow = append(out, daySeparator(l.at, now, m.vp.Width)), append(outRow, false)
			prev = nil
		}
		var quote string
		t := byID[l.replyTo]
		switch {
		case l.replyTo == 0:
		case t != nil:
			quote = m.quoteOf(t.from, t.text)
		case l.quote != "":
			quote = m.quoteDrawn(l.quote)
		default:
			quote = ink(cMuted, "│ trả lời một tin nhắn cũ")
		}
		mine := !l.out && (l.mentioned || (t != nil && t.out))
		head := mine || !followUp(l, prev)
		if head {
			runStart = i
			out, outRow = append(out, ""), append(outRow, false)
		}
		m.lineAt = append(m.lineAt, len(out))

		var block []string
		var pend []hitSpec
		when := ink(cMuted, l.at.Format("15:04"))
		if head && !l.out {
			h := inkB(nameColour(l.from), l.from) + "  " + when
			if l.mentioned {
				h += "  " + chip(cAccent, cWarm, " @bạn ", false)
			}
			block = append(block, h)
		}
		quoteIdx := -1
		if quote != "" {
			quoteIdx = len(block)
			block = append(block, quote)
		}
		body, hs := m.cachedBody(l, bw, cw, cache)
		for _, h := range hs {
			pend = append(pend, hitSpec{len(block) + h.idx, h.w, h.text})
		}
		block = append(block, body...)
		if t := m.thumbs[l.id]; l.id != 0 && t != nil {
			block = append(block, t.rows...)
		}
		bubble, bubbleRows := 0, len(block) // your own message sits in a block of its own colour, as wide as its widest row
		if l.out {
			for _, b := range block {
				bubble = max(bubble, ansi.StringWidth(b)+2)
			}
			bubble = min(bubble, cw)
		}
		if len(l.reacts) > 0 {
			block = append(block, reactionLine(l.reacts, bw))
		}
		if l.out && (i+1 == len(ls) || !followUp(ls[i+1], &ls[i])) { // the time and ✓✓ under the last of a run
			block = append(block, when+m.ticks(ls, runStart))
		}

		sel := m.browse && i == m.bsel
		bar := " "
		switch {
		case sel, mine:
			bar = ink(cAccent, "▌")
		}
		for bi, b := range block {
			inBubble := l.out && bi < bubbleRows && bubble > 0
			for _, h := range pend {
				if h.idx == bi {
					start := 2
					if l.out {
						start += cw - bubble + 1
					}
					x0, x1 := copyButtonCols(start, h.w)
					m.hits = append(m.hits, copyHit{len(out), x0, x1, h.text})
				}
			}
			if bi == quoteIdx {
				m.quoteAt = append(m.quoteAt, quoteHit{len(out), l.replyTo})
			}
			switch {
			case inBubble:
				b = strings.Repeat(" ", max(cw-bubble, 0)) + paint(" "+b, bubble, cFg, cOwn)
			case l.out:
				b = lipgloss.PlaceHorizontal(cw, lipgloss.Right, b)
			}
			row := bar + " " + b
			if sel {
				row = paint(row, m.vp.Width, cFg, cSel)
			}
			out, outRow = append(out, row), append(outRow, l.out)
		}
	}
	m.nLines = len(out)
	m.rows, m.rowOut = out, outRow
	m.links = m.links[:0]
	for r, row := range out {
		m.links = append(m.links, linkSpans(r, row)...)
	}
	stick := m.vp.AtBottom()
	m.paintContent()
	if stick {
		m.vp.GotoBottom()
	}
}

func sameDay(a, b time.Time) bool {
	a, b = a.Local(), b.Local()
	return a.Year() == b.Year() && a.YearDay() == b.YearDay()
}

// daySeparator is the centred "──── Hôm nay ────" line that starts each day of messages.
func daySeparator(t, now time.Time, w int) string {
	label := " " + dayLabel(t, now) + " "
	side := max((w-ansi.StringWidth(label))/2-2, 2)
	rule := ink(cBorder, strings.Repeat("─", side))
	return "  " + rule + ink(cMuted, label) + rule
}

// dayLabel names a day: Hôm nay, Hôm qua, the weekday within a week, else the date.
func dayLabel(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, x.Location()) }
	switch d := int(day(now).Sub(day(t)).Hours()/24 + 0.5); {
	case d <= 0:
		return "Hôm nay"
	case d == 1:
		return "Hôm qua"
	case d < 7:
		return weekdays[t.Weekday()] + ", " + t.Format("02/01")
	case t.Year() == now.Year():
		return t.Format("02/01")
	}
	return t.Format("02/01/2006")
}

// quoteRow draws a quote made by quoteText: a bar and the name in that person's colour, the text muted.
func quoteRow(q string) string {
	from, text, ok := strings.Cut(q, "\x00")
	if !ok {
		return ink(cMuted, "│ "+q)
	}
	return ink(nameColour(from), "│ "+from) + "  " + ink(cMuted, text)
}

// quoteHit is the row of a reply quote and the message it quotes.
type quoteHit struct{ row, id int }

type span struct{ from, to, id int }

func topicLabel(t topic) string {
	s := "  " + t.title
	if t.unread > 0 {
		s += "  " + strconv.Itoa(t.unread)
	}
	return s + "  "
}

// topicSpans lays the topic tabs out in `width` cells, starting late enough that the open one is visible.
func topicSpans(c *chat, width int) []span {
	active := 0
	for i, t := range c.topics {
		if t.id == c.topic {
			active = i
		}
	}
	first := 0
	for first < active {
		sum := 0
		for _, t := range c.topics[first : active+1] {
			sum += ansi.StringWidth(topicLabel(t))
		}
		if sum <= width {
			break
		}
		first++
	}
	var out []span
	x := 0
	for _, t := range c.topics[first:] {
		if x >= width {
			break
		}
		wd := min(ansi.StringWidth(topicLabel(t)), width-x)
		out = append(out, span{x, x + wd, t.id})
		x += wd
	}
	return out
}

// topicRows draws the tab labels and the underline below them.
func topicRows(c *chat, width int) (string, string) {
	var labels, under strings.Builder
	used := 0
	for _, sp := range topicSpans(c, width) {
		for _, t := range c.topics {
			if t.id != sp.id {
				continue
			}
			wd := sp.to - sp.from
			switch {
			case t.id == c.topic:
				labels.WriteString(inkB(cFg, fit(topicLabel(t), wd)))
				under.WriteString(ink(cAccent, strings.Repeat("━", wd)))
			case t.unread > 0:
				labels.WriteString(ink(cAccent, fit(topicLabel(t), wd)))
				under.WriteString(ink(cDivider, strings.Repeat("─", wd)))
			default:
				labels.WriteString(ink(cMuted, fit(topicLabel(t), wd)))
				under.WriteString(ink(cDivider, strings.Repeat("─", wd)))
			}
			used = sp.to
		}
	}
	under.WriteString(ink(cDivider, strings.Repeat("─", max(width-used, 0))))
	return labels.String(), under.String()
}

// chatItem draws one chat as itemRows lines of width w.
func chatItem(c chat, sel bool, w int, now time.Time) []string {
	bg, bar := cPanel, " "
	if sel {
		bg, bar = cSel, ink(cAccent, "▌")
	}
	cw := max(w-4, 4) // bar + space on the left, 2 on the right
	when := shortTime(c.date, now)
	tick := "" // ✓ sent, ✓✓ read: like Telegram, by the time of a chat whose newest message is ours
	if c.lastOut {
		tick = ink(cMuted, "✓") + " "
		if c.lastID != 0 && c.lastID <= c.readOut {
			tick = ink(cAccent, "✓✓") + " "
		}
	}
	badge := ""
	if c.unread > 0 {
		n := strconv.Itoa(c.unread)
		if c.unread > 99 {
			n = "99+"
		}
		badge = " " + chip(cBg, cAccent, " "+n+" ", true)
	}
	// the time on the first row, the unread count under it, on the right like Telegram
	name := fit(c.title, max(cw-ansi.StringWidth(when)-ansi.StringWidth(tick)-1, 1))
	switch {
	case c.unread > 0 && !c.muted:
		name = inkB(cFg, name)
	case c.muted:
		name = ink(cMuted, name)
	}
	r1 := bar + " " + name + " " + tick + ink(cMuted, when)
	r2 := bar + " " + ink(cMuted, fit(c.last, max(cw-ansi.StringWidth(badge), 1))) + badge
	if c.typing != "" {
		r2 = bar + " " + ink(cAccent, fit(c.typing, max(cw-ansi.StringWidth(badge), 1))) + badge
	}
	return []string{paint(r1, w, cFg, bg), paint(r2, w, cFg, bg), paint("", w, cFg, cPanel)}
}

// toastBox is the small box at the right of the title bar: a notice, an error or a note.
func (m model) toastBox(maxW int) string {
	text, col := m.toast, cAccent
	switch {
	case m.err != "":
		text, col = m.err, cErr
	case text == "" && m.note != "":
		text, col = m.note, cMuted
	}
	if text == "" || maxW < 6 {
		return ""
	}
	return chip(col, cBoost, "▌", false) + chip(cFg, cBoost, " "+cut(text, maxW-3)+" ", false)
}

// listHead is the top of the chat list: the app's name, how many chats are unread, and the search field.
// The field is a filled band without a border; half blocks above and below make it look a row and a half
// tall, with the text in the middle. Its colour turns warmer while the cursor is in it.
func (m model) listHead(w int) []string {
	unread := 0
	for _, c := range m.chats {
		if c.unread > 0 {
			unread++
		}
	}
	sub := "Telegram trong terminal"
	if unread > 0 {
		sub = strconv.Itoa(unread) + " chat chưa đọc"
	}
	field := cBoost
	if m.searching {
		field = cWarm
	}
	iw := max(w-4, 4) // 2 columns of margin on each side
	edge := func(ch string) string {
		return paint("  "+ink(field, strings.Repeat(ch, iw)), w, cFg, cPanel)
	}
	hint := keyCap("ctrl+k")
	// two cells of padding on each side: one space plus the input's prompt on the left, two after the key
	in := paint(" "+m.search.View(), iw-ansi.StringWidth(hint)-2, cFg, field) + paint(hint+"  ", ansi.StringWidth(hint)+2, cFg, field)
	return []string{
		paint("", w, cFg, cPanel),
		paint("  "+inkB(cAccent, "Bonagram"), w, cFg, cPanel),
		paint("  "+ink(cMuted, sub), w, cFg, cPanel),
		edge("▄"),
		paint("  "+in, w, cFg, cPanel),
		edge("▀"),
	}
}

// chatSubtitle is the muted line under the chat name: what kind of chat it is; for a person, their online status.
func chatSubtitle(c *chat, now time.Time) string {
	var p []string
	switch {
	case c.key[0] == 'u': // the name is already the title: the status and the username add something
		if c.bot {
			p = append(p, "bot")
		} else if s := presenceText(c.seen, now); s != "" {
			p = append(p, s)
		}
		if c.username != "" {
			p = append(p, "@"+clean(c.username))
		}
	case c.forum && len(c.topics) > 0:
		p = append(p, "Nhóm chủ đề · "+strconv.Itoa(len(c.topics))+" topic")
	case c.key[0] == 'g':
		p = append(p, "Nhóm")
	default:
		p = append(p, "Nhóm hoặc kênh")
	}
	if c.muted {
		p = append(p, "đã tắt tiếng")
	}
	return strings.Join(p, " · ")
}

// btnW is the width of the [Tệp] button in the input box (0 when the box is too narrow for it).
func (m *model) btnW() int {
	if max(m.vp.Width-6, 4) < 30 {
		return 0
	}
	return btnW
}

func (m model) buttonLabels() (clip, send string) {
	if m.cfg.icons {
		return "\uf0c6", "\uf1d8"
	}
	return "Tệp", "Gửi"
}

// buttonWidths is the cell width of the attach label (with the gap after it) and of the send button.
func (m model) buttonWidths() (clip, send int) {
	c, s := m.buttonLabels()
	if m.cfg.icons {
		return ansi.StringWidth(c) + 1, ansi.StringWidth(s) + 3
	}
	return ansi.StringWidth(c), ansi.StringWidth(s) + 2
}

// attachButton is the paperclip and the send button at the end of the message box: icons with a Nerd Font
// (BONAGRAM_ICONS), words without. Send is grey while there is nothing to send, amber once there is.
func (m model) attachButton() string {
	if m.btnW() == 0 {
		return ""
	}
	clip, send := m.buttonLabels()
	sendFg, sendBg, sendBold := cMuted, cKey, false
	if (strings.TrimSpace(m.in.Value()) != "" && m.ms == nil) || m.attach != nil { // a search query is not a message
		sendFg, sendBg, sendBold = cBg, cAccent, true
	}
	// Ghostty draws a Nerd Font icon two cells wide when a space follows it: one more space after each icon
	// keeps the paperclip off the send button and the send icon in the middle of its button
	gap, tail := " ", " "
	if m.cfg.icons {
		gap, tail = "  ", "  "
	}
	group := ink(cMuted, clip) + gap + chip(sendFg, sendBg, " "+send+tail, sendBold) + " "
	return paint(strings.Repeat(" ", m.buttonLead())+group, btnW, cFg, cSurface) // flush against the right border
}

// buttonLead is the blank cells before the attach button, which keep the two buttons against the border.
func (m model) buttonLead() int {
	clip, send := m.buttonWidths()
	return max(btnW-(clip+1+send+1), 0)
}

// composeBox is the message box (design "B"): a rounded border, amber once there is text; the attach and send
// buttons sit on its last row.
func (m model) composeBox(w int) []string {
	iw := max(w-6, 4) // inside the border, 2 columns of margin on each side
	col := cBorder
	if m.in.Value() != "" || m.attach != nil {
		col = cAccent
	}
	b := fg(col)
	row := func(s string) string { return paint("  "+s, w, cFg, cSurface) }
	out := []string{row(b.Render("╭" + strings.Repeat("─", iw) + "╮"))}
	text := m.in.View()
	for i, t := range text {
		btn := paint("", m.btnW(), cFg, cSurface)
		if i == len(text)-1 { // the buttons sit on the last row, next to where typing ends
			btn = m.attachButton()
		}
		out = append(out, row(b.Render("│")+paint(" "+t, iw-m.btnW(), cFg, cSurface)+btn+b.Render("│")))
	}
	return append(out, row(b.Render("╰"+strings.Repeat("─", iw)+"╯")))
}

// hints is the bottom row: the keys of what is open, as key caps, in the platform's spelling.
func (m model) hints(w int) string {
	type hint struct{ key, what string }
	hs := []hint{{"enter", "gửi"}, {"alt+enter", "xuống dòng"}, {"", "chuột phải: thao tác"}, {"F1", "trợ giúp"}}
	switch {
	case m.ms != nil:
		hs = []hint{{"enter", "tìm / mở tin"}, {"↑↓", "chọn kết quả"}, {"esc", "đóng"}}
	case m.searching:
		hs = []hint{{"↑↓", "chọn chat"}, {"enter", "mở"}, {"esc", "đóng"}}
	case m.browse:
		hs = []hint{{"↑↓", "chọn tin"}, {"enter", "trả lời"}, {"E", "sửa"}, {"D", "xoá"}, {"R", "reaction"}, {"F", "chuyển tiếp"}, {"esc", "thoát"}}
	case m.reply != nil || m.edit != nil:
		hs = []hint{{"enter", "gửi"}, {"alt+enter", "xuống dòng"}, {"esc", "huỷ"}}
	}
	if p := m.running(); p != nil {
		what := "huỷ gửi tệp"
		if p.down {
			what = "huỷ tải"
		}
		hs = append([]hint{{"ctrl+x", what}}, hs...)
	}
	var parts []string
	for _, h := range hs {
		switch {
		case h.key == "":
			parts = append(parts, ink(cMuted, h.what))
		case h.key == "↑↓" || h.key == "F1" || len(h.key) == 1:
			parts = append(parts, chip(cKeyFg, cKey, " "+h.key+" ", false)+" "+ink(cMuted, h.what))
		default:
			parts = append(parts, keyCap(h.key)+" "+ink(cMuted, h.what))
		}
	}
	return ansi.Truncate(strings.Join(parts, "   "), w, "…")
}

func (m model) View() string {
	if m.w == 0 {
		return ""
	}
	setCursor(-1, 0)
	if m.help {
		return m.helpView()
	}
	if m.viewer != nil {
		return m.viewerView()
	}
	now := time.Now()
	side, rw, bodyH := m.side, m.vp.Width, m.h-1

	// left: search + chats
	var left []string
	if m.rail {
		left = m.railView(bodyH)
	} else {
		tabs := ""
		if len(m.folders) > 0 {
			tabs, _ = m.folderTabs(side)
		}
		left = append(m.listHead(side), paint(tabs, side, cFg, cPanel))
		vis := m.visible()
		for p := m.top; p < len(vis) && len(left) < bodyH; p++ {
			c := m.chatAt(vis[p])
			sel := c.key == m.cur
			if m.filtering() {
				sel = p == m.sel
			}
			if c.key == m.selfKey { // Saved Messages: nobody else reads them
				c.lastOut = false
			}
			c.typing = m.typingText(c.key, -1, now)
			left = append(left, chatItem(c, sel, side, now)...)
		}
		for len(left) < bodyH {
			left = append(left, paint("", side, cFg, cPanel))
		}
	}

	// right: title, topics, messages, reply bar, input
	c := m.curChat()
	head := ink(cMuted, "Chọn một chat ở bên trái")
	end := m.toastBox(rw / 2)
	if c != nil {
		head = inkB(cFg, cut(c.title, max(rw/2, 10)))
		tp := -1
		if hasTopics(c) {
			tp = c.topic
		}
		if ty := m.typingText(c.key, tp, now); ty != "" {
			head += ink(cAccent, "  ·  "+ty)
		} else if sub := chatSubtitle(c, now); sub != "" {
			head += ink(cMuted, "  ·  "+sub)
		}
		if end == "" {
			end = ink(cMuted, "Tìm ") + keyCap("ctrl+f")
		}
	}
	headW := max(rw-4-ansi.StringWidth(end)-1, 1)
	right := []string{
		paint("", rw, cFg, cSurface),
		paint("  "+fit(head, headW)+" "+end, rw, cFg, cSurface),
		paint("", rw, cFg, cSurface),
	}
	if c != nil && hasTopics(c) {
		l1, l2 := topicRows(c, rw-4)
		right = append(right, paint("  "+l1, rw, cFg, cSurface), paint("  "+l2, rw, cFg, cSurface))
	}
	if ps := m.pins[m.mk()]; len(ps) > 0 {
		at := min(m.pinAt[m.mk()], len(ps)-1)
		label := "Ghim"
		if len(ps) > 1 {
			label = fmt.Sprintf("Ghim %d/%d", at+1, len(ps))
		}
		band := ink(cAccent, "▌"+label) + "  " + ink(cMuted, cut(ps[at].text, rw-8-ansi.StringWidth(label)))
		right = append(right, paint("  "+paint(band, rw-4, cFg, cPanel), rw, cFg, cSurface))
	}
	for i := 0; i < m.vp.Height; i++ { // the rows in view, straight from the content (viewport.View would
		l := "" // run every row through lipgloss again, which was 40% of a frame)
		if j := m.vp.YOffset + i; j >= 0 && j < len(m.vlines) {
			l = m.vlines[j]
		}
		right = append(right, paint(l, rw, cFg, cSurface))
	}
	if bar := m.barText(); bar != "" {
		right = append(right, paint("  "+ink(cMuted, cut(bar, rw-6)), rw, cFg, cSurface))
	}
	right = append(right, m.composeBox(rw)...)
	switch { // the real cursor goes where the next typed character will appear
	case m.searching:
		setCursor(4, 2+1+1+textWidth(m.search.Value(), m.search.Position(), m.search.Width)) // margin, padding, prompt
	case m.in.Focused():
		r, c := m.in.Cursor()
		setCursor(len(right)-1-len(m.in.View())+r, side+1+2+1+1+c)
	}

	div := paint("", 1, cFg, cPanel)
	rows := make([]string, bodyH)
	for i := range rows {
		rows[i] = left[i] + div
		if i < len(right) {
			rows[i] += right[i]
		} else {
			rows[i] += paint("", rw, cFg, cSurface)
		}
	}
	m.railTip(rows)
	if m.menu != nil {
		m.overlayMenu(rows)
	}
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(r)
		b.WriteByte('\n')
	}
	b.WriteString(paint("", side, cFg, cPanel) + div + paint("  "+m.hints(rw-4), rw, cFg, cSurface))
	return b.String()
}

func (m model) viewerView() string {
	pic := m.viewer.pic
	body := lipgloss.Place(m.w, max(m.h-2, 1), lipgloss.Center, lipgloss.Center, strings.Join(pic.rows, "\n"))
	if len(pic.rows) > 0 { // iTerm2/Sixel: painted over the blank rows once this frame is out
		queueDraw(1+(max(m.h-2, 1)-len(pic.rows))/2, (m.w-ansi.StringWidth(pic.rows[0]))/2, pic)
	}
	hint := "Esc / Q đóng · O mở bằng ứng dụng hệ thống"
	if m.err != "" {
		hint = m.err
	}
	var b strings.Builder
	b.WriteString(paint(" "+inkB(cFg, m.viewer.title), m.w, cFg, cPanel) + "\n")
	for _, l := range strings.Split(body, "\n") {
		b.WriteString(paint(l, m.w, cFg, cBg) + "\n")
	}
	b.WriteString(paint(lipgloss.PlaceHorizontal(m.w, lipgloss.Center, ink(cMuted, hint)), m.w, cFg, cBg))
	return b.String()
}

var _ tea.Model = model{}

// textWidth is the cell width of the text before the cursor (rune index pos), capped at the box width.
func textWidth(value string, pos, width int) int {
	r := []rune(value)
	return min(ansi.StringWidth(string(r[:min(max(pos, 0), len(r))])), width)
}

// reactionLine is the reactions as small chips, " 👍 3 " each; yours on the amber tint.
func reactionLine(rs []reaction, w int) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		f, b := cFg, cBoost
		if r.mine {
			f, b = cAccent, cWarm
		}
		parts[i] = chip(f, b, " "+r.emoji+" "+strconv.Itoa(r.count)+" ", false)
	}
	return ansi.Truncate(strings.Join(parts, " "), w, "…")
}

// bodyCache is a message body as last drawn, with what it was drawn for.
type bodyCache struct {
	text, hover string
	bw, cw      int
	out         bool
	rows        []string
	hits        []hitSpec
}

// cachedBody reuses the drawing of a message when nothing it depends on changed: drawing every body again
// (entities, wrapping, cards) is most of the cost of a render, and a render happens on every update.
func (m *model) cachedBody(l line, bw, cw int, next map[*tg.Message]bodyCache) ([]string, []hitSpec) {
	if l.msg == nil {
		return m.body(l, bw, cw)
	}
	c, ok := m.bodies[l.msg]
	if !ok || c.text != l.text || c.bw != bw || c.cw != cw || c.out != l.out || c.hover != m.hover {
		rows, hits := m.body(l, bw, cw)
		c = bodyCache{l.text, m.hover, bw, cw, l.out, rows, hits}
	}
	next[l.msg] = c
	return c.rows, c.hits
}

// quoteOf is the drawn quote of a loaded message, remembered: measuring and colouring the start of the
// replied-to text is costly and would repeat on every render.
func (m *model) quoteOf(from, text string) string {
	k := from + "\x00" + text
	if q, ok := m.quotes[k]; ok {
		return q
	}
	return m.remember(k, quoteRow(quoteText(from, text)))
}

// quoteDrawn is quoteRow of a quote fetched with the page (line.quote), remembered the same way.
func (m *model) quoteDrawn(q string) string {
	k := "\x01" + q
	if d, ok := m.quotes[k]; ok {
		return d
	}
	return m.remember(k, quoteRow(q))
}

func (m *model) remember(k, v string) string {
	if m.quotes == nil || len(m.quotes) > 4000 {
		m.quotes = map[string]string{}
	}
	m.quotes[k] = v
	return v
}

// Fast colouring. A lipgloss style costs dozens of allocations per Render (it builds a style, measures,
// pads, checks borders); the frame paints hundreds of small pieces on every update, so these write the
// escape sequences directly. The sequences are made once per colour and kept.
var seqCache sync.Map // "f#rrggbb", "b#rrggbb" → escape sequence

func seq(kind byte, c string) string {
	k := string(kind) + c
	if s, ok := seqCache.Load(k); ok {
		return s.(string)
	}
	r, g, b := rgb(c)
	code := 38
	if kind == 'b' {
		code = 48
	}
	s := fmt.Sprintf("\x1b[%d;2;%d;%d;%dm", code, r, g, b)
	seqCache.Store(k, s)
	return s
}

// ink colours s with c.
func ink(c, s string) string { return seq('f', c) + s + "\x1b[0m" }

// inkB colours s with c, in bold.
func inkB(c, s string) string { return "\x1b[1m" + seq('f', c) + s + "\x1b[0m" }

// chip is s in colour fgc on background bgc (a key cap, a badge, a reaction), bold when asked.
func chip(fgc, bgc, s string, bold bool) string {
	b := ""
	if bold {
		b = "\x1b[1m"
	}
	return b + seq('f', fgc) + seq('b', bgc) + s + "\x1b[0m"
}
