package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/tg"
)

func typeKeys(e editor, keys ...string) editor {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "alt+enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter, Alt: true}
		case "ctrl+j":
			msg = tea.KeyMsg{Type: tea.KeyCtrlJ}
		case "ctrl+z":
			msg = tea.KeyMsg{Type: tea.KeyCtrlZ}
		case "ctrl+w":
			msg = tea.KeyMsg{Type: tea.KeyCtrlW}
		case "ctrl+u":
			msg = tea.KeyMsg{Type: tea.KeyCtrlU}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		case "left":
			msg = tea.KeyMsg{Type: tea.KeyLeft}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "home":
			msg = tea.KeyMsg{Type: tea.KeyHome}
		case " ":
			msg = tea.KeyMsg{Type: tea.KeySpace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		e, _ = e.Update(msg)
	}
	return e
}

func TestEditorLinesAndWrap(t *testing.T) {
	e := editor{Width: 11} // rows of up to 10 cells
	e.Focus()
	e = typeKeys(e, "dòng", " ", "một", "alt+enter", "hai", "ctrl+j", "ba")
	if e.Value() != "dòng một\nhai\nba" || e.Height() != 3 {
		t.Fatalf("alt+enter and ctrl+j add lines: %q h=%d", e.Value(), e.Height())
	}
	if r, c := e.Cursor(); r != 2 || c != 2 {
		t.Fatalf("cursor after typing: row %d col %d", r, c)
	}
	e = typeKeys(e, "up", "up")
	if r, c := e.Cursor(); r != 0 || c != 2 || e.Position() != 2 {
		t.Fatalf("up keeps the column: row %d col %d pos %d", r, c, e.Position())
	}

	e.SetValue("một hai ba bốn năm sáu bảy")
	if got := strings.Join(e.View(), "|"); got != "một hai |ba bốn |năm sáu |bảy" {
		t.Fatalf("words wrap at spaces: %q", got)
	}
	e.SetValue(strings.Repeat("x", 100))
	if e.Height() != editorRows {
		t.Fatalf("the box stops growing at %d rows: %d", editorRows, e.Height())
	}
	e.CursorEnd()
	if r, _ := e.Cursor(); r != editorRows-1 {
		t.Fatalf("the cursor row stays visible: %d", r)
	}
	e.SetCursor(0)
	if r, _ := e.Cursor(); r != 0 || e.View()[0] != strings.Repeat("x", 10) {
		t.Fatal("moving to the start scrolls back up")
	}
}

// "á" as a + U+0301 and a family emoji are one character each for moving and deleting.
func TestEditorGraphemes(t *testing.T) {
	e := editor{Width: 40}
	e.Focus()
	e.SetValue("ca\u0301\n👨\u200d👩\u200d👧x")
	e.CursorEnd()
	e = typeKeys(e, "backspace", "backspace")
	if e.Value() != "ca\u0301\n" {
		t.Fatalf("backspace deletes the whole emoji: %q", e.Value())
	}
	e = typeKeys(e, "left", "left")
	if e.Position() != 1 {
		t.Fatalf("left crosses the newline, then a + accent at once: at %d", e.Position())
	}
	e = typeKeys(e, "right")
	if e.Position() != 3 {
		t.Fatalf("right crosses a + accent at once: at %d", e.Position())
	}
	e = typeKeys(e, "backspace")
	if e.Value() != "c\n" {
		t.Fatalf("backspace deletes a with its accent: %q", e.Value())
	}
}

func TestEditorPasteUndoAndDelete(t *testing.T) {
	e := editor{Width: 40}
	e.Focus()
	e, _ = e.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("a\r\nb\tc\x1b[2Jd")})
	if e.Value() != "a\nb    c[2Jd" {
		t.Fatalf("a paste keeps its lines and loses control characters: %q", e.Value())
	}
	e.SetValue("")
	e = typeKeys(e, "x", "i", "n", " ", "c", "h", "à", "o")
	e = typeKeys(e, "ctrl+w")
	if e.Value() != "xin " {
		t.Fatalf("ctrl+w deletes a word: %q", e.Value())
	}
	e = typeKeys(e, "ctrl+z")
	if e.Value() != "xin chào" {
		t.Fatalf("undo brings the word back: %q", e.Value())
	}
	e = typeKeys(e, "ctrl+z")
	if e.Value() != "xin " {
		t.Fatalf("letters typed in a row are one undo step: %q", e.Value())
	}
	e.SetValue("một\nhai")
	e.CursorEnd()
	e = typeKeys(e, "ctrl+u")
	if e.Value() != "một\n" {
		t.Fatalf("ctrl+u clears only the current line: %q", e.Value())
	}
	e = typeKeys(e, "ctrl+u")
	if e.Value() != "một" {
		t.Fatalf("at a line start ctrl+u joins the lines: %q", e.Value())
	}
}

func TestMultiLineMessage(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browse: typing
	h0 := a.m().vp.Height
	a.send(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("dòng 1\ndòng 2\ndòng 3")})
	m := a.m()
	if m.in.Height() != 3 || m.vp.Height != h0-2 {
		t.Fatalf("the box grows and the messages make room: box %d vp %d→%d", m.in.Height(), h0, m.vp.Height)
	}
	frameOK(t, *m)
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	req, ok := a.rec.last(t).(*tg.MessagesSendMessageRequest)
	if !ok || req.Message != "dòng 1\ndòng 2\ndòng 3" {
		t.Fatalf("the lines are sent as typed: %+v", a.rec.last(t))
	}
	if m = a.m(); m.in.Value() != "" || m.vp.Height != h0 {
		t.Fatalf("after sending the box is one row again: vp %d", m.vp.Height)
	}
	frameOK(t, *m)
}

func TestEmojiSuggestions(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	for _, r := range "ok :ha" {
		a.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m := a.m()
	if len(m.ec) == 0 || m.ec[0].e != "😂" || !strings.Contains(m.barText(), "😂") {
		t.Fatalf("':ha' suggests 😂: %v bar=%q", m.ec, m.barText())
	}
	frameOK(t, *m)
	a.send(tea.KeyMsg{Type: tea.KeyTab})
	if m = a.m(); m.in.Value() != "ok 😂" || len(m.ec) != 0 || m.barText() != "" {
		t.Fatalf("Tab puts the emoji in: %q", m.in.Value())
	}
	if _, _, ok := emojiToken("http://x", 7); ok {
		t.Fatal("a colon inside a word is not a suggestion")
	}
	if got := findEmoji("camon"); len(got) == 0 || got[0].e != "🙏" {
		t.Fatalf("Vietnamese keywords: %v", got)
	}
}
