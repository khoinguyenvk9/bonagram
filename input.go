package main

import (
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// mouseLeak repairs mouse reports that Bubble Tea cut in two between reads: the ESC (and sometimes "[") is
// taken as a key and the rest ("<65;10;12M") would be typed into the input box. It recognises that tail,
// swallows it, and turns it back into the mouse event. A real Esc still goes through at once.
type mouseLeak struct {
	state int // 0 idle, 1 after Esc, 2 after "[", 3 inside "<digits;digits;digits"
	num   []rune
}

func (l *mouseLeak) filter(_ tea.Model, msg tea.Msg) tea.Msg {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return msg
	}
	switch {
	case k.Type == tea.KeyEscape:
		l.state = 1
		return msg
	case k.Type == tea.KeyRunes && k.Alt && len(k.Runes) == 1 && k.Runes[0] == '[': // "ESC [" read as Alt+[
		l.state = 2
		return nil
	case k.Type != tea.KeyRunes || k.Paste || l.state == 0:
		l.state = 0
		return msg
	}
	var out tea.Msg
	for i, r := range k.Runes {
		switch {
		case l.state == 1 && r == '[':
			l.state = 2
		case l.state == 2 && r == '<':
			l.state, l.num = 3, l.num[:0]
		case l.state == 3 && (unicode.IsDigit(r) || r == ';'):
			l.num = append(l.num, r)
		case l.state == 3 && (r == 'M' || r == 'm'):
			if ev, ok := sgrMouse(string(l.num), r); ok {
				out = ev
			}
			l.state = 0
		default: // not a mouse report after all: the rest is ordinary typing
			l.state = 0
			k.Runes = k.Runes[i:]
			return k
		}
	}
	return out // nil swallows the message
}

// sgrMouse decodes the "b;x;y" of an SGR mouse report ending in M (press/motion) or m (release).
func sgrMouse(p string, final rune) (tea.MouseMsg, bool) {
	f := strings.Split(p, ";")
	if len(f) != 3 {
		return tea.MouseMsg{}, false
	}
	var n [3]int
	for i := range n {
		v, err := strconv.Atoi(f[i])
		if err != nil {
			return tea.MouseMsg{}, false
		}
		n[i] = v
	}
	b := n[0]
	ev := tea.MouseMsg{X: n[1] - 1, Y: n[2] - 1, Shift: b&4 != 0, Alt: b&8 != 0, Ctrl: b&16 != 0}
	if b&64 != 0 {
		ev.Button = [4]tea.MouseButton{tea.MouseButtonWheelUp, tea.MouseButtonWheelDown, tea.MouseButtonWheelLeft, tea.MouseButtonWheelRight}[b&3]
		ev.Action = tea.MouseActionPress
		return ev, true
	}
	ev.Button = [4]tea.MouseButton{tea.MouseButtonLeft, tea.MouseButtonMiddle, tea.MouseButtonRight, tea.MouseButtonNone}[b&3]
	switch {
	case final == 'm':
		ev.Action = tea.MouseActionRelease
	case b&32 != 0:
		ev.Action = tea.MouseActionMotion
	default:
		ev.Action = tea.MouseActionPress
	}
	return ev, true
}
