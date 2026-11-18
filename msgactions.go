package main

// The model side of actions.go: keys of the message-browsing mode (^B) and what happens with the results.

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const browseKeys = "↑↓ chọn   Enter mở/trả lời   c chép   e sửa   d xoá   r reaction   f chuyển tiếp   o link   m @mention   Esc thoát"

var actionVerb = map[string]string{
	"edit": "sửa tin", "delete": "xoá tin", "react": "thả reaction", "forward": "chuyển tiếp", "file": "gửi tệp", "geo": "gửi vị trí",
	"pin": "ghim tin", "mute": "đổi thông báo", "delchat": "xoá chat", "pinchat": "ghim chat",
}

// mine: a message of yours (in Saved Messages all of them are).
func (m *model) mine(l line, c *chat) bool { return l.out || (m.selfKey != "" && c.key == m.selfKey) }

// rawText is the text of a message as Telegram has it (a line we just sent has no server copy yet).
// editDraft is what goes into the input box to edit a message: its text with the formatting as marks.
func editDraft(l line) string {
	if l.msg != nil && len(l.msg.Entities) > 0 {
		return toMarkdown(l.msg)
	}
	return rawText(l)
}

func rawText(l line) string {
	if l.msg != nil {
		return l.msg.Message
	}
	return l.text
}

// numberSent gives the line we showed for a message we sent the id the server answered with, so it can be
// edited, deleted or reacted to straight away. If the server's own copy already arrived, that one stays.
func (m *model) numberSent(s sentMsg) {
	if s.id == 0 {
		return
	}
	ls := m.msgs[s.mk]
	dup := indexOfID(ls, s.id) >= 0
	for i := len(ls) - 1; i >= 0; i-- {
		if ls[i].out && ls[i].id == 0 && ls[i].text == s.text {
			if dup {
				m.msgs[s.mk] = append(ls[:i:i], ls[i+1:]...)
			} else {
				ls[i].id = s.id
			}
			if s.mk == m.mk() {
				m.render()
			}
			return
		}
	}
}

func (m *model) pinCmd(c chat, mk string) tea.Cmd {
	ctx, api, tp := m.ctx, m.api, 0
	if api == nil || c.peer == nil {
		return nil
	}
	if c.forum {
		tp = c.topic
	}
	return func() tea.Msg {
		list, err := loadPinned(ctx, api, c.peer, tp)
		if err != nil {
			return nil // the pinned line is a bonus: say nothing when it can not be read
		}
		return pinMsg{mk, list}
	}
}

// jumpPin shows the pinned message the row names and moves the row on to the next older one, like Telegram.
// One that is not loaded is opened with the messages around it (an old stretch, Esc goes back to the latest).
func (m *model) jumpPin() tea.Cmd {
	mk, c := m.mk(), m.curChat()
	ps := m.pins[mk]
	if c == nil || len(ps) == 0 {
		return nil
	}
	p := ps[min(m.pinAt[mk], len(ps)-1)]
	m.pinAt[mk] = (m.pinAt[mk] + 1) % len(ps)
	return m.jumpTo(p.id)
}

// jumpTo shows message id of the open view highlighted: at once when it is loaded, else with the messages
// around it (an old stretch; Esc goes back to the latest). Used by the pinned row and by reply quotes.
func (m *model) jumpTo(id int) tea.Cmd {
	if i := indexOfID(m.lines(), id); i >= 0 {
		return m.startBrowse(i)
	}
	c := m.curChat()
	if c == nil || m.api == nil {
		return nil
	}
	ctx, api, peer, mk, key, tp := m.ctx, m.api, c.peer, m.mk(), c.key, 0
	if c.forum {
		tp = c.topic
	}
	return func() tea.Msg {
		ls, err := loadAround(ctx, api, peer, tp, id)
		return aroundMsg{mk: mk, key: key, id: id, lines: ls, err: err, jump: true}
	}
}

// actionKey handles the keys of the browsing mode that act on the highlighted message l.
func (m *model) actionKey(s string, l line) (tea.Cmd, bool) {
	c := m.curChat()
	if c == nil || m.api == nil {
		return nil, false
	}
	switch s {
	case "e":
		if !m.mine(l, c) || l.id == 0 {
			return m.toastCmd("Chỉ sửa được tin của bạn đã gửi xong"), true
		}
		cp := l
		m.stash, m.edit = m.in.Value(), &cp
		m.browse = false
		m.in.Focus()
		m.in.SetValue(editDraft(l))
		m.in.CursorEnd()
		m.layout()
		m.render()
	case "d":
		if !m.mine(l, c) || l.id == 0 {
			return m.toastCmd("Chỉ xoá được tin của bạn đã gửi xong"), true
		}
		m.delID = l.id
		m.setPrompt("Xoá tin này với mọi người? y = xoá · phím khác = giữ")
	case "r":
		if l.id == 0 {
			return nil, true
		}
		cp := l
		m.react = &cp
		var b strings.Builder
		for i, e := range quickReactions {
			b.WriteString(strconv.Itoa(i+1) + e + "  ")
		}
		m.setPrompt("Reaction: " + b.String() + "· bấm lại reaction đã thả để bỏ")
	case "f":
		if l.id == 0 {
			return nil, true
		}
		cp := l
		m.fwd = &cp
		m.setPrompt("Chuyển tiếp: chọn chat đích bên trái, Enter để gửi · Esc huỷ")
		m.startSearch(true)
	case "o":
		links := linksOf(l.msg)
		if len(links) == 0 {
			return m.toastCmd("Tin này không có link"), true
		}
		if m.lkFor != l.id || l.id == 0 {
			m.lkFor, m.lkIdx = l.id, 0
		} else {
			m.lkIdx = (m.lkIdx + 1) % len(links)
		}
		u := links[m.lkIdx]
		if err := openURL(u); err != nil {
			return m.toastCmd("không mở được: " + err.Error()), true
		}
		note := "Đã mở " + u
		if len(links) > 1 {
			note += " (" + strconv.Itoa(m.lkIdx+1) + "/" + strconv.Itoa(len(links)) + ", bấm o để mở link kế)"
		}
		return m.toastCmd(note), true
	case "l":
		links := linksOf(l.msg)
		if len(links) == 0 {
			return m.toastCmd("Tin này không có link"), true
		}
		return copyCmd(strings.Join(links, "\n"), "link"), true
	case "m":
		ls := m.lines()
		for j := m.bsel - 1; j >= 0; j-- {
			if ls[j].mentioned {
				m.bsel = j
				m.render()
				m.reveal()
				return nil, true
			}
		}
		return m.toastCmd("Không có @mention nào phía trên trong các tin đã tải"), true
	default:
		return nil, false
	}
	return nil, true
}

// answerPrompt takes the key that answers a question asked by actionKey (delete? which reaction?).
func (m *model) answerPrompt(s string) tea.Cmd {
	c, ctx, api := m.curChat(), m.ctx, m.api
	if m.delID != 0 {
		id := m.delID
		m.delID = 0
		m.setPrompt("")
		if s == "y" && c != nil {
			return m.act("delete", m.mk(), id, "", func() error { return deleteMsg(ctx, api, c.peer, id) })
		}
		return nil
	}
	l := m.react
	m.react = nil
	m.setPrompt("")
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > len(quickReactions) || c == nil || l == nil {
		return nil
	}
	e := quickReactions[n-1]
	for _, r := range l.reacts {
		if r.mine && r.emoji == e {
			e = "" // the same one again takes it back
			break
		}
	}
	mk, id := m.mk(), l.id
	return func() tea.Msg {
		rs, ok, err := sendReaction(ctx, api, c.peer, id, e)
		return actMsg{kind: "react", mk: mk, id: id, text: e, key: c.key, rs: rs, rsOK: ok, err: err}
	}
}

// act runs one request in the background and reports it as an actMsg.
func (m *model) act(kind, mk string, id int, text string, do func() error) tea.Cmd {
	return func() tea.Msg { return actMsg{kind: kind, mk: mk, id: id, text: text, err: do()} }
}

func (m *model) finishEdit(text string) tea.Cmd {
	l, c, ctx, api := m.edit, m.curChat(), m.ctx, m.api
	m.edit = nil
	m.in.SetValue(m.stash)
	m.in.CursorEnd()
	m.layout()
	m.render()
	if l == nil || c == nil || text == strings.TrimSpace(editDraft(*l)) {
		return nil
	}
	return m.act("edit", m.mk(), l.id, text, func() error { return editText(ctx, api, c.peer, l.id, text) })
}

// forwardTo sends the message waiting in m.fwd to the chat highlighted in the search list.
func (m *model) forwardTo(vis []int) tea.Cmd {
	l, from := m.fwd, m.curChat()
	if l == nil || from == nil || m.sel >= len(vis) {
		return nil
	}
	to, src, ctx, api := m.chats[m.pick(vis[m.sel])], from.peer, m.ctx, m.api
	m.fwd = nil
	m.setPrompt("")
	m.stopSearch()
	do := m.act("forward", "", l.id, to.title, func() error { return forwardMsg(ctx, api, src, to.peer, l.id) })
	return func() tea.Msg {
		a := do().(actMsg)
		a.key = to.key
		return a
	}
}

// refresh fetches the newest messages of the open chat again when key is that chat: what we upload or
// forward is not announced back to us, unlike what other people send.
func (m *model) refresh(key string) tea.Cmd {
	if c := m.curChat(); c != nil && c.key == key {
		return m.loadCmd(*c, m.mk())
	}
	return nil
}

func (m *model) onAct(a actMsg) tea.Cmd {
	if a.err != nil {
		m.err = actionVerb[a.kind] + " lỗi: " + errText(a.err)
		return nil
	}
	m.err = ""
	ls := m.msgs[a.mk]
	i := indexOfID(ls, a.id)
	switch a.kind {
	case "edit":
		if i >= 0 {
			plain, fmts := parseMarkdown(a.text)
			tag := ""
			if ls[i].msg != nil {
				cp := *ls[i].msg
				cp.Message, cp.Entities = plain, fmts
				ls[i].msg, tag = &cp, mediaTag(cp.Media)
			}
			ls[i].segs = segments(plain, fmts)
			ls[i].text = strings.TrimSpace(clean(plain) + " " + tag)
			if a.mk == m.mk() {
				m.render()
			}
		}
		return m.toastCmd("Đã sửa tin")
	case "delete":
		if i >= 0 {
			m.msgs[a.mk] = append(ls[:i:i], ls[i+1:]...)
			if a.mk == m.mk() {
				m.bsel = min(m.bsel, max(len(m.lines())-1, 0))
				m.render()
			}
		}
		return m.toastCmd("Đã xoá tin")
	case "react":
		note := "Đã thả " + a.text
		if a.text == "" {
			note = "Đã bỏ reaction"
		}
		if !a.rsOK {
			return tea.Batch(m.toastCmd(note), m.refresh(a.key)) // no update in the answer: read the message again
		}
		if i >= 0 {
			ls[i].reacts = a.rs
			if a.mk == m.mk() {
				m.render()
			}
		}
		return m.toastCmd(note)
	case "forward":
		return tea.Batch(m.toastCmd("Đã chuyển tiếp tới "+a.text), m.refresh(a.key))
	case "file", "geo":
		return tea.Batch(m.toastCmd("Đã gửi "+a.text), m.refresh(a.key))
	case "pin": // rsOK = it was an unpin
		note := "Đã ghim tin"
		if a.rsOK {
			note = "Đã bỏ ghim"
		}
		cmds := []tea.Cmd{m.toastCmd(note)}
		if c := m.curChat(); c != nil && c.key == a.key {
			cmds = append(cmds, m.pinCmd(*c, a.mk))
		}
		return tea.Batch(cmds...)
	case "mute": // rsOK = muted now
		if i := m.index(a.key); i >= 0 {
			m.chats[i].muted = a.rsOK
		}
		if a.rsOK {
			return m.toastCmd("Đã tắt thông báo " + a.text)
		}
		return m.toastCmd("Đã bật thông báo " + a.text)
	case "delchat":
		return m.dropChat(a.key, a.text)
	case "pinchat": // rsOK = pinned now
		m.repin(a.key, a.rsOK)
		if a.rsOK {
			return m.toastCmd("Đã ghim " + a.text)
		}
		return m.toastCmd("Đã bỏ ghim " + a.text)
	}
	return nil
}

// repin moves a chat after (un)pinning it: a newly pinned chat goes to the top, like in Telegram; an unpinned
// one goes back among the others by the time of its last message.
func (m *model) repin(key string, pinned bool) {
	i := m.index(key)
	if i < 0 {
		return
	}
	c := m.chats[i]
	c.pinned = pinned
	rest := append(m.chats[:i:i], m.chats[i+1:]...)
	at := 0
	if !pinned {
		for at < len(rest) && (rest[at].pinned || rest[at].date > c.date) {
			at++
		}
	}
	m.chats = append(rest[:at:at], append([]chat{c}, rest[at:]...)...)
	m.fixTop()
}

// dropChat takes a deleted or left chat out of the list and its messages out of the cache; when it was open
// the view goes back to "pick a chat".
func (m *model) dropChat(key, title string) tea.Cmd {
	if i := m.index(key); i >= 0 {
		m.chats = append(m.chats[:i:i], m.chats[i+1:]...)
	}
	for mk := range m.msgs {
		if mk == key || strings.HasPrefix(mk, key+"#") {
			delete(m.msgs, mk)
		}
	}
	if m.cur == key {
		m.cur, m.browse, m.reply, m.edit = "", false, nil, nil
		m.in.SetValue("")
		m.layout()
		m.render()
		m.startSearch(true) // nothing open: back to picking a chat, as at start
	}
	m.fixTop()
	return m.toastCmd("Đã xoá " + title + " khỏi danh sách")
}
