package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

func TestReadMarks(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browse
	view := func() string { return ansi.Strip(a.mm.View()) }
	if v := view(); !strings.Contains(v, "✓") || strings.Contains(v, "✓✓") {
		t.Fatal("an own message nobody read shows one tick")
	}
	a.send(readOutMsg{key: "u3", id: 99}) // another chat: nothing changes here
	if strings.Contains(view(), "✓✓") {
		t.Fatal("a read in another chat must not mark this one")
	}
	a.send(readOutMsg{key: "u1", id: 5})
	if !strings.Contains(view(), "✓✓") {
		t.Fatal("read up to our message: two ticks")
	}
	a.send(readOutMsg{key: "u1", id: 2}) // an older, late update never takes the mark back
	if !strings.Contains(view(), "✓✓") {
		t.Fatal("the read mark only moves forward")
	}
	frameOK(t, *a.m())

	m := a.m()
	m.selfKey = "u1" // Saved Messages: no ticks at all
	m.render()
	a.set(m)
	if strings.Contains(view(), "✓") {
		t.Fatal("no ticks in Saved Messages")
	}
}

func TestSearchPeopleOnServer(t *testing.T) {
	a := newActionModel(t)
	a.rec.found = &tg.ContactsFound{
		Results: []tg.PeerClass{&tg.PeerUser{UserID: 3}, &tg.PeerUser{UserID: 9}},
		Users: []tg.UserClass{
			&tg.User{ID: 3, AccessHash: 33, FirstName: "Lan"},                         // already in the list: not repeated
			&tg.User{ID: 9, AccessHash: 99, FirstName: "Lan Anh", Username: "lananh"}, // new
		},
	}
	a.send(tea.KeyMsg{Type: tea.KeyCtrlK})
	a.runAll(a.key("la")) // too short: nothing asked
	if a.rec.count() != 0 {
		t.Fatal("two letters must not reach the server")
	}
	start := time.Now()
	a.runAll(a.key("n"))
	if _, ok := a.rec.last(t).(*tg.ContactsSearchRequest); !ok || time.Since(start) < peoplePause {
		t.Fatalf("the search should go out after the pause: %T", a.rec.last(t))
	}
	m := a.m()
	if len(m.found) != 1 || m.found[0].key != "u9" {
		t.Fatalf("only the unknown person is a result: %+v", m.found)
	}
	if v := ansi.Strip(a.mm.View()); !strings.Contains(v, "Lan Anh") || !strings.Contains(v, "tìm trên Telegram") {
		t.Fatal("the result is listed under the matches")
	}
	frameOK(t, *m)

	a.send(tea.KeyMsg{Type: tea.KeyDown}) // Lan (local), then Lan Anh (server)
	a.send(tea.KeyMsg{Type: tea.KeyEnter})
	m = a.m()
	c := m.curChat()
	if m.cur != "u9" || c == nil || c.peer.(*tg.InputPeerUser).AccessHash != 99 || c.last != "" || m.found != nil {
		t.Fatalf("Enter opens the person as a chat of the list: cur=%q", m.cur)
	}
	if m.searching {
		t.Fatal("opening leaves the search")
	}
}

func TestReadOnAnotherDevice(t *testing.T) {
	a := newActionModel(t)
	m := a.m()
	m.chats[1].unread = 5 // Lan
	m.chats = append(m.chats, chat{key: "c7", title: "Forum", forum: true, peer: &tg.InputPeerChannel{ChannelID: 7},
		topics: []topic{{1, "General", 2, 0}, {4, "Dev", 3, 0}}, topic: 1, unread: 5})
	a.set(m)
	if !strings.Contains(a.m().windowTitle(), "(10)") {
		t.Fatalf("before: %q", a.m().windowTitle())
	}

	a.send(readInMsg{key: "u3", unread: 0}) // read on the phone
	if a.m().chats[1].unread != 0 {
		t.Fatal("the badge follows a read made elsewhere")
	}
	a.send(readInMsg{key: "u3", unread: -1}) // a bad count never goes below zero
	if a.m().chats[1].unread != 0 {
		t.Fatal("unread stays at zero")
	}

	a.send(readInMsg{key: "c7"}) // the forum-wide update: ignored, its count is not trustworthy
	if a.m().chats[2].unread != 5 {
		t.Fatal("the forum's own count is not used")
	}
	a.rec.topics = []tg.ForumTopicClass{
		&tg.ForumTopic{ID: 1, Title: "General", UnreadCount: 2},
		&tg.ForumTopic{ID: 4, Title: "Dev", UnreadCount: 0},
	}
	batch := a.send(readInMsg{key: "c7", topic: 4})().(tea.BatchMsg)
	a.send(batch[1]()) // batch[0] waits for the next live event
	m = a.m()
	if m.chats[2].unread != 2 || m.chats[2].topics[1].unread != 0 || m.cur != "u1" {
		t.Fatalf("a topic read elsewhere refreshes the counts quietly: unread=%d cur=%q", m.chats[2].unread, m.cur)
	}
	if got := a.m().windowTitle(); !strings.Contains(got, "(2)") {
		t.Fatalf("the tab title follows: %q", got)
	}
}
