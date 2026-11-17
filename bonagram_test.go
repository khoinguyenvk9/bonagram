package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/tg"
)

func TestNormalizePhone(t *testing.T) {
	for in, want := range map[string]string{
		"0372 123-456":   "+84372123456",
		"+84372123456":   "+84372123456",
		"14155550100":    "+14155550100",
		"(0372) 123 456": "+84372123456",
	} {
		if got := normalizePhone(in); got != want {
			t.Errorf("normalizePhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAddLine(t *testing.T) {
	ls := addLine(nil, line{text: "hi", out: true}) // optimistic, id 0
	ls = addLine(ls, line{id: 7, text: "hi", out: true})
	if len(ls) != 1 || ls[0].id != 7 {
		t.Fatalf("server echo should adopt the pending line: %+v", ls)
	}
	ls = addLine(ls, line{id: 7, text: "hi", out: true})
	if len(ls) != 1 {
		t.Fatalf("same id twice must not duplicate: %+v", ls)
	}
	ls = addLine(ls, line{id: 8, text: "hi", out: true}) // a second, genuinely new "hi"
	if len(ls) != 2 {
		t.Fatalf("a different id is a new message: %+v", ls)
	}
}

func TestFoldAndShortTime(t *testing.T) {
	if fold("Nguyễn Đạt") != "nguyen dat" || !strings.Contains(fold("Nguyễn Đạt @DatNg"), fold("dat")) {
		t.Fatalf("fold: %q", fold("Nguyễn Đạt"))
	}
	loc := time.FixedZone("x", 7*3600)
	now := time.Date(2026, 11, 16, 15, 0, 0, 0, loc) // a Monday
	at := func(d time.Time) int { return int(d.Unix()) }
	for want, d := range map[string]time.Time{
		"09:30":   time.Date(2026, 11, 16, 9, 30, 0, 0, loc),
		"Hôm qua": time.Date(2026, 11, 15, 23, 0, 0, 0, loc),
		"Thứ 6":   time.Date(2026, 11, 13, 10, 0, 0, 0, loc),
		"01/11":   time.Date(2026, 11, 1, 10, 0, 0, 0, loc),
	} {
		if got := shortTime(at(d), now); got != want {
			t.Errorf("shortTime(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestToLine(t *testing.T) {
	m := &tg.Message{ID: 1, Message: "xem\x1b[2J", Media: &tg.MessageMediaPhoto{}, PeerID: &tg.PeerUser{UserID: 5}}
	l, ok := toLine(m, peer.Entities{})
	if !ok || l.text != "xem[2J [photo]" || l.from != "?" {
		t.Fatalf("control characters must be dropped, media tagged: %+v", l)
	}
	// a plain message inside a forum topic is not a reply; one with a top id is
	topicRoot := &tg.Message{ID: 2, PeerID: &tg.PeerChannel{ChannelID: 1}, ReplyTo: &tg.MessageReplyHeader{ForumTopic: true, ReplyToMsgID: 40}}
	if l, _ := toLine(topicRoot, peer.Entities{}); l.topic != 40 || l.replyTo != 0 {
		t.Fatalf("topic root pointer is not a reply: %+v", l)
	}
	reply := &tg.Message{ID: 3, PeerID: &tg.PeerChannel{ChannelID: 1}, ReplyTo: &tg.MessageReplyHeader{ForumTopic: true, ReplyToMsgID: 41, ReplyToTopID: 40}}
	if l, _ := toLine(reply, peer.Entities{}); l.topic != 40 || l.replyTo != 41 {
		t.Fatalf("reply inside a topic: %+v", l)
	}
	if mediaTag(nil) != "" || mediaTag(&tg.MessageMediaEmpty{}) != "" {
		t.Fatal("no media must give no tag")
	}
	if l, ok := toLine(&tg.MessageService{ID: 9, PeerID: &tg.PeerChat{ChatID: 1}}, peer.Entities{}); !ok || l.text != "[sự kiện]" || !l.event {
		t.Fatalf("service message: %+v", l)
	}
}

func TestOSCDropsInjection(t *testing.T) {
	got := osc("a;b\x1b]0;x\x07", "hi\x1b\\there")
	if strings.Count(got, "\x1b") != 1 || strings.Count(got, "\x07") != 1 {
		t.Fatalf("only the framing may stay: %q", got)
	}
}

func TestPlaceholdersAndFit(t *testing.T) {
	rows := placeholders(0x010203, 3, 2)
	if ansi.StringWidth(rows[0]) != 3 {
		t.Fatalf("a placeholder row must measure one cell per image cell, got %d", ansi.StringWidth(rows[0]))
	}
	if len(rows) != 2 || strings.Count(rows[0], "\U0010EEEE") != 3 || !strings.Contains(rows[0], "38;2;1;2;3m") {
		t.Fatalf("placeholders: %q", rows)
	}
	if c, r := fitCells(100, 100, 40, 8); c != 16 || r != 8 { // a square is twice as many columns as rows
		t.Fatalf("fitCells = %d x %d", c, r)
	}
	if c, r := fitCells(400, 100, 20, 20); c != 20 || r != 3 {
		t.Fatalf("wide image = %d x %d", c, r)
	}
}

func TestRenderImageHalfblocks(t *testing.T) {
	if kitty {
		t.Skip("would write to the terminal")
	}
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for x := 0; x < 10; x++ {
		for y := 0; y < 10; y++ {
			img.Set(x, y, color.RGBA{200, 0, 0, 255})
		}
	}
	p := filepath.Join(t.TempDir(), "a.png")
	f, _ := os.Create(p)
	png.Encode(f, img)
	f.Close()
	pic, err := renderImage(p, 10, 3)
	if err != nil || len(pic.rows) != 3 || !strings.Contains(pic.rows[0], "▀") {
		t.Fatalf("pic=%v err=%v", pic.rows, err)
	}
	if _, err := renderImage(filepath.Join(t.TempDir(), "missing.png"), 10, 3); err == nil {
		t.Fatal("a missing file must be an error, not a crash")
	}
}

func newTestModel(t *testing.T) model {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return newModel(ctx, nil, make(chan tea.Msg), config{maxMsgs: 500}, sideDef)
}

// Drives the model with fake events (no Telegram): list, search, open, new message, unread, reply, topics.
func TestModelFlow(t *testing.T) {
	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) { mm, _ = mm.Update(msg) }
	key := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

	send(tea.WindowSizeMsg{Width: 120, Height: 24})
	send(chatsMsg{chats: []chat{
		{key: "u1", title: "Đạt", last: "alo", unread: 2, peer: &tg.InputPeerUser{UserID: 1}},
		{key: "c2", title: "Nhóm", muted: true, peer: &tg.InputPeerChannel{ChannelID: 2}},
		{key: "u3", title: "Lan", username: "lan_vn", peer: &tg.InputPeerUser{UserID: 3}},
	}})
	if got := mm.(model).windowTitle(); got != "(2) bonagram" {
		t.Fatalf("title = %q", got)
	}

	// search ignores accents and also matches the username
	send(tea.KeyMsg{Type: tea.KeyCtrlK})
	for _, r := range "dat" {
		send(key(string(r)))
	}
	sm := mm.(model)
	if vis := sm.visible(); len(vis) != 1 || sm.chats[vis[0]].key != "u1" {
		t.Fatalf("search 'dat' should find Đạt only: %v", vis)
	}
	send(tea.KeyMsg{Type: tea.KeyEsc})
	if sm = mm.(model); sm.search.Value() != "" || len(sm.visible()) != 3 {
		t.Fatal("esc should clear the search")
	}

	// a new message lifts its chat; muted chat counts unread
	send(newMsg{key: "c2", title: "Nhóm", line: line{id: 1, from: "Bình", text: "tin mới", at: time.Now()}})
	m := mm.(model)
	if m.chats[0].key != "c2" || m.chats[0].unread != 1 || m.chats[0].last != "Bình: tin mới" {
		t.Fatalf("new message should lift its chat: %+v", m.chats[0])
	}

	send(tea.KeyMsg{Type: tea.KeyCtrlG}) // c2 first, then u1
	send(tea.KeyMsg{Type: tea.KeyCtrlG})
	m = mm.(model)
	if m.cur != "u1" || m.chats[m.index("u1")].unread != 0 {
		t.Fatalf("ctrl+g should end on u1 with its badge cleared, cur=%q", m.cur)
	}

	send(histMsg{mk: "u1", key: "u1", more: true, lines: []line{
		{id: 1, from: "Đạt", sender: "u1", text: "xin chào", at: time.Now()},
		{id: 2, from: "Bạn", out: true, text: "chào", at: time.Now()},
	}})
	v := mm.View()
	if !strings.Contains(v, "xin chào") || !strings.Contains(v, "Nhóm") || !strings.Contains(v, "Tìm chat") || !strings.Contains(v, "Nhắn tin") {
		t.Fatalf("view is missing chat, message, search box or input:\n%s", v)
	}
	// the title bar names the open chat
	if first := strings.Split(ansi.Strip(v), "\n")[1]; !strings.Contains(first, "Đạt") {
		t.Fatalf("title bar should show the open chat: %q", first)
	}
	lines := strings.Split(v, "\n")
	if len(lines) != 24 {
		t.Fatalf("frame must be exactly the window height, got %d lines", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 120 {
			t.Fatalf("line %d is %d cells wide, want 120", i, w)
		}
	}

	// browse, then reply to the first message
	send(tea.KeyMsg{Type: tea.KeyCtrlB})
	send(tea.KeyMsg{Type: tea.KeyUp})
	send(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(model)
	if m.reply == nil || m.reply.id != 1 || m.browse {
		t.Fatalf("enter on a text message should reply to it: %+v browse=%v", m.reply, m.browse)
	}
	if !strings.Contains(mm.View(), "↩ Đạt: xin chào") {
		t.Fatal("reply bar missing")
	}
	send(tea.KeyMsg{Type: tea.KeyEsc})
	if mm.(model).reply != nil {
		t.Fatal("esc should cancel the reply")
	}

	// older messages go above, in order, without duplicates
	send(olderMsg{mk: "u1", more: true, lines: []line{{id: 0, text: "cũ hơn", at: time.Now()}}})
	if ls := mm.(model).msgs["u1"]; len(ls) != 3 || ls[0].text != "cũ hơn" || ls[1].id != 1 {
		t.Fatalf("older lines go first: %+v", ls)
	}
	send(olderMsg{mk: "nope", lines: []line{{id: 5}}}) // a view we do not have: ignored
	if n := len(mm.(model).msgs["nope"]); n != 0 {
		t.Fatalf("older for an unknown view must not create it: %d", n)
	}

	// a message from a chat we have never seen, with no access hash, must not crash
	send(newMsg{key: "u99", title: "Lạ", line: line{id: 2, text: "?"}})
	if !utf8.ValidString(mm.View()) {
		t.Fatal("invalid view")
	}
}

func TestForumTopics(t *testing.T) {
	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) { mm, _ = mm.Update(msg) }
	send(tea.WindowSizeMsg{Width: 120, Height: 24})
	send(chatsMsg{chats: []chat{{key: "c1", title: "Diễn đàn", forum: true, unread: 3, peer: &tg.InputPeerChannel{ChannelID: 1}}}})
	send(tea.KeyMsg{Type: tea.KeyCtrlN}) // opens the forum: topics are requested first
	if mm.(model).chats[0].topics != nil {
		t.Fatal("topics are loaded lazily")
	}
	send(topicsMsg{key: "c1", topics: []topic{{1, "General", 1}, {7, "Kèo", 2}}})
	m := mm.(model)
	if m.chats[0].topic != 1 || mkFor(&m.chats[0], 1) != "c1#1" {
		t.Fatalf("first topic should open: %+v", m.chats[0])
	}
	if !strings.Contains(mm.View(), "Kèo  2") {
		t.Fatal("topic tab with its unread count is missing")
	}
	send(tea.KeyMsg{Type: tea.KeyCtrlT})
	if m = mm.(model); m.chats[0].topic != 7 {
		t.Fatalf("ctrl+t should select the next topic: %+v", m.chats[0])
	}
	// a message in the other topic counts there, not in the open one
	send(newMsg{key: "c1", line: line{id: 5, from: "A", text: "x", topic: 1, at: time.Now()}})
	m = mm.(model)
	if m.chats[0].topics[0].unread != 2 || m.chats[0].topics[1].unread != 2 {
		t.Fatalf("topic unread: %+v", m.chats[0].topics)
	}
}

// The kitty path writes to the real terminal, so capture stdout and check the escape sequence itself:
// chunked inline PNG, a virtual placement (U=1) of the right size, and placeholder rows that match.
func TestKittySequence(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for i := range img.Pix { // noise, so the PNG needs several 4 KiB chunks
		img.Pix[i] = byte(i * 7919 >> 3)
	}
	p := filepath.Join(t.TempDir(), "n.png")
	f, _ := os.Create(p)
	png.Encode(f, img)
	f.Close()

	r, w, _ := os.Pipe()
	done := make(chan []byte)
	go func() { // read while it is written: a Windows pipe holds only a few KB
		raw, _ := io.ReadAll(r)
		done <- raw
	}()
	old, oldKitty := os.Stdout, kitty
	os.Stdout, kitty = w, true
	pic, err := renderImage(p, 20, 10)
	w.Close()
	os.Stdout, kitty = old, oldKitty
	raw := <-done
	if err != nil {
		t.Fatal(err)
	}

	chunks := regexp.MustCompile(`\x1b_G([^;]*);([^\x1b]*)\x1b\\`).FindAllStringSubmatch(string(raw), -1)
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	var data strings.Builder
	for i, c := range chunks {
		last := i == len(chunks)-1
		if len(c[2]) > 4096 || strings.Contains(c[1], "m=0") != last || strings.Contains(c[1], "m=1") == last {
			t.Fatalf("chunk %d: control %q, %d bytes", i, c[1], len(c[2]))
		}
		if (i == 0) != strings.HasPrefix(c[1], "a=T,U=1,f=100,i=") {
			t.Fatalf("only the first chunk carries the header: %q", c[1])
		}
		data.WriteString(c[2])
	}
	b, err := base64.StdEncoding.DecodeString(data.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := png.Decode(bytes.NewReader(b)); err != nil {
		t.Fatalf("payload is not a PNG: %v", err)
	}
	// 4:3 image in a 20 x 10 box: 20 columns x 8 rows (cells are twice as tall as wide)
	if !strings.Contains(chunks[0][1], ",c=20,r=8,") || len(pic.rows) != 8 || pic.id == 0 {
		t.Fatalf("placement %q rows=%d id=%d", chunks[0][1], len(pic.rows), pic.id)
	}
	if !strings.Contains(pic.rows[0], fmt.Sprintf("38;2;%d;%d;%dm", pic.id>>16&255, pic.id>>8&255, pic.id&255)) {
		t.Fatal("placeholder colour must encode the image id")
	}
}

// Frames are followed by a cursor move to the focused input (IME text shows there); leaving the
// alternate screen is passed through untouched.
func TestRealCursor(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	o := lockedOut{f}
	read := func() string { b, _ := os.ReadFile(f.Name()); return string(b) }

	setCursor(4, 9)
	if n, err := o.Write([]byte("frame")); err != nil || n != 5 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if got := read(); got != "\x1b[?7lframe\x1b[5;10H\x1b[?25h\x1b[?7h" {
		t.Fatalf("got %q", got)
	}
	f.Truncate(0)
	f.Seek(0, 0)
	setCursor(-1, 0)
	o.Write([]byte("x"))
	o.Write([]byte("\x1b[?1049l"))
	if got := read(); got != "\x1b[?7lx\x1b[?25l\x1b[?7h\x1b[?1049l" {
		t.Fatalf("hidden cursor / exit: %q", got)
	}
}

func TestCursorFollowsInput(t *testing.T) {
	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) { mm, _ = mm.Update(msg) }
	send(tea.WindowSizeMsg{Width: 100, Height: 24})
	send(chatsMsg{chats: []chat{{key: "u1", title: "Đạt", peer: &tg.InputPeerUser{UserID: 1}}}})
	mm.View()
	if p := realCursor.Load(); p == nil || p.row != 1 || p.col != 4 { // empty search box: margin 2, border, prompt
		t.Fatalf("search cursor: %+v", p)
	}
	send(tea.KeyMsg{Type: tea.KeyCtrlN})
	for _, r := range "tiếng" {
		send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	mm.View()
	// 24 rows: keys bar = row 23, compose box = rows 20..22, text row 21; column = side 36 + divider 1 + margin 2 + border 1 + space 1 + 5 chars
	if p := realCursor.Load(); p == nil || p.row != 21 || p.col != 36+1+2+1+1+5 {
		t.Fatalf("input cursor: %+v", p)
	}
}

func TestMouseLeakFilter(t *testing.T) {
	runes := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	esc := tea.KeyMsg{Type: tea.KeyEscape}
	altBracket := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("["), Alt: true}
	wheelDown := func(msg tea.Msg) bool {
		m, ok := msg.(tea.MouseMsg)
		return ok && m.Button == tea.MouseButtonWheelDown && m.X == 9 && m.Y == 11
	}
	run := func(in ...tea.Msg) []tea.Msg {
		l := &mouseLeak{}
		var out []tea.Msg
		for _, m := range in {
			if r := l.filter(nil, m); r != nil {
				out = append(out, r)
			}
		}
		return out
	}

	// Esc, then the whole tail in one message: Esc passes, the tail becomes a wheel event
	if got := run(esc, runes("[<65;10;12M")); len(got) != 2 || !wheelDown(got[1]) {
		t.Fatalf("whole tail: %#v", got)
	}
	// the tail itself cut in two reads
	if got := run(esc, runes("[<65;1"), runes("0;12M")); len(got) != 2 || !wheelDown(got[1]) {
		t.Fatalf("split tail: %#v", got)
	}
	// "ESC [" read as Alt+[
	if got := run(altBracket, runes("<65;10;12M")); len(got) != 1 || !wheelDown(got[0]) {
		t.Fatalf("alt+[: %#v", got)
	}
	// ordinary typing is untouched, also straight after a real Esc
	if got := run(runes("xin chào")); len(got) != 1 || string(got[0].(tea.KeyMsg).Runes) != "xin chào" {
		t.Fatalf("typing: %#v", got)
	}
	if got := run(esc, runes("hello")); len(got) != 2 || string(got[1].(tea.KeyMsg).Runes) != "hello" {
		t.Fatalf("esc then typing: %#v", got)
	}
	// a release report is decoded too
	if got := run(esc, runes("[<0;5;6m")); len(got) != 2 || got[1].(tea.MouseMsg).Action != tea.MouseActionRelease {
		t.Fatalf("release: %#v", got)
	}
}

func TestSidebarWheel(t *testing.T) {
	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) { mm, _ = mm.Update(msg) }
	send(tea.WindowSizeMsg{Width: 100, Height: 24})
	var cs []chat
	for i := 0; i < 30; i++ {
		cs = append(cs, chat{key: fmt.Sprintf("u%d", i), title: fmt.Sprintf("Chat %d", i), peer: &tg.InputPeerUser{UserID: int64(i)}})
	}
	send(chatsMsg{chats: cs})
	wheel := func(b tea.MouseButton) {
		send(tea.MouseMsg{X: 5, Y: 10, Button: b, Action: tea.MouseActionPress})
	}
	for i := 0; i < 4; i++ {
		wheel(tea.MouseButtonWheelDown)
	}
	if top := mm.(model).top; top != 4 {
		t.Fatalf("wheel down should scroll the list by one chat each: top=%d", top)
	}
	if v := ansi.Strip(mm.View()); strings.Contains(v, "Chat 0 ") || !strings.Contains(v, "Chat 4") {
		t.Fatal("the list did not scroll")
	}
	for i := 0; i < 10; i++ {
		wheel(tea.MouseButtonWheelUp)
	}
	if top := mm.(model).top; top != 0 {
		t.Fatalf("wheel up stops at the first chat: top=%d", top)
	}
	for i := 0; i < 100; i++ {
		wheel(tea.MouseButtonWheelDown)
	}
	end := mm.(model)
	if top, rows := end.top, end.listRows(); top != 30-rows {
		t.Fatalf("wheel down stops when the last chat is on screen: top=%d rows=%d", top, rows)
	}
	if v := mm.(model).in.Value(); v != "" {
		t.Fatalf("scrolling must not type anything: %q", v)
	}
}

func TestReadEnvAcceptsExport(t *testing.T) {
	p := filepath.Join(t.TempDir(), "env")
	os.WriteFile(p, []byte("export TG_API_ID=12\nTG_API_HASH=\"abc\"\n# c=1\n"), 0o600)
	if kv := readEnv(p); kv["TG_API_ID"] != "12" || kv["TG_API_HASH"] != "abc" || len(kv) != 2 {
		t.Fatalf("%v", kv)
	}
}

// Emoji sequences, bidi marks, wide CJK and combining marks must never make a row wider or narrower than
// its column: that is what pushed text onto the next line and made chats overlap.
func TestTrickyTextKeepsWidths(t *testing.T) {
	tricky := []string{
		"Dogs 🐕‍🦺", "❤️ love ❤️", "🛠️ tools 🍽️", "🇻🇳 Việt Nam 🇺🇸", "مرحبا ‮evil", "é́ cmb",
		"日本語のチャット です", "a​b⁠c", "emoji 👨‍👩‍👧‍👦 family", "tab\there", "ví dụ ️",
	}
	for _, s := range tricky {
		for _, c := range []string{clean(s), s} { // cleaned (the real path) and raw (defence in depth)
			for _, w := range []int{20, 36} {
				ch := chat{key: "u1", title: c, last: c + " " + c, unread: 3, date: int(time.Now().Unix())}
				for i, row := range chatItem(ch, w == 36, w, time.Now()) {
					if got := ansi.StringWidth(row); got != w {
						t.Errorf("%q w=%d row %d is %d cells", c, w, i, got)
					}
					if strings.Contains(row, "\n") {
						t.Errorf("%q: row %d contains a newline", c, i)
					}
				}
			}
		}
	}
	for _, r := range clean("a‍b‮c️d e") {
		if r == 0x200d || r == 0x202e || r == 0xfe0f || r == 0x2028 {
			t.Fatalf("clean left %U", r)
		}
	}

	// the whole frame stays exactly window-sized with such chats in the list and such text in the view
	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) { mm, _ = mm.Update(msg) }
	send(tea.WindowSizeMsg{Width: 110, Height: 24})
	var cs []chat
	for i, s := range tricky {
		cs = append(cs, chat{key: fmt.Sprintf("u%d", i), title: s, last: s, peer: &tg.InputPeerUser{UserID: int64(i)}})
	}
	send(chatsMsg{chats: cs})
	send(tea.KeyMsg{Type: tea.KeyCtrlN})
	var ls []line
	for i, s := range tricky {
		ls = append(ls, line{id: i + 1, from: s, sender: s, text: s + " " + s + " " + s, at: time.Now().Add(time.Duration(i) * time.Hour)})
	}
	send(histMsg{mk: "u0", key: "u0", lines: ls})
	frame := strings.Split(mm.View(), "\n")
	if len(frame) != 24 {
		t.Fatalf("frame has %d lines", len(frame))
	}
	for i, l := range frame {
		if w := ansi.StringWidth(l); w != 110 {
			t.Fatalf("line %d is %d cells, want 110", i, w)
		}
	}
}

func TestToReactions(t *testing.T) {
	chosen := tg.ReactionCount{Reaction: &tg.ReactionEmoji{Emoticon: "❤️"}, Count: 1}
	chosen.SetChosenOrder(1)
	rs := toReactions(tg.MessageReactions{Results: []tg.ReactionCount{
		{Reaction: &tg.ReactionEmoji{Emoticon: "👍"}, Count: 3},
		chosen,
		{Reaction: &tg.ReactionCustomEmoji{DocumentID: 7}, Count: 2},
		{Reaction: &tg.ReactionPaid{}, Count: 5},
		{Reaction: &tg.ReactionEmoji{Emoticon: "😂"}, Count: 0}, // nobody: dropped
		{Reaction: &tg.ReactionEmpty{}, Count: 1},              // nothing to show: dropped
	}})
	want := []reaction{{"👍", 3, false}, {"❤", 1, true}, {"✦", 2, false}, {"⭐", 5, false}}
	if fmt.Sprint(rs) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", rs, want)
	}
	// a partial ("min") update does not say which reactions are yours
	if rs := toReactions(tg.MessageReactions{Min: true, Results: []tg.ReactionCount{chosen}}); rs[0].mine {
		t.Fatal("min reactions must not claim to be mine")
	}
	l, _ := toLine(&tg.Message{ID: 1, PeerID: &tg.PeerUser{UserID: 1}, Reactions: tg.MessageReactions{Results: []tg.ReactionCount{{Reaction: &tg.ReactionEmoji{Emoticon: "👍"}, Count: 2}}}}, peer.Entities{})
	if len(l.reacts) != 1 || l.reacts[0].count != 2 {
		t.Fatalf("reactions of a loaded message: %+v", l.reacts)
	}
}

// Reactions show under the message, update live, and a burst of updates is drawn once, only if in view.
func TestReactionsLive(t *testing.T) {
	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) tea.Cmd {
		var c tea.Cmd
		mm, c = mm.Update(msg)
		return c
	}
	send(tea.WindowSizeMsg{Width: 110, Height: 24})
	send(chatsMsg{chats: []chat{
		{key: "u1", title: "Đạt", peer: &tg.InputPeerUser{UserID: 1}},
		{key: "u2", title: "Lan", peer: &tg.InputPeerUser{UserID: 2}},
	}})
	send(tea.KeyMsg{Type: tea.KeyCtrlN})
	send(histMsg{mk: "u1", key: "u1", lines: []line{{id: 5, from: "Đạt", sender: "u1", text: "xin chào", at: time.Now()}}})
	send(histMsg{mk: "u2", key: "u2", lines: []line{{id: 9, from: "Lan", sender: "u2", text: "alo", at: time.Now()}}})
	if strings.Contains(ansi.Strip(mm.View()), "👍") {
		t.Fatal("no reactions yet")
	}

	send(reactMsg{key: "u1", id: 5, rs: []reaction{{"👍", 1, false}}})
	if strings.Contains(ansi.Strip(mm.View()), "👍") {
		t.Fatal("the redraw is deferred, not immediate")
	}
	send(reactMsg{key: "u1", id: 5, rs: []reaction{{"👍", 3, false}, {"❤", 1, true}}}) // burst: no second tick
	if !mm.(model).redraw {
		t.Fatal("a redraw should be pending")
	}
	send(renderTick{})
	if v := ansi.Strip(mm.View()); !strings.Contains(v, "👍 3  ❤ 1") {
		t.Fatalf("reaction line missing:\n%s", v)
	}
	if mm.(model).redraw {
		t.Fatal("tick clears the pending flag")
	}

	// another chat: data is kept (shown when opened) but nothing is scheduled; unknown ids are ignored
	send(reactMsg{key: "u2", id: 9, rs: []reaction{{"🔥", 2, false}}})
	send(reactMsg{key: "u2", id: 12345, rs: []reaction{{"🔥", 2, false}}})
	send(reactMsg{key: "nope", id: 1})
	if mm.(model).redraw {
		t.Fatal("a reaction elsewhere must not trigger a redraw")
	}
	send(tea.KeyMsg{Type: tea.KeyCtrlN})
	if v := ansi.Strip(mm.View()); !strings.Contains(v, "🔥 2") {
		t.Fatalf("reaction stored for the other chat is missing:\n%s", v)
	}
}

func u16(s string) int { return len(utf16.Encode([]rune(s))) }

func TestSegmentsUTF16(t *testing.T) {
	pre := `fmt.Println("é 😀")`
	head := "Xin chào 😀 đây\n"
	msg := head + pre + "\nhết `x` ok"
	inl := strings.Index(msg, "`x`")
	es := []tg.MessageEntityClass{
		&tg.MessageEntityBold{Offset: 0, Length: 3}, // not code: ignored
		&tg.MessageEntityCode{Offset: u16(msg[:inl]) + 1, Length: 1},
		&tg.MessageEntityPre{Offset: u16(head), Length: u16(pre), Language: "go"},
	}
	// the Code entity above points at the "x" inside the backticks; the backticks stay plain text
	got := segments(msg, es)
	want := []seg{
		{segText, head, ""},
		{segPre, pre, "go"},
		{segText, "\nhết `", ""},
		{segCode, "x", ""},
		{segText, "` ok", ""},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got  %+v\nwant %+v", got, want)
	}
	if segments("plain", []tg.MessageEntityClass{&tg.MessageEntityBold{Length: 2}}) != nil {
		t.Fatal("no code entity, no segments")
	}
	// overlapping, empty and out-of-range entities are ignored, never trusted
	bad := []tg.MessageEntityClass{
		&tg.MessageEntityPre{Offset: 0, Length: 4},
		&tg.MessageEntityCode{Offset: 2, Length: 4}, // overlaps the previous one
		&tg.MessageEntityCode{Offset: 3, Length: 0},
		&tg.MessageEntityPre{Offset: 5, Length: 999},
		&tg.MessageEntityCode{Offset: -1, Length: 2},
	}
	if got := segments("abcdefgh", bad); len(got) != 2 || got[0] != (seg{segPre, "abcd", ""}) || got[1].raw != "efgh" {
		t.Fatalf("bad entities: %+v", got)
	}
}

func codeMsg(id int, out bool, blocks ...string) line {
	var text string
	var es []tg.MessageEntityClass
	for i, b := range blocks {
		text += "đoạn " + fmt.Sprint(i+1) + "\n"
		es = append(es, &tg.MessageEntityPre{Offset: u16(text), Length: u16(b), Language: "go"})
		text += b + "\n"
	}
	return line{id: id, from: "Đạt", sender: "u1", out: out, text: text, at: time.Now(), segs: segments(text, es)}
}

func TestCodeBlockCopyButton(t *testing.T) {
	var copied []string
	old := clipboard
	clipboard = func(s string) { copied = append(copied, s) }
	defer func() { clipboard = old }()

	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) tea.Cmd {
		var c tea.Cmd
		mm, c = mm.Update(msg)
		return c
	}
	send(tea.WindowSizeMsg{Width: 110, Height: 30})
	send(chatsMsg{chats: []chat{{key: "u1", title: "Đạt", peer: &tg.InputPeerUser{UserID: 1}}}})
	send(tea.KeyMsg{Type: tea.KeyCtrlN})
	code1 := "func main() {\n\tfmt.Println(\"xin chào\")\n}"
	code2 := "x := 1"
	send(histMsg{mk: "u1", key: "u1", lines: []line{codeMsg(1, false, code1, code2), codeMsg(2, true, code2)}})

	v := ansi.Strip(mm.View())
	for _, s := range []string{"go ", "[copy]", "func main() {", "    fmt.Println(\"xin chào\")", "đoạn 1"} {
		if !strings.Contains(v, s) {
			t.Fatalf("view is missing %q:\n%s", s, v)
		}
	}
	if strings.Contains(v, "\t") {
		t.Fatal("tabs must be expanded in the display")
	}
	m := mm.(model)
	if len(m.hits) != 3 {
		t.Fatalf("three buttons expected, got %+v", m.hits)
	}
	// clicking a button copies exactly that block (raw: the tab is kept)
	for i, want := range []string{code1, code2, code2} {
		h := m.hits[i]
		x, y := m.side+1+h.x0+2, m.msgY()+h.row-m.vp.YOffset
		cmd := send(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		if cmd == nil {
			t.Fatalf("button %d: click did nothing", i)
		}
		toast := cmd().(toastMsg)
		if copied[len(copied)-1] != want || !strings.Contains(toast.text, "Đã chép") {
			t.Fatalf("button %d copied %q (%s), want %q", i, copied[len(copied)-1], toast.text, want)
		}
	}
	// a click next to the button is an ordinary message click, not a copy
	h := m.hits[0]
	before := len(copied)
	if cmd := send(tea.MouseMsg{X: m.side + 1 + 3, Y: m.msgY() + h.row - m.vp.YOffset, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}); cmd != nil {
		cmd()
	}
	if len(copied) != before {
		t.Fatal("clicking the language label must not copy")
	}

	// keyboard: C copies the first block of the selected message, again the next, then round
	send(tea.KeyMsg{Type: tea.KeyEscape})
	send(tea.KeyMsg{Type: tea.KeyCtrlB})
	send(tea.KeyMsg{Type: tea.KeyUp}) // message 1
	copied = nil
	for i, want := range []string{code1, code2, code1} {
		cmd := send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
		toast := cmd().(toastMsg)
		if copied[i] != want || !strings.Contains(toast.text, fmt.Sprintf("%d/2", i%2+1)) {
			t.Fatalf("press %d copied %q (%s)", i, copied[i], toast.text)
		}
	}

	// every frame line keeps the window width with blocks on screen (also with the long-line wrap)
	send(histMsg{mk: "u1", key: "u1", lines: []line{codeMsg(1, false, strings.Repeat("très long ", 40)), codeMsg(2, true, "ok")}})
	for i, l := range strings.Split(mm.View(), "\n") {
		if w := ansi.StringWidth(l); w != 110 {
			t.Fatalf("line %d is %d cells wide", i, w)
		}
	}
}

func TestCopyWholeMessageWithoutCode(t *testing.T) {
	var got string
	old := clipboard
	clipboard = func(s string) { got = s }
	defer func() { clipboard = old }()
	m := newTestModel(t)
	cmd := (&m).copyFrom(line{id: 3, text: "tin thường [photo]", msg: &tg.Message{Message: "tin thường"}})
	cmd()
	if got != "tin thường" {
		t.Fatalf("copied %q", got)
	}
	if !strings.HasPrefix(osc52("hi"), "\x1b]52;c;aGk=") {
		t.Fatalf("osc52: %q", osc52("hi"))
	}
}

func TestSaveCredsKeepsOptions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "env")
	os.WriteFile(p, []byte("# note\nBONAGRAM_MAX_MESSAGES=200\nexport TG_API_ID=1\nTG_API_HASH=old\n"), 0o644)
	if err := saveCreds(p, 42, "newhash"); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "# note\nBONAGRAM_MAX_MESSAGES=200\nTG_API_ID=42\nTG_API_HASH=newhash\n" {
		t.Fatalf("%q", got)
	}
	fresh := filepath.Join(t.TempDir(), "new")
	saveCreds(fresh, 1, "h")
	if st, _ := os.Stat(fresh); st.Mode().Perm() != 0o600 {
		t.Fatalf("new env file must be private: %v", st.Mode().Perm())
	}
}

// Windows of any size must draw without panicking (also with topics, a reply bar and the image viewer).
func TestTinyWindows(t *testing.T) {
	for _, withViewer := range []bool{false, true} {
		for w := 1; w <= 130; w += 3 {
			for h := 1; h <= 46; h += 3 {
				var mm tea.Model = newTestModel(t)
				send := func(msg tea.Msg) { mm, _ = mm.Update(msg) }
				send(tea.WindowSizeMsg{Width: w, Height: h})
				send(chatsMsg{chats: []chat{
					{key: "c1", title: "Diễn đàn dài dài dài", forum: true, unread: 120, last: "x", date: int(time.Now().Unix()), peer: &tg.InputPeerChannel{ChannelID: 1}},
					{key: "u2", title: "Lan", peer: &tg.InputPeerUser{UserID: 2}},
				}})
				send(tea.KeyMsg{Type: tea.KeyCtrlN})
				send(topicsMsg{key: "c1", topics: []topic{{1, "General", 0}, {7, "Một topic có tên rất dài", 3}}})
				send(histMsg{mk: "c1#1", key: "c1", lines: []line{codeMsg(1, false, "x := 1\nreturn"), codeMsg(2, true, "ok")}})
				send(tea.KeyMsg{Type: tea.KeyCtrlR})
				send(tea.KeyMsg{Type: tea.KeyCtrlB})
				if withViewer {
					send(viewMsg{viewer{pic: picture{rows: []string{"████", "████"}}, title: "t"}})
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("panic at %dx%d (viewer=%v): %v", w, h, withViewer, r)
						}
					}()
					mm.View()
				}()
			}
		}
	}
}

func TestConfigFPS(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for env, want := range map[string]int{"": 60, "30": 30, "5": 10, "999": 120, "abc": 60} {
		t.Setenv("BONAGRAM_FPS", env)
		if got := loadConfig().fps; got != want {
			t.Errorf("BONAGRAM_FPS=%q: fps=%d, want %d", env, got, want)
		}
	}
}

// A forum's own unread count is not kept up to date by the server when a topic is read, so the badge is the
// sum over its topics (this was stuck on 7 while every topic said 0).
func TestForumUnreadIsSumOfTopics(t *testing.T) {
	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) { mm, _ = mm.Update(msg) }
	send(tea.WindowSizeMsg{Width: 110, Height: 24})
	send(chatsMsg{chats: []chat{{key: "c1", title: "Nhóm Dev", forum: true, unread: 7, peer: &tg.InputPeerChannel{ChannelID: 1}}}})
	if mm.(model).windowTitle() != "(7) bonagram" {
		t.Fatal("before the topics are known the forum's own count is all there is")
	}
	send(tea.KeyMsg{Type: tea.KeyCtrlN})
	send(topicsMsg{key: "c1", topics: []topic{{1, "General", 0}, {4, "Nhóm Dev", 0}}})
	if m := mm.(model); m.chats[0].unread != 0 || m.windowTitle() != "bonagram" {
		t.Fatalf("every topic is read: badge must go, unread=%d title=%q", m.chats[0].unread, m.windowTitle())
	}

	// with unread topics the badge counts them and falls as each topic is read
	send(topicsMsg{key: "c1", topics: []topic{{1, "General", 2}, {4, "Nhóm Dev", 3}}})
	if mm.(model).chats[0].unread != 5 {
		t.Fatalf("unread = %d, want 5", mm.(model).chats[0].unread)
	}
	send(histMsg{mk: "c1#1", key: "c1", lines: []line{{id: 10, from: "A", text: "x", at: time.Now()}}})
	if mm.(model).chats[0].unread != 3 {
		t.Fatalf("reading the open topic must lower the badge by its own count: %d", mm.(model).chats[0].unread)
	}
	send(tea.KeyMsg{Type: tea.KeyCtrlT})
	send(histMsg{mk: "c1#4", key: "c1", lines: []line{{id: 20, from: "A", text: "y", at: time.Now()}}})
	if mm.(model).chats[0].unread != 0 {
		t.Fatalf("both topics read: %d", mm.(model).chats[0].unread)
	}
}

func TestReloadKeepsSelectedTopic(t *testing.T) {
	var mm tea.Model = newTestModel(t)
	send := func(msg tea.Msg) { mm, _ = mm.Update(msg) }
	forum := func(topics []topic) chat {
		return chat{key: "c1", title: "F", forum: true, topics: topics, peer: &tg.InputPeerChannel{ChannelID: 1}}
	}
	send(tea.WindowSizeMsg{Width: 110, Height: 24})
	c := forum([]topic{{1, "General", 0}, {7, "Kèo", 0}})
	c.topic = 7
	send(chatsMsg{chats: []chat{c}})
	// a reload that already carries topics keeps the topic the user is in, or falls back to the first
	fresh := forum([]topic{{1, "General", 4}, {7, "Kèo", 1}})
	fresh.topic = 1
	send(chatsMsg{chats: []chat{fresh}})
	if m := mm.(model); m.chats[0].topic != 7 || m.chats[0].topics[0].unread != 4 {
		t.Fatalf("topic=%d topics=%+v", m.chats[0].topic, m.chats[0].topics)
	}
	// a reload without topics (their request failed) keeps what we had
	send(chatsMsg{chats: []chat{{key: "c1", title: "F", forum: true, peer: &tg.InputPeerChannel{ChannelID: 1}}}})
	if m := mm.(model); m.chats[0].topic != 7 || len(m.chats[0].topics) != 2 {
		t.Fatalf("topic=%d topics=%+v", m.chats[0].topic, m.chats[0].topics)
	}
}
