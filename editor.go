package main

// editor is the message box: multi-line, word-wrapped, growing up to maxRows rows, with undo. It replaces
// bubbles' one-line textinput and keeps the part of its API the app uses (Value, SetValue, Position,
// SetCursor, CursorEnd, Focus, Blur, Focused, Update), plus where the cursor is drawn, which the real
// terminal cursor needs for IME composing (see setCursor).

import (
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const (
	editorRows = 6   // the box grows up to this many rows, then scrolls
	undoMax    = 100 // undo steps kept
)

// vrow is one drawn row: runes [start, end) of the value; last = the row ends a line (a newline or the end).
type vrow struct {
	start, end int
	last       bool
}

type edSnap struct {
	val []rune
	pos int
}

type editor struct {
	Placeholder string
	Width       int // cells for the text

	val   []rune
	pos   int // cursor, a rune index into val
	focus bool
	top   int // first row shown
	undo  []edSnap
	typed bool // the last change was typing a letter: the next one joins the same undo step

	ver            int    // bumped on every change of val
	wrap           []vrow // rows() for (wrapVer, wrapW), kept by fix: rows is asked for several times per key
	wrapVer, wrapW int
}

func (e editor) Value() string { return string(e.val) }

func (e *editor) SetValue(s string) {
	e.undo, e.typed = nil, false // a new text (a draft, a sent message gone): nothing to undo into
	e.val = sanitize(s)
	e.ver++
	e.pos = min(e.pos, len(e.val))
	e.fix()
}

func (e editor) Position() int    { return e.pos }
func (e *editor) SetCursor(n int) { e.pos = max(0, min(n, len(e.val))); e.fix() }
func (e *editor) CursorEnd()      { e.SetCursor(len(e.val)) }
func (e *editor) Focus()          { e.focus = true }
func (e *editor) Blur()           { e.focus = false }
func (e editor) Focused() bool    { return e.focus }
func (e *editor) SetWidth(w int)  { e.Width = max(w, 1); e.fix() }

// rows is the value cut into drawn rows (the last column holds the cursor); fix keeps it computed.
func (e editor) rows() []vrow {
	if e.wrap != nil && e.wrapVer == e.ver && e.wrapW == e.Width {
		return e.wrap
	}
	return wrapRows(e.val, max(e.Width-1, 1))
}
func (e editor) Height() int        { return min(len(e.rows()), editorRows) }
func (e editor) isSpace(i int) bool { return i >= 0 && i < len(e.val) && unicode.IsSpace(e.val[i]) }

// sanitize keeps what can be typed and drawn: newlines (any style) become "\n", a tab four spaces, other
// control characters go (a paste could carry escape sequences).
func sanitize(s string) []rune {
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n", "\t", "    ").Replace(s)
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if r == '\n' || !unicode.IsControl(r) {
			out = append(out, r)
		}
	}
	return out
}

// wrapRows cuts val into rows of at most lim cells, at a space when the row has one.
func wrapRows(val []rune, lim int) []vrow {
	var rows []vrow
	start, w, space := 0, 0, -1
	for i := 0; i <= len(val); i++ {
		if i == len(val) || val[i] == '\n' {
			rows = append(rows, vrow{start, i, true})
			start, w, space = i+1, 0, -1
			continue
		}
		rw := 1
		if val[i] >= 0x80 { // ASCII is one cell; anything else is measured
			rw = ansi.StringWidth(string(val[i]))
		}
		if w+rw > lim && i > start {
			cut := i
			if space >= start && space+1 < i {
				cut = space + 1 // the space stays at the end of the row
			}
			rows = append(rows, vrow{start, cut, false})
			start, w, space = cut, ansi.StringWidth(string(val[cut:i])), -1
		}
		if val[i] == ' ' {
			space = i
		}
		w += rw
	}
	return rows
}

// rowOf is the row the cursor is on: at the end of a wrapped row it belongs to the next one.
func rowOf(rows []vrow, pos int) int {
	for r, v := range rows {
		if pos >= v.start && (pos < v.end || (pos == v.end && v.last)) {
			return r
		}
	}
	return len(rows) - 1
}

// Cursor is where the cursor is drawn: the row among those shown and the cell in it.
func (e editor) Cursor() (row, col int) {
	rows := e.rows()
	r := rowOf(rows, e.pos)
	return r - e.top, ansi.StringWidth(string(e.val[rows[r].start:e.pos]))
}

// fix keeps the cursor's row among the rows shown.
func (e *editor) fix() {
	if e.wrap == nil || e.wrapVer != e.ver || e.wrapW != e.Width {
		e.wrap, e.wrapVer, e.wrapW = wrapRows(e.val, max(e.Width-1, 1)), e.ver, e.Width
	}
	rows := e.rows()
	r, n := rowOf(rows, e.pos), min(len(rows), editorRows)
	switch {
	case r < e.top:
		e.top = r
	case r >= e.top+n:
		e.top = r - n + 1
	}
	e.top = max(0, min(e.top, len(rows)-n))
}

// View is the rows shown, as plain text (the box paints them); the placeholder when empty.
func (e editor) View() []string {
	if len(e.val) == 0 {
		return []string{fg(cMuted).Render(e.Placeholder)}
	}
	rows := e.rows()
	n := min(len(rows), editorRows)
	out := make([]string, 0, n)
	for _, v := range rows[e.top : e.top+n] {
		out = append(out, string(e.val[v.start:v.end]))
	}
	return out
}

// colPos is the rune index in row v closest to cell col from the left.
func (e editor) colPos(v vrow, col int) int {
	end := v.end
	if !v.last && end > v.start {
		end-- // the end of a wrapped row is the start of the next one
	}
	p, w := v.start, 0
	for p < end {
		rw := ansi.StringWidth(string(e.val[p]))
		if w+rw > col {
			break
		}
		w += rw
		p++
	}
	return p
}

// ClickAt puts the cursor at a cell of the rows shown (a click in the box).
func (e *editor) ClickAt(row, col int) {
	rows := e.rows()
	if r := e.top + row; r >= 0 && r < len(rows) {
		e.pos = e.colPos(rows[r], col)
		e.typed = false
	}
}

// snap records the value before a change for undo; letters typed in a row are one step.
func (e *editor) snap(typing bool) {
	if typing && e.typed {
		return
	}
	e.typed = typing
	if n := len(e.undo); n > 0 && string(e.undo[n-1].val) == string(e.val) {
		return
	}
	e.undo = append(e.undo, edSnap{append([]rune(nil), e.val...), e.pos})
	if len(e.undo) > undoMax {
		e.undo = e.undo[1:]
	}
}

func (e *editor) insert(rs []rune) {
	if len(rs) == 0 {
		return
	}
	e.snap(len(rs) == 1 && !unicode.IsSpace(rs[0]))
	e.val = append(e.val[:e.pos:e.pos], append(rs, e.val[e.pos:]...)...)
	e.ver++
	e.pos += len(rs)
}

func (e *editor) cut(from, to int) {
	if from >= to {
		return
	}
	e.snap(false)
	e.val = append(e.val[:from:from], e.val[to:]...)
	e.ver++
	e.pos = from
}

func (e editor) wordLeft() int {
	p := e.pos
	for p > 0 && e.isSpace(p-1) {
		p--
	}
	for p > 0 && !e.isSpace(p-1) {
		p--
	}
	return p
}

func (e editor) wordRight() int {
	p := e.pos
	for p < len(e.val) && e.isSpace(p) {
		p++
	}
	for p < len(e.val) && !e.isSpace(p) {
		p++
	}
	return p
}

func (e editor) lineStart() int {
	p := e.pos
	for p > 0 && e.val[p-1] != '\n' {
		p--
	}
	return p
}

func (e editor) lineEnd() int {
	p := e.pos
	for p < len(e.val) && e.val[p] != '\n' {
		p++
	}
	return p
}

// vertical moves the cursor d rows, keeping its column; false when there is no row there (the key is
// then free for something else).
func (e *editor) vertical(d int) bool {
	rows := e.rows()
	r := rowOf(rows, e.pos)
	if r+d < 0 || r+d >= len(rows) {
		return false
	}
	col := ansi.StringWidth(string(e.val[rows[r].start:e.pos]))
	e.pos = e.colPos(rows[r+d], col)
	return true
}

// Update takes a key. Enter is not handled here: it sends (the app decides); Alt+Enter and Ctrl+J insert a
// newline (terminals report Shift+Enter as plain Enter, or as Alt+Enter when set up to).
func (e editor) Update(msg tea.Msg) (editor, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok || !e.focus {
		return e, nil
	}
	moved := true // a move ends the current undo step
	switch k.String() {
	case "alt+enter", "ctrl+j":
		e.insert([]rune{'\n'})
	case "backspace", "ctrl+h":
		if e.pos > 0 {
			e.cut(e.pos-1, e.pos)
		}
	case "delete", "ctrl+d":
		if e.pos < len(e.val) {
			e.cut(e.pos, e.pos+1)
		}
	case "ctrl+w", "alt+backspace":
		e.cut(e.wordLeft(), e.pos)
	case "alt+d", "alt+delete":
		e.cut(e.pos, e.wordRight())
	case "ctrl+u": // to the start of the line; at the start, the newline before it
		if s := e.lineStart(); s < e.pos {
			e.cut(s, e.pos)
		} else if e.pos > 0 {
			e.cut(e.pos-1, e.pos)
		}
	case "ctrl+z":
		if n := len(e.undo); n > 0 {
			s := e.undo[n-1]
			e.undo, e.val, e.pos = e.undo[:n-1], s.val, s.pos
			e.ver++
		}
	case "left":
		e.pos = max(e.pos-1, 0)
	case "right":
		e.pos = min(e.pos+1, len(e.val))
	case "alt+left", "ctrl+left", "alt+b":
		e.pos = e.wordLeft()
	case "alt+right", "ctrl+right", "alt+f":
		e.pos = e.wordRight()
	case "home", "ctrl+a":
		e.pos = e.lineStart()
	case "end", "ctrl+e":
		e.pos = e.lineEnd()
	case "up":
		e.vertical(-1)
	case "down":
		e.vertical(1)
	default:
		moved = false
		switch {
		case k.Type == tea.KeyRunes && (k.Paste || !k.Alt):
			e.insert(sanitize(string(k.Runes)))
		case k.Type == tea.KeySpace:
			e.insert([]rune{' '})
		}
	}
	if moved {
		e.typed = false
	}
	e.fix()
	return e, nil
}
