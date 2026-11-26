package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func TestCanSeeReactors(t *testing.T) {
	rs := []reaction{{"👍", 1, false}}
	pm, grp, ch := &chat{key: "u1"}, &chat{key: "c2"}, &chat{key: "c3", broadcast: true}
	for _, c := range []struct {
		c    *chat
		l    line
		want bool
	}{
		{pm, line{id: 1, reacts: rs}, true},
		{pm, line{id: 1}, false},              // no reactions
		{pm, line{reacts: rs}, false},         // not numbered yet
		{grp, line{id: 1, reacts: rs}, false}, // Telegram did not say it can be seen
		{grp, line{id: 1, reacts: rs, msg: &tg.Message{Reactions: tg.MessageReactions{CanSeeList: true}}}, true},
		{grp, line{id: 1, reacts: rs, seeWho: true}, true},
		{ch, line{id: 1, reacts: rs, seeWho: true}, false}, // channels hide who reacted
		{nil, line{id: 1, reacts: rs}, false},
	} {
		if got := canSeeReactors(c.c, c.l); got != c.want {
			t.Errorf("%+v %+v: %v", c.c, c.l, got)
		}
	}
	got := privateReactors(&chat{key: "u1", title: "Đạt"}, []reaction{{"👍", 2, true}, {"❤️", 1, false}, {"🔥", 1, true}})
	var s []string
	for _, r := range got {
		s = append(s, r.name+r.emoji)
	}
	if strings.Join(s, " ") != "Bạn👍 Đạt👍 Đạt❤️ Bạn🔥" {
		t.Fatalf("private: %v", s)
	}
}

func groupWithReactedMessage(t *testing.T) *actionModel {
	a := newGroupModel(t, "g4", &tg.InputPeerChat{ChatID: 4})
	msg := &tg.Message{ID: 9, Message: "đi ăn không", Reactions: tg.MessageReactions{CanSeeList: true}}
	a.send(histMsg{mk: "g4", key: "g4", lines: []line{{id: 9, from: "Hà", sender: "u12", text: "đi ăn không", at: time.Now(), msg: msg,
		reacts: []reaction{{"👍", 2, true}, {"❤️", 1, false}}}}})
	a.rec.reactors = []tg.MessagePeerReaction{
		{PeerID: &tg.PeerUser{UserID: 11}, Reaction: &tg.ReactionEmoji{Emoticon: "👍"}},
		{PeerID: &tg.PeerUser{UserID: 13}, Reaction: &tg.ReactionEmoji{Emoticon: "👍"}, My: true},
		{PeerID: &tg.PeerUser{UserID: 12}, Reaction: &tg.ReactionEmoji{Emoticon: "❤️"}},
		{PeerID: nil, Reaction: &tg.ReactionEmoji{Emoticon: "😮"}}, // a broken entry is skipped
	}
	return a
}

// openReactors right-clicks message 9 and picks "Ai đã thả react"; it returns the command that asks the server.
func openReactors(t *testing.T, a *actionModel) tea.Cmd {
	t.Helper()
	m := a.m()
	l := m.lines()[0]
	m.menu = &ctxMenu{x: 50, y: 5, items: m.menuFor(l)}
	for i, it := range m.menu.items {
		if it.label == "Ai đã thả react" {
			cmd := m.pickMenu(i)
			a.set(m)
			return cmd
		}
	}
	t.Fatal("no 'Ai đã thả react' in the message menu")
	return nil
}

func menuLabels(m *model) string {
	var s []string
	for _, it := range m.menu.items {
		s = append(s, strings.TrimSpace(it.label+" "+it.hint))
	}
	return strings.Join(s, " | ")
}

func TestReactorsMenu(t *testing.T) {
	a := groupWithReactedMessage(t)
	cmd := openReactors(t, a)
	if m := a.m(); m.menu == nil || m.menu.title != "Đang tải…" {
		t.Fatal("the menu should wait for the list")
	}
	a.run(cmd)
	m := a.m()
	if req, ok := a.rec.last(t).(*tg.MessagesGetMessageReactionsListRequest); !ok || req.ID != 9 {
		t.Fatalf("request: %T", a.rec.last(t))
	}
	if got := menuLabels(m); got != "Tất cả · 3 ✓ | 👍 · 2 | ❤ · 1 |  | Khôi Nguyễn 👍 | Bạn 👍 | Hà ❤" {
		t.Fatalf("menu: %q", got)
	}
	frameOK(t, *m)
	if m.menu.title != "Ai đã thả react" {
		t.Fatalf("all listed: no count in the title: %q", m.menu.title)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Ai đã thả react") || !strings.Contains(v, "Khôi Nguyễn") {
		t.Fatal("the list is not drawn")
	}
	// filter by ❤ (clean drops the variation selector), then open Hà's chat (not in the list yet: it joins it)
	m.pickMenu(2)
	if got := menuLabels(m); !strings.HasSuffix(got, "|  | Hà ❤") || !strings.Contains(got, "❤ · 1 ✓") {
		t.Fatalf("filtered: %q", got)
	}
	a.set(m)
	cmd = m.pickMenu(len(m.menu.items) - 1)
	a.set(m)
	if m.menu != nil || m.cur != "u12" || m.index("u12") < 0 {
		t.Fatalf("a click on Hà opens her chat: cur=%q", m.cur)
	}
	if c := m.chats[m.index("u12")]; c.title != "Hà" {
		t.Fatalf("chat: %+v", c)
	}
	_ = cmd
}

func TestReactorsMenuEdges(t *testing.T) {
	// the menu was closed before the answer came: it stays closed
	a := groupWithReactedMessage(t)
	cmd := openReactors(t, a)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.run(cmd)
	if a.m().menu != nil {
		t.Fatal("a late answer must not reopen the menu")
	}
	// Telegram asks to wait: said in Vietnamese
	b := groupWithReactedMessage(t)
	b.rec.err = tgerr.New(420, "FLOOD_WAIT_7")
	b.run(openReactors(t, b))
	if m := b.m(); m.menu != nil || !strings.Contains(m.err, "thử lại sau 8 giây") {
		t.Fatalf("flood wait: menu=%v err=%q", m.menu != nil, m.err)
	}
	// a private chat needs no request
	c := newActionModel(t)
	c.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := c.m()
	ls := m.msgs["u1"]
	ls[1].reacts = []reaction{{"😂", 2, true}}
	m.msgs["u1"] = ls
	m.menu = &ctxMenu{x: 50, y: 5, items: m.menuFor(ls[1])}
	n := c.rec.count()
	for i, it := range m.menu.items {
		if it.label == "Ai đã thả react" {
			m.pickMenu(i)
			break
		}
	}
	if c.rec.count() != n || menuLabels(m) != "Bạn 😂 | Đạt 😂" {
		t.Fatalf("private: %q", menuLabels(m))
	}
	// a live update that says the list can be seen turns the entry on
	g := newGroupModel(t, "c5", &tg.InputPeerChannel{ChannelID: 5, AccessHash: 1})
	g.send(histMsg{mk: "c5", key: "c5", lines: []line{{id: 3, from: "Hà", sender: "u12", text: "x", at: time.Now(), msg: &tg.Message{ID: 3}}}})
	g.send(reactMsg{key: "c5", id: 3, rs: []reaction{{"👍", 1, false}}, canSee: true})
	gm := g.m()
	if !canSeeReactors(gm.curChat(), gm.lines()[0]) {
		t.Fatal("canSee from a live update")
	}
}

// Many people: the box keeps inside the window and the title says how many are listed.
func TestReactorsMenuFitsWindow(t *testing.T) {
	a := groupWithReactedMessage(t)
	a.rec.reactors = nil
	for i := 0; i < 40; i++ {
		a.rec.reactors = append(a.rec.reactors, tg.MessagePeerReaction{PeerID: &tg.PeerUser{UserID: 11}, Reaction: &tg.ReactionEmoji{Emoticon: "👍"}})
	}
	a.run(openReactors(t, a))
	m := a.m()
	if !strings.Contains(m.menu.title, "(22/40)") || m.menu.y < 0 || m.menu.y+m.menu.head()+len(m.menu.items)+1 > m.h-1 {
		t.Fatalf("title %q, box at %d with %d items in %d rows", m.menu.title, m.menu.y, len(m.menu.items), m.h)
	}
	frameOK(t, *m)
}

// More than one page: the title counts what Telegram says there is in all.
func TestReactorsMoreThanAPage(t *testing.T) {
	a := groupWithReactedMessage(t)
	a.rec.reactors = nil
	for i := 0; i < 150; i++ {
		a.rec.reactors = append(a.rec.reactors, tg.MessagePeerReaction{PeerID: &tg.PeerUser{UserID: 11}, Reaction: &tg.ReactionEmoji{Emoticon: "👍"}})
	}
	a.run(openReactors(t, a))
	if m := a.m(); !strings.Contains(m.menu.title, "(22/150)") {
		t.Fatalf("title %q", m.menu.title)
	}
}
