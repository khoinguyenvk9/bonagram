package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/tg"
)

const (
	sideDef, sideMin, sideMax = 36, 24, 70
	minRight                  = 40 // room kept for the messages when the splitter is dragged
)

type config struct {
	maxMsgs                 int
	previews                bool
	previewRows, previewCnt int
	images                  string // "", "kitty" or "blocks": force how images are drawn
	fps                     int    // renderer frame rate cap
}

type thumb struct {
	rows []string
	done bool
}

type viewer struct {
	pic   picture
	path  string
	title string
}

type (
	chatsMsg struct {
		chats    []chat
		archived map[string]bool
		err      error
	}
	topicsMsg struct {
		key    string
		topics []topic
		err    error
	}
	histMsg struct {
		mk, key string
		lines   []line
		more    bool
		err     error
	}
	olderMsg struct {
		mk    string
		lines []line
		more  bool
		err   error
	}
	thumbMsg struct {
		id  int
		pic picture
		err error
	}
	// reactMsg: the reactions of one message changed. renderTick: time to redraw after a burst of them.
	reactMsg struct {
		key string
		id  int
		rs  []reaction
	}
	renderTick struct{}
	// editMsg: a message was edited (by its author, or by a bot streaming its answer).
	editMsg struct {
		key  string
		line line
	}
	viewMsg  struct{ v viewer }
	toastMsg struct{ text string }
	toastOff struct{ seq int }
	// sentMsg: the server accepted (or refused) text typed into chat view mk, and numbered it id.
	sentMsg struct {
		mk, text string
		id       int
		err      error
	}
	// actMsg: the result of an action on a message (edit, delete, react, forward, upload).
	actMsg struct {
		kind, mk, text string
		key            string // chat whose messages should be fetched again (a file or forward that landed there)
		id             int
		rs             []reaction // react: the message's reactions after the change
		rsOK           bool
		err            error
	}
	pinMsg struct{ mk, text string } // the pinned message of a chat/topic, "" when it has none
	newMsg struct {
		key, title, username string
		forum                bool
		peer                 tg.InputPeerClass // nil when the update carried no access hash
		line                 line
	}
)

type model struct {
	ctx     context.Context
	api     *tg.Client
	events  <-chan tea.Msg
	cfg     config
	track   func(context.Context, []chanTrack) // registers channels with the updates manager (may be nil)
	selfKey string                             // chat key of Saved Messages: everything in it is yours, although the server does not mark it "out"

	chats    []chat
	archived map[string]bool
	cur      string // key of the open chat
	top      int    // first visible row of the chat list
	side     int    // chat list width
	drag     bool

	msgs    map[string][]line // by chat, or chat#topic in a forum
	more    map[string]bool   // older messages may exist
	told    map[string]bool   // "start of chat" already shown
	loading bool
	redraw  bool // a renderTick is already scheduled
	thumbs  map[int]*thumb
	lineAt  []int     // first content line of each message of the open view
	hits    []copyHit // copy buttons of the open view
	cpFor   int       // message whose code block was copied last, and which block
	cpIdx   int
	nLines  int

	searching bool
	search    textinput.Model
	sel       int // highlighted result while searching
	browse    bool
	bsel      int // highlighted message while browsing
	reply     *line
	edit      *line  // own message being edited (the text is in the input)
	stash     string // what was in the input before editing started
	fwd       *line  // message waiting for a target chat (chosen in the search list)
	react     *line  // message waiting for a reaction key
	delID     int    // message waiting for the y that confirms deleting it
	prompt    string // question shown above the input
	lkFor     int    // message whose link was opened last, and which link
	lkIdx     int
	drafts    map[string]string // unsent text per chat/topic
	pins      map[string]string // pinned message per chat/topic

	help    bool // the guide (F1) is open
	helpTop int  // first line of the guide shown

	viewer *viewer
	vp     viewport.Model
	in     textinput.Model

	w, h     int
	focused  bool
	err      string
	note     string
	toast    string
	toastSeq int
	title    string
}

func newModel(ctx context.Context, api *tg.Client, events <-chan tea.Msg, cfg config, side int) model {
	in := textinput.New()
	in.Cursor.SetMode(cursor.CursorHide) // the real terminal cursor is used instead, see setCursor
	in.Prompt = ""
	in.Placeholder = "Nhắn tin…"
	in.PlaceholderStyle = fg(cMuted)
	in.TextStyle = fg(cFg)
	s := textinput.New()
	s.Cursor.SetMode(cursor.CursorHide)
	s.Prompt = " "
	s.Placeholder = "Tìm chat…"
	s.PlaceholderStyle = fg(cMuted)
	s.TextStyle = fg(cFg)
	s.Focus() // start in the search box
	return model{
		ctx: ctx, api: api, events: events, cfg: cfg, side: side, focused: true, searching: true,
		msgs: map[string][]line{}, more: map[string]bool{}, told: map[string]bool{}, thumbs: map[int]*thumb{},
		archived: map[string]bool{}, drafts: map[string]string{}, pins: map[string]string{}, vp: viewport.New(0, 0), in: in, search: s,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.loadChatsCmd(), m.wait())
}

// --- commands -----------------------------------------------------------------

func (m model) wait() tea.Cmd {
	return func() tea.Msg {
		select {
		case ev := <-m.events:
			return ev
		case <-m.ctx.Done():
			return tea.QuitMsg{}
		}
	}
}

func (m model) loadChatsCmd() tea.Cmd {
	ctx, api, track := m.ctx, m.api, m.track
	return func() tea.Msg {
		cs, ar, ts, err := loadChats(ctx, api)
		if track != nil && len(ts) > 0 {
			go track(ctx, ts) // may take a while for many channels: do not hold the chat list back
		}
		return chatsMsg{cs, ar, err}
	}
}

func (m model) topicsCmd(c chat) tea.Cmd {
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		ts, err := loadTopics(ctx, api, c.peer)
		return topicsMsg{c.key, ts, err}
	}
}

func (m model) loadCmd(c chat, mk string) tea.Cmd {
	ctx, api, tp := m.ctx, m.api, 0
	if c.forum {
		tp = c.topic
	}
	return func() tea.Msg {
		ls, more, err := loadMessages(ctx, api, c.peer, tp, 0, page)
		return histMsg{mk, c.key, ls, more, err}
	}
}

// readCmd marks the open chat (or forum topic) read up to message `upto`.
func (m model) readCmd(c chat, upto int) tea.Cmd {
	ctx, api, tp := m.ctx, m.api, 0
	if c.forum {
		tp = c.topic
	}
	return func() tea.Msg {
		_ = markRead(ctx, api, c.peer, tp, upto) // best effort: the badge is already cleared locally
		return nil
	}
}

func (m model) sendCmd(c chat, mk, text string, replyTo int) tea.Cmd {
	ctx, api, tp := m.ctx, m.api, 0
	if c.forum {
		tp = c.topic
	}
	return func() tea.Msg {
		id, err := sendText(ctx, api, c.peer, text, replyTo, tp)
		return sentMsg{mk, text, id, err}
	}
}

func (m *model) toastCmd(text string) tea.Cmd {
	m.toastSeq++
	m.toast = clean(text)
	seq := m.toastSeq
	return tea.Tick(6*time.Second, func(time.Time) tea.Msg { return toastOff{seq} })
}

// --- helpers --------------------------------------------------------------------

func (m model) index(key string) int {
	for i := range m.chats {
		if m.chats[i].key == key {
			return i
		}
	}
	return -1
}

func (m *model) curChat() *chat {
	if i := m.index(m.cur); i >= 0 {
		return &m.chats[i]
	}
	return nil
}

func hasTopics(c *chat) bool { return c.forum && len(c.topics) > 0 }

// mkFor is the message-cache key of chat c (and forum topic tp).
func mkFor(c *chat, tp int) string {
	if hasTopics(c) {
		return c.key + "#" + strconv.Itoa(tp)
	}
	return c.key
}

func (m *model) mk() string {
	if c := m.curChat(); c != nil {
		return mkFor(c, c.topic)
	}
	return ""
}

func (m *model) lines() []line { return m.msgs[m.mk()] }

func (m *model) topicOf(c *chat) *topic {
	for i := range c.topics {
		if c.topics[i].id == c.topic {
			return &c.topics[i]
		}
	}
	return nil
}

func (m *model) unreadHere(c *chat) int {
	if t := m.topicOf(c); t != nil {
		return t.unread
	}
	return c.unread
}

func (m *model) clearUnread(c *chat) {
	if t := m.topicOf(c); t != nil {
		c.unread = max(0, c.unread-t.unread)
		t.unread = 0
		return
	}
	c.unread = 0
}

func lastID(ls []line) int {
	for i := len(ls) - 1; i >= 0; i-- {
		if ls[i].id != 0 {
			return ls[i].id
		}
	}
	return 0
}

func (m *model) bump(key string) {
	if i := m.index(key); i >= 0 {
		c := m.chats[i]
		m.chats = placed(append(m.chats[:i:i], m.chats[i+1:]...), c, i)
	}
}

// placed puts chat c, which has fresh activity and is not in rest, back into the list: at the top of the
// unpinned chats, or, if it is pinned, in the slot it had (was). Pinned chats never move below activity.
func placed(rest []chat, c chat, was int) []chat {
	at := 0
	if c.pinned {
		at = min(max(was, 0), len(rest))
	} else {
		for at < len(rest) && rest[at].pinned {
			at++
		}
	}
	out := make([]chat, 0, len(rest)+1)
	return append(append(append(out, rest[:at]...), c), rest[at:]...)
}

// filtering: the search box has text, so the list shows only the matches.
func (m *model) filtering() bool { return strings.TrimSpace(m.search.Value()) != "" }

// visible lists the chat indexes shown on the left: all of them, or the search matches.
func (m *model) visible() []int {
	q := fold(strings.TrimSpace(m.search.Value()))
	out := make([]int, 0, len(m.chats))
	for i := range m.chats {
		if q == "" || strings.Contains(fold(m.chats[i].title+" "+m.chats[i].username), q) {
			out = append(out, i)
		}
	}
	return out
}

func (m *model) listRows() int { return max((m.h-1-listY0)/itemRows, 1) }

// fixTop keeps the highlighted chat inside the visible rows.
func (m *model) fixTop() {
	vis, pos := m.visible(), -1
	for p, i := range vis {
		if (m.filtering() && p == m.sel) || (!m.filtering() && m.chats[i].key == m.cur) {
			pos = p
		}
	}
	n := m.listRows()
	switch {
	case pos < 0:
		m.top = min(m.top, max(len(vis)-1, 0))
	case pos < m.top:
		m.top = pos
	case pos >= m.top+n:
		m.top = pos - n + 1
	}
}

// msgY is the screen row where the message view starts.
func (m *model) msgY() int {
	y := headRows
	if c := m.curChat(); c != nil && hasTopics(c) {
		y += 2
	}
	if m.pins[m.mk()] != "" {
		y++ // the pinned message row
	}
	return y
}

// barText is the row above the input: a question, the message being edited, or the one being answered.
func (m *model) barText() string {
	switch {
	case m.prompt != "":
		return m.prompt
	case m.edit != nil:
		return "✎ Sửa tin: " + m.edit.text + "   (Enter lưu · Esc huỷ)"
	case m.reply != nil:
		return "↩ " + m.reply.from + ": " + m.reply.text
	}
	return ""
}

func (m *model) setPrompt(s string) {
	m.prompt = clean(s)
	m.layout()
	m.render()
}

func (m *model) draftKey() string {
	if c := m.curChat(); c != nil {
		return c.key + "#" + strconv.Itoa(c.topic)
	}
	return ""
}

// leave runs before the open chat/topic changes: it keeps the unsent text as that chat's draft and drops
// anything half done (reply, edit, a question waiting for a key).
func (m *model) leave() {
	if k := m.draftKey(); k != "" {
		text := m.in.Value()
		if m.edit != nil {
			text = m.stash
		}
		if text == "" {
			delete(m.drafts, k)
		} else {
			m.drafts[k] = text
		}
	}
	m.reply, m.edit, m.fwd, m.react, m.delID, m.prompt, m.browse = nil, nil, nil, nil, 0, "", false
	m.in.SetValue("")
}

// arrive puts back the draft of the chat/topic that was just opened.
func (m *model) arrive() {
	m.in.SetValue(m.drafts[m.draftKey()])
	m.in.CursorEnd()
}

func (m *model) layout() {
	rows := m.msgY() + composeRow + 1 // + the keys bar
	if m.barText() != "" {
		rows++
	}
	m.vp.Width = max(m.w-m.side-1, 10)
	m.vp.Height = max(m.h-rows, 1)
	m.in.Width = max(m.vp.Width-9, 1)
	m.search.Width = max(m.side-7, 1)
}

func (m *model) clampSide() {
	m.side = max(sideMin, min(m.side, sideMax, m.w-minRight))
}

func (m model) windowTitle() string {
	n := 0
	for _, c := range m.chats {
		if !c.muted {
			n += c.unread
		}
	}
	if n == 0 {
		return "bonagram"
	}
	return fmt.Sprintf("(%d) bonagram", n)
}

// osc is an OSC 777 desktop notification; control characters and ';' are dropped because chat names
// and message text come from outside and could end the sequence early.
func osc(title, body string) string {
	clean := func(s string, n int) string {
		r := []rune(strings.Map(func(r rune) rune {
			switch {
			case r == '\r' || r == '\n' || r == '\t':
				return ' '
			case unicode.IsControl(r):
				return -1
			}
			return r
		}, s))
		return string(r[:min(len(r), n)])
	}
	return "\x1b]777;notify;" + strings.ReplaceAll(clean(title, 80), ";", ",") + ";" + clean(body, 120) + "\x07"
}

// --- update ----------------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	if t := m.windowTitle(); t != m.title {
		m.title = t
		cmd = tea.Batch(cmd, tea.SetWindowTitle(t))
	}
	return m, cmd
}

func (m *model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.clampSide()
		m.layout()
		m.render()
		m.fixTop()

	case tea.FocusMsg:
		m.focused = true
		if c := m.curChat(); c != nil && m.unreadHere(c) > 0 {
			up := lastID(m.lines())
			m.clearUnread(c)
			return m.readCmd(*c, up)
		}
	case tea.BlurMsg:
		m.focused = false

	case chatsMsg:
		if msg.err != nil {
			m.err = "tải danh sách chat lỗi: " + msg.err.Error()
			return nil
		}
		old := map[string]chat{}
		for _, c := range m.chats {
			old[c.key] = c
		}
		for i, c := range msg.chats { // keep what only this app knows: topics and the selected one
			o, ok := old[c.key]
			if !ok {
				continue
			}
			if c.topics == nil { // not fetched this time: keep what we had
				msg.chats[i].topics, msg.chats[i].topic = o.topics, o.topic
				continue
			}
			for _, t := range c.topics { // keep the topic the user was in, if it still exists
				if t.id == o.topic {
					msg.chats[i].topic = o.topic
				}
			}
		}
		m.chats, m.archived = msg.chats, msg.archived
		m.fixTop()

	case topicsMsg:
		i := m.index(msg.key)
		if i < 0 {
			return nil
		}
		c := &m.chats[i]
		if msg.err != nil {
			m.err = "tải topic lỗi: " + msg.err.Error()
		}
		c.topics = append([]topic{}, msg.topics...) // non-nil: loaded, even if empty
		c.unread = sumUnread(c.topics)
		if m.topicOf(c) == nil {
			c.topic = 0
			if len(c.topics) > 0 {
				c.topic = c.topics[0].id
			}
		}
		if c.key == m.cur {
			return m.enter(c)
		}

	case histMsg:
		if msg.err != nil {
			m.err = "tải tin nhắn lỗi: " + msg.err.Error()
			return nil
		}
		m.msgs[msg.mk], m.more[msg.mk] = msg.lines, msg.more
		if c := m.curChat(); c != nil && msg.mk == m.mk() {
			m.render()
			m.vp.GotoBottom()
			cmds := []tea.Cmd{m.thumbCmds()}
			if c.forum && c.topic > 0 && m.unreadHere(c) > 0 { // a topic is read up to a message id: needs the list
				up := lastID(msg.lines)
				m.clearUnread(c)
				cmds = append(cmds, m.readCmd(*c, up))
			}
			return tea.Batch(cmds...)
		}

	case olderMsg:
		m.loading = false
		if msg.err != nil {
			m.err = "tải tin cũ lỗi: " + msg.err.Error()
			return nil
		}
		cur, has := m.msgs[msg.mk]
		if !has {
			return nil
		}
		m.more[msg.mk] = msg.more && len(msg.lines) > 0
		if len(msg.lines) == 0 {
			return nil
		}
		anchor, delta := lastIDFirst(cur), m.vp.YOffset
		m.msgs[msg.mk] = append(msg.lines, cur...)
		if msg.mk == m.mk() {
			m.render()
			if i := indexOfID(m.lines(), anchor); i >= 0 && i < len(m.lineAt) {
				m.vp.SetYOffset(m.lineAt[i] + delta) // keep the message the user was looking at in place
			}
			return m.thumbCmds()
		}

	case thumbMsg:
		if t := m.thumbs[msg.id]; t != nil {
			t.rows, t.done = msg.pic.rows, true
			m.render()
		}

	case viewMsg:
		v := msg.v
		m.viewer = &v
	case toastMsg:
		return m.toastCmd(msg.text)
	case toastOff:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
	case sentMsg:
		if msg.err != nil {
			m.err = "gửi lỗi: " + msg.err.Error()
			return nil
		}
		m.numberSent(msg)
	case actMsg:
		return m.onAct(msg)
	case pinMsg:
		if msg.text == "" {
			delete(m.pins, msg.mk)
		} else {
			m.pins[msg.mk] = clean(msg.text)
		}
		if msg.mk == m.mk() {
			m.layout()
			m.render()
		}
	case newMsg:
		return m.onNew(msg)
	case reactMsg:
		return m.onReact(msg)
	case editMsg:
		return m.onEdit(msg)
	case renderTick:
		m.redraw = false
		m.render()
	case tea.KeyMsg:
		return m.onKey(msg)
	case tea.MouseMsg:
		return m.onMouse(msg)
	}
	return nil
}

func lastIDFirst(ls []line) int { // id of the first line that has one
	for _, l := range ls {
		if l.id != 0 {
			return l.id
		}
	}
	return 0
}

func indexOfID(ls []line, id int) int {
	for i := range ls {
		if id != 0 && ls[i].id == id {
			return i
		}
	}
	return -1
}

// open shows chat i. A forum needs its topics first; enter() then loads the topic's messages.
func (m *model) open(i int) tea.Cmd {
	m.leave()
	c := &m.chats[i]
	m.cur, m.err, m.note = c.key, "", ""
	m.arrive()
	m.stopSearch()
	if c.forum { // always ask again: topics, their unread counts and new topics may have changed
		m.layout()
		m.fixTop()
		m.render()
		return m.topicsCmd(*c)
	}
	return m.enter(c)
}

// enter loads (if needed) and shows the open chat/topic and marks it read.
func (m *model) enter(c *chat) tea.Cmd {
	m.layout()
	mk := mkFor(c, c.topic)
	_, cached := m.msgs[mk]
	var cmds []tea.Cmd
	if !cached {
		cmds = append(cmds, m.loadCmd(*c, mk))
	}
	if m.unreadHere(c) > 0 && (cached || !(c.forum && c.topic > 0)) {
		up := lastID(m.msgs[mk])
		m.clearUnread(c)
		cmds = append(cmds, m.readCmd(*c, up))
	}
	m.fixTop()
	m.render()
	m.vp.GotoBottom()
	return tea.Batch(append(cmds, m.thumbCmds(), m.pinCmd(*c, mk))...)
}

func (m *model) step(d int) tea.Cmd {
	n := len(m.chats)
	if n == 0 {
		return nil
	}
	i := m.index(m.cur)
	if i < 0 {
		i = 0
		if d < 0 {
			i = n - 1
		}
	} else {
		i = (i + d + n) % n
	}
	return m.open(i)
}

func (m *model) nextUnread() tea.Cmd {
	n, start := len(m.chats), m.index(m.cur)
	for k := 1; k <= n; k++ {
		if i := (start + k + n) % n; m.chats[i].key != m.cur && m.chats[i].unread > 0 {
			return m.open(i)
		}
	}
	return nil
}

func (m *model) topicStep(d int) tea.Cmd {
	c := m.curChat()
	if c == nil || len(c.topics) < 2 {
		return nil
	}
	m.leave()
	for i, t := range c.topics {
		if t.id == c.topic {
			c.topic = c.topics[(i+d+len(c.topics))%len(c.topics)].id
			break
		}
	}
	m.err, m.note = "", ""
	m.arrive()
	m.stopSearch()
	return m.enter(c)
}

func (m *model) onNew(ev newMsg) tea.Cmd {
	cmds := []tea.Cmd{m.wait()}
	i := m.index(ev.key)
	var c chat
	switch {
	case i >= 0:
		c = m.chats[i]
		m.chats = append(m.chats[:i:i], m.chats[i+1:]...)
	case m.archived[ev.key]:
		return tea.Batch(cmds...) // archived chats stay hidden
	case ev.peer != nil:
		c = chat{key: ev.key, title: ev.title, username: ev.username, forum: ev.forum, peer: ev.peer}
	default: // unknown chat and no access hash: refetch the list instead
		return tea.Batch(append(cmds, m.loadChatsCmd())...)
	}
	l := ev.line
	if l.from == "?" { // short updates carry no sender: in a private chat it is the chat itself
		l.from = c.title
	}
	body := flat(l.text)
	switch {
	case l.out:
		c.last = "Bạn: " + body
	case c.key[0] != 'u':
		c.last = firstWord(l.from) + ": " + body
	default:
		c.last = body
	}
	c.date = int(l.at.Unix())

	var t *topic
	if c.forum {
		for j := range c.topics {
			if c.topics[j].id == l.topic {
				t = &c.topics[j]
			}
		}
		if c.topics != nil && t == nil && c.key == m.cur { // a topic created meanwhile
			cmds = append(cmds, m.topicsCmd(c))
		}
	}
	mk := mkFor(&c, l.topic)
	if _, ok := m.msgs[mk]; ok {
		m.msgs[mk] = addLine(m.msgs[mk], l)
	}
	inView := c.key == m.cur && (!hasTopics(&c) || c.topic == l.topic)
	if !l.out {
		if inView && m.focused {
			cmds = append(cmds, m.readCmd(c, l.id))
		} else {
			c.unread++
			if t != nil {
				t.unread++
			}
			if !c.muted {
				termWrite("\a")
				where := c.title
				if t != nil {
					where += " › " + t.title
				}
				cmds = append(cmds, m.toastCmd(where+": "+body))
				if !m.focused {
					termWrite(osc(where, body))
				}
			}
		}
	}
	m.chats = placed(m.chats, c, i)
	m.fixTop()
	if inView {
		m.render()
		cmds = append(cmds, m.thumbCmds())
	}
	return tea.Batch(cmds...)
}

func firstWord(s string) string {
	if f := strings.Fields(s); len(f) > 0 {
		return f[0]
	}
	return s
}

// --- keys -------------------------------------------------------------------------

func (m *model) onKey(k tea.KeyMsg) tea.Cmd {
	s := k.String()
	if s == "ctrl+q" || s == "ctrl+c" {
		return tea.Quit
	}
	if m.help {
		return m.helpKey(s)
	}
	if m.viewer != nil {
		return m.viewerKey(s)
	}
	switch s { // shortcuts that work wherever the focus is
	case "f1":
		m.help, m.helpTop = true, 0
		return nil
	case "ctrl+k":
		m.startSearch(true)
		return nil
	case "ctrl+n":
		return m.step(1)
	case "ctrl+p":
		return m.step(-1)
	case "ctrl+g":
		return m.nextUnread()
	case "ctrl+t":
		return m.topicStep(1)
	case "ctrl+y":
		return m.topicStep(-1)
	}
	switch {
	case m.searching:
		return m.searchKey(k)
	case m.browse:
		return m.browseKey(s)
	}
	switch s {
	case "ctrl+b":
		return m.startBrowse(len(m.lines()) - 1)
	case "ctrl+r":
		if ls := m.lines(); len(ls) > 0 {
			m.setReply(&ls[len(ls)-1])
		}
		return nil
	case "esc":
		m.cancelCompose()
		return nil
	case "pgup", "pgdown":
		return m.scroll(k, s == "pgup")
	case "enter":
		return m.send()
	}
	var c tea.Cmd
	m.in, c = m.in.Update(k)
	return c
}

// scroll moves the message view; reaching the top while scrolling up loads older messages.
func (m *model) scroll(k tea.Msg, up bool) tea.Cmd {
	m.vp, _ = m.vp.Update(k)
	if up && m.vp.AtTop() {
		return m.olderCmd()
	}
	return nil
}

func (m *model) send() tea.Cmd {
	text, c := strings.TrimSpace(m.in.Value()), m.curChat()
	if text == "" || c == nil || m.api == nil {
		return nil
	}
	if m.edit != nil {
		return m.finishEdit(text)
	}
	if rest, ok := strings.CutPrefix(text, "/file "); ok {
		return m.fileCmd(*c, shellPath(rest))
	}
	m.in.SetValue("")
	m.err, m.note = "", ""
	rt, tp := 0, 1
	if m.reply != nil {
		rt = m.reply.id
	}
	if c.forum && c.topic > 0 {
		tp = c.topic
	}
	mk, now := m.mk(), time.Now()
	m.msgs[mk] = addLine(m.msgs[mk], line{from: "Bạn", text: text, out: true, at: now, replyTo: rt, topic: tp})
	c.last, c.date = "Bạn: "+flat(text), int(now.Unix())
	cp := *c
	m.bump(cp.key)
	m.reply = nil
	m.layout()
	m.render()
	m.vp.GotoBottom()
	return m.sendCmd(cp, mk, text, rt)
}

// cancelCompose is Esc in the input: drops the edit or the reply.
func (m *model) cancelCompose() {
	if m.edit != nil {
		m.in.SetValue(m.stash)
		m.in.CursorEnd()
		m.edit = nil
	}
	m.setReply(nil)
}

func (m *model) setReply(l *line) {
	m.reply = nil
	if l != nil {
		cp := *l
		m.reply = &cp
	}
	m.browse = false
	m.in.Focus()
	m.layout()
	m.render()
}

func (m *model) startBrowse(i int) tea.Cmd {
	if len(m.lines()) == 0 {
		return nil
	}
	m.browse, m.bsel = true, max(i, 0)
	m.in.Blur()
	m.render()
	m.reveal()
	return nil
}

func (m *model) browseKey(s string) tea.Cmd {
	ls := m.lines()
	if m.delID != 0 || m.react != nil {
		return m.answerPrompt(s)
	}
	if m.bsel < len(ls) {
		if cmd, ok := m.actionKey(s, ls[m.bsel]); ok {
			return cmd
		}
	}
	switch s {
	case "esc", "ctrl+b":
		m.browse = false
		m.in.Focus()
		m.render()
	case "up":
		if m.bsel == 0 {
			return m.olderCmd()
		}
		m.bsel--
		m.render()
		m.reveal()
	case "down":
		m.bsel = min(m.bsel+1, len(ls)-1)
		m.render()
		m.reveal()
	case "pgup", "pgdown":
		kt := tea.KeyPgDown
		if s == "pgup" {
			kt = tea.KeyPgUp
		}
		return m.scroll(tea.KeyMsg{Type: kt}, s == "pgup")
	case "c":
		if m.bsel < len(ls) {
			return m.copyFrom(ls[m.bsel])
		}
	case "ctrl+r":
		if m.bsel < len(ls) {
			m.setReply(&ls[m.bsel])
		}
	case "enter":
		if m.bsel < len(ls) {
			l := ls[m.bsel]
			if _, p := photoOf(l.msg); p {
				return m.mediaCmd(l)
			}
			if _, d := docOf(l.msg); d {
				return m.mediaCmd(l)
			}
			m.setReply(&l)
		}
	}
	return nil
}

// reveal scrolls the view so the highlighted message is on screen.
func (m *model) reveal() {
	if !m.browse || m.bsel >= len(m.lineAt) {
		return
	}
	start, end := m.lineAt[m.bsel], m.nLines
	if m.bsel+1 < len(m.lineAt) {
		end = m.lineAt[m.bsel+1]
	}
	switch {
	case start < m.vp.YOffset:
		m.vp.SetYOffset(start)
	case end > m.vp.YOffset+m.vp.Height:
		m.vp.SetYOffset(max(end-m.vp.Height, start))
	}
}

func (m *model) searchKey(k tea.KeyMsg) tea.Cmd {
	vis := m.visible()
	switch k.String() {
	case "esc":
		if m.fwd != nil {
			m.fwd = nil
			m.setPrompt("")
		}
		m.stopSearch()
	case "enter":
		if m.fwd != nil {
			return m.forwardTo(vis)
		}
		if m.sel < len(vis) {
			return m.open(vis[m.sel])
		}
	case "up":
		m.sel = max(m.sel-1, 0)
	case "down":
		m.sel = min(m.sel+1, max(len(vis)-1, 0))
	default:
		before := m.search.Value()
		var c tea.Cmd
		m.search, c = m.search.Update(k)
		if m.search.Value() != before {
			m.sel, m.top = 0, 0
		}
		return c
	}
	m.fixTop()
	return nil
}

// startSearch puts the cursor in the search box (clear: start from an empty query).
func (m *model) startSearch(clear bool) {
	m.searching, m.browse = true, false
	if clear {
		m.search.SetValue("")
	}
	m.sel, m.top = 0, 0
	m.search.Focus()
	m.in.Blur()
	m.fixTop()
}

// stopSearch clears the query and moves the cursor to the message box, once a chat is open.
func (m *model) stopSearch() {
	m.search.SetValue("")
	m.sel = 0
	if m.cur == "" { // nothing to type into yet: stay in the search box
		return
	}
	m.searching = false
	m.search.Blur()
	m.in.Focus()
	m.fixTop()
}

// helpKey: the guide scrolls with the arrows and closes with Esc, F1, Q or Enter.
func (m *model) helpKey(s string) tea.Cmd {
	switch s {
	case "esc", "f1", "q", "enter":
		m.help = false
	case "up", "k":
		m.helpTop = max(m.helpTop-1, 0)
	case "down", "j":
		m.helpTop = min(m.helpTop+1, m.helpMax())
	case "pgup":
		m.helpTop = max(m.helpTop-max(m.h-3, 1), 0)
	case "pgdown", " ":
		m.helpTop = min(m.helpTop+max(m.h-3, 1), m.helpMax())
	case "home":
		m.helpTop = 0
	case "end":
		m.helpTop = m.helpMax()
	}
	return nil
}

func (m *model) viewerKey(s string) tea.Cmd {
	switch s {
	case "esc", "q":
		dropImage(m.viewer.pic.id)
		m.viewer = nil
	case "o":
		if err := openExternal(m.viewer.path); err != nil {
			m.err = "không mở được: " + err.Error()
		}
	}
	return nil
}

// --- mouse --------------------------------------------------------------------------

func (m *model) onMouse(e tea.MouseMsg) tea.Cmd {
	if m.help {
		switch e.Button {
		case tea.MouseButtonWheelUp:
			m.helpTop = max(m.helpTop-3, 0)
		case tea.MouseButtonWheelDown:
			m.helpTop = min(m.helpTop+3, m.helpMax())
		}
		return nil
	}
	if m.viewer != nil {
		return nil
	}
	press := e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonLeft
	switch {
	case press && e.X == m.side: // the divider
		m.drag = true
	case m.drag && e.Action == tea.MouseActionMotion:
		m.side = e.X
		m.clampSide()
		m.layout()
		m.render()
	case m.drag && e.Action == tea.MouseActionRelease:
		m.drag = false
		saveSide(m.side)
	case e.X < m.side && (e.Button == tea.MouseButtonWheelUp || e.Button == tea.MouseButtonWheelDown):
		step := 1
		if e.Button == tea.MouseButtonWheelUp {
			step = -1
		}
		m.top = max(0, min(m.top+step, len(m.visible())-m.listRows()))
	case e.X < m.side:
		if !press {
			break
		}
		if e.Y < listY0-1 { // the search box
			m.startSearch(false)
		} else if e.Y >= listY0 {
			vis := m.visible()
			if p := m.top + (e.Y-listY0)/itemRows; p < len(vis) {
				return m.open(vis[p])
			}
		}
	case press && e.Y >= headRows && e.Y < headRows+2 && m.curChat() != nil && hasTopics(m.curChat()):
		c := m.curChat()
		for _, sp := range topicSpans(c, m.vp.Width-4) {
			if x := e.X - m.side - 1 - 2; x >= sp.from && x < sp.to && sp.id != c.topic {
				m.leave()
				c.topic = sp.id
				m.arrive()
				return m.enter(c)
			}
		}
	case press && e.Y >= m.msgY()+m.vp.Height && e.Y < m.h-1 && m.curChat() != nil: // the input box and the row above it
		m.focusInput()
	case press:
		return m.clickMessage(e.X, e.Y)
	case e.Button == tea.MouseButtonWheelUp || e.Button == tea.MouseButtonWheelDown:
		return m.scroll(e, e.Button == tea.MouseButtonWheelUp)
	}
	return nil
}

// focusInput is a click on the input box: leave browsing or searching and drop any question that was open.
func (m *model) focusInput() {
	m.delID, m.react, m.fwd = 0, nil, nil
	m.prompt = ""
	if m.searching {
		m.stopSearch()
	}
	m.browse = false
	m.in.Focus()
	m.layout()
	m.render()
}

// clickMessage highlights the message (browse mode, so e/d/r/f/o work on it straight away); a photo or file
// opens. Enter then makes it the reply target.
func (m *model) clickMessage(x, y int) tea.Cmd {
	top := m.msgY()
	if y < top || y >= top+m.vp.Height || len(m.lineAt) == 0 {
		return nil
	}
	row, i := m.vp.YOffset+y-top, 0
	for _, h := range m.hits { // a copy button wins over selecting the message
		if h.row == row && x-m.side-1 >= h.x0 && x-m.side-1 < h.x1 {
			return copyCmd(h.text, "khối code")
		}
	}
	for j, at := range m.lineAt {
		if at <= row {
			i = j
		}
	}
	if m.searching {
		m.searching = false
		m.search.Blur()
	}
	if m.delID != 0 || m.react != nil { // a question about another message is dropped
		m.delID, m.react = 0, nil
		m.setPrompt("")
	}
	m.bsel, m.browse = i, true
	m.in.Blur()
	m.render()
	l := m.lines()[i]
	if _, p := photoOf(l.msg); p {
		return m.mediaCmd(l)
	}
	if _, d := docOf(l.msg); d {
		return m.mediaCmd(l)
	}
	return nil
}

// --- media --------------------------------------------------------------------------

var openable = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".heic": true,
	".mp4": true, ".mov": true, ".mkv": true, ".webm": true, ".m4v": true,
	".mp3": true, ".m4a": true, ".ogg": true, ".oga": true, ".opus": true, ".wav": true, ".flac": true,
	".pdf": true, ".txt": true, ".md": true, ".csv": true, ".rtf": true, ".epub": true,
	".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
}

// mediaCmd downloads a photo or file: images open in the viewer, other known types in the system app.
// Anything else is only saved, because opening an arbitrary file from a chat could run it.
func (m *model) mediaCmd(l line) tea.Cmd {
	ctx, api, w, h := m.ctx, m.api, m.w, m.h
	title := l.from + "  " + l.at.Format("15:04")
	work := func() tea.Msg {
		if p, ok := photoOf(l.msg); ok {
			path, err := downloadPhoto(ctx, api, p, false)
			if err != nil {
				return toastMsg{"tải ảnh lỗi: " + err.Error()}
			}
			if pic, err := renderImage(path, w, h-2); err == nil {
				return viewMsg{viewer{pic, path, title}}
			}
			openExternal(path)
			return toastMsg{"Đã mở ảnh bằng ứng dụng hệ thống"}
		}
		d, ok := docOf(l.msg)
		if !ok {
			return nil
		}
		path, err := downloadDoc(ctx, api, d)
		if err != nil {
			return toastMsg{"tải tệp lỗi: " + err.Error()}
		}
		if strings.HasPrefix(d.MimeType, "image/") {
			if pic, err := renderImage(path, w, h-2); err == nil {
				return viewMsg{viewer{pic, path, title}}
			}
		}
		if openable[strings.ToLower(filepath.Ext(path))] {
			openExternal(path)
			return toastMsg{"Đã mở " + docName(d)}
		}
		return toastMsg{"Đã lưu: " + path}
	}
	return tea.Batch(m.toastCmd("Đang tải…"), work)
}

// thumbCmds downloads thumbnails for the newest few photos of the open view (BONAGRAM_PREVIEWS=1).
func (m *model) thumbCmds() tea.Cmd {
	if !m.cfg.previews || m.cfg.previewRows <= 0 || m.cfg.previewCnt <= 0 || m.api == nil {
		return nil
	}
	var photos []line
	for _, l := range m.lines() {
		if _, ok := photoOf(l.msg); ok && l.id != 0 {
			photos = append(photos, l)
		}
	}
	photos = photos[max(0, len(photos)-m.cfg.previewCnt):]
	maxC, rows := min(48, max(12, m.vp.Width*6/10)), m.cfg.previewRows
	var cmds []tea.Cmd
	for _, l := range photos {
		if m.thumbs[l.id] != nil {
			continue
		}
		m.thumbs[l.id] = &thumb{}
		ctx, api, id, p := m.ctx, m.api, l.id, l.msg
		cmds = append(cmds, func() tea.Msg {
			ph, _ := photoOf(p)
			path, err := downloadPhoto(ctx, api, ph, true)
			if err != nil {
				return thumbMsg{id: id, err: err}
			}
			pic, err := renderImage(path, maxC, rows)
			return thumbMsg{id: id, pic: pic, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// olderCmd loads the page of messages before the oldest one shown.
func (m *model) olderCmd() tea.Cmd {
	c := m.curChat()
	if c == nil || m.loading || m.api == nil {
		return nil
	}
	mk, ls := m.mk(), m.lines()
	if len(ls) == 0 {
		return nil
	}
	tell := func(s string) tea.Cmd {
		if !m.told[mk] {
			m.told[mk], m.note = true, s
		}
		return nil
	}
	room := m.cfg.maxMsgs - len(ls)
	switch {
	case !m.more[mk]:
		return tell("Đầu cuộc trò chuyện")
	case room <= 0:
		return tell(fmt.Sprintf("Đã tải tối đa %d tin (BONAGRAM_MAX_MESSAGES)", m.cfg.maxMsgs))
	}
	oldest := lastIDFirst(ls)
	if oldest == 0 {
		return nil
	}
	m.loading = true
	ctx, api, p, tp, n := m.ctx, m.api, c.peer, 0, min(page, room)
	if c.forum {
		tp = c.topic
	}
	return func() tea.Msg {
		ls, more, err := loadMessages(ctx, api, p, tp, oldest, n)
		return olderMsg{mk, ls, more, err}
	}
}

// onReact stores new reactions on the message wherever it is cached, but redraws at most every 250 ms and
// only if it is in the open view: a popular post can get a burst of these.
func (m *model) onReact(ev reactMsg) tea.Cmd {
	cmds := []tea.Cmd{m.wait()}
	shown, open := false, m.mk()
	for mk, ls := range m.msgs {
		if mk != ev.key && !strings.HasPrefix(mk, ev.key+"#") {
			continue
		}
		if i := indexOfID(ls, ev.id); i >= 0 {
			ls[i].reacts = ev.rs
			shown = shown || mk == open
		}
	}
	if shown && !m.redraw {
		m.redraw = true
		cmds = append(cmds, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return renderTick{} }))
	}
	return tea.Batch(cmds...)
}

// onEdit replaces the content of an edited message wherever it is cached. A bot that streams its answer sends
// dozens of these, so the redraw is throttled like the one for reactions, and the chat list preview follows
// when the message is the newest of its chat.
func (m *model) onEdit(ev editMsg) tea.Cmd {
	cmds := []tea.Cmd{m.wait()}
	shown, open := false, m.mk()
	for mk, ls := range m.msgs {
		if mk != ev.key && !strings.HasPrefix(mk, ev.key+"#") {
			continue
		}
		i := indexOfID(ls, ev.line.id)
		if i < 0 {
			continue
		}
		old, n := ls[i], ev.line
		n.from, n.sender, n.out, n.at, n.replyTo, n.topic = old.from, old.sender, old.out, old.at, old.replyTo, old.topic
		if n.msg != nil && n.msg.Reactions.Results == nil {
			n.reacts = old.reacts // an edit without reactions is not a removal of them
		}
		ls[i] = n
		shown = shown || mk == open
		if i == len(ls)-1 {
			if ci := m.index(ev.key); ci >= 0 {
				c := &m.chats[ci]
				switch body := flat(n.text); {
				case n.out:
					c.last = "Bạn: " + body
				case c.key[0] != 'u':
					c.last = firstWord(n.from) + ": " + body
				default:
					c.last = body
				}
			}
		}
	}
	if shown && !m.redraw {
		m.redraw = true
		cmds = append(cmds, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return renderTick{} }))
	}
	return tea.Batch(cmds...)
}
