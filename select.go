package main

// Selecting text with the mouse: press, drag, release copies what was swept. The app owns the mouse (so
// clicks work), which stops the terminal from selecting; this does it inside the message view instead.

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const gutter = 2 // the bar and the space before every message row

// selection is a drag in the message view, in content coordinates (row of the whole view, column of the row).
type selection struct {
	on, has        bool // dragging now / a range is marked (it stays until the next key or click)
	r0, c0, r1, c1 int
	url            string // the link the press landed on: a click without a drag opens it
}

// bounds returns the marked range with the start first.
func (s selection) bounds() (r0, c0, r1, c1 int) {
	r0, c0, r1, c1 = s.r0, s.c0, s.r1, s.c1
	if r1 < r0 || (r1 == r0 && c1 < c0) {
		r0, c0, r1, c1 = r1, c1, r0, c0
	}
	return
}

// span is the part of row r (0-based, among rows) that the selection covers: columns [from, to).
func (m *model) selSpan(r int) (from, to int, ok bool) {
	r0, c0, r1, c1 := m.txt.bounds()
	if r < r0 || r > r1 || r >= len(m.rows) {
		return 0, 0, false
	}
	from, to = gutter, ansi.StringWidth(strings.TrimRight(ansi.Strip(m.rows[r]), " "))
	if r == r0 {
		from = max(c0, gutter)
	}
	if r == r1 {
		to = min(to, c1+1)
	}
	return from, to, from < to
}

// paintContent puts the rows in the view, the selected cells in reverse video.
func (m *model) paintContent() {
	rows := m.rows
	if m.txt.has {
		rows = make([]string, len(m.rows))
		for r, row := range m.rows {
			rows[r] = row
			if from, to, ok := m.selSpan(r); ok {
				rows[r] = ansi.Cut(row, 0, from) + "\x1b[7m" + ansi.Strip(ansi.Cut(row, from, to)) + "\x1b[27m" + ansi.Cut(row, to, ansi.StringWidth(row))
			}
		}
	}
	m.vp.SetContent(strings.Join(rows, "\n"))
}

// selText is the plain text under the selection, one line per row. Own messages are pushed to the right
// with spaces, so those rows lose their leading ones.
func (m *model) selText() string {
	var out []string
	for r, row := range m.rows {
		if from, to, ok := m.selSpan(r); ok {
			s := strings.TrimRight(ansi.Strip(ansi.Cut(row, from, to)), " ")
			if m.rowOut[r] {
				s = strings.TrimLeft(s, " ")
			}
			out = append(out, s)
		}
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// cell turns a mouse position into a row and column of the message view; ok is false above or below it.
func (m *model) cell(x, y int) (r, c int, ok bool) {
	top := m.msgY()
	if y < top || y >= top+m.vp.Height {
		return 0, 0, false
	}
	return m.vp.YOffset + y - top, max(x-m.side-1, 0), true
}

func (m *model) beginSel(e tea.MouseMsg) {
	had := m.txt.has
	m.txt = selection{}
	if r, c, ok := m.cell(e.X, e.Y); ok {
		m.txt = selection{on: true, r0: r, c0: c, r1: r, c1: c}
		if l := m.linkAt(r, c); l != nil {
			m.txt.url = l.url
		}
	}
	if had {
		m.paintContent()
	}
}

// dragSel follows the mouse; past the top or bottom edge the view scrolls one line per movement.
func (m *model) dragSel(e tea.MouseMsg) {
	top := m.msgY()
	switch {
	case e.Y < top:
		m.vp.LineUp(1)
	case e.Y >= top+m.vp.Height:
		m.vp.LineDown(1)
	}
	y := min(max(e.Y, top), top+m.vp.Height-1)
	r, c, _ := m.cell(e.X, y)
	m.txt.r1, m.txt.c1 = min(r, len(m.rows)-1), c
	m.txt.has = m.txt.r1 != m.txt.r0 || m.txt.c1 != m.txt.c0
	m.paintContent()
}

// endSel finishes the drag: a drag copies the selected text, a click that lands on the link it started on opens it.
func (m *model) endSel(e tea.MouseMsg) tea.Cmd {
	m.txt.on = false
	if !m.txt.has {
		if r, c, ok := m.cell(e.X, e.Y); ok && m.txt.url != "" {
			if l := m.linkAt(r, c); l != nil && l.url == m.txt.url {
				return openLinkCmd(l.url)
			}
		}
		return nil
	}
	if t := m.selText(); t != "" {
		return copyCmd(t, "đoạn đã chọn")
	}
	m.txt.has = false
	m.paintContent()
	return nil
}

// openURL is a variable so tests do not open a browser.
var openURL = openExternal

func openLinkCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if err := openURL(url); err != nil {
			return toastMsg{"không mở được: " + err.Error()}
		}
		return toastMsg{"Đã mở " + url}
	}
}

// linkAt is the link drawn at a row and column of the message view, nil when none.
func (m *model) linkAt(r, c int) *linkSpan {
	for i := range m.links {
		if l := &m.links[i]; l.row == r && c >= l.x0 && c < l.x1 {
			return l
		}
	}
	return nil
}

// hoverAt follows the mouse without a button: the link under it is underlined (and nothing else changes).
func (m *model) hoverAt(e tea.MouseMsg) {
	url := ""
	if r, c, ok := m.cell(e.X, e.Y); ok && e.X > m.side {
		if l := m.linkAt(r, c); l != nil {
			url = l.url
		}
	}
	if url != m.hover {
		m.hover = url
		m.render()
	}
}

// clearSel drops the marked range (any key does).
func (m *model) clearSel() {
	if m.txt.has {
		m.txt.has = false
		m.paintContent()
	}
}
