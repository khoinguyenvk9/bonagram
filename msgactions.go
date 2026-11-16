package main

// The model side of actions.go: keys of the message-browsing mode (^B) and what happens with the results.

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

const browseKeys = "↑↓ chọn   Enter mở/trả lời   c chép   e sửa   d xoá   r reaction   f chuyển tiếp   o link   m @mention   Esc thoát"

var actionVerb = map[string]string{
	"edit": "sửa tin", "delete": "xoá tin", "react": "thả reaction", "forward": "chuyển tiếp", "file": "gửi tệp",
}

// mine: a message of yours (in Saved Messages all of them are).
func (m *model) mine(l line, c *chat) bool { return l.out || (m.selfKey != "" && c.key == m.selfKey) }

// rawText is the text of a message as Telegram has it (a line we just sent has no server copy yet).
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
		text, err := loadPinned(ctx, api, c.peer, tp)
		if err != nil {
			return nil // the pinned line is a bonus: say nothing when it can not be read
		}
		return pinMsg{mk, text}
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
		m.in.SetValue(rawText(l))
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
		if err := openExternal(u); err != nil {
			return m.toastCmd("không mở được: " + err.Error()), true
		}
		note := "Đã mở " + u
		if len(links) > 1 {
			note += " (" + strconv.Itoa(m.lkIdx+1) + "/" + strconv.Itoa(len(links)) + ", bấm o để mở link kế)"
		}
		return m.toastCmd(note), true
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
	if l == nil || c == nil || text == strings.TrimSpace(rawText(*l)) {
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
	to, src, ctx, api := m.chats[vis[m.sel]], from.peer, m.ctx, m.api
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
			tag := ""
			if ls[i].msg != nil {
				cp := *ls[i].msg
				cp.Message, cp.Entities = a.text, nil
				ls[i].msg, tag = &cp, mediaTag(cp.Media)
			}
			ls[i].segs = nil
			ls[i].text = strings.TrimSpace(clean(a.text) + " " + tag)
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
	case "file":
		return tea.Batch(m.toastCmd("Đã gửi "+a.text), m.refresh(a.key))
	}
	return nil
}
