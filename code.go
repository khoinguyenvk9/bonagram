package main

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"sort"
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
		if l.out {
			return fg(cAccent).Render(s)
		}
		return s
	}
	wrap := func(s string) []string {
		var out []string
		for _, b := range strings.Split(ansi.Wrap(s, bw, ""), "\n") {
			out = append(out, strings.TrimLeft(b, " "))
		}
		return out
	}
	if len(l.segs) == 0 {
		var out []string
		for _, b := range wrap(l.text) {
			out = append(out, style(b))
		}
		return out, nil
	}
	var out []string
	var hits []hitSpec
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
		var para strings.Builder
		for k, s := range l.segs[i:j] {
			raw := s.raw
			if s.kind == segText {
				if k == 0 {
					raw = strings.TrimLeft(raw, "\n")
				}
				if k == j-i-1 {
					raw = strings.TrimRight(raw, "\n")
				}
				para.WriteString(style(clean(raw)))
			} else {
				para.WriteString(inlineCode.Render(clean(raw)))
			}
		}
		if ansi.Strip(para.String()) != "" {
			out = append(out, wrap(para.String())...)
		}
		i = j
	}
	if l.msg != nil {
		if tag := mediaTag(l.msg.Media); tag != "" {
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
