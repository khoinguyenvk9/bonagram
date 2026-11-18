package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

// recorder is a Telegram client that answers every request with an empty success and remembers it.
type recorder struct {
	mu       sync.Mutex
	calls    []bin.Encoder
	reply    tg.UpdatesClass      // what sending a message answers with
	users    []tg.UserClass       // the members of a group
	msgs     []tg.MessageClass    // what looking up messages by id answers
	file     []byte               // what downloading a file answers
	fileErrs []error              // errors for the next downloads, one each
	found    *tg.ContactsFound    // what searching people answers
	topics   []tg.ForumTopicClass // what listing forum topics answers
}

func (r *recorder) Invoke(_ context.Context, in bin.Encoder, out bin.Decoder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, in)
	if _, ok := in.(*tg.UploadGetFileRequest); ok && len(r.fileErrs) > 0 {
		err := r.fileErrs[0]
		r.fileErrs = r.fileErrs[1:]
		return err
	}
	switch box := out.(type) {
	case *tg.UpdatesBox:
		box.Updates = r.reply
	case *tg.MessageMediaBox: // an uploaded photo or document, as the server stores it
		box.MessageMedia = &tg.MessageMediaPhoto{Photo: &tg.Photo{ID: 1}}
	case *tg.ChannelsChannelParticipantsBox:
		box.ChannelParticipants = &tg.ChannelsChannelParticipants{Users: r.users}
	case *tg.MessagesChatFull:
		box.Users = r.users
	case *tg.MessagesMessagesBox:
		box.Messages = &tg.MessagesMessages{Messages: r.msgs, Users: r.users}
	case *tg.UploadFileBox:
		box.File = &tg.UploadFile{Type: &tg.StorageFileUnknown{}, Bytes: r.file}
	case *tg.BoolBox:
		box.Bool = &tg.BoolTrue{} // file parts are accepted
	case *tg.MessagesForumTopics:
		box.Topics = r.topics
	case *tg.ContactsFound:
		if r.found != nil {
			*box = *r.found
		}
	}
	return nil
}

func (r *recorder) last(t *testing.T) bin.Encoder {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		t.Fatal("no request was sent")
	}
	return r.calls[len(r.calls)-1]
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// actionModel is a model with two chats, the first one open with an own message (id 5) and one from Đạt (id 6).
type actionModel struct {
	mm  tea.Model
	rec *recorder
}

func (a *actionModel) send(msg tea.Msg) tea.Cmd {
	var c tea.Cmd
	a.mm, c = a.mm.Update(msg)
	return c
}
func (a *actionModel) key(s string) tea.Cmd {
	return a.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
}
func (a *actionModel) m() *model {
	m := a.mm.(model) // a copy: tests read it, or change it and put it back with a.set
	return &m
}
func (a *actionModel) set(m *model) { a.mm = *m }

// run executes a command and feeds its result back into the model.
func (a *actionModel) run(c tea.Cmd) {
	if c == nil {
		return
	}
	switch msg := c().(type) {
	case nil:
	case tea.BatchMsg: // what tea.Batch returns: run each command
		for _, sub := range msg {
			a.run(sub)
		}
	default:
		a.send(msg)
	}
}

// runAll also runs the commands that the results of the commands ask for (a pause, then a request, then its answer).
func (a *actionModel) runAll(c tea.Cmd) {
	if c == nil {
		return
	}
	switch msg := c().(type) {
	case nil:
	case tea.BatchMsg:
		for _, sub := range msg {
			a.runAll(sub)
		}
	default:
		a.runAll(a.send(msg))
	}
}

func newActionModel(t *testing.T) *actionModel {
	t.Helper()
	rec := &recorder{}
	m := newTestModel(t)
	m.api = tg.NewClient(rec)
	a := &actionModel{mm: m, rec: rec}
	a.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	a.send(chatsMsg{chats: []chat{
		{key: "u1", title: "Đạt", peer: &tg.InputPeerUser{UserID: 1}},
		{key: "u3", title: "Lan", peer: &tg.InputPeerUser{UserID: 3}},
	}})
	a.send(tea.KeyMsg{Type: tea.KeyCtrlN}) // opens u1
	own := &tg.Message{ID: 5, Message: "xin chào", Out: true}
	a.send(histMsg{mk: "u1", key: "u1", lines: []line{
		{id: 5, from: "Bạn", text: "xin chào", out: true, at: time.Now(), msg: own},
		{id: 6, from: "Đạt", text: "ok nhé https://example.com", at: time.Now(), mentioned: true,
			msg: &tg.Message{ID: 6, Message: "ok nhé https://example.com", Entities: []tg.MessageEntityClass{&tg.MessageEntityURL{Offset: 7, Length: 19}}}},
	}})
	a.send(tea.KeyMsg{Type: tea.KeyCtrlB}) // browse, last message highlighted
	return a
}

func (a *actionModel) selectMsg(t *testing.T, id int) {
	t.Helper()
	for i := 0; i < 5 && a.m().lines()[a.m().bsel].id != id; i++ {
		k := tea.KeyUp
		if a.m().lines()[a.m().bsel].id < id {
			k = tea.KeyDown
		}
		a.send(tea.KeyMsg{Type: k})
	}
	if a.m().lines()[a.m().bsel].id != id {
		t.Fatalf("could not select message %d", id)
	}
}

func TestEditOwnMessage(t *testing.T) {
	a := newActionModel(t)
	a.selectMsg(t, 6)
	a.key("e") // not ours
	if a.m().edit != nil {
		t.Fatal("must not edit somebody else's message")
	}
	a.selectMsg(t, 5)
	a.key("e")
	m := a.m()
	if m.edit == nil || m.in.Value() != "xin chào" || !strings.Contains(m.barText(), "Sửa tin") || m.browse {
		t.Fatalf("e should load the text into the input: edit=%v in=%q", m.edit, m.in.Value())
	}
	m.in.SetValue("xin chào các bạn")
	a.set(m)
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	req, ok := a.rec.last(t).(*tg.MessagesEditMessageRequest)
	if !ok || req.ID != 5 || req.Message != "xin chào các bạn" {
		t.Fatalf("wrong request: %+v", a.rec.last(t))
	}
	m = a.m()
	if m.edit != nil || m.in.Value() != "" || m.lines()[0].text != "xin chào các bạn" {
		t.Fatalf("after saving: edit=%v in=%q text=%q", m.edit, m.in.Value(), m.lines()[0].text)
	}
	if n := len(m.lines()); n != 2 {
		t.Fatalf("editing must not add a message, have %d", n)
	}
}

func TestEditEscapeKeepsDraft(t *testing.T) {
	a := newActionModel(t)
	m := a.m()
	m.in.SetValue("nháp dở")
	a.set(m)
	a.selectMsg(t, 5)
	a.key("e")
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	if m = a.m(); m.edit != nil || m.in.Value() != "nháp dở" || m.barText() != "" {
		t.Fatalf("esc should drop the edit and bring the draft back: in=%q bar=%q", m.in.Value(), m.barText())
	}
	if a.rec.count() != 0 {
		t.Fatal("cancelling must not send anything")
	}
}

func TestDeleteNeedsConfirmation(t *testing.T) {
	a := newActionModel(t)
	a.selectMsg(t, 5)
	a.key("d")
	if a.m().delID != 5 || !strings.Contains(a.m().barText(), "Xoá") {
		t.Fatal("d should ask first")
	}
	a.run(a.key("x")) // any other key cancels
	if a.m().delID != 0 || a.rec.count() != 0 || len(a.m().lines()) != 2 {
		t.Fatal("a key other than y must keep the message")
	}
	a.key("d")
	a.run(a.key("y"))
	req, ok := a.rec.last(t).(*tg.MessagesDeleteMessagesRequest)
	if !ok || len(req.ID) != 1 || req.ID[0] != 5 || !req.Revoke {
		t.Fatalf("wrong request: %+v", a.rec.last(t))
	}
	if ls := a.m().lines(); len(ls) != 1 || ls[0].id != 6 || a.m().bsel != 0 {
		t.Fatalf("the message should be gone and the selection still valid: %+v bsel=%d", ls, a.m().bsel)
	}
	a.selectMsg(t, 6)
	a.key("d")
	if a.m().delID != 0 {
		t.Fatal("somebody else's message can not be deleted")
	}
}

func TestReactionToggle(t *testing.T) {
	a := newActionModel(t)
	a.selectMsg(t, 6)
	a.key("r")
	a.run(a.key("4")) // 🔥
	req, ok := a.rec.last(t).(*tg.MessagesSendReactionRequest)
	if !ok || req.MsgID != 6 || len(req.Reaction) != 1 || req.Reaction[0].(*tg.ReactionEmoji).Emoticon != "🔥" {
		t.Fatalf("wrong request: %+v", a.rec.last(t))
	}
	m := a.m()
	m.msgs["u1"][1].reacts = []reaction{{"🔥", 2, true}}
	a.set(m)
	a.key("r")
	a.run(a.key("4")) // the same again takes it back
	if req = a.rec.last(t).(*tg.MessagesSendReactionRequest); len(req.Reaction) != 0 {
		t.Fatalf("should remove the reaction: %+v", req)
	}
	n := a.rec.count()
	a.key("r")
	a.run(a.key("9")) // not an offered number
	if a.rec.count() != n || a.m().react != nil || a.m().barText() != "" {
		t.Fatal("an unknown key must just close the question")
	}
}

func TestForwardToChosenChat(t *testing.T) {
	a := newActionModel(t)
	a.selectMsg(t, 6)
	a.key("f")
	m := a.m()
	if !m.searching || m.fwd == nil || !strings.Contains(m.barText(), "Chuyển tiếp") {
		t.Fatal("f should start choosing a chat")
	}
	a.send(tea.KeyMsg{Type: tea.KeyDown}) // second chat in the list = Lan
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	req, ok := a.rec.last(t).(*tg.MessagesForwardMessagesRequest)
	if !ok || len(req.ID) != 1 || req.ID[0] != 6 || req.ToPeer.(*tg.InputPeerUser).UserID != 3 || req.FromPeer.(*tg.InputPeerUser).UserID != 1 {
		t.Fatalf("wrong request: %+v", a.rec.last(t))
	}
	if m = a.m(); m.cur != "u1" || m.fwd != nil || m.barText() != "" {
		t.Fatalf("forwarding must not change chat: cur=%q", m.cur)
	}

	a.send(tea.KeyMsg{Type: tea.KeyCtrlB})
	a.key("f")
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	if m = a.m(); m.fwd != nil || m.barText() != "" {
		t.Fatal("esc should cancel forwarding")
	}
}

func TestDraftsFollowTheChat(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browse
	for _, r := range "dở dang" {
		a.key(string(r))
	}
	a.send(tea.KeyMsg{Type: tea.KeyCtrlN}) // -> Lan
	if m := a.m(); m.cur != "u3" || m.in.Value() != "" {
		t.Fatalf("a new chat starts with an empty input: cur=%q in=%q", m.cur, m.in.Value())
	}
	for _, r := range "khác" {
		a.key(string(r))
	}
	a.send(tea.KeyMsg{Type: tea.KeyCtrlP}) // back to Đạt
	if m := a.m(); m.cur != "u1" || m.in.Value() != "dở dang" {
		t.Fatalf("draft of Đạt should be back: in=%q", m.in.Value())
	}
	a.send(tea.KeyMsg{Type: tea.KeyCtrlN})
	if m := a.m(); m.in.Value() != "khác" {
		t.Fatalf("draft of Lan should be back: in=%q", m.in.Value())
	}
}

func TestOpenLinkAndMentionJump(t *testing.T) {
	a := newActionModel(t)
	a.selectMsg(t, 5)
	if c := a.key("o"); c == nil { // a toast: nothing to open
		t.Fatal("a message without a link should say so")
	}
	a.selectMsg(t, 6)
	a.key("m") // nothing mentioned above message 6
	if a.m().bsel != 1 {
		t.Fatal("no @mention above: stay")
	}
}

func TestLinksOf(t *testing.T) {
	msg := "😀 xem example.com/a và https://go.dev rồi javascript:alert(1)"
	u := func(s string) int { return u16(s) }
	e := []tg.MessageEntityClass{
		&tg.MessageEntityURL{Offset: u("😀 xem "), Length: u("example.com/a")},
		&tg.MessageEntityURL{Offset: u("😀 xem example.com/a và "), Length: u("https://go.dev")},
		&tg.MessageEntityURL{Offset: u("😀 xem example.com/a và https://go.dev rồi "), Length: u("javascript:alert(1)")},
		&tg.MessageEntityTextURL{Offset: 0, Length: 2, URL: "tg://resolve?domain=x"},
		&tg.MessageEntityTextURL{Offset: 0, Length: 2, URL: "http://a.b/c"},
		&tg.MessageEntityURL{Offset: 9999, Length: 5}, // outside the text
	}
	got := linksOf(&tg.Message{Message: msg, Entities: e})
	want := []string{"https://example.com/a", "https://go.dev", "http://a.b/c"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("links = %v, want %v", got, want)
	}
	if linksOf(nil) != nil {
		t.Fatal("nil message")
	}
}

func TestShellPath(t *testing.T) {
	for in, want := range map[string]string{
		`/tmp/a b.png`:       `/tmp/a b.png`,
		`/tmp/a\ b\ (1).png`: `/tmp/a b (1).png`,
		`'/tmp/it is.txt'`:   `/tmp/it is.txt`,
		`"/tmp/x y.txt"`:     `/tmp/x y.txt`,
		"  /tmp/plain.txt  ": `/tmp/plain.txt`,
	} {
		if runtime.GOOS == "windows" && strings.Contains(in, `\`) {
			continue // there a backslash is a folder separator, not an escape
		}
		if got := shellPath(in); got != want {
			t.Errorf("shellPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSendFileRejectsBadPaths(t *testing.T) {
	rec := &recorder{}
	api := tg.NewClient(rec)
	dir := t.TempDir()
	if err := sendFiles(context.Background(), api, &tg.InputPeerUser{UserID: 1}, []string{dir}, "", 0); err == nil {
		t.Error("a directory must be refused")
	}
	if err := sendFiles(context.Background(), api, &tg.InputPeerUser{UserID: 1}, []string{dir + "/missing"}, "", 0); err == nil {
		t.Error("a missing file must be refused")
	}
	if rec.count() != 0 {
		t.Fatal("nothing should reach the server")
	}
}

func TestPinnedRowKeepsFrame(t *testing.T) {
	a := newActionModel(t)
	before := a.m().msgY()
	a.send(pinMsg{mk: "u1", list: []pinned{{6, "Quy định: không spam 😀 và không quảng cáo, đọc kỹ trước khi đăng bài vào nhóm này nhé"}}})
	m := a.m()
	if m.msgY() != before+1 {
		t.Fatalf("the pinned row should push the messages down by one: %d -> %d", before, m.msgY())
	}
	lines := strings.Split(strings.TrimSuffix(m.View(), "\n"), "\n")
	if len(lines) != m.h {
		t.Fatalf("frame has %d lines, want %d", len(lines), m.h)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.w {
			t.Fatalf("line %d is %d cells wide, want %d", i, w, m.w)
		}
	}
	if !strings.Contains(m.View(), "Ghim") {
		t.Fatal("the pinned message should be shown")
	}
	a.send(pinMsg{mk: "u1"}) // unpinned
	if a.m().msgY() != before {
		t.Fatal("no pinned message: the row goes away")
	}
}

func TestPromptKeepsFrame(t *testing.T) {
	a := newActionModel(t)
	a.selectMsg(t, 5)
	a.key("r")
	m := a.m()
	lines := strings.Split(strings.TrimSuffix(m.View(), "\n"), "\n")
	if len(lines) != m.h {
		t.Fatalf("frame has %d lines, want %d", len(lines), m.h)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.w {
			t.Fatalf("line %d is %d cells wide, want %d", i, w, m.w)
		}
	}
	if !strings.Contains(m.View(), "1👍") {
		t.Fatal("the reaction choices should be shown")
	}
}

func TestJustSentMessageCanBeEdited(t *testing.T) {
	a := newActionModel(t)
	a.rec.reply = &tg.UpdateShortSentMessage{ID: 77}
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browse
	for _, r := range "vừa gõ" {
		a.key(string(r))
	}
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	ls := a.m().lines()
	if last := ls[len(ls)-1]; last.text != "vừa gõ" || last.id != 77 {
		t.Fatalf("the line should be numbered by the send reply: %+v", last)
	}
	a.send(tea.KeyMsg{Type: tea.KeyCtrlB})
	a.key("e")
	if m := a.m(); m.edit == nil || m.in.Value() != "vừa gõ" {
		t.Fatal("a message just sent must be editable at once")
	}
	// the server's own copy arrives later: it must not show twice
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.send(newMsg{key: "u1", line: line{id: 77, out: true, text: "vừa gõ", at: time.Now(), msg: &tg.Message{ID: 77, Message: "vừa gõ"}}})
	n := 0
	for _, l := range a.m().lines() {
		if l.id == 77 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("message 77 shown %d times", n)
	}
}

// A bot streams its answer by editing one message again and again: every edit must show up.
func TestStreamedEditsShowUp(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.send(newMsg{key: "u1", line: line{id: 9, from: "Bot", text: "Xin", at: time.Now(), msg: &tg.Message{ID: 9, Message: "Xin"}}})
	var last tea.Cmd
	for _, s := range []string{"Xin chào", "Xin chào, đây là", "Xin chào, đây là câu trả lời đầy đủ"} {
		last = a.send(editMsg{key: "u1", line: line{id: 9, from: "?", text: s, at: time.Now(), msg: &tg.Message{ID: 9, Message: s}}})
		if last == nil {
			t.Fatal("handling an edit must re-arm wait(), or live updates stop")
		}
	}
	m := a.m()
	var got line
	for _, l := range m.lines() {
		if l.id == 9 {
			got = l
		}
	}
	if got.text != "Xin chào, đây là câu trả lời đầy đủ" || got.from != "Bot" {
		t.Fatalf("the edited text should replace the old one and keep the author: %+v", got)
	}
	if m.chats[m.index("u1")].last != "Xin chào, đây là câu trả lời đầy đủ" {
		t.Fatalf("the preview should follow the newest message: %q", m.chats[m.index("u1")].last)
	}
	a.send(renderTick{})
	if !strings.Contains(a.m().View(), "câu trả lời đầy đủ") {
		t.Fatal("the screen should show the full text")
	}
	a.send(editMsg{key: "u1", line: line{id: 12345, text: "không có trong bộ nhớ"}}) // unknown message: ignored
}

func TestEventText(t *testing.T) {
	e := peer.NewEntities(map[int64]*tg.User{
		1: {ID: 1, FirstName: "Lan"}, 2: {ID: 2, FirstName: "Bình", LastName: "Trần"},
	}, nil, nil)
	lan := &tg.PeerUser{UserID: 1}
	for _, c := range []struct {
		a    tg.MessageActionClass
		want string
	}{
		{&tg.MessageActionChatAddUser{Users: []int64{2}}, "đã thêm Bình Trần vào nhóm"},
		{&tg.MessageActionChatAddUser{Users: []int64{1}}, "đã tham gia nhóm"},
		{&tg.MessageActionChatDeleteUser{UserID: 1}, "đã rời nhóm"},
		{&tg.MessageActionChatDeleteUser{UserID: 2}, "đã xoá Bình Trần khỏi nhóm"},
		{&tg.MessageActionChatEditTitle{Title: "Dev \x1b[31mGo"}, "đã đổi tên thành “Dev [31mGo”"},
		{&tg.MessageActionPinMessage{}, "đã ghim một tin"},
		{&tg.MessageActionTopicCreate{Title: "Bug"}, "đã tạo topic “Bug”"},
		{&tg.MessageActionSetMessagesTTL{Period: 86400}, "đã đặt tin tự xoá sau 1 ngày"},
		{&tg.MessageActionSetMessagesTTL{}, "đã tắt tin tự xoá"},
		{&tg.MessageActionChatJoinedByLink{}, "đã tham gia bằng link mời"},
		{&tg.MessageActionCustomAction{Message: "Đã bật chế độ"}, "Đã bật chế độ"},
		{&tg.MessageActionBotAllowed{}, "sự kiện: BotAllowed"},
	} {
		if got := eventText(e, lan, c.a); got != c.want {
			t.Errorf("%T: %q, want %q", c.a, got, c.want)
		}
	}
	call := &tg.MessageActionPhoneCall{Video: true}
	call.SetDuration(125)
	if got := eventText(e, lan, call); got != "cuộc gọi video · 2:05" {
		t.Errorf("call: %q", got)
	}
	if got := eventText(e, lan, &tg.MessageActionPhoneCall{Reason: &tg.PhoneCallDiscardReasonMissed{}}); got != "cuộc gọi thoại nhỡ" {
		t.Errorf("missed call: %q", got)
	}
	l, ok := toLine(&tg.MessageService{ID: 3, PeerID: &tg.PeerChat{ChatID: 1}, FromID: lan, Action: &tg.MessageActionPinMessage{}}, e)
	if !ok || !l.event || l.text != "[đã ghim một tin]" || l.from != "Lan" {
		t.Fatalf("event line: %+v", l)
	}
}

func TestPrivateSubtitleIsJustTheUsername(t *testing.T) {
	if got := chatSubtitle(&chat{key: "u1", username: "lan_vn"}); got != "@lan_vn" {
		t.Errorf("with username: %q", got)
	}
	if got := chatSubtitle(&chat{key: "u1"}); got != "" {
		t.Errorf("without username there is nothing to add: %q", got)
	}
	if got := chatSubtitle(&chat{key: "u1", muted: true}); got != "đã tắt tiếng" {
		t.Errorf("muted: %q", got)
	}
}

func TestPinnedChatsStayOnTop(t *testing.T) {
	a := newActionModel(t)
	a.send(chatsMsg{chats: []chat{
		{key: "u1", title: "Ghim 1", pinned: true, peer: &tg.InputPeerUser{UserID: 1}},
		{key: "u2", title: "Ghim 2", pinned: true, peer: &tg.InputPeerUser{UserID: 2}},
		{key: "u3", title: "Thường 3", peer: &tg.InputPeerUser{UserID: 3}},
		{key: "u4", title: "Thường 4", peer: &tg.InputPeerUser{UserID: 4}},
	}})
	order := func() string {
		var k []string
		for _, c := range a.m().chats {
			k = append(k, c.key)
		}
		return strings.Join(k, " ")
	}
	msg := func(key string, id int) newMsg {
		return newMsg{key: key, title: key, peer: &tg.InputPeerUser{UserID: 9}, line: line{id: id, from: key, text: "hi", at: time.Now()}}
	}
	a.send(msg("u4", 50)) // an ordinary chat goes to the top of the ordinary ones, below the pinned
	if got := order(); got != "u1 u2 u4 u3" {
		t.Fatalf("after activity in u4: %s", got)
	}
	a.send(msg("u2", 51)) // a pinned chat stays where it is
	if got := order(); got != "u1 u2 u4 u3" {
		t.Fatalf("after activity in pinned u2: %s", got)
	}
	if c := a.m().chats[1]; c.key != "u2" || c.unread != 1 || c.last != "u2: hi" && c.last != "hi" {
		t.Fatalf("a pinned chat still gets its preview and badge: %+v", c)
	}
	a.send(msg("u5", 52)) // a chat not in the list yet: unpinned, below the pinned ones
	if got := order(); got != "u1 u2 u5 u4 u3" {
		t.Fatalf("after a new chat: %s", got)
	}
}

func TestReactionShowsAfterSending(t *testing.T) {
	a := newActionModel(t)
	rc := tg.ReactionCount{Reaction: &tg.ReactionEmoji{Emoticon: "🔥"}, Count: 1}
	rc.SetChosenOrder(0)
	a.rec.reply = &tg.Updates{Updates: []tg.UpdateClass{&tg.UpdateMessageReactions{
		Peer: &tg.PeerUser{UserID: 1}, MsgID: 6, Reactions: tg.MessageReactions{Results: []tg.ReactionCount{rc}},
	}}}
	a.selectMsg(t, 6)
	a.key("r")
	a.run(a.key("4"))
	ls := a.m().lines()
	if got := ls[len(ls)-1].reacts; len(got) != 1 || got[0] != (reaction{"🔥", 1, true}) {
		t.Fatalf("the reaction we sent must show up at once: %+v", got)
	}
	if !strings.Contains(a.m().View(), "🔥") {
		t.Fatal("the reaction is not drawn")
	}
}

func TestClickSelectsMessageForActions(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browse mode
	m := a.m()
	if m.browse {
		t.Fatal("setup: should not be browsing")
	}
	y := m.msgY() + m.lineAt[0] - m.vp.YOffset // the first message (own, id 5)
	a.send(tea.MouseMsg{X: m.side + 5, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	m = a.m()
	if !m.browse || m.lines()[m.bsel].id != 5 || m.reply != nil {
		t.Fatalf("a click should highlight the message without replying: browse=%v reply=%v", m.browse, m.reply)
	}
	a.key("e") // an action works right after the click, no Ctrl+B
	if a.m().edit == nil || a.m().edit.id != 5 {
		t.Fatal("e should edit the clicked message")
	}
}

func TestClickInputLeavesBrowse(t *testing.T) {
	a := newActionModel(t)
	a.selectMsg(t, 5)
	a.key("d") // a question is open too
	m := a.m()
	if !m.browse || m.delID == 0 {
		t.Fatal("setup: should be browsing with a question open")
	}
	a.send(tea.MouseMsg{X: m.side + 8, Y: m.h - 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}) // inside the input box
	m = a.m()
	if m.browse || !m.in.Focused() || m.delID != 0 || m.barText() != "" {
		t.Fatalf("a click on the input should return to typing: browse=%v focused=%v", m.browse, m.in.Focused())
	}
}

func TestParsePaths(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "my file.txt")
	os.WriteFile(spaced, []byte("x"), 0o600)
	if got := parsePaths(spaced); len(got) != 1 || got[0] != spaced {
		t.Errorf("an existing path with spaces is one file: %q", got)
	}
	if runtime.GOOS == "windows" {
		return // backslashes are folder separators there
	}
	for in, want := range map[string][]string{
		`/a/b.png /a/c.png`:           {"/a/b.png", "/a/c.png"},
		`'/a/it is.txt' "/a/x y.txt"`: {"/a/it is.txt", "/a/x y.txt"},
		`/a/p\ q.png   /a/r.png`:      {"/a/p q.png", "/a/r.png"},
		`  /only.txt  `:               {"/only.txt"},
		``:                            nil,
	} {
		got := parsePaths(in)
		if len(got) != len(want) {
			t.Errorf("parsePaths(%q) = %q, want %q", in, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("parsePaths(%q) = %q, want %q", in, got, want)
			}
		}
	}
}

func TestGroupsKeepKindsApart(t *testing.T) {
	var us []upload
	for i := 0; i < 12; i++ {
		us = append(us, upload{photo: true})
	}
	us = append(us, upload{}, upload{}, upload{photo: true})
	var sizes []int
	for _, g := range groups(us) {
		sizes = append(sizes, len(g))
	}
	if fmt.Sprint(sizes) != "[10 2 2 1]" {
		t.Fatalf("groups of %v, want [10 2 2 1]", sizes)
	}
}

func TestSendFilesWithCaption(t *testing.T) {
	dir := t.TempDir()
	var photos, docs []string
	for _, n := range []string{"a.png", "b.jpg"} {
		os.WriteFile(filepath.Join(dir, n), []byte("img"), 0o600)
		photos = append(photos, filepath.Join(dir, n))
	}
	os.WriteFile(filepath.Join(dir, "c.pdf"), []byte("doc"), 0o600)
	docs = append(docs, filepath.Join(dir, "c.pdf"))
	rec := &recorder{}
	api := tg.NewClient(rec)
	if err := sendFiles(context.Background(), api, &tg.InputPeerUser{UserID: 1}, docs, "xem giúp", 7); err != nil {
		t.Fatal(err)
	}
	req, ok := rec.last(t).(*tg.MessagesSendMediaRequest)
	if !ok || req.Message != "xem giúp" {
		t.Fatalf("one file: want a media message with the caption, got %+v", rec.last(t))
	}
	if r, ok := req.ReplyTo.(*tg.InputReplyToMessage); !ok || r.ReplyToMsgID != 7 {
		t.Fatalf("reply target lost: %+v", req.ReplyTo)
	}
	if err := sendFiles(context.Background(), api, &tg.InputPeerUser{UserID: 1}, photos, "hai ảnh", 0); err != nil {
		t.Fatal(err)
	}
	al, ok := rec.last(t).(*tg.MessagesSendMultiMediaRequest)
	if !ok || len(al.MultiMedia) != 2 || al.MultiMedia[0].Message != "hai ảnh" || al.MultiMedia[1].Message != "" {
		t.Fatalf("two photos should go as one album, caption on the first: %+v", rec.last(t))
	}
}

func TestAttachFlow(t *testing.T) {
	a := newActionModel(t)
	dir := t.TempDir()
	f1, f2 := filepath.Join(dir, "a b.png"), filepath.Join(dir, "c.txt")
	os.WriteFile(f1, []byte("x"), 0o600)
	os.WriteFile(f2, []byte("y"), 0o600)
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browse
	m := a.m()
	m.in.SetValue("/file '" + f1 + "' " + f2)
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyEnter})
	m = a.m()
	if len(m.attach) != 2 || m.in.Value() != "" || !strings.Contains(m.barText(), "Đính kèm (2)") {
		t.Fatalf("both files should be staged: %q bar=%q", m.attach, m.barText())
	}
	if a.rec.count() != 0 {
		t.Fatal("staging must not send anything")
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Tệp") || !strings.Contains(v, "Gửi") {
		t.Fatal("the file button is missing from the input box")
	}
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	if a.m().attach != nil || a.m().barText() != "" {
		t.Fatal("esc should drop the staged files")
	}
	m = a.m()
	m.in.SetValue("/file " + dir + "/missing.png")
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyEnter})
	if m = a.m(); m.attach != nil || m.err == "" {
		t.Fatal("a missing file must be refused with an error")
	}
}

func TestFileButtonClick(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	x0 := m.side + 1 + 3 + max(m.vp.Width-6, 4) - m.btnW()
	cmd := a.send(tea.MouseMsg{X: x0 + m.buttonLead() + 1, Y: m.h - 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd == nil || !a.m().picking { // the command (the dialog) is deliberately not run here
		t.Fatal("a click on [Tệp] should open the picker")
	}
	a2 := newActionModel(t)
	a2.send(tea.KeyMsg{Type: tea.KeyEsc})
	a2.send(tea.MouseMsg{X: x0 - 3, Y: m.h - 3, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if a2.m().picking {
		t.Fatal("a click left of the button is just focusing the input")
	}
}

func newGroupModel(t *testing.T, key string, p tg.InputPeerClass) *actionModel {
	t.Helper()
	rec := &recorder{users: []tg.UserClass{
		&tg.User{ID: 11, AccessHash: 111, FirstName: "Khôi", LastName: "Nguyễn", Username: "khoinv"},
		&tg.User{ID: 12, AccessHash: 222, FirstName: "Hà"},
		&tg.User{ID: 13, FirstName: "Me", Self: true},
	}}
	m := newTestModel(t)
	m.api = tg.NewClient(rec)
	a := &actionModel{mm: m, rec: rec}
	a.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	a.send(chatsMsg{chats: []chat{{key: key, title: "Nhóm", peer: p}}})
	a.send(tea.KeyMsg{Type: tea.KeyCtrlN})
	return a
}

func (a *actionModel) typeText(s string) {
	for _, r := range s {
		a.runAll(a.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})) // the pause, the request, the answer
	}
}

func TestMentionSuggestionsInSupergroup(t *testing.T) {
	a := newGroupModel(t, "c9", &tg.InputPeerChannel{ChannelID: 9, AccessHash: 1})
	a.typeText("chào @kh")
	m := a.m()
	if len(m.mc) != 2 || !strings.Contains(m.barText(), "Khôi Nguyễn (@khoinv)") || strings.Contains(m.barText(), "Me") {
		t.Fatalf("suggestions: %+v bar=%q", m.mc, m.barText())
	}
	req, ok := a.rec.last(t).(*tg.ChannelsGetParticipantsRequest)
	if !ok || req.Filter.(*tg.ChannelParticipantsSearch).Q != "kh" {
		t.Fatalf("the server should be asked to search \"kh\": %+v", a.rec.last(t))
	}
	a.send(tea.KeyMsg{Type: tea.KeyTab}) // the first one: has a username
	if got := a.m().in.Value(); got != "chào @khoinv " || len(a.m().mc) != 0 || a.m().barText() != "" {
		t.Fatalf("input after the pick: %q bar=%q", got, a.m().barText())
	}
	// somebody without a username goes in as an entity
	a.typeText("@")
	a.send(tea.KeyMsg{Type: tea.KeyDown})
	a.send(tea.KeyMsg{Type: tea.KeyEnter}) // picks Hà, does not send
	if got := a.m().in.Value(); got != "chào @khoinv @Hà " {
		t.Fatalf("input: %q", got)
	}
	a.typeText("ơi")
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	req2, ok := a.rec.last(t).(*tg.MessagesSendMessageRequest)
	if !ok || req2.Message != "chào @khoinv @Hà ơi" || len(req2.Entities) != 1 {
		t.Fatalf("send: %+v", a.rec.last(t))
	}
	e := req2.Entities[0].(*tg.InputMessageEntityMentionName)
	if e.Offset != 13 || e.Length != 3 || e.UserID.(*tg.InputUser).UserID != 12 {
		t.Fatalf("entity: %+v", e)
	}
}

func TestMentionInBasicGroupAsksOnce(t *testing.T) {
	a := newGroupModel(t, "g5", &tg.InputPeerChat{ChatID: 5})
	a.typeText("@ha")
	if len(a.m().mc) != 1 || a.m().mc[0].name != "Hà" {
		t.Fatalf("local filter: %+v", a.m().mc)
	}
	n := a.rec.count()
	a.typeText("x")
	if a.rec.count() != n || len(a.m().mc) != 0 {
		t.Fatal("a basic group's members must be asked for only once")
	}
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	if len(a.m().mc) != 0 || a.m().barText() != "" {
		t.Fatal("esc closes the suggestions")
	}
}

func TestMentionNotInPrivateChatOrEmail(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.typeText("@kh")
	if a.rec.count() != 0 || len(a.m().mc) != 0 {
		t.Fatal("no suggestions in a private chat")
	}
	g := newGroupModel(t, "c9", &tg.InputPeerChannel{ChannelID: 9, AccessHash: 1})
	g.typeText("a@kh")
	if g.rec.count() != 0 {
		t.Fatal("an e-mail address is not a mention")
	}
}
