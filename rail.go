package main

// The chat list folds into a rail of initials when the window is narrow (design board "Cửa sổ hẹp"): one
// coloured tile per chat with its unread count, the full name on hover. Searching (Ctrl+K, or forwarding)
// opens the full list again until a chat is picked.

import (
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

const (
	railW    = 8 // cells of the folded list: bar, space, a 4-cell tile, the unread count
	foldSnap = 4 // dragging the divider this far left of the narrowest list folds it
)

// narrow: the window has no room for the list at its width beside the messages, so it folds by itself.
func (m *model) narrow() bool { return m.w > 0 && m.w < m.sideWant+minRight }

// fitSide sets the drawn width of the chat list: the rail in a narrow window or when the user folded it
// (unless the list is being searched), else the width the user dragged it to, kept inside the window.
func (m *model) fitSide() {
	m.rail = m.w > 0 && (m.narrow() || m.folded) && !m.searching
	if m.rail {
		m.side = railW
		return
	}
	m.side = max(sideMin, min(m.sideWant, sideMax, m.w-minRight))
}

// initials are the first letters of the first two words of a name ("Nhóm Dev Go" → "ND"), or the first two
// letters of a one-word name; "?" when it has no letters.
func initials(name string) string {
	letter := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	var out []rune
	first := "" // the word the first letter came from
	for _, w := range strings.Fields(name) {
		for _, r := range w {
			if letter(r) {
				out = append(out, unicode.ToUpper(r))
				if first == "" {
					first = w
				}
				break
			}
		}
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 1 { // one word with letters: its second letter too
		seen := false
		for _, r := range first {
			if letter(r) {
				if seen {
					out = append(out, unicode.ToUpper(r))
					break
				}
				seen = true
			}
		}
	}
	s := string(out)
	switch {
	case s == "":
		return "?"
	case ansi.StringWidth(s) > 2: // wide letters (CJK): one is enough
		return string(out[:1])
	}
	return s
}

// nameColour is the palette colour of a name, the same one nameStyle gives it in the messages.
func nameColour(name string) string {
	sum := 0
	for _, r := range name {
		sum += int(r)
	}
	return palette[sum%len(palette)]
}

// railView is the folded list, bodyH rows tall: "B", the search key, then one tile per chat (itemRows rows
// each, like the full list, so clicks and scrolling map the same way).
func (m model) railView(bodyH int) []string {
	row := func(s string) string { return paint(s, railW, cFg, cPanel) }
	key := keyCap("ctrl+k")
	left := []string{
		row(""),
		row("   " + inkB(cAccent, "B")),
		row(""),
		row(""),
		row(strings.Repeat(" ", max((railW-ansi.StringWidth(key))/2, 0)) + key),
		row(""),
		row(""),
	}
	vis := m.visible()
	for p := m.top; p < len(vis) && len(left) < bodyH; p++ {
		left = append(left, railTile(m.chatAt(vis[p]), m.chatAt(vis[p]).key == m.cur)...)
	}
	for len(left) < bodyH {
		left = append(left, row(""))
	}
	return left[:bodyH]
}

// railTile is one chat in the rail: the initials on a tile of the name's colour (a half block below makes it
// about a row and a half tall), the unread count beside its top, the accent bar when it is open.
func railTile(c chat, open bool) []string {
	col := nameColour(c.title)
	bar := " "
	if open {
		bar = ink(cAccent, "▌")
	}
	tile := chip(cBg, col, fit(" "+initials(c.title), 4), true)
	badge := ""
	if c.unread > 0 {
		n := strconv.Itoa(c.unread)
		if c.unread > 9 {
			n = "9+"
		}
		badge = chip(cBg, cAccent, n, true)
	}
	under := ink(col, "▀▀▀▀")
	return []string{
		paint(bar+" "+tile+badge, railW, cFg, cPanel),
		paint(bar+" "+under, railW, cFg, cPanel),
		paint("", railW, cFg, cPanel),
	}
}

// railChat is the chat whose tile is at screen row y, or nil.
func (m *model) railChat(y int) *chat {
	if y < listY0 {
		return nil
	}
	vis := m.visible()
	if p := m.top + (y-listY0)/itemRows; p < len(vis) && (y-listY0)%itemRows < 2 && vis[p] < len(m.chats) {
		return &m.chats[vis[p]]
	}
	return nil
}

// railTip draws the full name of the hovered tile next to the rail.
func (m model) railTip(rows []string) {
	if !m.rail || m.railHot < listY0 {
		return
	}
	c := m.railChat(m.railHot)
	if c == nil {
		return
	}
	text := " " + inkB(cFg, c.title) + "  " + ink(cMuted, shortTime(c.date, time.Now())) + " "
	if c.unread > 0 {
		text += ink(cAccent, strconv.Itoa(c.unread)+" chưa đọc") + " "
	}
	w := min(ansi.StringWidth(text), max(m.w-m.side-1, 1))
	y := listY0 + (m.railHot-listY0)/itemRows*itemRows
	overlayAt(rows, m.side, y, []string{paint(text, w, cFg, cBoost)}, w)
}
