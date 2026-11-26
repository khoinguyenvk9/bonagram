package main

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/bin"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// listRow is the first two rows of chat key in the drawn list, without colours.
func listRow(t *testing.T, m *model, title string) string {
	t.Helper()
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	for i, r := range rows {
		if i < listY0 || i+1 >= len(rows) {
			continue
		}
		left := ansi.Truncate(r, m.side, "")
		if strings.HasPrefix(strings.TrimLeft(left, " ▌"), title+" ") {
			return left + "\n" + ansi.Truncate(rows[i+1], m.side, "")
		}
	}
	t.Fatalf("chat %q not in the list", title)
	return ""
}

func TestListReadMarks(t *testing.T) {
	now := time.Now()
	row := func(c chat) string { return ansi.Strip(strings.Join(chatItem(c, false, 36, now), "\n")) }
	if r := row(chat{key: "u1", title: "Đạt", last: "Bạn: alo", date: int(now.Unix()), lastOut: true, lastID: 9, readOut: 8}); !strings.Contains(r, "✓ ") || strings.Contains(r, "✓✓") {
		t.Fatalf("sent, not read: %q", r)
	}
	if r := row(chat{key: "u1", title: "Đạt", last: "Bạn: alo", date: int(now.Unix()), lastOut: true, lastID: 9, readOut: 9}); !strings.Contains(r, "✓✓") {
		t.Fatalf("read: %q", r)
	}
	if r := row(chat{key: "u1", title: "Đạt", last: "alo", date: int(now.Unix()), lastID: 9, readOut: 9}); strings.Contains(r, "✓") {
		t.Fatalf("their message has no tick: %q", r)
	}
	if r := row(chat{key: "u1", title: "Đạt", last: "Bạn: alo", lastOut: true}); !strings.Contains(r, "✓") || strings.Contains(r, "✓✓") {
		t.Fatalf("not numbered yet: one tick: %q", r)
	}
	long := row(chat{key: "u1", title: strings.Repeat("Tên rất dài ", 6), date: int(now.Unix()), lastOut: true, lastID: 1, readOut: 1})
	for _, l := range strings.Split(long, "\n") {
		if ansi.StringWidth(l) != 36 {
			t.Fatalf("row width %d: %q", ansi.StringWidth(l), l)
		}
	}

	// the flow: send → ✓, numbered → still ✓, read on the other side → ✓✓; Saved Messages never has ticks
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	m.in.SetValue("chào")
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyEnter})
	if r := listRow(t, a.m(), "Đạt"); !strings.Contains(r, "✓") || strings.Contains(r, "✓✓") {
		t.Fatalf("just sent: %q", r)
	}
	a.send(sentMsg{mk: "u1", text: "chào", id: 40})
	a.send(readOutMsg{key: "u1", id: 40})
	if r := listRow(t, a.m(), "Đạt"); !strings.Contains(r, "✓✓") {
		t.Fatalf("read by Đạt: %q", r)
	}
	a.send(newMsg{key: "u1", line: line{id: 41, from: "Đạt", sender: "u1", text: "ok", at: time.Now()}})
	if r := listRow(t, a.m(), "Đạt"); strings.Contains(r, "✓") {
		t.Fatalf("their answer is the newest: no tick: %q", r)
	}
	m = a.m()
	m.selfKey = "u3"
	m.chats[m.index("u3")].lastOut, m.chats[m.index("u3")].lastID = true, 1
	a.set(m)
	if r := listRow(t, a.m(), "Lan"); strings.Contains(r, "✓") {
		t.Fatalf("Saved Messages: %q", r)
	}
	frameOK(t, *a.m())
}

func TestTypingShows(t *testing.T) {
	a := newActionModel(t)
	a.send(typingMsg{key: "u1", who: "u1", action: "đang soạn tin"})
	m := a.m()
	if r := listRow(t, m, "Đạt"); !strings.Contains(r, "đang soạn tin…") {
		t.Fatalf("list: %q", r)
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Đạt  ·  đang soạn tin…") {
		t.Fatal("the title row should say Đạt is typing")
	}
	frameOK(t, *m)
	// it ends by itself after 6 s, at once with a stop, and when their message arrives
	m.pruneTyping(time.Now().Add(7 * time.Second))
	if m.typingText("u1", -1, time.Now()) != "" {
		t.Fatal("typing must expire")
	}
	a.send(typingMsg{key: "u1", who: "u1", action: "đang ghi âm"})
	a.send(typingMsg{key: "u1", who: "u1", stop: true})
	if a.m().typingText("u1", -1, time.Now()) != "" {
		t.Fatal("a cancel action stops it")
	}
	a.send(typingMsg{key: "u1", who: "u1", action: "đang soạn tin"})
	a.send(newMsg{key: "u1", line: line{id: 50, from: "Đạt", sender: "u1", text: "đây", at: time.Now()}})
	if a.m().typingText("u1", -1, time.Now()) != "" {
		t.Fatal("their message ends the typing")
	}

	// groups name who types; names come from the update, else from what they wrote before
	g := &model{typing: map[string][]typer{}, msgs: map[string][]line{"g7": {{sender: "u5", from: "Minh Trần"}}}}
	g.onTyping(typingMsg{key: "g7", who: "u4", name: "Lan", action: "đang soạn tin"})
	if got := g.typingText("g7", -1, time.Now()); got != "Lan đang soạn tin…" {
		t.Fatalf("one: %q", got)
	}
	g.onTyping(typingMsg{key: "g7", who: "u5", action: "đang soạn tin"})
	if got := g.typingText("g7", -1, time.Now()); got != "Lan và Minh đang soạn tin…" {
		t.Fatalf("two: %q", got)
	}
	g.onTyping(typingMsg{key: "g7", who: "u6", action: "đang soạn tin"})
	if got := g.typingText("g7", -1, time.Now()); got != "3 người đang soạn tin…" {
		t.Fatalf("three: %q", got)
	}
	// forum topics: only the open topic's typing in the title row
	g.onTyping(typingMsg{key: "c8", who: "u4", name: "Lan", topic: 12, action: "đang soạn tin"})
	if g.typingText("c8", 30, time.Now()) != "" || g.typingText("c8", 12, time.Now()) == "" || g.typingText("c8", -1, time.Now()) == "" {
		t.Fatal("topic filter")
	}

	e := tg.Entities{Users: map[int64]*tg.User{4: {ID: 4, FirstName: "Lan", LastName: "Ng"}}}
	if ev, ok := typingFrom(e, "g7", &tg.PeerUser{UserID: 4}, 0, &tg.SendMessageUploadPhotoAction{}); !ok || ev.who != "u4" || ev.name != "Lan" || ev.action != "đang gửi ảnh" {
		t.Fatalf("typingFrom: %+v", ev)
	}
	if _, ok := typingFrom(e, "g7", &tg.PeerUser{UserID: 4}, 0, &tg.SendMessageGamePlayAction{}); ok {
		t.Fatal("actions not shown are dropped")
	}
	if ev, ok := typingFrom(tg.Entities{}, "g7", &tg.PeerUser{UserID: 9}, 0, &tg.SendMessageTypingAction{}); !ok || ev.name != "" {
		t.Fatal("a short update without users still counts")
	}
}

func TestOwnTypingIsAnnounced(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.typeText("chào")
	if n := len(a.rec.typed()); n != 1 {
		t.Fatalf("typing announced %d times, want once per 5 s", n)
	}
	if _, ok := a.rec.typed()[0].Action.(*tg.SendMessageTypingAction); !ok {
		t.Fatal("action")
	}
	m := a.m()
	m.typedAt = time.Now().Add(-6 * time.Second)
	a.set(m)
	a.typeText("x")
	if n := len(a.rec.typed()); n != 2 {
		t.Fatalf("after 5 s it is announced again: %d", n)
	}
	m = a.m()
	m.in.SetValue("")
	m.selfKey, m.typedKey = "u1", "" // Saved Messages: nobody to tell
	a.set(m)
	a.typeText("y")
	if n := len(a.rec.typed()); n != 2 {
		t.Fatalf("Saved Messages: %d", n)
	}
	m = a.m()
	m.in.SetValue("")
	a.set(m)
	a.typeText("/file") // a command is not a message
	if n := len(a.rec.typed()); n != 2 {
		t.Fatal("a command was announced as typing")
	}
}

func TestPresenceText(t *testing.T) {
	now := time.Date(2026, 11, 25, 15, 0, 0, 0, time.Local)
	at := func(d time.Duration) int { return int(now.Add(-d).Unix()) }
	for _, c := range []struct {
		p    presence
		want string
	}{
		{presence{until: int(now.Add(time.Minute).Unix())}, "đang hoạt động"},
		{presence{until: at(30 * time.Second)}, "vừa hoạt động"},
		{presence{seen: at(5 * time.Minute)}, "hoạt động 5 phút trước"},
		{presence{seen: at(3 * time.Hour)}, "hoạt động lúc 12:00"},
		{presence{seen: at(20 * time.Hour)}, "hoạt động hôm qua lúc 19:00"},
		{presence{seen: at(10 * 24 * time.Hour)}, "hoạt động ngày 15/11"},
		{presence{seen: at(400 * 24 * time.Hour)}, "hoạt động ngày 21/10/2025"},
		{presence{kind: "recently"}, "hoạt động gần đây"},
		{presence{kind: "week"}, "hoạt động trong tuần"},
		{presence{}, ""},
	} {
		if got := presenceText(c.p, now); got != c.want {
			t.Errorf("%+v: %q, want %q", c.p, got, c.want)
		}
	}
	if presenceOf(&tg.UserStatusOnline{Expires: 9}).until != 9 || presenceOf(&tg.UserStatusLastMonth{}).kind != "month" || presenceOf(nil) != (presence{}) {
		t.Fatal("presenceOf")
	}
	c := &chat{key: "u1", username: "dat", seen: presence{until: int(time.Now().Add(time.Minute).Unix())}}
	if got := chatSubtitle(c, time.Now()); got != "đang hoạt động · @dat" {
		t.Fatalf("subtitle: %q", got)
	}
	if got := chatSubtitle(&chat{key: "u2", bot: true}, time.Now()); got != "bot" {
		t.Fatalf("bot: %q", got)
	}
	a := newActionModel(t)
	a.send(statusMsg{key: "u1", p: presence{until: int(time.Now().Add(time.Minute).Unix())}})
	if !strings.Contains(ansi.Strip(a.m().View()), "đang hoạt động") {
		t.Fatal("a live status update shows in the title row")
	}
}

func TestFolders(t *testing.T) {
	filters := []tg.DialogFilterClass{
		&tg.DialogFilterDefault{},
		&tg.DialogFilter{ID: 2, Title: tg.TextWithEntities{Text: "Công việc"}, Groups: true,
			IncludePeers: []tg.InputPeerClass{&tg.InputPeerUser{UserID: 1}}, ExcludePeers: []tg.InputPeerClass{&tg.InputPeerChannel{ChannelID: 8}}},
		&tg.DialogFilter{ID: 3, Title: tg.TextWithEntities{Text: "Cá nhân"}, Contacts: true, NonContacts: true, ExcludeMuted: true,
			PinnedPeers: []tg.InputPeerClass{&tg.InputPeerSelf{}}},
		&tg.DialogFilterChatlist{ID: 4, Title: tg.TextWithEntities{Text: "Bạn bè"}, IncludePeers: []tg.InputPeerClass{&tg.InputPeerUser{UserID: 3}}},
	}
	fs := toFolders(filters, "u99")
	if len(fs) != 4 || !fs[0].all || fs[1].title != "Công việc" || !fs[2].include["u99"] {
		t.Fatalf("folders: %+v", fs)
	}
	if toFolders([]tg.DialogFilterClass{&tg.DialogFilterDefault{}}, "") != nil || toFolders(nil, "") != nil {
		t.Fatal("no folder made by the user: no tabs")
	}
	if got := toFolders(filters[1:2], ""); len(got) != 2 || !got[0].all {
		t.Fatal("'Tất cả' comes first when the server leaves it out")
	}
	chats := map[string]*chat{
		"u1": {key: "u1"}, "u3": {key: "u3", contact: true}, "u5": {key: "u5", contact: true, muted: true}, "u6": {key: "u6", bot: true},
		"g2": {key: "g2"}, "c7": {key: "c7"}, "c8": {key: "c8"}, "c9": {key: "c9", broadcast: true}, "u99": {key: "u99", muted: true},
	}
	want := map[int]string{1: "c7 g2 u1", 2: "u1 u3 u99", 3: "u3"}
	for i, w := range want {
		var got []string
		for _, k := range []string{"c7", "c8", "c9", "g2", "u1", "u3", "u5", "u6", "u99"} {
			if fs[i].has(chats[k]) {
				got = append(got, k)
			}
		}
		if strings.Join(got, " ") != w {
			t.Errorf("%s: %v, want %s", fs[i].title, got, w)
		}
	}

	// in the app: tabs over the list, a click or Ctrl+L switches, a search looks everywhere
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.send(foldersMsg{list: []folder{{title: "Tất cả", all: true}, {title: "Bạn bè", include: map[string]bool{"u3": true}, exclude: map[string]bool{}}}})
	m := a.m()
	tabs := ansi.Strip(strings.Split(m.View(), "\n")[listY0-1])
	if !strings.Contains(tabs, "Tất cả") || !strings.Contains(tabs, "Bạn bè") {
		t.Fatalf("tabs row: %q", tabs)
	}
	frameOK(t, *m)
	_, spans := m.folderTabs(m.side)
	a.send(tea.MouseMsg{X: spans[1].x0, Y: listY0 - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.send(tea.MouseMsg{X: spans[1].x0, Y: listY0 - 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	m = a.m()
	if m.folder != 1 || len(m.visible()) != 1 || m.chats[m.visible()[0]].key != "u3" {
		t.Fatalf("click on Bạn bè: folder=%d visible=%v", m.folder, m.visible())
	}
	m.search.SetValue("đạt")
	if v := m.visible(); len(v) != 1 || m.chats[v[0]].key != "u1" {
		t.Fatal("a search ignores the folder")
	}
	m.search.SetValue("")
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyCtrlL})
	if a.m().folder != 0 || len(a.m().visible()) != 2 {
		t.Fatal("Ctrl+L goes round to Tất cả")
	}
	if cmd := a.send(foldersChanged{}); cmd == nil {
		t.Fatal("a change elsewhere reads the folders again")
	}
}

// The tabs are kept between frames; a new unread message in a folder's chat still shows on its tab at once.
func TestFolderTabsFollowUnread(t *testing.T) {
	a := newActionModel(t)
	a.send(foldersMsg{list: []folder{{title: "Tất cả", all: true}, {title: "Bạn bè", include: map[string]bool{"u3": true}, exclude: map[string]bool{}}}})
	tabs := func() string { return ansi.Strip(strings.Split(a.m().View(), "\n")[listY0-1]) }
	if strings.Contains(tabs(), "Bạn bè 1") {
		t.Fatal("nothing unread yet")
	}
	a.send(newMsg{key: "u3", line: line{id: 70, from: "Lan", sender: "u3", text: "alo", at: time.Now()}})
	if !strings.Contains(tabs(), "Bạn bè 1") {
		t.Fatalf("the tab should count Lan's chat: %q", tabs())
	}
	a.send(foldersMsg{list: []folder{{title: "Tất cả", all: true}, {title: "Gia đình", include: map[string]bool{}, exclude: map[string]bool{}}}})
	if !strings.Contains(tabs(), "Gia đình") || strings.Contains(tabs(), "Bạn bè") {
		t.Fatalf("folders read again: %q", tabs())
	}
}

// More folders than fit: arrows show what is hidden, the wheel and the arrows scroll the row, Ctrl+L and a
// wider list keep the folder shown in view.
func TestFolderTabsScroll(t *testing.T) {
	a := newActionModel(t)
	list := []folder{{title: "Tất cả", all: true}}
	for _, n := range []string{"Cá nhân", "Công việc", "Bạn bè", "Gia đình", "Kênh", "Bot"} {
		list = append(list, folder{title: n, include: map[string]bool{}, exclude: map[string]bool{}})
	}
	a.send(foldersMsg{list: list})
	tabs := func() string { // the tab row of the list, without colours
		return ansi.Strip(ansi.Truncate(strings.Split(a.m().View(), "\n")[listY0-1], a.m().side, ""))
	}
	wheel := func(b tea.MouseButton) {
		a.send(tea.MouseMsg{X: 5, Y: listY0 - 1, Action: tea.MouseActionPress, Button: b})
	}
	if r := tabs(); !strings.HasSuffix(strings.TrimRight(r, " "), "›") || strings.HasPrefix(r, "‹") || !strings.Contains(r, "Tất cả") {
		t.Fatalf("start: more on the right only: %q", r)
	}
	frameOK(t, *a.m())

	top := a.m().top
	wheel(tea.MouseButtonWheelDown)
	if r := tabs(); !strings.HasPrefix(r, "‹") || strings.Contains(r, "Tất cả") || a.m().top != top {
		t.Fatalf("the wheel scrolls the tabs, not the list: %q", r)
	}
	for i := 0; i < 10; i++ {
		wheel(tea.MouseButtonWheelDown)
	}
	if r := tabs(); !strings.Contains(r, "Bot") || strings.HasSuffix(strings.TrimRight(r, " "), "›") {
		t.Fatalf("scrolled to the end, and no further: %q", r)
	}
	end := a.m().tabOff
	wheel(tea.MouseButtonWheelDown)
	if a.m().tabOff != end {
		t.Fatal("scrolled past the last tab")
	}
	frameOK(t, *a.m())

	// a click on ‹ scrolls back; the arrow does not change the folder
	a.send(tea.MouseMsg{X: 0, Y: listY0 - 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.send(tea.MouseMsg{X: 0, Y: listY0 - 1, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if a.m().tabOff != end-1 || a.m().folder != 0 {
		t.Fatalf("click on ‹: off=%d folder=%d", a.m().tabOff, a.m().folder)
	}
	// Ctrl+L round to the first folder brings it back into view, and on to the last one
	for i := 0; i < len(list); i++ {
		a.send(tea.KeyMsg{Type: tea.KeyCtrlL})
		if m := a.m(); !m.tabShown(m.folder) {
			t.Fatalf("folder %d (%s) is not in view: %q", m.folder, m.folders[m.folder].title, tabs())
		}
	}
	// scrolled to the end, then a wide list: they all show, no arrows
	for i := 0; i < 10; i++ {
		wheel(tea.MouseButtonWheelDown)
	}
	m := a.m()
	m.sideWant = 70
	a.set(m)
	a.send(tea.WindowSizeMsg{Width: 200, Height: 30})
	if r := tabs(); strings.Contains(r, "‹") || strings.Contains(r, "›") || !strings.Contains(r, "Tất cả") || !strings.Contains(r, "Bot") {
		t.Fatalf("wide: %q", r)
	}
	frameOK(t, *a.m())
}

func peerKeys(ps []tg.InputPeerClass) string {
	var ks []string
	for _, p := range ps {
		ks = append(ks, inputKey(p, "u99"))
	}
	return strings.Join(ks, " ")
}

func TestWithChat(t *testing.T) {
	u1, u3, g2 := &tg.InputPeerUser{UserID: 1}, &tg.InputPeerUser{UserID: 3}, &tg.InputPeerChat{ChatID: 2}
	raw := &tg.DialogFilter{ID: 5, Title: tg.TextWithEntities{Text: "Việc"}, Groups: true,
		IncludePeers: []tg.InputPeerClass{u1}, PinnedPeers: []tg.InputPeerClass{u3}, ExcludePeers: []tg.InputPeerClass{g2}}
	f := toFolders([]tg.DialogFilterClass{raw}, "u99")[1]
	chat2 := &chat{key: "g2", peer: g2}
	chat1 := &chat{key: "u1", peer: u1}
	chat3 := &chat{key: "u3", peer: u3}
	chat4 := &chat{key: "u4", peer: &tg.InputPeerUser{UserID: 4}}

	nf, err := withChat(&f, chat2, true, "u99") // excluded group: un-excluded and named
	if err != nil || peerKeys(nf.ExcludePeers) != "" || peerKeys(nf.IncludePeers) != "u1 g2" || peerKeys(nf.PinnedPeers) != "u3" {
		t.Fatalf("add excluded: %v inc=%q exc=%q pin=%q", err, peerKeys(nf.IncludePeers), peerKeys(nf.ExcludePeers), peerKeys(nf.PinnedPeers))
	}
	if nf, _ = withChat(&f, chat3, true, "u99"); peerKeys(nf.IncludePeers) != "u1" || peerKeys(nf.PinnedPeers) != "u3" {
		t.Fatal("a chat pinned in the folder stays pinned when added again")
	}
	if nf, _ = withChat(&f, chat4, true, "u99"); peerKeys(nf.IncludePeers) != "u1 u4" {
		t.Fatalf("add: %q", peerKeys(nf.IncludePeers))
	}
	if nf, _ = withChat(&f, chat3, false, "u99"); peerKeys(nf.PinnedPeers) != "" || peerKeys(nf.ExcludePeers) != "g2" {
		t.Fatal("remove a pinned person: unpinned, not excluded (the folder takes no people by kind)")
	}
	if nf, _ = withChat(&f, chat1, false, "u99"); peerKeys(nf.IncludePeers) != "" {
		t.Fatal("remove an included person")
	}
	g7 := &chat{key: "g7", peer: &tg.InputPeerChat{ChatID: 7}}
	if nf, _ = withChat(&f, g7, false, "u99"); peerKeys(nf.ExcludePeers) != "g2 g7" {
		t.Fatalf("a group the folder takes by kind is excluded: %q", peerKeys(nf.ExcludePeers))
	}
	if peerKeys(raw.IncludePeers) != "u1" || peerKeys(raw.PinnedPeers) != "u3" || peerKeys(raw.ExcludePeers) != "g2" {
		t.Fatal("the server's filter must not change")
	}
	all := folder{all: true}
	if _, err := withChat(&all, chat1, true, ""); err == nil {
		t.Fatal("'Tất cả' cannot be edited")
	}
	if _, err := withChat(&f, &chat{key: "u5"}, true, ""); err == nil {
		t.Fatal("a chat without a peer")
	}
}

type failing struct{ err error }

func (f failing) Invoke(context.Context, bin.Encoder, bin.Decoder) error { return f.err }

func TestSaveFolderErrors(t *testing.T) {
	nf := &tg.DialogFilter{ID: 5}
	for code, want := range map[string]string{"FILTER_INCLUDE_EMPTY": "ít nhất một chat", "FILTER_INCLUDE_TOO_MUCH": "tối đa"} {
		err := saveFolder(context.Background(), tg.NewClient(failing{tgerr.New(400, code)}), nf)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", code, err)
		}
	}
	rec := &recorder{}
	if err := saveFolder(context.Background(), tg.NewClient(rec), nf); err != nil {
		t.Fatal(err)
	}
	req, ok := rec.last(t).(*tg.MessagesUpdateDialogFilterRequest)
	if f, has := req.GetFilter(); !ok || req.ID != 5 || !has || f.(*tg.DialogFilter).ID != 5 {
		t.Fatalf("request: %+v", rec.last(t))
	}
}

// Right-click a chat › Thư mục: ✓ on its folders, a pick adds or removes it, the tabs and the list follow.
func TestFolderMenu(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	raw := []tg.DialogFilterClass{
		&tg.DialogFilterDefault{},
		&tg.DialogFilter{ID: 2, Title: tg.TextWithEntities{Text: "Bạn bè"}, IncludePeers: []tg.InputPeerClass{&tg.InputPeerUser{UserID: 3}}},
		&tg.DialogFilterChatlist{ID: 3, Title: tg.TextWithEntities{Text: "Chia sẻ"}},
	}
	a.send(foldersMsg{list: toFolders(raw, "u99")})
	m := a.m()
	lan := m.chats[m.index("u3")]
	m.openChatMenu(lan, 2, 10)
	var sub *menuItem
	for i := range m.menu.items {
		if m.menu.items[i].label == "Thư mục" {
			sub = &m.menu.items[i]
		}
	}
	if sub == nil {
		t.Fatal("no Thư mục item in the chat menu")
	}
	sub.run(m)
	if m.menu.items[0].label != "Bạn bè" || m.menu.items[0].hint != "✓" || m.menu.items[1].hint != "" || !strings.Contains(m.menu.title, "Lan") {
		t.Fatalf("sub-menu: %+v", m.menu.items)
	}
	cmd := m.menu.items[0].run(m) // Lan is in Bạn bè: take her out
	a.set(m)
	a.run(cmd)
	m = a.m()
	if req, ok := a.rec.last(t).(*tg.MessagesUpdateDialogFilterRequest); !ok || req.ID != 2 {
		t.Fatalf("request: %T", a.rec.last(t))
	}
	if m.folders[1].has(&lan) || !strings.Contains(m.toast, "Đã bỏ Lan khỏi Bạn bè") {
		t.Fatalf("after removing: toast=%q", m.toast)
	}
	// and back in
	m.openChatMenu(lan, 2, 10)
	for _, it := range m.menu.items {
		if it.label == "Thư mục" {
			it.run(m)
		}
	}
	cmd = m.menu.items[0].run(m)
	a.set(m)
	a.run(cmd)
	if m = a.m(); !m.folders[1].has(&lan) || !strings.Contains(m.toast, "Đã thêm Lan vào Bạn bè") {
		t.Fatalf("after adding: toast=%q", m.toast)
	}
	// a shared folder is edited in Telegram only
	n := a.rec.count()
	m.openChatMenu(lan, 2, 10)
	for _, it := range m.menu.items {
		if it.label == "Thư mục" {
			it.run(m)
		}
	}
	m.menu.items[1].run(m)
	if a.rec.count() != n || !strings.Contains(m.toast, "Thư mục chia sẻ") {
		t.Fatal("a shared folder must not be sent")
	}
	// no folders: no item
	m.folders = nil
	m.openChatMenu(lan, 2, 10)
	for _, it := range m.menu.items {
		if it.label == "Thư mục" {
			t.Fatal("Thư mục shown without folders")
		}
	}
}

// The note box is half the title row: with a long file name the percent must still be on screen.
func TestProgressSurvivesLongNames(t *testing.T) {
	a := newActionModel(t)
	m := a.m()
	x := newXfer("bao-cao-tong-ket-du-an-quy-4-ban-cuoi-cung-that-su.mp4", 350<<20, false)
	x.parts[1] = 147 << 20
	m.xfers, m.note = []*xfer{x}, x.text()
	if v := ansi.Strip(m.View()); !strings.Contains(v, "42% · 147.0 MB / 350.0 MB") {
		t.Fatal("the progress was cut off")
	}
}
