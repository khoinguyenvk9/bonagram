package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Theme colours.
const (
	cBg      = "#171513"
	cSurface = "#1d1a17"
	cPanel   = "#26221e"
	cFg      = "#e4dacb"
	cMuted   = "#8d867a"
	cAccent  = "#e0a458"
	cErr     = "#d16d6d"
	cBoost   = "#2f2b27"
	cSel     = "#3a3025"
	cDivider = "#35312d"
	cBorder  = "#4d4840"
)

var palette = []string{"#c3a6d8", "#9fc5a8", "#e0b48a", "#8fb8d8", "#d8a0a0", "#c9c98f", "#a0c8c8", "#b8b8e0"}

const (
	followGap  = 5 * time.Minute
	itemRows   = 3 // a chat in the list: name, preview, blank
	listY0     = 4 // the 3-row search box, then a blank row
	headRows   = 3 // the header band beside the search box: blank, chat name, what it is
	composeRow = 3 // bordered input
	keysText   = "F1 help  ^K search  ^N ^P chats  ^T ^Y topics  ^G unread  ^B messages  ^R reply  Esc cancel"
)

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

func sgr(foreground, background string) string {
	rgb := func(h string) (int, int, int) {
		v, _ := strconv.ParseUint(strings.TrimPrefix(h, "#"), 16, 32)
		return int(v >> 16 & 255), int(v >> 8 & 255), int(v & 255)
	}
	r, g, b := rgb(foreground)
	r2, g2, b2 := rgb(background)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm", r, g, b, r2, g2, b2)
}

// paint fills a line to width w with a background: the colours are re-applied after every reset inside
// s, so styled pieces never punch holes in it.
func paint(s string, w int, foreground, background string) string {
	if ansi.StringWidth(s) > w {
		s = ansi.Truncate(s, w, "")
	}
	base := sgr(foreground, background)
	s = strings.ReplaceAll(s, "\x1b[0m", "\x1b[0m"+base)
	return base + s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0)) + "\x1b[0m"
}

func nameStyle(name string) lipgloss.Style {
	sum := 0
	for _, r := range name {
		sum += int(r)
	}
	return fg(palette[sum%len(palette)]).Bold(true)
}

// followUp: same author within a few minutes of the previous message, shown without a name/time line.
func followUp(l line, prev *line) bool {
	if prev == nil || l.event || prev.event || l.out != prev.out || l.sender != prev.sender {
		return false
	}
	d := l.at.Sub(prev.at)
	return d >= 0 && d < followGap
}

func cut(s string, n int) string {
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
		m.lineAt, m.hits, m.nLines = m.lineAt[:0], m.hits[:0], 0
		m.vp.SetContent(strings.Repeat("\n", max(m.vp.Height/2-1, 0)) + lipgloss.PlaceHorizontal(m.vp.Width, lipgloss.Center,
			fg(cMuted).Render("Chọn một chat ở bên trái · Ctrl+K để tìm · Ctrl+N / Ctrl+P để đổi chat · F1 hướng dẫn")))
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
	m.lineAt = m.lineAt[:0]
	m.hits = m.hits[:0]
	for i, l := range ls {
		var prev *line
		if i > 0 {
			prev = &ls[i-1]
		}
		var quote string
		if t := byID[l.replyTo]; !l.out && t != nil && t.out {
			quote = `↪ bạn: "` + cut(t.text, 60) + `"`
		}
		mine := !l.out && (l.mentioned || quote != "")
		head := mine || !followUp(l, prev)
		if head && i > 0 {
			out = append(out, "")
		}
		m.lineAt = append(m.lineAt, len(out))

		var block []string
		var pend []hitSpec
		when := fg(cMuted).Render(l.at.Format("15:04"))
		switch {
		case head && l.out:
			block = append(block, when)
		case head:
			h := nameStyle(l.from).Render(l.from) + "  " + when
			if l.mentioned {
				h += "  " + fg(cAccent).Bold(true).Render("@bạn")
			}
			block = append(block, h)
		}
		if quote != "" {
			block = append(block, fg(cMuted).Render(quote))
		}
		body, hs := m.body(l, bw, cw)
		for _, h := range hs {
			pend = append(pend, hitSpec{len(block) + h.idx, h.w, h.text})
		}
		block = append(block, body...)
		if t := m.thumbs[l.id]; l.id != 0 && t != nil {
			block = append(block, t.rows...)
		}
		if len(l.reacts) > 0 {
			block = append(block, reactionLine(l.reacts, bw))
		}

		sel := m.browse && i == m.bsel
		bar := " "
		switch {
		case sel, mine:
			bar = fg(cAccent).Render("▌")
		}
		for bi, b := range block {
			for _, h := range pend {
				if h.idx == bi {
					start := 2
					if l.out {
						start += cw - h.w
					}
					x0, x1 := copyButtonCols(start, h.w)
					m.hits = append(m.hits, copyHit{len(out), x0, x1, h.text})
				}
			}
			if l.out {
				b = lipgloss.PlaceHorizontal(cw, lipgloss.Right, b)
			}
			row := bar + " " + b
			if sel {
				row = paint(row, m.vp.Width, cFg, cSel)
			}
			out = append(out, row)
		}
	}
	m.nLines = len(out)
	stick := m.vp.AtBottom()
	m.vp.SetContent(strings.Join(out, "\n"))
	if stick {
		m.vp.GotoBottom()
	}
}

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
				labels.WriteString(fg(cFg).Bold(true).Render(fit(topicLabel(t), wd)))
				under.WriteString(fg(cAccent).Render(strings.Repeat("━", wd)))
			case t.unread > 0:
				labels.WriteString(fg(cAccent).Render(fit(topicLabel(t), wd)))
				under.WriteString(fg(cDivider).Render(strings.Repeat("─", wd)))
			default:
				labels.WriteString(fg(cMuted).Render(fit(topicLabel(t), wd)))
				under.WriteString(fg(cDivider).Render(strings.Repeat("─", wd)))
			}
			used = sp.to
		}
	}
	under.WriteString(fg(cDivider).Render(strings.Repeat("─", max(width-used, 0))))
	return labels.String(), under.String()
}

// chatItem draws one chat as itemRows lines of width w.
func chatItem(c chat, sel bool, w int, now time.Time) []string {
	bg, bar := cPanel, " "
	if sel {
		bg, bar = cSel, fg(cAccent).Render("▌")
	}
	cw := max(w-4, 4) // bar + space on the left, 2 on the right
	when := shortTime(c.date, now)
	badge := ""
	if c.unread > 0 {
		n := strconv.Itoa(c.unread)
		if c.unread > 99 {
			n = "99+"
		}
		badge = "  " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(cBg)).Background(lipgloss.Color(cAccent)).Render(" "+n+" ")
	}
	nameW := max(cw-ansi.StringWidth(when)-1-ansi.StringWidth(badge), 1)
	name := fit(c.title, nameW)
	switch {
	case c.unread > 0 && !c.muted:
		name = fg(cFg).Bold(true).Render(name)
	case c.muted:
		name = fg(cMuted).Render(name)
	}
	r1 := bar + " " + name + badge + " " + fg(cMuted).Render(when)
	r2 := bar + " " + fg(cMuted).Render(fit(c.last, cw))
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
	return fg(col).Background(lipgloss.Color(cBoost)).Render("▌") +
		lipgloss.NewStyle().Foreground(lipgloss.Color(cFg)).Background(lipgloss.Color(cBoost)).Render(" "+cut(text, maxW-3)+" ")
}

// searchBox is the bordered box at the top left. It is drawn like the message box (muted border, accent
// when the cursor is in it) so the two read as the same kind of control.
func (m model) searchBox(w int) []string {
	iw := max(w-6, 4) // 2 columns of margin and 1 of border on each side
	col := cBorder
	if m.searching {
		col = cAccent
	}
	b := fg(col)
	row := func(s string) string { return paint("  "+s+"  ", w, cFg, cPanel) }
	return []string{
		row(b.Render("╭" + strings.Repeat("─", iw) + "╮")),
		row(b.Render("│") + paint(m.search.View(), iw, cFg, cPanel) + b.Render("│")),
		row(b.Render("╰" + strings.Repeat("─", iw) + "╯")),
	}
}

// chatSubtitle is the muted line under the chat name: what kind of chat it is.
func chatSubtitle(c *chat) string {
	var p []string
	switch {
	case c.key[0] == 'u': // the name is already the title: only the username adds anything
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

func (m model) attachButton() string {
	if m.btnW() == 0 {
		return ""
	}
	return paint(" "+fg(cAccent).Render("[Tệp]")+" ", btnW, cFg, cSurface)
}

func (m model) composeBox(w int) []string {
	iw := max(w-6, 4) // inside the border, 2 columns of margin on each side
	col := cBorder
	if m.in.Focused() {
		col = cAccent
	}
	b := fg(col)
	row := func(s string) string { return paint("  "+s, w, cFg, cSurface) }
	return []string{
		row(b.Render("╭" + strings.Repeat("─", iw) + "╮")),
		row(b.Render("│") + paint(" "+m.in.View(), iw-m.btnW(), cFg, cSurface) + m.attachButton() + b.Render("│")),
		row(b.Render("╰" + strings.Repeat("─", iw) + "╯")),
	}
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
	left := append(m.searchBox(side), paint("", side, cFg, cPanel))
	vis := m.visible()
	for p := m.top; p < len(vis) && len(left) < bodyH; p++ {
		c := m.chats[vis[p]]
		sel := c.key == m.cur
		if m.filtering() {
			sel = p == m.sel
		}
		left = append(left, chatItem(c, sel, side, now)...)
	}
	for len(left) < bodyH {
		left = append(left, paint("", side, cFg, cPanel))
	}

	// right: title, topics, messages, reply bar, input
	c := m.curChat()
	title, sub, titleStyle := "bonagram", "Chọn một chat ở bên trái", fg(cAccent).Bold(true)
	if c != nil {
		title, sub, titleStyle = c.title, chatSubtitle(c), fg(cFg).Bold(true)
	}
	toast := m.toastBox(rw / 2)
	titleW := max(rw-4-ansi.StringWidth(toast)-1, 1)
	right := []string{
		paint("", rw, cFg, cPanel),
		paint("  "+titleStyle.Render(fit(title, titleW))+" "+toast, rw, cFg, cPanel),
		paint("  "+fg(cMuted).Render(cut(sub, rw-4)), rw, cFg, cPanel),
	}
	if c != nil && hasTopics(c) {
		l1, l2 := topicRows(c, rw-4)
		right = append(right, paint("  "+l1, rw, cFg, cSurface), paint("  "+l2, rw, cFg, cSurface))
	}
	if pin := m.pins[m.mk()]; pin != "" {
		right = append(right, paint("  "+fg(cAccent).Render("▍ Ghim  ")+fg(cMuted).Render(cut(pin, rw-14)), rw, cFg, cSurface))
	}
	for _, l := range strings.Split(m.vp.View(), "\n") {
		right = append(right, paint(l, rw, cFg, cSurface))
	}
	if bar := m.barText(); bar != "" {
		right = append(right, paint("  "+fg(cMuted).Render(cut(bar, rw-6)), rw, cFg, cSurface))
	}
	right = append(right, m.composeBox(rw)...)
	switch { // the real cursor goes where the next typed character will appear
	case m.searching:
		setCursor(1, 2+1+1+textWidth(m.search.Value(), m.search.Position(), m.search.Width)) // margin, border, prompt
	case m.in.Focused():
		setCursor(len(right)-2, side+1+2+1+1+textWidth(m.in.Value(), m.in.Position(), m.in.Width))
	}

	div := paint("", 1, cFg, cDivider)
	var b strings.Builder
	for i := 0; i < bodyH; i++ {
		b.WriteString(left[i] + div)
		if i < len(right) {
			b.WriteString(right[i])
		} else {
			b.WriteString(paint("", rw, cFg, cSurface))
		}
		b.WriteByte('\n')
	}
	keys := keysText
	if m.browse {
		keys = browseKeys
	}
	b.WriteString(paint("  "+fg(cMuted).Render(keys), m.w, cFg, cBg))
	return b.String()
}

func (m model) viewerView() string {
	body := lipgloss.Place(m.w, max(m.h-2, 1), lipgloss.Center, lipgloss.Center, strings.Join(m.viewer.pic.rows, "\n"))
	hint := "Esc / Q đóng · O mở bằng ứng dụng hệ thống"
	if m.err != "" {
		hint = m.err
	}
	var b strings.Builder
	b.WriteString(paint(" "+fg(cFg).Bold(true).Render(m.viewer.title), m.w, cFg, cPanel) + "\n")
	for _, l := range strings.Split(body, "\n") {
		b.WriteString(paint(l, m.w, cFg, cBg) + "\n")
	}
	b.WriteString(paint(lipgloss.PlaceHorizontal(m.w, lipgloss.Center, fg(cMuted).Render(hint)), m.w, cFg, cBg))
	return b.String()
}

var _ tea.Model = model{}

// textWidth is the cell width of the text before the cursor (rune index pos), capped at the box width.
func textWidth(value string, pos, width int) int {
	r := []rune(value)
	return min(ansi.StringWidth(string(r[:min(max(pos, 0), len(r))])), width)
}

// reactionLine is "👍 3  ❤ 1", your own reactions in the accent colour.
func reactionLine(rs []reaction, w int) string {
	parts := make([]string, len(rs))
	for i, r := range rs {
		s := r.emoji + " " + strconv.Itoa(r.count)
		if r.mine {
			parts[i] = fg(cAccent).Bold(true).Render(s)
		} else {
			parts[i] = fg(cMuted).Render(s)
		}
	}
	return ansi.Truncate(strings.Join(parts, "  "), w, "…")
}
