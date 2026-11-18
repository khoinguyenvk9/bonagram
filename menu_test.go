package main

import (
	"math"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

// rightClick opens the menu of message id, at the first row of that message.
func rightClick(t *testing.T, a *actionModel, id int) {
	t.Helper()
	m := a.m()
	for i, l := range m.lines() {
		if l.id == id {
			y := m.msgY() + m.lineAt[i] - m.vp.YOffset
			a.send(tea.MouseMsg{X: m.side + 10, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
			a.send(tea.MouseMsg{X: m.side + 10, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonRight})
			if a.m().menu == nil {
				t.Fatalf("no menu for message %d", id)
			}
			return
		}
	}
	t.Fatalf("message %d not loaded", id)
}

// clickItem left-clicks the menu item whose label starts with label.
func clickItem(t *testing.T, a *actionModel, label string) {
	t.Helper()
	mn := a.m().menu
	for i, it := range mn.items {
		if strings.HasPrefix(it.label, label) {
			y := mn.y + mn.head() + i
			a.run(a.send(tea.MouseMsg{X: mn.x + 2, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}))
			a.send(tea.MouseMsg{X: mn.x + 2, Y: y, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
			return
		}
	}
	t.Fatalf("no item %q in %v", label, labels(mn.items))
}

// clickReact left-clicks emoji n of the reaction row.
func clickReact(t *testing.T, a *actionModel, n int) {
	t.Helper()
	mn := a.m().menu
	if mn.reacts == nil {
		t.Fatal("no reaction row")
	}
	a.run(a.send(tea.MouseMsg{X: mn.x + menuPad + n*reactSlot + 1, Y: mn.y + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}))
}

func labels(it []menuItem) []string {
	var out []string
	for _, i := range it {
		out = append(out, i.label)
	}
	return out
}

func TestRightClickMenu(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})

	rightClick(t, a, 6) // somebody else's message with a link
	got := strings.Join(labels(a.m().menu.items), "|")
	if !strings.Contains(got, "Mở link") || strings.Contains(got, "Sửa") || strings.Contains(got, "Xoá") {
		t.Fatalf("others' messages offer links but no edit/delete: %s", got)
	}
	if v := ansi.Strip(a.mm.View()); !strings.Contains(v, "Trả lời") || !strings.Contains(v, "Chuyển tiếp") {
		t.Fatal("the menu is drawn")
	}
	frameOK(t, *a.m())
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	if a.m().menu != nil {
		t.Fatal("Esc closes the menu")
	}

	rightClick(t, a, 6)
	clickReact(t, a, 3) // 🔥, one click from the top row
	if a.m().menu != nil {
		t.Fatal("reacting closes the menu")
	}
	if req, ok := a.rec.last(t).(*tg.MessagesSendReactionRequest); !ok || req.MsgID != 6 || req.Reaction[0].(*tg.ReactionEmoji).Emoticon != "🔥" {
		t.Fatalf("the reaction goes out: %+v", a.rec.last(t))
	}

	rightClick(t, a, 5) // our own message
	clickItem(t, a, "Xoá")
	n := a.rec.count()
	clickItem(t, a, "Huỷ")
	if a.rec.count() != n || len(a.m().lines()) != 2 {
		t.Fatal("Huỷ keeps the message")
	}
	rightClick(t, a, 5)
	clickItem(t, a, "Xoá")
	clickItem(t, a, "Xoá với mọi người")
	if req, ok := a.rec.last(t).(*tg.MessagesDeleteMessagesRequest); !ok || req.ID[0] != 5 {
		t.Fatalf("delete goes out: %+v", a.rec.last(t))
	}
	if a.m().menu != nil {
		t.Fatal("the menu closes after an action")
	}

	rightClick(t, a, 6)                    // keyboard works in the menu too
	a.send(tea.KeyMsg{Type: tea.KeyEnter}) // first item: reply
	if m := a.m(); m.menu != nil || m.reply == nil || m.reply.id != 6 || !m.in.Focused() {
		t.Fatal("Trả lời sets the reply and puts the cursor in the input")
	}
}

func TestClickPicksForwardTarget(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	rightClick(t, a, 6)
	clickItem(t, a, "Chuyển tiếp")
	if a.m().fwd == nil {
		t.Fatal("forwarding starts")
	}
	y := listY0 + itemRows // second chat in the list: Lan
	a.run(a.send(tea.MouseMsg{X: 3, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}))
	req, ok := a.rec.last(t).(*tg.MessagesForwardMessagesRequest)
	if !ok || req.ToPeer.(*tg.InputPeerUser).UserID != 3 {
		t.Fatalf("a click on a chat forwards there: %+v", a.rec.last(t))
	}
	if a.m().cur != "u1" {
		t.Fatal("forwarding does not change the open chat")
	}
}

func TestMenuAtEdgeKeepsFrame(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	y := m.msgY() + m.lineAt[1] - m.vp.YOffset
	a.send(tea.MouseMsg{X: m.w - 1, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	mn := a.m().menu
	if mn == nil || mn.x+mn.width() > m.w || mn.y+len(mn.items)+2 > m.h-1 {
		t.Fatalf("the menu must fit on screen: %+v", mn)
	}
	frameOK(t, *a.m())
	a.send(tea.MouseMsg{X: 1, Y: 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}) // elsewhere
	if a.m().menu != nil {
		t.Fatal("a click elsewhere closes the menu")
	}
}

func TestPinnedRowJumps(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.send(pinMsg{mk: "u1", list: []pinned{{6, "ok nhé"}, {99, "quy định cũ"}}})
	if v := ansi.Strip(a.mm.View()); !strings.Contains(v, "Ghim 1/2") || !strings.Contains(v, "ok nhé") {
		t.Fatal("the row names the newest pin and how many there are")
	}
	frameOK(t, *a.m())
	click := func() tea.Cmd {
		y := a.m().msgY() - 1
		return a.send(tea.MouseMsg{X: a.m().side + 5, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	}

	a.run(click()) // pin 6 is loaded: selected at once
	m := a.m()
	if !m.browse || m.lines()[m.bsel].id != 6 || m.pinAt["u1"] != 1 {
		t.Fatalf("a click selects the pinned message: browse=%v bsel=%d", m.browse, m.bsel)
	}
	if !strings.Contains(ansi.Strip(a.mm.View()), "Ghim 2/2  quy định cũ") {
		t.Fatal("then the row moves on to the older pin")
	}

	a.rec.msgs = []tg.MessageClass{
		&tg.Message{ID: 100, Message: "sau", PeerID: &tg.PeerUser{UserID: 1}},
		&tg.Message{ID: 99, Message: "quy định cũ", PeerID: &tg.PeerUser{UserID: 1}},
		&tg.Message{ID: 98, Message: "trước", PeerID: &tg.PeerUser{UserID: 1}},
	}
	a.run(click()) // pin 99 is not loaded: the messages around it are
	m = a.m()
	if !m.detached["u1"] || !m.browse || m.lines()[m.bsel].id != 99 || m.pinAt["u1"] != 0 {
		t.Fatalf("an old pin opens with the messages around it: detached=%v lines=%d", m.detached["u1"], len(m.lines()))
	}
	if _, ok := a.rec.last(t).(*tg.MessagesGetHistoryRequest); !ok {
		t.Fatalf("read around the pin: %T", a.rec.last(t))
	}
	frameOK(t, *m)
}

func TestPinFromMenu(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	rightClick(t, a, 6)
	clickItem(t, a, "Ghim")
	if got := strings.Join(labels(a.m().menu.items), "|"); got != "Ghim cho cả hai bên|Chỉ ghim cho tôi" {
		t.Fatalf("a private chat offers both sides or only me: %s", got)
	}
	clickItem(t, a, "Chỉ ghim cho tôi")
	if pin, ok := a.rec.last(t).(*tg.MessagesUpdatePinnedMessageRequest); !ok || pin.ID != 6 || !pin.PmOneside || pin.Unpin {
		t.Fatalf("pin for me only: %+v", a.rec.last(t))
	}
	batch := a.send(actMsg{kind: "pin", mk: "u1", key: "u1", id: 6})().(tea.BatchMsg)
	a.run(batch[1]) // batch[0] is the toast and its 6 s timer
	if _, ok := a.rec.last(t).(*tg.MessagesSearchRequest); !ok {
		t.Fatalf("after pinning the pinned row is read again: %T", a.rec.last(t))
	}

	a.send(pinMsg{mk: "u1", list: []pinned{{6, "ok nhé"}}})
	rightClick(t, a, 6)
	clickItem(t, a, "Bỏ ghim")
	if p, ok := a.rec.last(t).(*tg.MessagesUpdatePinnedMessageRequest); !ok || !p.Unpin {
		t.Fatalf("a pinned message offers unpin: %+v", a.rec.last(t))
	}
}

func TestGroupPinOptions(t *testing.T) {
	a := newActionModel(t)
	m := a.m()
	m.chats[0].key, m.chats[0].peer = "c5", &tg.InputPeerChannel{ChannelID: 5}
	m.cur = "c5"
	m.msgs["c5"] = m.msgs["u1"]
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	rightClick(t, a, 6)
	clickItem(t, a, "Ghim")
	if got := strings.Join(labels(a.m().menu.items), "|"); got != "Ghim và báo cho mọi người|Ghim im lặng" {
		t.Fatalf("a group offers notify or silent: %s", got)
	}
	clickItem(t, a, "Ghim im lặng")
	if p, ok := a.rec.last(t).(*tg.MessagesUpdatePinnedMessageRequest); !ok || !p.Silent || p.PmOneside {
		t.Fatalf("silent pin: %+v", a.rec.last(t))
	}
}

// chatRightClick opens the menu of the n-th chat in the list.
func chatRightClick(t *testing.T, a *actionModel, n int) {
	t.Helper()
	y := listY0 + n*itemRows
	a.send(tea.MouseMsg{X: 3, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonRight})
	if a.m().menu == nil {
		t.Fatalf("no menu for chat %d", n)
	}
}

func TestChatMenuMuteAndDelete(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})

	chatRightClick(t, a, 1) // Lan
	clickItem(t, a, "Tắt thông báo")
	req, ok := a.rec.last(t).(*tg.AccountUpdateNotifySettingsRequest)
	if !ok || req.Settings.MuteUntil != math.MaxInt32 || a.m().index("u3") < 0 || !a.m().chats[a.m().index("u3")].muted {
		t.Fatalf("mute for good: %+v", a.rec.last(t))
	}
	chatRightClick(t, a, 1)
	clickItem(t, a, "Bật thông báo")
	if req := a.rec.last(t).(*tg.AccountUpdateNotifySettingsRequest); req.Settings.MuteUntil != 0 || a.m().chats[1].muted {
		t.Fatal("unmute")
	}

	chatRightClick(t, a, 1)
	clickItem(t, a, "Xoá chat")
	n := a.rec.count()
	clickItem(t, a, "Huỷ")
	if a.rec.count() != n || a.m().index("u3") < 0 {
		t.Fatal("Huỷ keeps the chat")
	}
	chatRightClick(t, a, 1)
	clickItem(t, a, "Xoá chat")
	clickItem(t, a, "Chỉ xoá phía tôi")
	if req, ok := a.rec.last(t).(*tg.MessagesDeleteHistoryRequest); !ok || req.Revoke {
		t.Fatalf("delete on my side only: %+v", a.rec.last(t))
	}
	if a.m().index("u3") >= 0 || a.m().cur != "u1" {
		t.Fatal("the chat leaves the list, the open one stays open")
	}

	chatRightClick(t, a, 0) // Đạt, the open chat
	clickItem(t, a, "Xoá chat")
	clickItem(t, a, "Xoá cho cả hai bên")
	if req := a.rec.last(t).(*tg.MessagesDeleteHistoryRequest); !req.Revoke {
		t.Fatal("delete for both")
	}
	if m := a.m(); m.cur != "" || len(m.chats) != 0 || m.msgs["u1"] != nil || !m.searching {
		t.Fatalf("deleting the open chat closes it: cur=%q", m.cur)
	}
	frameOK(t, *a.m())
}

func TestLeaveGroupAndSavedMessages(t *testing.T) {
	a := newActionModel(t)
	m := a.m()
	m.chats[1] = chat{key: "c5", title: "Nhóm", peer: &tg.InputPeerChannel{ChannelID: 5, AccessHash: 55}}
	m.selfKey = "u1"
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})

	chatRightClick(t, a, 0) // Saved Messages
	if got := strings.Join(labels(a.m().menu.items), "|"); strings.Contains(got, "Xoá") {
		t.Fatalf("Saved Messages can not be deleted from here: %s", got)
	}
	a.send(tea.KeyMsg{Type: tea.KeyEsc})

	chatRightClick(t, a, 1)
	clickItem(t, a, "Rời nhóm")
	clickItem(t, a, "Rời và xoá")
	if req, ok := a.rec.last(t).(*tg.ChannelsLeaveChannelRequest); !ok || req.Channel.(*tg.InputChannel).ChannelID != 5 {
		t.Fatalf("leave the channel: %T", a.rec.last(t))
	}
	if a.m().index("c5") >= 0 {
		t.Fatal("a left group leaves the list")
	}
}

func TestPinChatFromMenu(t *testing.T) {
	a := newActionModel(t)
	m := a.m()
	m.chats[0].date, m.chats[1].date = 200, 100 // Đạt is newer than Lan
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})

	chatRightClick(t, a, 1) // Lan
	clickItem(t, a, "Ghim lên đầu")
	if req, ok := a.rec.last(t).(*tg.MessagesToggleDialogPinRequest); !ok || !req.Pinned {
		t.Fatalf("pin the chat: %+v", a.rec.last(t))
	}
	if m := a.m(); m.chats[0].key != "u3" || !m.chats[0].pinned {
		t.Fatalf("a pinned chat goes to the top: %q", m.chats[0].key)
	}
	frameOK(t, *a.m())

	chatRightClick(t, a, 0)
	clickItem(t, a, "Bỏ ghim chat")
	if req := a.rec.last(t).(*tg.MessagesToggleDialogPinRequest); req.Pinned {
		t.Fatal("unpin")
	}
	if m := a.m(); m.chats[0].key != "u1" || m.chats[1].key != "u3" || m.chats[1].pinned {
		t.Fatal("an unpinned chat goes back by the time of its last message")
	}
}

func TestMenuReactionRowKeysAndIcons(t *testing.T) {
	a := newActionModel(t)
	m := a.m()
	m.cfg.icons = true
	m.msgs["u1"][1].reacts = []reaction{{"👍", 1, true}}
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	rightClick(t, a, 6)
	v := ansi.Strip(a.mm.View())
	if !strings.Contains(v, "👍") || !strings.Contains(v, icReply+"  Trả lời") {
		t.Fatal("the reaction row and the icons are drawn")
	}
	frameOK(t, *a.m())

	a.send(tea.KeyMsg{Type: tea.KeyUp}) // from the first item up to the reaction row
	if mn := a.m().menu; mn.sel != -1 || mn.rsel != 0 {
		t.Fatalf("up from the first item reaches the reactions: sel=%d", mn.sel)
	}
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter})) // 👍 is already mine: taken back
	if req, ok := a.rec.last(t).(*tg.MessagesSendReactionRequest); !ok || len(req.Reaction) != 0 {
		t.Fatalf("Enter on a reaction you gave takes it back: %+v", a.rec.last(t))
	}

	rightClick(t, a, 6)
	a.run(a.key("4")) // a digit reacts at once
	if req := a.rec.last(t).(*tg.MessagesSendReactionRequest); req.Reaction[0].(*tg.ReactionEmoji).Emoticon != "🔥" {
		t.Fatalf("4 = 🔥: %+v", req)
	}

	m = a.m()
	m.cfg.icons = false
	a.set(m)
	rightClick(t, a, 6)
	if strings.Contains(ansi.Strip(a.mm.View()), icReply) {
		t.Fatal("BONAGRAM_ICONS=off draws no icons")
	}
	frameOK(t, *a.m())
}

func TestSendButtonAndDayLabels(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	m.in.SetValue("gửi bằng nút")
	a.set(m)
	x0 := m.side + 1 + 3 + max(m.vp.Width-6, 4) - m.btnW()
	clip, _ := m.buttonWidths()
	a.run(a.send(tea.MouseMsg{X: x0 + m.buttonLead() + clip + 2, Y: m.h - 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}))
	if req, ok := a.rec.last(t).(*tg.MessagesSendMessageRequest); !ok || req.Message != "gửi bằng nút" {
		t.Fatalf("a click on the send button sends: %+v", a.rec.last(t))
	}
	if v := ansi.Strip(a.mm.View()); !strings.Contains(v, "Hôm nay") {
		t.Fatal("messages of today start with a day line")
	}

	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.Local)
	for want, d := range map[string]time.Time{
		"Hôm nay":      now.Add(-time.Hour),
		"Hôm qua":      now.AddDate(0, 0, -1),
		"Thứ 2, 16/11": now.AddDate(0, 0, -2),
		"01/11":        now.AddDate(0, 0, -17),
		"18/11/2025":   now.AddDate(-1, 0, 0),
	} {
		if got := dayLabel(d, now); got != want {
			t.Errorf("dayLabel(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestButtonsWithIconsSitAgainstTheBorder(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	m.cfg.icons = true
	m.in.SetValue("có icon")
	a.set(m)
	rows := strings.Split(ansi.Strip(a.mm.View()), "\n")
	last := rows[m.h-3]
	if !strings.Contains(last, "   │") { // send icon, the space Ghostty draws it over, its pad, the outer pad, the border
		t.Fatalf("the buttons sit against the right border: %q", last)
	}
	frameOK(t, *a.m())
	x0 := m.side + 1 + 3 + max(m.vp.Width-6, 4) - m.btnW()
	if cmd := a.send(tea.MouseMsg{X: x0 + m.buttonLead(), Y: m.h - 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}); cmd == nil || !a.m().picking {
		t.Fatal("the paperclip opens the picker")
	}
	m = a.m()
	m.picking = false
	a.set(m)
	clip, _ := m.buttonWidths()
	a.run(a.send(tea.MouseMsg{X: x0 + m.buttonLead() + clip + 2, Y: m.h - 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}))
	if req, ok := a.rec.last(t).(*tg.MessagesSendMessageRequest); !ok || req.Message != "có icon" {
		t.Fatalf("the send icon sends: %+v", a.rec.last(t))
	}
}
