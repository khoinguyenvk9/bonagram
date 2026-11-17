package main

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

type segKind int

const (
	segText segKind = iota
	segCode         // `inline code`
	segPre          // fenced code block
)

// seg is a stretch of a message body: plain text, inline code or a code block (lang = its language).
type seg struct {
	kind      segKind
	raw, lang string
}

const (
	cCode    = "#262320" // code block body
	copyBtn  = "[copy]"
	maxBlock = 100 // widest code block, in cells
)

// segments splits a message at its code entities, or returns nil when it has none. Entity offsets count
// UTF-16 units (an emoji is 2), not characters; an entity that overlaps the previous one or reaches past
// the end of the text is ignored rather than trusted.
func segments(msg string, es []tg.MessageEntityClass) []seg {
	type span struct {
		off, end int
		kind     segKind
		lang     string
	}
	var sp []span
	for _, e := range es {
		switch v := e.(type) {
		case *tg.MessageEntityPre:
			sp = append(sp, span{v.Offset, v.Offset + v.Length, segPre, v.Language})
		case *tg.MessageEntityCode:
			sp = append(sp, span{v.Offset, v.Offset + v.Length, segCode, ""})
		}
	}
	if len(sp) == 0 {
		return nil
	}
	sort.SliceStable(sp, func(i, j int) bool { return sp[i].off < sp[j].off })
	u := utf16.Encode([]rune(msg))
	var out []seg
	pos := 0
	for _, s := range sp {
		if s.off < pos || s.end > len(u) || s.end <= s.off {
			continue
		}
		if s.off > pos {
			out = append(out, seg{segText, string(utf16.Decode(u[pos:s.off])), ""})
		}
		out = append(out, seg{s.kind, string(utf16.Decode(u[s.off:s.end])), s.lang})
		pos = s.end
	}
	if pos < len(u) {
		out = append(out, seg{segText, string(utf16.Decode(u[pos:])), ""})
	}
	return out
}

const cLink = "#6fa8dc" // links, mentions and the like

func fgSeq(c string) string {
	r, g, b := rgb(c)
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)
}

const osc8 = "\x1b]8;"

// linkSeq starts a link: an OSC 8 hyperlink (terminals that support it underline it when Cmd/Ctrl is held
// over it and open it on Cmd/Ctrl+click). The id ties the pieces of one link together when it wraps.
func linkSeq(id, url string) string { return osc8 + "id=" + id + ";" + url + "\x1b\\" }

const linkEnd = osc8 + ";\x1b\\"

// inline is the text of a message between UTF-16 offsets from and to, with its entities drawn: bold, italic,
// underline, strike, spoiler (dimmed), inline code, links (blue, an OSC 8 hyperlink, underlined while the mouse
// is over the address hot), and mentions and hashtags in the link colour. The address of a hidden link is written
// after its words ("docs (https://...)"), because a terminal cannot show a link behind a word.
func inline(m *tg.Message, from, to int, hot string) string {
	u := utf16.Encode([]rune(m.Message))
	from, to = max(from, 0), min(to, len(u))
	if from >= to {
		return ""
	}
	text := func(a, b int) string { return clean(string(utf16.Decode(u[a:b]))) }
	type ent struct {
		off, end int
		sgr      string
		link     *linkEnt
	}
	var es []ent
	for _, e := range m.Entities {
		off, end := e.GetOffset(), e.GetOffset()+e.GetLength()
		if off < from {
			off = from
		}
		end = min(end, to)
		if off >= end {
			continue
		}
		var sg string
		switch e.(type) {
		case *tg.MessageEntityBold:
			sg = "\x1b[1m"
		case *tg.MessageEntityItalic, *tg.MessageEntityBlockquote:
			sg = "\x1b[3m"
		case *tg.MessageEntityUnderline:
			sg = "\x1b[4m"
		case *tg.MessageEntityStrike:
			sg = "\x1b[9m"
		case *tg.MessageEntitySpoiler:
			sg = "\x1b[2m"
		case *tg.MessageEntityCode:
			sg = sgr(cFg, cDivider)
		case *tg.MessageEntityMention, *tg.MessageEntityMentionName, *tg.MessageEntityHashtag,
			*tg.MessageEntityBotCommand, *tg.MessageEntityCashtag, *tg.MessageEntityEmail, *tg.MessageEntityPhone:
			sg = fgSeq(cLink)
		}
		if sg != "" {
			es = append(es, ent{off: off, end: end, sgr: sg})
		}
	}
	links := linkEntities(m)
	for i := range links {
		l := &links[i]
		if l.end == 0 || l.off >= to || l.end <= from {
			continue
		}
		sg := fgSeq(cLink)
		if l.url == hot {
			sg += "\x1b[4m"
		}
		es = append(es, ent{off: max(l.off, from), end: min(l.end, to), sgr: sg, link: l})
	}
	if len(es) == 0 {
		return text(from, to)
	}
	cuts := []int{from, to}
	for _, e := range es {
		cuts = append(cuts, e.off, e.end)
	}
	sort.Ints(cuts)
	var b strings.Builder
	linkN := 0
	var cur *linkEnt
	for k := 0; k+1 < len(cuts); k++ {
		a, z := cuts[k], cuts[k+1]
		if a == z {
			continue
		}
		var styles string
		var link *linkEnt
		for _, e := range es {
			if e.off <= a && e.end >= z {
				styles += e.sgr
				if e.link != nil && link == nil {
					link = e.link
				}
			}
		}
		if link != cur {
			if cur != nil {
				b.WriteString(linkEnd)
				if cur.hidden && cur.end <= a {
					b.WriteString(fg(cMuted).Render(" (" + clean(cur.url) + ")"))
				}
			}
			if link != nil {
				b.WriteString(linkSeq("b"+strconv.Itoa(m.ID)+"_"+strconv.Itoa(linkN), clean(link.url)))
				linkN++
			}
			cur = link
		}
		if styles != "" {
			b.WriteString(styles + text(a, z) + "\x1b[0m")
		} else {
			b.WriteString(text(a, z))
		}
	}
	if cur != nil {
		b.WriteString(linkEnd)
		if cur.hidden && cur.end <= to {
			b.WriteString(fg(cMuted).Render(" (" + clean(cur.url) + ")"))
		}
	}
	return b.String()
}

// carry makes each wrapped row of a text stand alone: the styles and the link still in force at the end of a row
// are closed there and opened again at the start of the next, so nothing leaks into the rest of the screen.
func carry(rows []string) []string {
	sgrs, link := "", ""
	for i, r := range rows {
		pre := link + sgrs
		for p := 0; p < len(r); {
			e := strings.IndexByte(r[p:], 0x1b)
			if e < 0 {
				break
			}
			p += e
			switch {
			case strings.HasPrefix(r[p:], "\x1b["):
				n := strings.IndexAny(r[p+2:], "mABCDHJKfhlsu")
				if n < 0 {
					p = len(r)
					break
				}
				seq := r[p : p+2+n+1]
				if r[p+2+n] == 'm' {
					if params := seq[2 : len(seq)-1]; params == "" || params == "0" {
						sgrs = ""
					} else {
						sgrs += seq
					}
				}
				p += len(seq)
			case strings.HasPrefix(r[p:], osc8):
				n := strings.Index(r[p:], "\x1b\\")
				if n < 0 {
					p = len(r)
					break
				}
				seq := r[p : p+n+2]
				if _, url, _ := strings.Cut(seq[len(osc8):len(seq)-2], ";"); url == "" {
					link = ""
				} else {
					link = seq
				}
				p += len(seq)
			default:
				p++
			}
		}
		if sgrs != "" {
			r += "\x1b[0m"
		}
		if link != "" {
			r += linkEnd
		}
		rows[i] = pre + r
	}
	return rows
}

// linkSpan is a link on screen: its row in the message view, the columns [x0, x1) of that row, and its address.
type linkSpan struct {
	row, x0, x1 int
	url         string
}

// linkSpans finds the links in one drawn row of the message view.
func linkSpans(r int, row string) []linkSpan {
	var out []linkSpan
	col, p, start, url := 0, 0, 0, ""
	for {
		j := strings.Index(row[p:], osc8)
		if j < 0 {
			return out
		}
		j += p
		col += ansi.StringWidth(row[p:j])
		e := strings.Index(row[j:], "\x1b\\")
		if e < 0 {
			return out
		}
		_, u, _ := strings.Cut(row[j+len(osc8):j+e], ";")
		p = j + e + 2
		if u == "" {
			if url != "" {
				out = append(out, linkSpan{r, start, col, url})
				url = ""
			}
		} else {
			url, start = u, col
		}
	}
}

// hitSpec marks a copy button inside the lines returned by body: line idx, block width w, text to copy.
type hitSpec struct {
	idx, w int
	text   string
}

type copyHit struct {
	row, x0, x1 int // content line and columns of the button
	text        string
}

var inlineCode = lipgloss.NewStyle().Foreground(lipgloss.Color(cFg)).Background(lipgloss.Color(cDivider))

// body draws the text of a message: wrapped plain text, or, when it has code, paragraphs and code blocks.
func (m *model) body(l line, bw, cw int) ([]string, []hitSpec) {
	style := func(s string) string {
		if l.out { // keep the colour after a link or a muted piece ended with a reset
			r, g, b := rgb(cAccent)
			return fg(cAccent).Render(strings.ReplaceAll(s, "\x1b[0m", fmt.Sprintf("\x1b[0m\x1b[38;2;%d;%d;%dm", r, g, b)))
		}
		return s
	}
	wrap := func(s string) []string {
		var out []string
		for _, b := range strings.Split(ansi.Wrap(s, bw, ""), "\n") {
			out = append(out, strings.TrimLeft(b, " "))
		}
		return carry(out)
	}
	if len(l.segs) == 0 {
		text := l.text
		var card []string
		if l.msg != nil {
			card = mediaCard(l.msg, bw, m.hover)
			tag := mediaTag(l.msg.Media)
			if card != nil {
				tag = ""
			}
			text = strings.TrimSpace(inline(l.msg, 0, len(l.msg.Message)*2, m.hover) + " " + tag)
		}
		var out []string
		if text != "" || card == nil {
			for _, b := range wrap(text) {
				out = append(out, style(b))
			}
		}
		return append(out, card...), nil
	}
	var out []string
	var hits []hitSpec
	offs := make([]int, len(l.segs)+1) // where each piece starts, in UTF-16 units of the message
	for k, s := range l.segs {
		offs[k+1] = offs[k] + len(utf16.Encode([]rune(s.raw)))
	}
	for i := 0; i < len(l.segs); {
		if s := l.segs[i]; s.kind == segPre {
			rows, w := codeBlock(s, min(cw, maxBlock))
			hits = append(hits, hitSpec{len(out), w, strings.TrimRight(s.raw, "\n")})
			out = append(out, rows...)
			i++
			continue
		}
		j := i
		for j < len(l.segs) && l.segs[j].kind != segPre {
			j++
		}
		var para string
		if l.msg != nil { // the entities draw it: bold, links, inline code...
			from, to := offs[i], offs[j]
			if l.segs[i].kind == segText {
				from += len(utf16.Encode([]rune(l.segs[i].raw))) - len(utf16.Encode([]rune(strings.TrimLeft(l.segs[i].raw, "\n"))))
			}
			if l.segs[j-1].kind == segText {
				to -= len(utf16.Encode([]rune(l.segs[j-1].raw))) - len(utf16.Encode([]rune(strings.TrimRight(l.segs[j-1].raw, "\n"))))
			}
			para = inline(l.msg, from, to, m.hover)
		} else {
			var b strings.Builder
			for k, s := range l.segs[i:j] {
				raw := s.raw
				if s.kind == segText {
					if k == 0 {
						raw = strings.TrimLeft(raw, "\n")
					}
					if k == j-i-1 {
						raw = strings.TrimRight(raw, "\n")
					}
					b.WriteString(clean(raw))
				} else {
					b.WriteString(inlineCode.Render(clean(raw)))
				}
			}
			para = b.String()
		}
		if ansi.Strip(para) != "" {
			for _, b := range wrap(para) {
				out = append(out, style(b))
			}
		}
		i = j
	}
	if l.msg != nil {
		if card := mediaCard(l.msg, bw, m.hover); card != nil {
			out = append(out, card...)
		} else if tag := mediaTag(l.msg.Media); tag != "" {
			out = append(out, style(tag))
		}
	}
	return out, hits
}

// codeBlock draws a block: a header with the language and the copy button, then the code on a darker
// background. Tabs become 4 spaces and long lines are hard-wrapped, so nothing is hidden or misaligned.
func codeBlock(s seg, maxW int) ([]string, int) {
	src := strings.Split(strings.TrimRight(clean(strings.ReplaceAll(s.raw, "\t", "    ")), "\n"), "\n")
	lang := strings.TrimSpace(clean(s.lang))
	btnW := ansi.StringWidth(copyBtn)
	longest := 0
	for _, x := range src {
		longest = max(longest, ansi.StringWidth(x))
	}
	w := min(maxW, max(longest+2, ansi.StringWidth(lang)+btnW+4, 20))
	hdr := " " + fg(cMuted).Render(fit(lang, w-btnW-3)) + " " + fg(cAccent).Bold(true).Render(copyBtn) + " "
	rows := []string{paint(hdr, w, cFg, cDivider)}
	for _, x := range src {
		for _, ln := range strings.Split(ansi.Hardwrap(x, w-2, true), "\n") {
			rows = append(rows, paint(" "+ln, w, cFg, cCode))
		}
	}
	return rows, w
}

// copyButtonCols are the screen columns (inside the message view) of a button in a block of width w
// that starts at column start; the hit area is one cell wider than the label on each side.
func copyButtonCols(start, w int) (int, int) {
	end := start + w
	return end - ansi.StringWidth(copyBtn) - 2, end
}

// --- copying ---------------------------------------------------------------------

// clipboard is a variable so tests do not touch the real clipboard.
var clipboard = clipboardWrite

func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\a"
}

// clipboardWrite uses OSC 52 (Ghostty, Kitty, WezTerm, iTerm2, also over SSH) and, where one exists, the
// system tool too, because some terminals (Terminal.app) ignore OSC 52 and tmux needs set-clipboard.
func clipboardWrite(text string) {
	if len(text) <= 70000 { // terminals cap the size of an OSC 52 payload
		termWrite(osc52(text))
	}
	for _, c := range [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}} {
		if p, err := exec.LookPath(c[0]); err == nil {
			cmd := exec.Command(p, c[1:]...)
			cmd.Stdin = strings.NewReader(text)
			_ = cmd.Run()
			return
		}
	}
}

func copyCmd(text, what string) tea.Cmd {
	return func() tea.Msg {
		clipboard(text)
		return toastMsg{fmt.Sprintf("Đã chép %s (%d dòng)", what, strings.Count(strings.TrimRight(text, "\n"), "\n")+1)}
	}
}

// copyFrom copies the code block of a message (pressing again moves on to its next block); a message
// without code is copied whole.
func (m *model) copyFrom(l line) tea.Cmd {
	var pres []string
	for _, s := range l.segs {
		if s.kind == segPre {
			pres = append(pres, strings.TrimRight(s.raw, "\n"))
		}
	}
	if len(pres) == 0 {
		text := l.text
		if l.msg != nil {
			text = l.msg.Message
		}
		return copyCmd(text, "tin nhắn")
	}
	if m.cpFor != l.id || l.id == 0 {
		m.cpFor, m.cpIdx = l.id, 0
	} else {
		m.cpIdx = (m.cpIdx + 1) % len(pres)
	}
	what := "khối code"
	if len(pres) > 1 {
		what = fmt.Sprintf("khối code %d/%d", m.cpIdx+1, len(pres))
	}
	return copyCmd(pres[m.cpIdx], what)
}
