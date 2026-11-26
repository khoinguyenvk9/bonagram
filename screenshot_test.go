package main

// Screenshots for the README, drawn from fake data (no Telegram account is used):
//
//	BONAGRAM_SHOTS=docs go test -run TestScreenshots .
//
// writes docs/*.html (the frame as coloured HTML); scripts/shots.sh turns them into PNG with headless Chrome.

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gotd/td/tg"
	"github.com/muesli/termenv"
)

const shotW, shotH = 116, 50

func TestScreenshots(t *testing.T) {
	dir := os.Getenv("BONAGRAM_SHOTS")
	if dir == "" {
		t.Skip("set BONAGRAM_SHOTS=<folder> to draw the README screenshots")
	}
	lipgloss.SetColorProfile(termenv.TrueColor) // tests have no terminal: colours would be dropped
	oldK, oldR := kitty, rawProto
	kitty, rawProto = false, ""
	defer func() { kitty, rawProto = oldK, oldR }()

	a := demoModel(t)
	writeShot(t, filepath.Join(dir, "screenshot.html"), a.mm.View())
	shotScenes(t, dir)
	a = demoModel(t)

	// Ctrl+F: searching the messages of the chat
	a.send(tea.KeyMsg{Type: tea.KeyCtrlF})
	a.send(searchMsg{seq: 0, q: "test", lines: searchDemo()})
	m := a.m()
	m.ms.seq, m.ms.ran, m.ms.res = 0, "test", searchDemo()
	m.in.SetValue("test")
	m.in.CursorEnd()
	m.ms.sel = 1
	m.render()
	a.set(m)
	writeShot(t, filepath.Join(dir, "screenshot-search.html"), a.mm.View())
}

func demoModel(t *testing.T) *actionModel {
	rec := &recorder{}
	m := newTestModel(t)
	m.api = tg.NewClient(rec)
	m.selfKey = "u100"
	a := &actionModel{mm: m, rec: rec}
	a.send(tea.WindowSizeMsg{Width: shotW, Height: shotH})
	a.send(tea.FocusMsg{})
	y, mo, d := time.Now().Date() // today, so the picture always reads "Hôm nay" and today's times
	at := func(h, mi int) time.Time { return time.Date(y, mo, d, h, mi, 0, 0, time.Local) }
	a.send(chatsMsg{chats: []chat{
		{key: "u1", title: "Minh Anh", last: "Tối nay họp lúc mấy giờ nhỉ?", date: int(at(15, 5).Unix()), peer: &tg.InputPeerUser{UserID: 1}, contact: true,
			seen: presence{until: int(time.Now().Add(time.Hour).Unix())}},
		{key: "c9", title: "Nhóm Dev Go Việt Nam", last: "Nhung: [file] bao-cao-hieu-nang.pdf", date: int(at(15, 9).Unix()), peer: &tg.InputPeerChannel{ChannelID: 9}, pinned: true},
		{key: "u2", title: "Lan Phương", last: "Bạn: ok nhé, mai gặp", date: int(at(9, 32).Unix()), peer: &tg.InputPeerUser{UserID: 2}, contact: true,
			lastOut: true, lastID: 30, readOut: 30},
		{key: "c3", title: "Thông báo công ty", last: "Hành chính: nghỉ lễ 2/9", unread: 5, date: int(at(8, 0).Add(-24 * time.Hour).Unix()), peer: &tg.InputPeerChannel{ChannelID: 3}},
		{key: "u4", title: "Quang Huy", last: "[location] 10.77690, 106.70090", date: int(at(8, 0).Add(-72 * time.Hour).Unix()), peer: &tg.InputPeerUser{UserID: 4}},
		{key: "u5", title: "Bố", last: "Con nhớ ăn cơm nhé", date: int(at(8, 0).Add(-120 * time.Hour).Unix()), peer: &tg.InputPeerUser{UserID: 5}, contact: true},
	}})
	work := &tg.DialogFilter{ID: 2, Title: tg.TextWithEntities{Text: "Công việc"}, Groups: true, Broadcasts: true}
	family := &tg.DialogFilter{ID: 3, Title: tg.TextWithEntities{Text: "Gia đình"}, IncludePeers: []tg.InputPeerClass{&tg.InputPeerUser{UserID: 5}}}
	friends := &tg.DialogFilter{ID: 4, Title: tg.TextWithEntities{Text: "Bạn bè"}, IncludePeers: []tg.InputPeerClass{&tg.InputPeerUser{UserID: 1}, &tg.InputPeerUser{UserID: 2}}}
	a.send(foldersMsg{list: toFolders([]tg.DialogFilterClass{&tg.DialogFilterDefault{}, work, family, friends}, "u100")})
	a.send(typingMsg{key: "u1", who: "u1", action: "đang soạn tin"})
	a.send(typingMsg{key: "c9", who: "u12", name: "Hà", action: "đang soạn tin"})
	for i := 0; i < 2; i++ { // open the group
		a.send(tea.KeyMsg{Type: tea.KeyCtrlN})
	}
	msg := func(id int, text string, es ...tg.MessageEntityClass) *tg.Message {
		return &tg.Message{ID: id, Message: text, Entities: es}
	}
	u16 := func(s, sub string) (int, int) { // UTF-16 offset and length of sub in s
		i := strings.Index(s, sub)
		return len([]rune(s[:i])) + strings.Count(s[:i], "😀"), len([]rune(sub))
	}
	t1 := "Mọi người ơi, review giúp mình PR #482 nhé: https://github.com/team/app/pull/482"
	o1, l1 := u16(t1, "PR #482")
	o1b, l1b := u16(t1, "https://github.com/team/app/pull/482")
	t2 := "Ok, để mình xem. Có cần chạy thêm test không?"
	o2, l2 := u16(t2, "Có cần chạy thêm test không?")
	t3 := "Mình viết thử hàm tính tổng nhé:\nfunc Sum(xs []int) int {\n    total := 0\n    for _, x := range xs {\n        total += x\n    }\n    return total\n}"
	o3, _ := u16(t3, "func Sum")
	t4 := "Gọn rồi, thêm test cho slice rỗng là merge được"
	t5 := "Đã thêm, mọi người xem lại giúp. Tài liệu ở đây"
	o5, l5 := u16(t5, "ở đây")
	pdf := docMsg(7, "bao-cao-hieu-nang.pdf", "application/pdf", 2400000)
	pdf.Message = "Gửi cả nhà báo cáo hiệu năng tuần này, có so sánh với tuần trước nhé"
	geo := &tg.Message{ID: 8, Media: &tg.MessageMediaGeo{Geo: &tg.GeoPoint{Lat: 10.7769, Long: 106.7009}}}
	l := func(id int, from, sender string, h, mi int, m *tg.Message, out bool, reply int) line {
		text := strings.TrimSpace(m.Message + " " + mediaTag(m.Media))
		return line{id: id, from: from, sender: sender, out: out, text: text, at: at(h, mi), msg: m, replyTo: reply, segs: segments(m.Message, m.Entities)}
	}
	ls := []line{
		l(1, "Bình", "u11", 14, 56, msg(1, t1, &tg.MessageEntityBold{Offset: o1, Length: l1}, &tg.MessageEntityURL{Offset: o1b, Length: l1b}), false, 0),
		l(2, "Hà", "u12", 14, 57, msg(2, t2, &tg.MessageEntityItalic{Offset: o2, Length: l2}), false, 1),
		l(3, "Bình", "u11", 14, 59, msg(3, t3, &tg.MessageEntityPre{Offset: o3, Length: len([]rune(t3)) - o3, Language: "go"}), false, 0),
		l(4, "Bạn", "", 15, 1, msg(4, t4), true, 2),
		l(5, "Bình", "u11", 15, 3, msg(5, t5, &tg.MessageEntityTextURL{Offset: o5, Length: l5, URL: "https://go.dev/doc"}), false, 4),
		l(7, "Nhung", "u13", 15, 9, pdf, false, 0),
		l(8, "Quang Huy", "u4", 15, 10, geo, false, 0),
	}
	ls[1].reacts = []reaction{{"♥", 3, true}, {"👍", 1, false}}
	ls[1].seeWho = true
	ls[4].mentioned = true
	a.send(histMsg{mk: "c9", key: "c9", lines: ls})
	a.send(pinMsg{mk: "c9", list: []pinned{{1, "Quy định nhóm: không spam, gửi code trong khối code, hỏi rõ ràng"}}})
	return a
}

func searchDemo() []line {
	at := func(d, h, mi int) time.Time { return time.Date(2026, 11, d, h, mi, 0, 0, time.Local) }
	return []line{
		{id: 2, from: "Hà", text: "Ok, để mình xem. Có cần chạy thêm test không?", at: at(17, 14, 57)},
		{id: 4, from: "Bạn", text: "Gọn rồi, thêm test cho slice rỗng là merge được", at: at(17, 15, 1)},
		{id: 31, from: "Bình", text: "CI đang đỏ vì test tích hợp chạy quá 5 phút, mình tăng timeout lên 10 phút nhé", at: at(14, 10, 22)},
		{id: 27, from: "Hà", text: "Ai viết test cho phần thanh toán chưa? Sprint này cần xong", at: at(12, 9, 5)},
		{id: 18, from: "Minh Anh", text: "Gửi mọi người checklist test trước khi release", at: at(3, 16, 40)},
	}
}

// writeShot turns a frame into HTML: each SGR colour, bold, italic, underline, strike and reverse becomes a span.
func writeShot(t *testing.T, path, frame string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><meta charset="utf-8"><style>
html,body{margin:0;background:#ff00ff}
pre{margin:0;display:inline-block;white-space:pre;background:` + cBg + `;color:` + cFg + `;font:15px/18px Menlo,monospace;padding:0}
div{height:18px;white-space:pre}
span{display:inline-block;height:18px;vertical-align:top}
</style><pre><div>`)
	type st struct {
		fg, bg                    string
		bold, it, ul, strike, rev bool
		dim                       bool
	}
	var s st
	open := false
	span := func() {
		if open {
			b.WriteString("</span>")
		}
		fg, bg := s.fg, s.bg
		if s.rev {
			fg, bg = bg, fg
			if fg == "" {
				fg = cBg
			}
			if bg == "" {
				bg = cFg
			}
		}
		var css []string
		if fg != "" {
			css = append(css, "color:"+fg)
		}
		if bg != "" {
			css = append(css, "background:"+bg)
		}
		if s.bold {
			css = append(css, "font-weight:bold")
		}
		if s.it {
			css = append(css, "font-style:italic")
		}
		var deco []string
		if s.ul {
			deco = append(deco, "underline")
		}
		if s.strike {
			deco = append(deco, "line-through")
		}
		if len(deco) > 0 {
			css = append(css, "text-decoration:"+strings.Join(deco, " "))
		}
		if s.dim {
			css = append(css, "opacity:.6")
		}
		b.WriteString(`<span style="` + strings.Join(css, ";") + `">`)
		open = true
	}
	hex := func(r, g, bl string) string {
		ri, _ := strconv.Atoi(r)
		gi, _ := strconv.Atoi(g)
		bi, _ := strconv.Atoi(bl)
		return fmt.Sprintf("#%02x%02x%02x", ri, gi, bi)
	}
	for i := 0; i < len(frame); {
		c := frame[i]
		if c == '\n' { // every row is its own block of exactly one line height, its spans fill it
			if open {
				b.WriteString("</span>")
				open = false
			}
			b.WriteString("</div><div>")
			span()
			i++
			continue
		}
		if c != 0x1b {
			j := i
			for j < len(frame) && frame[j] != 0x1b && frame[j] != '\n' {
				j++
			}
			b.WriteString(html.EscapeString(frame[i:j]))
			i = j
			continue
		}
		if i+1 < len(frame) && frame[i+1] == ']' { // OSC: skip to BEL or ST
			j := i + 2
			for j < len(frame) && frame[j] != '\a' && !(frame[j] == 0x1b && j+1 < len(frame) && frame[j+1] == '\\') {
				j++
			}
			if j < len(frame) && frame[j] == 0x1b {
				j++
			}
			i = j + 1
			continue
		}
		if i+1 < len(frame) && frame[i+1] == '[' {
			j := i + 2
			for j < len(frame) && (frame[j] < 0x40 || frame[j] > 0x7e) {
				j++
			}
			if j < len(frame) && frame[j] == 'm' {
				p := strings.Split(frame[i+2:j], ";")
				for k := 0; k < len(p); k++ {
					switch p[k] {
					case "", "0":
						s = st{}
					case "1":
						s.bold = true
					case "2":
						s.dim = true
					case "3":
						s.it = true
					case "4":
						s.ul = true
					case "7":
						s.rev = true
					case "9":
						s.strike = true
					case "22":
						s.bold, s.dim = false, false
					case "23":
						s.it = false
					case "24":
						s.ul = false
					case "27":
						s.rev = false
					case "29":
						s.strike = false
					case "39":
						s.fg = ""
					case "49":
						s.bg = ""
					case "38", "48":
						if k+4 < len(p) && p[k+1] == "2" {
							col := hex(p[k+2], p[k+3], p[k+4])
							if p[k] == "38" {
								s.fg = col
							} else {
								s.bg = col
							}
							k += 4
						}
					}
				}
				span()
			}
			i = j + 1
			continue
		}
		i++
	}
	if open {
		b.WriteString("</span>")
	}
	b.WriteString("</div></pre>")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shotScenes draws the other README pictures, each from a fresh demo: the right-click menu of a message, who
// reacted, adding a chat to a folder, a video being sent, and a private chat beside the folded list.
func shotScenes(t *testing.T, dir string) {
	// right-click on Hà's message: quick reactions on top, then the actions
	a := demoModel(t)
	m := a.m()
	m.openMenu(60, msgRow(t, m, 2))
	writeShot(t, filepath.Join(dir, "screenshot-menu.html"), m.View())

	// … › Ai đã thả react
	a = demoModel(t)
	a.rec.users = []tg.UserClass{
		&tg.User{ID: 11, FirstName: "Bình"}, &tg.User{ID: 13, FirstName: "Nhung"}, &tg.User{ID: 4, FirstName: "Quang", LastName: "Huy"},
		&tg.User{ID: 100, FirstName: "Tôi", Self: true},
	}
	a.rec.reactors = []tg.MessagePeerReaction{
		{PeerID: &tg.PeerUser{UserID: 100}, Reaction: &tg.ReactionEmoji{Emoticon: "♥"}, My: true},
		{PeerID: &tg.PeerUser{UserID: 11}, Reaction: &tg.ReactionEmoji{Emoticon: "♥"}},
		{PeerID: &tg.PeerUser{UserID: 13}, Reaction: &tg.ReactionEmoji{Emoticon: "♥"}},
		{PeerID: &tg.PeerUser{UserID: 4}, Reaction: &tg.ReactionEmoji{Emoticon: "👍"}},
	}
	m = a.m()
	m.openMenu(60, msgRow(t, m, 2))
	for i, it := range m.menu.items {
		if it.label == "Ai đã thả react" {
			a.set(m)
			cmd := m.pickMenu(i)
			a.set(m)
			a.run(cmd)
			break
		}
	}
	writeShot(t, filepath.Join(dir, "screenshot-reactions.html"), a.mm.View())

	// right-click Lan Phương in the list › Thư mục
	a = demoModel(t)
	m = a.m()
	m.openChatMenu(m.chats[m.index("u2")], 4, listY0+2*itemRows+1)
	for _, it := range m.menu.items {
		if it.label == "Thư mục" {
			it.run(m)
		}
	}
	writeShot(t, filepath.Join(dir, "screenshot-folders.html"), m.View())

	// a video on its way: the progress in the title row, Ctrl+X in the hints
	a = demoModel(t)
	m = a.m()
	m.browse, m.bsel = false, 0
	m.in.Focus()
	x := newXfer("demo-sprint-12.mp4", 350<<20, false) // a long name: the progress must still show
	m.typing = map[string][]typer{}
	x.parts[1] = 147 << 20
	m.xfers, m.note = []*xfer{x}, x.text()
	m.render()
	writeShot(t, filepath.Join(dir, "screenshot-upload.html"), m.View())

	// a private chat with the list folded into initials: online status, read marks
	a = demoModel(t)
	m = a.m()
	m.folded = true
	a.set(m)
	a.send(tea.WindowSizeMsg{Width: shotW, Height: shotH})
	m = a.m()
	m.typing = map[string][]typer{}
	a.set(m)
	y, mo, d := time.Now().Date()
	at := func(h, mi int) time.Time { return time.Date(y, mo, d, h, mi, 0, 0, time.Local) }
	m = a.m()
	m.open(m.index("u1"))
	a.set(m)
	a.send(histMsg{mk: "u1", key: "u1", lines: []line{
		{id: 20, from: "Bạn", out: true, text: "Chiều nay mình gửi bản thiết kế mới nhé", at: at(14, 2)},
		{id: 21, from: "Bạn", out: true, text: "Có cả phần màn hình gọn cho cửa sổ hẹp", at: at(14, 3)},
		{id: 22, from: "Minh Anh", sender: "u1", text: "Tuyệt, gửi luôn đi để mình xem trước buổi họp", at: at(14, 40)},
		{id: 23, from: "Bạn", out: true, text: "Đã gửi vào nhóm rồi đó", at: at(15, 1)},
		{id: 24, from: "Minh Anh", sender: "u1", text: "Tối nay họp lúc mấy giờ nhỉ?", at: at(15, 5)},
	}})
	m = a.m()
	m.chats[m.index("u1")].readOut = 23
	m.render()
	a.set(m)
	writeShot(t, filepath.Join(dir, "screenshot-rail.html"), a.mm.View())
}

// msgRow is a screen row of message id in the open view.
func msgRow(t *testing.T, m *model, id int) int {
	t.Helper()
	for y := 0; y < m.h; y++ {
		if i, ok := m.msgIndexAt(y); ok && m.lines()[i].id == id {
			return y
		}
	}
	t.Fatalf("message %d is not on screen", id)
	return 0
}
