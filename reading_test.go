package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

func plainView(m *model) string { return ansi.Strip(m.View()) }

func TestReplyQuotedInGroup(t *testing.T) {
	a := newGroupModel(t, "c9", &tg.InputPeerChannel{ChannelID: 9, AccessHash: 1})
	now := time.Now()
	a.send(histMsg{mk: "c9", key: "c9", lines: []line{
		{id: 1, from: "Bình", sender: "u2", text: "đi ăn không", at: now},
		{id: 2, from: "An", sender: "u1", text: "đi chứ", at: now, replyTo: 1},
		{id: 3, from: "An", sender: "u1", text: "ok", at: now.Add(time.Hour), replyTo: 50, quote: `Cường: "chiều nay họp"`},
		{id: 4, from: "An", sender: "u1", text: "nữa", at: now.Add(2 * time.Hour), replyTo: 51},
	}})
	v := plainView(a.m())
	for _, want := range []string{`↪ Bình: "đi ăn không"`, `↪ Cường: "chiều nay họp"`, "↪ trả lời một tin nhắn cũ"} {
		if !strings.Contains(v, want) {
			t.Errorf("missing %q in:\n%s", want, v)
		}
	}
}

func TestFillQuotesAsksOnceForOlderMessages(t *testing.T) {
	rec := &recorder{
		users: []tg.UserClass{&tg.User{ID: 11, FirstName: "Khôi", LastName: "Nguyễn"}},
		msgs: []tg.MessageClass{&tg.Message{ID: 50, Message: "bản gốc", FromID: &tg.PeerUser{UserID: 11},
			PeerID: &tg.PeerChannel{ChannelID: 9}}},
	}
	ls := []line{{id: 60, replyTo: 50}, {id: 61, replyTo: 50}, {id: 62, replyTo: 60}, {id: 63}}
	fillQuotes(context.Background(), tg.NewClient(rec), &tg.InputPeerChannel{ChannelID: 9, AccessHash: 1}, ls)
	req, ok := rec.last(t).(*tg.ChannelsGetMessagesRequest)
	if !ok || len(req.ID) != 1 || rec.count() != 1 {
		t.Fatalf("one lookup of the one missing message (50) expected: %+v (%d calls)", rec.last(t), rec.count())
	}
	if want := `Khôi Nguyễn: "bản gốc"`; ls[0].quote != want || ls[1].quote != want || ls[2].quote != "" {
		t.Fatalf("quotes: %q %q %q", ls[0].quote, ls[1].quote, ls[2].quote)
	}
}

func TestHiddenLinkShownAndCopied(t *testing.T) {
	a := newActionModel(t)
	text := "xem tài liệu ở đây"
	msg := &tg.Message{ID: 7, Message: text, Entities: []tg.MessageEntityClass{
		&tg.MessageEntityTextURL{Offset: 4, Length: 8, URL: "https://docs.example/x?a=1"}}}
	a.send(histMsg{mk: "u1", key: "u1", lines: []line{{id: 7, from: "Đạt", text: text, at: time.Now(), msg: msg}}})
	if v := plainView(a.m()); !strings.Contains(v, "xem tài liệu (https://docs.example/x?a=1) ở đây") {
		t.Fatalf("the address behind the words must be shown:\n%s", v)
	}
	var got string
	old := clipboard
	clipboard = func(s string) { got = s }
	defer func() { clipboard = old }()
	m := a.m() // still browsing from the setup: the only message is the highlighted one
	m.bsel = 0
	a.set(m)
	a.run(a.key("l"))
	if got != "https://docs.example/x?a=1" {
		t.Fatalf("l should copy the link, got %q", got)
	}
}

func TestDragSelectsAndCopiesText(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	row, col := -1, -1
	for r, s := range m.rows {
		if c := strings.Index(ansi.Strip(s), "xin chào"); c >= 0 {
			row, col = r, ansi.StringWidth(ansi.Strip(s)[:c])
		}
	}
	if row < 0 {
		t.Fatal("setup: the first message is not in the view")
	}
	y := m.msgY() + row - m.vp.YOffset
	x := m.side + 1 + col
	ev := func(dx int, act tea.MouseAction) tea.MouseMsg {
		return tea.MouseMsg{X: x + dx, Y: y, Button: tea.MouseButtonLeft, Action: act}
	}
	var got string
	old := clipboard
	clipboard = func(s string) { got = s }
	defer func() { clipboard = old }()
	a.send(ev(0, tea.MouseActionPress))
	a.send(ev(2, tea.MouseActionMotion))
	if !strings.Contains(a.m().vp.View(), "\x1b[7m") {
		t.Fatal("the selected cells should be shown in reverse video")
	}
	a.run(a.send(ev(2, tea.MouseActionRelease)))
	if got != "xin" {
		t.Fatalf("copied %q, want \"xin\"", got)
	}
	a.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) // any key clears the marking
	if strings.Contains(a.m().vp.View(), "\x1b[7m") {
		t.Fatal("a key should clear the selection")
	}
}

func TestUnknownSenderInGroupIsNotTheGroupName(t *testing.T) {
	a := newGroupModel(t, "c9", &tg.InputPeerChannel{ChannelID: 9, AccessHash: 1})
	a.send(histMsg{mk: "c9", key: "c9", lines: []line{{id: 1, from: "Bình", sender: "u2", text: "chào", at: time.Now()}}})
	live := func(id int, text string) newMsg {
		return newMsg{key: "c9", title: "Nhóm", line: line{id: id, from: "?", sender: "u2", text: text, at: time.Now()}}
	}
	a.send(live(2, "tin mới")) // the update lacked the user: his name was seen before
	if ls := a.m().lines(); ls[len(ls)-1].from != "Bình" {
		t.Fatalf("sender shown as %q", ls[len(ls)-1].from)
	}
	ev := live(3, "lạ")
	ev.line.sender = "u99" // never seen
	a.send(ev)
	if ls := a.m().lines(); ls[len(ls)-1].from == "Nhóm" || ls[len(ls)-1].from == "?" {
		t.Fatalf("an unknown sender must not take the group's name: %q", ls[len(ls)-1].from)
	}
}

// linkOnScreen is the first link in the view and the screen position of its first cell.
func linkOnScreen(t *testing.T, m *model) (l linkSpan, x, y int) {
	t.Helper()
	if len(m.links) == 0 {
		t.Fatal("no link on screen")
	}
	l = m.links[0]
	return l, m.side + 1 + l.x0, m.msgY() + l.row - m.vp.YOffset
}

func TestLinksAreMarkedForTheTerminal(t *testing.T) {
	a := newActionModel(t) // message 6: "ok nhé https://example.com"
	m := a.m()
	l, _, _ := linkOnScreen(t, m)
	if l.url != "https://example.com" || l.x1-l.x0 != 19 {
		t.Fatalf("link on screen: %+v", l)
	}
	if !strings.Contains(m.vp.View(), "\x1b]8;id=b6_0;https://example.com\x1b\\") {
		t.Fatal("the link should be an OSC 8 hyperlink so Cmd+click works in the terminal")
	}
	if !strings.Contains(plainView(m), "ok nhé https://example.com") {
		t.Fatal("the text itself must not change")
	}
	// only web addresses are links
	bad := &tg.Message{ID: 9, Message: "x", Entities: []tg.MessageEntityClass{&tg.MessageEntityTextURL{Offset: 0, Length: 1, URL: "javascript:alert(1)"}}}
	if strings.Contains(inline(bad, 0, 1, ""), osc8) {
		t.Fatal("javascript: must not become a link")
	}
}

func TestLongLinkKeepsItsMarkOnEveryRow(t *testing.T) {
	url := "https://example.com/" + strings.Repeat("abcdefghij", 8)
	msg := &tg.Message{ID: 3, Message: "xem " + url + " nhé", Entities: []tg.MessageEntityClass{&tg.MessageEntityURL{Offset: 4, Length: len(url)}}}
	rows := carry(strings.Split(ansi.Wrap(inline(msg, 0, 1000, ""), 30, ""), "\n"))
	if len(rows) < 3 {
		t.Fatalf("setup: should wrap, got %d rows", len(rows))
	}
	var got string
	for i, r := range rows {
		sp := linkSpans(i, r)
		if strings.Count(r, osc8) != 2*len(sp) {
			t.Fatalf("row %d is not balanced: %q", i, r)
		}
		for _, s := range sp {
			got += ansi.Strip(ansi.Cut(r, s.x0, s.x1))
		}
	}
	if got != url {
		t.Fatalf("the link pieces should add up to the address:\n%q\n%q", got, url)
	}
}

func TestHoverUnderlinesAndClickOpensLink(t *testing.T) {
	a := newActionModel(t)
	opened := ""
	old, oldClip := openURL, clipboard
	openURL, clipboard = func(u string) error { opened = u; return nil }, func(string) {} // not the real browser or clipboard
	defer func() { openURL, clipboard = old, oldClip }()
	l, x, y := linkOnScreen(t, a.m())
	mouse := func(act tea.MouseAction, b tea.MouseButton, dx int) tea.Cmd {
		return a.send(tea.MouseMsg{X: x + dx, Y: y, Button: b, Action: act})
	}
	if strings.Contains(a.m().vp.View(), "\x1b[4m") {
		t.Fatal("setup: nothing is underlined before hovering")
	}
	mouse(tea.MouseActionMotion, tea.MouseButtonNone, 3)
	if a.m().hover != l.url || !strings.Contains(a.m().vp.View(), "\x1b[4m") {
		t.Fatalf("hovering should underline the link: hover=%q", a.m().hover)
	}
	mouse(tea.MouseActionMotion, tea.MouseButtonNone, -3) // off the link
	if a.m().hover != "" || strings.Contains(a.m().vp.View(), "\x1b[4m") {
		t.Fatal("leaving the link should remove the underline")
	}
	// a drag that starts on a link selects text and does not open it
	mouse(tea.MouseActionPress, tea.MouseButtonLeft, 0)
	mouse(tea.MouseActionMotion, tea.MouseButtonLeft, 5)
	a.run(mouse(tea.MouseActionRelease, tea.MouseButtonLeft, 5))
	if opened != "" {
		t.Fatal("dragging over a link must not open it")
	}
	// a plain click opens it
	mouse(tea.MouseActionPress, tea.MouseButtonLeft, 2)
	a.run(mouse(tea.MouseActionRelease, tea.MouseButtonLeft, 2))
	if opened != "https://example.com" {
		t.Fatalf("a click should open the link, opened %q", opened)
	}
}
