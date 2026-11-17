package main

// Searching the text of the messages of the open chat (Ctrl+F): the server finds them, the results replace the
// message view as a list, and Enter on one loads the messages around it and highlights it.

import (
	"context"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

const (
	searchLimit  = 50 // results asked for; no paging
	aroundBefore = 20 // messages loaded around a result: this many newer ...
	aroundTotal  = 40 // ... and this many in all
)

type msearch struct {
	stash string // what was in the input box
	ran   string // the query the results are for
	res   []line // newest first
	sel   int
	busy  bool
	seq   int
}

type (
	searchMsg struct {
		seq   int
		q     string
		lines []line
		err   error
	}
	aroundMsg struct {
		mk, key string
		id      int
		lines   []line
		err     error
	}
)

// searchChat asks the server for the messages of a chat (of one forum topic when topic > 0) that contain q.
func searchChat(ctx context.Context, api *tg.Client, p tg.InputPeerClass, topic int, q string) ([]line, error) {
	req := &tg.MessagesSearchRequest{Peer: p, Q: q, Filter: &tg.InputMessagesFilterEmpty{}, Limit: searchLimit}
	if topic > 0 {
		req.TopMsgID = topic
	}
	res, err := api.MessagesSearch(ctx, req)
	if err != nil {
		return nil, err
	}
	var out []line
	for _, l := range linesOf(res) {
		if !l.event {
			out = append(out, l)
		}
	}
	return out, nil
}

// loadAround reads the messages around message id, oldest first.
func loadAround(ctx context.Context, api *tg.Client, p tg.InputPeerClass, topic, id int) ([]line, error) {
	var res tg.MessagesMessagesClass
	var err error
	if topic > 0 {
		res, err = api.MessagesGetReplies(ctx, &tg.MessagesGetRepliesRequest{Peer: p, MsgID: topic, OffsetID: id, AddOffset: -aroundBefore, Limit: aroundTotal})
	} else {
		res, err = api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{Peer: p, OffsetID: id, AddOffset: -aroundBefore, Limit: aroundTotal})
	}
	if err != nil {
		return nil, err
	}
	ls := linesOf(res)
	sort.Slice(ls, func(i, j int) bool { return ls[i].id < ls[j].id })
	fillQuotes(ctx, api, p, ls)
	return ls, nil
}

func (m *model) startMsgSearch() {
	c := m.curChat()
	if c == nil || m.api == nil {
		return
	}
	m.ms = &msearch{stash: m.in.Value()}
	m.browse, m.searching = false, false
	m.in.SetValue("")
	m.in.Focus()
	m.layout()
	m.render()
}

func (m *model) stopMsgSearch() {
	if m.ms == nil {
		return
	}
	m.in.SetValue(m.ms.stash)
	m.in.CursorEnd()
	m.ms = nil
	m.layout()
	m.render()
}

func (m *model) msBar() string {
	s := m.ms
	title := ""
	if c := m.curChat(); c != nil {
		title = c.title
	}
	switch {
	case s.busy:
		return "🔍 " + title + ": đang tìm…"
	case len(s.res) > 0:
		return "🔍 " + title + ": " + strconv.Itoa(len(s.res)) + " kết quả   (↑↓ chọn · Enter mở · gõ lại + Enter tìm tiếp · Esc đóng)"
	case s.ran != "":
		return "🔍 " + title + ": không thấy \"" + s.ran + "\"   (Esc đóng)"
	}
	return "🔍 Tìm trong " + title + ": gõ từ khoá rồi Enter   (Esc đóng)"
}

func (m *model) msKey(k tea.KeyMsg) tea.Cmd {
	s, c := m.ms, m.curChat()
	switch k.String() {
	case "esc":
		m.stopMsgSearch()
		return nil
	case "up", "down":
		if len(s.res) > 0 {
			s.sel = max(0, min(s.sel+map[string]int{"up": -1, "down": 1}[k.String()], len(s.res)-1))
			m.render()
		}
		return nil
	case "enter":
		q := strings.TrimSpace(m.in.Value())
		if c == nil {
			return nil
		}
		if q == s.ran && len(s.res) > 0 {
			l := s.res[s.sel]
			ctx, api, p, mk, key, tp := m.ctx, m.api, c.peer, m.mk(), c.key, 0
			if c.forum {
				tp = c.topic
			}
			return func() tea.Msg {
				ls, err := loadAround(ctx, api, p, tp, l.id)
				return aroundMsg{mk, key, l.id, ls, err}
			}
		}
		if q == "" {
			return nil
		}
		s.seq++
		s.busy, s.res = true, nil
		m.layout()
		m.render()
		ctx, api, p, seq, tp := m.ctx, m.api, c.peer, s.seq, 0
		if c.forum {
			tp = c.topic
		}
		return func() tea.Msg {
			ls, err := searchChat(ctx, api, p, tp, q)
			return searchMsg{seq, q, ls, err}
		}
	case "pgup", "pgdown", "ctrl+b", "ctrl+o", "ctrl+r":
		return nil
	}
	var cmd tea.Cmd
	m.in, cmd = m.in.Update(k)
	return cmd
}

func (m *model) onSearchResult(r searchMsg) {
	s := m.ms
	if s == nil || r.seq != s.seq {
		return
	}
	s.busy, s.ran, s.res, s.sel = false, r.q, r.lines, 0
	if r.err != nil {
		m.err = "tìm tin lỗi: " + errText(r.err)
	}
	m.layout()
	m.render()
	m.vp.GotoTop()
}

// onAround shows the messages around a search result and highlights it. The view then holds an old stretch, not
// the end of the chat: new messages are not added to it until Esc goes back to the latest.
func (m *model) onAround(a aroundMsg) tea.Cmd {
	if a.err != nil {
		m.err = "mở tin lỗi: " + errText(a.err)
		return nil
	}
	i := indexOfID(a.lines, a.id)
	if i < 0 || m.ms == nil || a.mk != m.mk() {
		return nil
	}
	m.stopMsgSearch()
	m.msgs[a.mk], m.more[a.mk], m.detached[a.mk] = a.lines, true, true
	m.bsel, m.browse = i, true
	m.in.Blur()
	m.layout()
	m.render()
	m.reveal()
	return m.thumbCmds()
}

// backToLatest leaves an old stretch of the chat (after a search) and reloads the newest messages.
func (m *model) backToLatest() tea.Cmd {
	mk, c := m.mk(), m.curChat()
	if c == nil || !m.detached[mk] {
		return nil
	}
	delete(m.detached, mk)
	m.layout()
	return m.loadCmd(*c, mk)
}

// renderResults draws the search results in place of the messages.
func (m *model) renderResults() {
	s := m.ms
	m.lineAt, m.hits, m.links, m.nLines = m.lineAt[:0], m.hits[:0], m.links[:0], len(s.res)
	rows := make([]string, len(s.res))
	for i, l := range s.res {
		text := flat(l.text)
		if at := strings.Index(strings.ToLower(text), strings.ToLower(s.ran)); at > 20 {
			text = string([]rune(text)[len([]rune(text[:at]))-20:])
			if sp := strings.IndexByte(text, ' '); sp >= 0 && sp < 20 { // start at a word, not in the middle of one
				text = text[sp+1:]
			}
			text = "…" + text
		}
		bar, head := " ", fg(cMuted).Render(l.at.Format("02/01 15:04"))+"  "+nameStyle(l.from).Render(l.from)+": "
		row := ansi.Truncate(bar+" "+head+text, max(m.vp.Width-1, 1), "…")
		if i == s.sel {
			row = paint("▌ "+ansi.Truncate(head+text, max(m.vp.Width-3, 1), "…"), m.vp.Width, cFg, cSel)
		}
		rows[i] = row
	}
	m.rows, m.rowOut = rows, make([]bool, len(rows))
	m.vp.SetContent(strings.Join(rows, "\n"))
	switch {
	case s.sel < m.vp.YOffset:
		m.vp.SetYOffset(s.sel)
	case s.sel >= m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(s.sel - m.vp.Height + 1)
	}
}
