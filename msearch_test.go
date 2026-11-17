package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/tg"
)

func TestSearchMessagesAndJump(t *testing.T) {
	a := newActionModel(t)
	a.rec.users = []tg.UserClass{&tg.User{ID: 1, FirstName: "Đạt"}}
	a.rec.msgs = []tg.MessageClass{&tg.Message{ID: 70, Message: "hẹn họp thứ hai nhé", Date: int(time.Now().Unix()),
		FromID: &tg.PeerUser{UserID: 1}, PeerID: &tg.PeerUser{UserID: 1}}}
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browsing
	a.send(tea.KeyMsg{Type: tea.KeyCtrlF})
	if m := a.m(); m.ms == nil || !strings.Contains(m.barText(), "Tìm trong Đạt") {
		t.Fatalf("Ctrl+F should open the search: bar=%q", a.m().barText())
	}
	a.typeText("họp")
	a.runAll(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	req, ok := a.rec.last(t).(*tg.MessagesSearchRequest)
	if !ok || req.Q != "họp" || req.Limit != searchLimit {
		t.Fatalf("the server should be asked to search: %+v", a.rec.last(t))
	}
	if v := plainView(a.m()); !strings.Contains(v, "Đạt: hẹn họp thứ hai nhé") || !strings.Contains(a.m().barText(), "1 kết quả") {
		t.Fatalf("the result should be listed:\n%s\nbar=%q", v, a.m().barText())
	}
	a.runAll(a.send(tea.KeyMsg{Type: tea.KeyEnter})) // open it
	h, ok := a.rec.last(t).(*tg.MessagesGetHistoryRequest)
	if !ok || h.OffsetID != 70 || h.AddOffset != -aroundBefore {
		t.Fatalf("the messages around it should be loaded: %+v", a.rec.last(t))
	}
	m := a.m()
	if m.ms != nil || !m.browse || m.lines()[m.bsel].id != 70 || !m.detached["u1"] {
		t.Fatalf("the result should be highlighted in an old stretch: ms=%v browse=%v detached=%v", m.ms, m.browse, m.detached)
	}
	if !strings.Contains(m.barText(), "Đang xem tin cũ") {
		t.Fatalf("the bar should say so: %q", m.barText())
	}
	// a new message does not join the old stretch
	n := len(m.lines())
	a.send(newMsg{key: "u1", title: "Đạt", line: line{id: 99, from: "Đạt", sender: "u1", text: "mới", at: time.Now()}})
	if len(a.m().lines()) != n {
		t.Fatal("a live message must not be appended to an old stretch")
	}
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browsing
	a.runAll(a.send(tea.KeyMsg{Type: tea.KeyEsc}))
	if a.m().detached["u1"] || a.m().barText() != "" {
		t.Fatal("Esc should go back to the latest messages")
	}
}

func TestSearchEscapeRestoresDraft(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	m.in.SetValue("nháp dở")
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyCtrlF})
	a.typeText("abc")
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	if m = a.m(); m.ms != nil || m.in.Value() != "nháp dở" || a.rec.count() != 0 {
		t.Fatalf("draft=%q ms=%v requests=%d", m.in.Value(), m.ms, a.rec.count())
	}
}
