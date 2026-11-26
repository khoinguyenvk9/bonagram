package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const (
	sideDef, sideMin, sideMax = 36, 24, 70
	minRight                  = 40 // room kept for the messages when the splitter is dragged
)

type config struct {
	maxMsgs                 int
	previews                bool
	premium                 bool // the account has Telegram Premium: files up to 4000 MB instead of 2000
	previewRows, previewCnt int
	images                  string // "", "kitty" or "blocks": force how images are drawn
	fps                     int    // renderer frame rate cap
	icons                   bool   // Nerd Font icons in menus (BONAGRAM_ICONS=off for fonts without them)
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
		quiet  bool // a background refresh (a read elsewhere): update the counts, do not reopen the view
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
		key    string
		id     int
		rs     []reaction
		unread bool // someone reacted to an own message and it is not seen yet
		canSee bool // who reacted can be listed (can_see_list)
	}
	renderTick struct{}
	// editMsg: a message was edited (by its author, or by a bot streaming its answer).
	editMsg struct {
		key  string
		line line
	}
	viewMsg  struct{ v viewer }
	toastMsg struct{ text string }
	playMsg  struct{ path string } // a downloaded video, to play in the terminal
	doneMsg  struct {              // the end of a download: a message to show, an error that stays on screen
		text string
		bad  bool
		x    *xfer // the download that ended, if it was shown
	}
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
		up             *xfer            // file: the send that ended
		folder         *tg.DialogFilter // folder: what Telegram accepted
	}
	pinMsg struct { // the pinned messages of a chat/topic, newest first; none when nothing is pinned
		mk   string
		list []pinned
	}
	// readInMsg: messages of chat key were read on another device; unread is what is left. In a forum topic
	// (topic > 0) the update carries no count, so the topics are fetched again.
	readInMsg struct {
		key           string
		topic, unread int
	}
	// readOutMsg: the other side read our messages up to id in chat key (topic > 0: in that forum topic).
	readOutMsg struct {
		key       string
		topic, id int
	}
	// peopleTick: the chat search paused, time to ask the server. peopleMsg: its answer.
	peopleTick struct{ seq int }
	peopleMsg  struct {
		seq   int
		found []chat
		err   error
	}
	newMsg struct {
		key, title, username string
		forum                bool
		peer                 tg.InputPeerClass // nil when the update carried no access hash
		line                 line
	}
)

type model struct {
	ctx      context.Context
	api      *tg.Client
	events   <-chan tea.Msg
	cfg      config
	track    func(context.Context, []chanTrack) // registers channels with the updates manager (may be nil)
	xfers    []*xfer                            // sends and downloads in progress, oldest first; the newest is m.note
	typing   map[string][]typer                 // chat key → who is typing there now (typing.go)
	typingOn bool                               // the ticker that drops quiet typers runs
	typedKey string                             // our own typing was last announced in this chat, at typedAt
	typedAt  time.Time
	folders  []folder // the user's chat folders, "all chats" included; none when they made no folder
	folder   int      // index of the folder shown
	tabMemo  *tabMemo // the folder tabs drawn last, reused while nothing they show changed (View is per frame)
	tabOff   int      // first folder tab drawn (the row scrolls sideways when the tabs do not fit)
	tabW     int      // the list width fitTabs last fitted the row to
	selfKey  string   // chat key of Saved Messages: everything in it is yours, although the server does not mark it "out"

	chats    []chat
	archived map[string]bool
	cur      string // key of the open chat
	top      int    // first visible row of the chat list
	side     int    // chat list width as drawn (railW when folded)
	sideWant int    // the width the user gave it (dragged, saved)
	rail     bool   // the list is folded into initials (narrow window, or folded by the user)
	folded   bool   // the user dragged the divider all the way left: folded in any window
	railHot  int    // screen row the mouse is over in the rail, 0 when none
	drag     bool

	msgs    map[string][]line // by chat, or chat#topic in a forum
	more    map[string]bool   // older messages may exist
	told    map[string]bool   // "start of chat" already shown
	loading bool
	redraw  bool // a renderTick is already scheduled
	thumbs  map[int]*thumb
	lineAt  []int      // first content line of each message of the open view
	hits    []copyHit  // copy buttons of the open view
	quoteAt []quoteHit // reply quotes of the open view: a click jumps to the message replied to
	cpFor   int        // message whose code block was copied last, and which block
	cpIdx   int
	nLines  int

	searching bool
	search    textinput.Model
	sel       int    // highlighted result while searching
	found     []chat // server results for the search that are not in the list; shown after the matches
	fseq      int
	browse    bool
	bsel      int // highlighted message while browsing
	reply     *line
	edit      *line               // own message being edited (the text is in the input)
	stash     string              // what was in the input before editing started
	fwd       *line               // message waiting for a target chat (chosen in the search list)
	members   map[string][]member // members of basic groups, loaded at most once per chat
	mc        []member            // @mention suggestions shown in the bar
	msel      int
	mstart    int // rune index of the "@" being completed
	mq        string
	mseq      int
	mactive   bool
	ec        []emoji // emoji suggestions for the ":word" being typed
	esel      int
	estart    int                       // rune index of the ":"
	pend      []member                  // picked mentions without a username: sent as entities
	ms        *msearch                  // searching the messages of this chat (Ctrl+F)
	detached  map[string]bool           // the view holds an old stretch found by a search, not the end of the chat
	rows      []string                  // the message view as drawn, row by row (for selecting text)
	rowOut    []bool                    // which rows belong to a message of ours
	vlines    []string                  // the message view's content, row by row, as last set (setContent)
	txt       selection                 // text selected with the mouse
	quotes    map[string]string         // quote lines already measured
	bodies    map[*tg.Message]bodyCache // message bodies drawn by the last render
	links     []linkSpan                // the links on screen, from the last render
	hover     string                    // the address of the link the mouse is over
	attach    []string                  // files staged by /file or the picker, waiting for a caption and Enter
	picking   bool                      // the file dialog is open
	react     *line                     // message waiting for a reaction key
	delID     int                       // message waiting for the y that confirms deleting it
	prompt    string                    // question shown above the input
	lkFor     int                       // message whose link was opened last, and which link
	lkIdx     int
	drafts    map[string]string   // unsent text per chat/topic
	pins      map[string][]pinned // pinned messages per chat/topic, newest first
	pinAt     map[string]int      // which one the pinned row shows (a click jumps to it, then moves to the next)

	menu    *ctxMenu // the right-click menu of a message, nil when closed
	help    bool     // the guide (F1) is open
	helpTop int      // first line of the guide shown

	viewer *viewer
	vp     viewport.Model
	in     editor
	inH    int // rows of the message box at the last layout

	w, h     int
	focused  bool
	err      string
	note     string
	toast    string
	toastSeq int
	title    string
}

func newModel(ctx context.Context, api *tg.Client, events <-chan tea.Msg, cfg config, side int) model {
	want := side
	if side == railW { // saved folded: unfolding goes back to the default width
		want = sideDef
	}
	in := editor{Placeholder: "Nhắn tin…   :emoji · @nhắc tên"} // the real terminal cursor shows where typing goes, see setCursor
	s := textinput.New()
	s.Cursor.SetMode(cursor.CursorHide)
	s.Prompt = " "
	s.Placeholder = "Tìm chat…"
	s.PlaceholderStyle = fg(cMuted)
	s.TextStyle = fg(cFg)
	s.Focus() // start in the search box
	return model{
		ctx: ctx, api: api, events: events, cfg: cfg, side: side, sideWant: want, folded: side == railW, focused: true, searching: true,
		msgs: map[string][]line{}, more: map[string]bool{}, detached: map[string]bool{}, told: map[string]bool{}, thumbs: map[int]*thumb{},
		archived: map[string]bool{}, typing: map[string][]typer{}, tabMemo: &tabMemo{}, drafts: map[string]string{}, pins: map[string][]pinned{}, pinAt: map[string]int{}, vp: viewport.New(0, 0), in: in, search: s,
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.loadChatsCmd(), m.foldersCmd(), m.wait())
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
		return topicsMsg{key: c.key, topics: ts, err: err}
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

// reactReadCmd marks the reactions of chat c seen, locally and on the server.
func (m *model) reactReadCmd(c *chat) tea.Cmd {
	if c.reacts == 0 {
		return nil
	}
	c.reacts = 0
	ctx, api, p := m.ctx, m.api, c.peer
	return func() tea.Msg {
		_ = readReactions(ctx, api, p) // best effort, like readCmd
		return nil
	}
}

func (m model) sendCmd(c chat, mk, text string, replyTo int, ents []tg.MessageEntityClass) tea.Cmd {
	ctx, api, tp := m.ctx, m.api, 0
	if c.forum {
		tp = c.topic
	}
	return func() tea.Msg {
		id, err := sendText(ctx, api, c.peer, text, replyTo, tp, ents)
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
	f := m.curFolder() // a search looks through every chat, not only the folder's
	for i := range m.chats {
		if q == "" && f != nil && !f.has(&m.chats[i]) {
			continue
		}
		if q == "" || strings.Contains(fold(m.chats[i].title+" "+m.chats[i].username), q) {
			out = append(out, i)
		}
	}
	if q != "" {
		for j := range m.found { // indexes past the chats are search results (chatAt, pick)
			out = append(out, len(m.chats)+j)
		}
	}
	return out
}

// chatAt is entry i of visible(): a chat of the list, or a server search result.
func (m *model) chatAt(i int) chat {
	if i < len(m.chats) {
		return m.chats[i]
	}
	return m.found[i-len(m.chats)]
}

// pick turns entry i of visible() into an index of m.chats: a search result joins the list first.
func (m *model) pick(i int) int {
	if i < len(m.chats) {
		return i
	}
	c := m.found[i-len(m.chats)]
	m.found = nil
	if j := m.index(c.key); j >= 0 { // arrived in the list meanwhile
		return j
	}
	// shortcut: a channel opened this way is not registered with the updates manager (no pts known), so
	// its live messages may be late; reopening the app tracks it once it is in the dialogs.
	c.last = ""
	m.chats = placed(m.chats, c, -1)
	return m.index(c.key)
}

const (
	peoplePause = 400 * time.Millisecond // typing pause before asking the server for people
	peopleMin   = 3                      // shorter queries are not sent (Telegram refuses or floods them)
)

// peopleCheck runs after the search text changed: drops old server results and asks again after a pause.
func (m *model) peopleCheck() tea.Cmd {
	m.found = nil
	m.fseq++
	q := strings.TrimPrefix(strings.TrimSpace(m.search.Value()), "@")
	if m.api == nil || len([]rune(q)) < peopleMin {
		return nil
	}
	seq := m.fseq
	return tea.Tick(peoplePause, func(time.Time) tea.Msg { return peopleTick{seq} })
}

func (m *model) onPeopleTick(t peopleTick) tea.Cmd {
	if t.seq != m.fseq || !m.filtering() {
		return nil
	}
	ctx, api, q := m.ctx, m.api, strings.TrimPrefix(strings.TrimSpace(m.search.Value()), "@")
	return func() tea.Msg {
		found, err := searchPeople(ctx, api, q)
		return peopleMsg{t.seq, found, err}
	}
}

func (m *model) onPeople(r peopleMsg) tea.Cmd {
	if r.seq != m.fseq || !m.filtering() {
		return nil
	}
	if r.err != nil {
		return m.toastCmd("tìm trên Telegram lỗi: " + errText(r.err))
	}
	m.found = nil
	for _, c := range r.found {
		if m.index(c.key) < 0 && !m.archived[c.key] {
			m.found = append(m.found, c)
		}
	}
	m.fixTop()
	return nil
}

// onReadIn follows a read made on another device (the phone): the badge drops to what is still unread.
// A forum's own count is not used (it is not lowered when topics are read, see sumUnread): its topics are
// fetched again instead.
func (m *model) onReadIn(ev readInMsg) tea.Cmd {
	i := m.index(ev.key)
	if i < 0 {
		return m.wait()
	}
	c := &m.chats[i]
	if c.forum {
		if ev.topic == 0 && c.topics != nil {
			return m.wait() // the forum-wide update; the topic ones follow and carry the change
		}
		cmd := m.topicsCmd(*c)
		return tea.Batch(m.wait(), func() tea.Msg {
			t, _ := cmd().(topicsMsg)
			t.quiet = true
			return t
		})
	}
	c.unread = max(0, ev.unread)
	return m.wait()
}

// onReadOut moves the read mark of our messages (✓✓) forward and redraws when the chat is open.
func (m *model) onReadOut(ev readOutMsg) tea.Cmd {
	i := m.index(ev.key)
	if i < 0 {
		return m.wait()
	}
	c := &m.chats[i]
	if ev.topic > 0 {
		for j := range c.topics {
			if c.topics[j].id == ev.topic {
				c.topics[j].readOut = max(c.topics[j].readOut, ev.id)
			}
		}
	} else {
		c.readOut = max(c.readOut, ev.id)
	}
	if c.key == m.cur {
		m.render()
	}
	return m.wait()
}

// readOut is how far the other side read our messages in the open view.
func (m *model) readOut() int {
	c := m.curChat()
	if c == nil {
		return 0
	}
	// shortcut: General (id 1) is assumed to follow the group's own mark; not checked against a real forum
	if t := m.topicOf(c); hasTopics(c) && t != nil && t.id != 1 {
		return t.readOut
	}
	return c.readOut
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
	if len(m.pins[m.mk()]) > 0 {
		y++ // the pinned message row
	}
	return y
}

// barText is the row above the input: a question, the message being edited, or the one being answered.
func (m *model) barText() string {
	switch {
	case m.prompt != "":
		return m.prompt
	case len(m.mc) > 0:
		return mentionBar(m.mc, m.msel)
	case len(m.ec) > 0:
		return emojiBar(m.ec, m.esel)
	case m.ms != nil:
		return m.msBar()
	case m.edit != nil:
		return "✎ Sửa tin: " + m.edit.text + "   (Enter lưu · Esc huỷ)"
	case m.attach != nil:
		return attachBar(m.attach)
	case m.reply != nil:
		return "↩ " + m.reply.from + ": " + m.reply.text
	case m.detached[m.mk()]:
		return "↺ Đang xem tin cũ   (Esc: về tin mới nhất)"
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
	m.reply, m.edit, m.fwd, m.react, m.delID, m.prompt, m.browse, m.attach = nil, nil, nil, nil, 0, "", false, nil
	m.mactive, m.mc, m.pend, m.ms, m.ec = false, nil, nil, nil, nil
	if mk := m.mk(); m.detached[mk] { // an old stretch is not kept: the chat opens at its end next time
		delete(m.detached, mk)
		delete(m.msgs, mk)
	}
	m.in.SetValue("")
}

// arrive puts back the draft of the chat/topic that was just opened.
func (m *model) arrive() {
	m.in.SetValue(m.drafts[m.draftKey()])
	m.in.CursorEnd()
}

func (m *model) layout() {
	rows := m.msgY() + m.in.Height() + 2 + 1 // the bordered message box, the keys bar
	if m.barText() != "" {
		rows++
	}
	m.vp.Width = max(m.w-m.side-1, 10)
	m.vp.Height = max(m.h-rows, 1)
	m.in.SetWidth(m.vp.Width - 9 - m.btnW())
	m.inH = m.in.Height()
	m.search.Width = max(m.side-8-ansi.StringWidth(keyCap("ctrl+k")), 1) // margins, padding, prompt and the key cap
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
	if was := m.side; m.w > 0 { // searching opens the folded list, picking a chat folds it again
		if m.fitSide(); m.side != was {
			m.layout()
			m.render()
		}
	}
	if len(m.folders) > 0 && m.side != m.tabW { // a new width: keep the folder shown in view
		m.fitTabs()
	}
	if m.w > 0 && m.in.Height() != m.inH { // the message box grew or shrank: the message view gives or takes the rows
		bottom := m.vp.AtBottom()
		m.layout()
		if bottom {
			m.vp.GotoBottom()
		}
	}
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
		drawnID.Store(0) // a resize repaints everything: a painted picture must be painted again
		m.fitSide()
		m.layout()
		m.render()
		m.fixTop()

	case tea.FocusMsg:
		m.focused = true
		if c := m.curChat(); c != nil {
			cmd := m.reactReadCmd(c)
			if m.unreadHere(c) > 0 {
				up := lastID(m.lines())
				m.clearUnread(c)
				cmd = tea.Batch(cmd, m.readCmd(*c, up))
			}
			return cmd
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
		if msg.err != nil && msg.quiet {
			return nil // keep what we have; a background refresh failing is not worth a message
		}
		was := c.topic
		c.topics = append([]topic{}, msg.topics...) // non-nil: loaded, even if empty
		c.unread = sumUnread(c.topics)
		if m.topicOf(c) == nil {
			c.topic = 0
			if len(c.topics) > 0 {
				c.topic = c.topics[0].id
			}
		}
		if c.key == m.cur && (!msg.quiet || c.topic != was) {
			return m.enter(c)
		}

	case searchMsg:
		m.onSearchResult(msg)
		return nil

	case aroundMsg:
		return m.onAround(msg)

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
	case playMsg:
		m.note = ""
		return tea.ExecProcess(exec.Command(mpvPath(), mpvArgs(msg.path)...), func(err error) tea.Msg {
			if err != nil {
				return doneMsg{"phát video lỗi: " + err.Error(), true, nil}
			}
			return doneMsg{"", false, nil}
		})
	case xferTick:
		return m.onXferTick(msg.p)
	case typingMsg:
		return m.onTyping(msg)
	case typingTick:
		return m.onTypingTick()
	case statusMsg:
		return m.onStatus(msg)
	case foldersMsg:
		return m.onFolders(msg)
	case foldersChanged:
		return m.foldersReload()
	case doneMsg:
		if msg.x != nil {
			m.endXfer(msg.x)
		} else if len(m.xfers) == 0 {
			m.note = ""
		}
		switch {
		case msg.bad:
			m.err = msg.text
		case msg.text != "":
			return m.toastCmd(msg.text)
		}
		return nil
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
	case mentionTick:
		return m.onMentionTick(msg)
	case peopleTick:
		return m.onPeopleTick(msg)
	case peopleMsg:
		return m.onPeople(msg)
	case readOutMsg:
		return m.onReadOut(msg)
	case readInMsg:
		return m.onReadIn(msg)
	case mentionMsg:
		return m.onMention(msg)
	case pickedMsg:
		m.picking = false
		switch {
		case msg.err != nil:
			m.err = "chọn tệp lỗi: " + msg.err.Error()
		case len(msg.paths) > 0 && m.curChat() != nil:
			m.stage(msg.paths)
		}
	case pinMsg:
		if len(msg.list) == 0 {
			delete(m.pins, msg.mk)
		} else {
			m.pins[msg.mk] = msg.list
		}
		m.pinAt[msg.mk] = 0
		if msg.mk == m.mk() {
			m.layout()
			m.render()
		}
	case newMsg:
		return m.onNew(msg)
	case reactMsg:
		return m.onReact(msg)
	case reactorsMsg:
		return m.onReactors(msg)
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
	i = m.pick(i)
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
	cmds := []tea.Cmd{m.reactReadCmd(c)}
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
	l, reread := ev.line, false
	m.typingDone(ev.key, l.sender)
	if l.from == "?" { // the update did not carry the sender
		if c.key[0] == 'u' { // in a private chat it is the chat itself
			l.from = c.title
		} else if l.from = m.nameOf(l.sender); l.from == "" { // in a group: a name seen before, else read the chat again
			l.from, reread = "Thành viên", true
		}
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
	c.lastID, c.lastOut = l.id, l.out && (l.msg == nil || !l.msg.Post)

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
	if _, ok := m.msgs[mk]; ok && !m.detached[mk] {
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
	if reread {
		cmds = append(cmds, m.refresh(c.key)) // the history carries the sender's name
	}
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
	m.clearSel()
	if m.menu != nil {
		return m.menuKey(s)
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
	case "ctrl+f":
		if m.ms == nil && !m.help {
			m.startMsgSearch()
		}
		return nil
	case "ctrl+l":
		if len(m.folders) > 0 {
			m.setFolder(m.folder + 1)
		}
		return nil
	case "ctrl+t":
		return m.topicStep(1)
	case "ctrl+y":
		return m.topicStep(-1)
	case "ctrl+x":
		if m.cancelXfer() {
			return nil
		}
	}
	switch {
	case m.ms != nil:
		return m.msKey(k)
	case m.searching:
		return m.searchKey(k)
	case m.browse:
		return m.browseKey(s)
	}
	if len(m.mc) > 0 && m.mentionKey(s) {
		return nil
	}
	if len(m.ec) > 0 && m.emojiKey(s) {
		return nil
	}
	switch s {
	case "ctrl+b":
		return m.startBrowse(len(m.lines()) - 1)
	case "ctrl+o":
		return m.pickCmd()
	case "ctrl+r":
		if ls := m.lines(); len(ls) > 0 {
			m.setReply(&ls[len(ls)-1])
		}
		return nil
	case "esc":
		busy := m.attach != nil || m.edit != nil || m.reply != nil
		m.cancelCompose()
		if busy {
			return nil
		}
		return m.backToLatest()
	case "pgup", "pgdown":
		return m.scroll(k, s == "pgup")
	case "enter":
		return m.send()
	}
	var c tea.Cmd
	before := m.in.ver
	m.in, c = m.in.Update(k)
	m.emojiCheck()
	var typed tea.Cmd
	if m.in.ver != before && m.edit == nil && m.api != nil { // the text changed: we are typing (not a command)
		if v := m.in.val; len(v) > 0 && v[0] != '/' && slices.ContainsFunc(v, func(r rune) bool { return !unicode.IsSpace(r) }) {
			typed = m.typedCmd(m.curChat())
		}
	}
	return tea.Batch(c, m.mentionCheck(), typed)
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
	if c == nil || m.api == nil {
		return nil
	}
	if m.attach != nil {
		return m.sendAttached(*c, text)
	}
	if text == "" {
		return nil
	}
	if m.edit != nil {
		return m.finishEdit(text)
	}
	if rest, ok := strings.CutPrefix(text, "/file "); ok {
		m.stage(parsePaths(rest))
		return nil
	}
	for _, cmd := range []string{"/position", "/vitri"} {
		if rest, ok := strings.CutPrefix(text, cmd); ok && (rest == "" || rest[0] == ' ') {
			return m.positionCmd(*c, rest)
		}
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
	plain, fmts := parseMarkdown(text) // **bold** and the like become entities
	local := line{from: "Bạn", text: plain, out: true, at: now, replyTo: rt, topic: tp}
	if len(fmts) > 0 { // drawn at once as it will look; the server's copy replaces it
		local.msg, local.segs = &tg.Message{Message: plain, Entities: fmts, Out: true}, segments(plain, fmts)
	}
	m.msgs[mk] = addLine(m.msgs[mk], local)
	c.last, c.date = "Bạn: "+flat(plain), int(now.Unix())
	c.lastID, c.lastOut = 0, true
	cp := *c
	m.bump(cp.key)
	m.reply = nil
	m.layout()
	m.render()
	m.vp.GotoBottom()
	m.mactive, m.mc = false, nil
	return m.sendCmd(cp, mk, plain, rt, append(fmts, m.mentionEnts(plain)...))
}

// cancelCompose is Esc in the input: drops the edit or the reply.
func (m *model) cancelCompose() {
	if m.attach != nil {
		m.attach = nil
		m.layout()
		m.render()
		return
	}
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
			return tea.Batch(c, m.peopleCheck())
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
		m.found = nil
	}
	m.sel, m.top = 0, 0
	m.search.Focus()
	m.in.Blur()
	m.fixTop()
}

// stopSearch clears the query and moves the cursor to the message box, once a chat is open.
func (m *model) stopSearch() {
	m.search.SetValue("")
	m.found = nil
	m.fseq++
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
		raw := m.viewer.pic.raw != ""
		m.viewer = nil
		if raw { // pixels painted by the terminal are only gone once every cell is drawn again
			drawnID.Store(0)
			return tea.ClearScreen
		}
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
	if m.menu != nil {
		if cmd, done := m.menuMouse(e); done {
			return cmd
		}
	}
	if e.Action == tea.MouseActionMotion && e.Button == tea.MouseButtonNone && !m.drag && !m.txt.on {
		m.railHot = 0
		if m.rail && e.X < m.side {
			m.railHot = e.Y // the name of the tile shows next to the rail
		}
		m.hoverAt(e)
		return nil
	}
	if e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonRight && e.X < m.side && e.Y >= listY0 { // a chat in the list
		vis := m.visible()
		if p := m.top + (e.Y-listY0)/itemRows; p < len(vis) && vis[p] < len(m.chats) {
			m.openChatMenu(m.chats[vis[p]], e.X, e.Y)
		}
		return nil
	}
	if e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonRight && e.X > m.side {
		m.openMenu(e.X, e.Y)
		return nil
	}
	press := e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonLeft
	switch {
	case press && e.X == m.side && (!m.rail || m.folded): // the divider; a rail folded by a narrow window does not drag (no room to unfold)
		m.drag = true
	case m.drag && e.Action == tea.MouseActionMotion:
		// past the narrowest list the drag folds it into initials, like Telegram; back out unfolds it
		if m.folded = e.X < sideMin-foldSnap; !m.folded {
			m.sideWant = e.X
		}
		m.fitSide()
		m.layout()
		m.render()
	case m.drag && e.Action == tea.MouseActionRelease:
		m.drag = false
		if m.folded {
			saveSide(railW)
		} else {
			saveSide(m.sideWant)
		}
	case m.txt.on && e.Action == tea.MouseActionMotion:
		m.dragSel(e)
	case m.txt.on && e.Action == tea.MouseActionRelease:
		return m.endSel(e)
	case e.X < m.side && e.Y == listY0-1 && len(m.folders) > 0 && !m.rail && tea.MouseEvent(e).IsWheel(): // the wheel over the tabs scrolls them
		if e.Button == tea.MouseButtonWheelUp || e.Button == tea.MouseButtonWheelLeft {
			m.scrollTabs(-1)
		} else {
			m.scrollTabs(1)
		}
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
		} else if e.Y == listY0-1 && len(m.folders) > 0 && !m.rail { // the folder tabs
			m.clickFolder(e.X)
		} else if e.Y >= listY0 {
			vis := m.visible()
			if p := m.top + (e.Y-listY0)/itemRows; p < len(vis) {
				if m.fwd != nil { // choosing where to forward: the click picks the target
					m.sel = p
					return m.forwardTo(vis)
				}
				return m.open(vis[p])
			}
		}
	case press && e.Y == m.msgY()-1 && len(m.pins[m.mk()]) > 0 && m.curChat() != nil: // the pinned row
		return m.jumpPin()
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
		if x0 := m.side + 1 + 3 + max(m.vp.Width-6, 4) - m.btnW(); m.btnW() > 0 && e.Y == m.h-3 && e.X >= x0 && e.X < x0+m.btnW() {
			clip, _ := m.buttonWidths()
			switch at := e.X - x0 - m.buttonLead(); {
			case at < 0: // the blank before the buttons
			case at < clip+1: // the paperclip and the space after it
				return m.pickCmd()
			default:
				return m.send()
			}
		}
		if top := m.h - 2 - m.in.Height(); e.Y >= top && e.Y < m.h-2 { // a click in the text moves the cursor there
			m.in.ClickAt(e.Y-top, e.X-(m.side+1+2+1+1))
			m.emojiCheck()
		}
	case press:
		m.beginSel(e)
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
	i, ok := m.msgIndexAt(y)
	if !ok {
		return nil
	}
	row := m.vp.YOffset + y - m.msgY()
	for _, h := range m.hits { // a copy button wins over selecting the message
		if h.row == row && x-m.side-1 >= h.x0 && x-m.side-1 < h.x1 {
			return copyCmd(h.text, "khối code")
		}
	}
	for _, q := range m.quoteAt { // so does a reply quote: it leads to the message replied to
		if q.row == row {
			return m.jumpTo(q.id)
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
	if d, ok := docOf(l.msg); ok { // a file can be big: Enter downloads it
		return m.toastCmd("Enter để tải " + docName(d) + " (" + humanSize(d.Size) + ")")
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

// mediaCmd downloads a photo or file: images open in the viewer; a file is saved in the Downloads folder and
// opened in the system app when its type is a known harmless one, otherwise shown in the file manager (opening an
// arbitrary file from a chat could run it). The answer is a doneMsg, so an error stays on screen.
func (m *model) mediaCmd(l line) tea.Cmd {
	ctx, api, w, h := m.ctx, m.api, m.w, m.h
	title := l.from + "  " + l.at.Format("15:04")
	var p tg.InputPeerClass
	if c := m.curChat(); c != nil {
		p = c.peer
	}
	app := m.ctx
	var pr *xfer // a file shows its progress and can be stopped; a photo is small
	if d, ok := docOf(l.msg); ok {
		pr = newXfer(docName(d), d.Size, true)
		ctx = m.startXfer(ctx, pr)
	} else {
		m.note = "⬇ Đang tải ảnh…"
	}
	work := func() tea.Msg {
		if ph, ok := photoOf(l.msg); ok {
			path, err := downloadPhoto(ctx, api, ph, false)
			if err != nil {
				return doneMsg{"tải ảnh lỗi: " + errText(err), true, nil}
			}
			if pic, err := renderViewerImage(path, w, h-2); err == nil {
				return viewMsg{viewer{pic, path, title}}
			}
			openURL(path)
			return doneMsg{"Đã mở ảnh bằng ứng dụng hệ thống", false, nil}
		}
		d, ok := docOf(l.msg)
		if !ok {
			return doneMsg{"", false, nil}
		}
		defer pr.stop()
		defer pr.done.Store(true)
		path, err := downloadDoc(ctx, api, d, pr)
		if tgerr.Is(err, "FILE_REFERENCE_EXPIRED") && p != nil && l.id != 0 { // an old message: its file needs a fresh reference
			if res, e := getMessages(ctx, api, p, []tg.InputMessageClass{&tg.InputMessageID{ID: l.id}}); e == nil {
				for _, nl := range linesOf(res) {
					if nd, ok := docOf(nl.msg); ok && nl.id == l.id {
						d = nd
						path, err = downloadDoc(ctx, api, d, pr)
					}
				}
			}
		}
		switch {
		case errors.Is(err, context.Canceled) && app.Err() == nil:
			return doneMsg{"Đã huỷ tải " + pr.label, false, pr}
		case err != nil:
			return doneMsg{"tải tệp lỗi: " + errText(err), true, pr}
		}
		if strings.HasPrefix(d.MimeType, "image/") {
			if pic, err := renderViewerImage(path, w, h-2); err == nil {
				return viewMsg{viewer{pic, path, title}}
			}
		}
		if k := infoOf(d).kind; (k == "video" || k == "gif") && mpvPath() != "" {
			return playMsg{path}
		}
		if openable[strings.ToLower(filepath.Ext(path))] {
			openURL(path)
			return doneMsg{"Đã lưu và mở " + path, false, pr}
		}
		revealFile(path)
		return doneMsg{"Đã lưu " + path, false, pr}
	}
	if pr != nil {
		return tea.Batch(work, xferTickCmd(pr))
	}
	return work
}

// thumbCmds downloads thumbnails for the newest few photos of the open view (BONAGRAM_PREVIEWS=1), pictures sent
// as files included (their server-made thumbnail, not the original).
func (m *model) thumbCmds() tea.Cmd {
	if m.api == nil {
		return nil
	}
	stickers := m.stickerCmds()
	if !m.cfg.previews || m.cfg.previewRows <= 0 || m.cfg.previewCnt <= 0 {
		return stickers
	}
	var photos []line
	for _, l := range m.lines() {
		if _, ok := photoOf(l.msg); ok && l.id != 0 {
			photos = append(photos, l)
		} else if _, _, ok := imageDocOf(l.msg); ok && l.id != 0 {
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
			var path string
			var err error
			if ph, ok := photoOf(p); ok {
				path, err = downloadPhoto(ctx, api, ph, true)
			} else if d, typ, ok := imageDocOf(p); ok {
				path, err = downloadDocThumb(ctx, api, d, typ)
			} else {
				err = fmt.Errorf("không có ảnh xem trước")
			}
			if err != nil {
				return thumbMsg{id: id, err: err}
			}
			pic, err := renderImage(path, maxC, rows)
			return thumbMsg{id: id, pic: pic, err: err}
		})
	}
	return tea.Batch(append(cmds, stickers)...)
}

const (
	stickerRows = 6  // height of a sticker in the message view
	stickerMax  = 12 // stickers drawn per view (the newest)
)

// stickerCmds draws the newest static stickers of the open view (animated ones stay a label).
func (m *model) stickerCmds() tea.Cmd {
	var ls []line
	for _, l := range m.lines() {
		if d, ok := staticSticker(l.msg); ok && l.id != 0 && d != nil {
			ls = append(ls, l)
		}
	}
	var cmds []tea.Cmd
	for _, l := range ls[max(0, len(ls)-stickerMax):] {
		if m.thumbs[l.id] != nil {
			continue
		}
		m.thumbs[l.id] = &thumb{}
		ctx, api, id, msg := m.ctx, m.api, l.id, l.msg
		cmds = append(cmds, func() tea.Msg {
			d, _ := staticSticker(msg)
			path, err := downloadSticker(ctx, api, d)
			if err != nil {
				return thumbMsg{id: id, err: err}
			}
			pic, err := renderImage(path, stickerRows*2, stickerRows)
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
			ls[i].reacts, ls[i].seeWho = ev.rs, ev.canSee
			shown = shown || mk == open
		}
	}
	if shown && !m.redraw {
		m.redraw = true
		cmds = append(cmds, tea.Tick(250*time.Millisecond, func(time.Time) tea.Msg { return renderTick{} }))
	}
	if i := m.index(ev.key); ev.unread && i >= 0 {
		c := &m.chats[i]
		c.reacts++
		if ev.key == m.cur && m.focused { // seen right away
			cmds = append(cmds, m.reactReadCmd(c))
		}
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
		n.from, n.sender, n.out, n.at, n.replyTo, n.quote, n.topic = old.from, old.sender, old.out, old.at, old.replyTo, old.quote, old.topic
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

// nameOf finds the name of a sender (peer key) among the lines already loaded; "" when none has it.
func (m *model) nameOf(key string) string {
	if key == "" {
		return ""
	}
	for _, ls := range m.msgs {
		for i := len(ls) - 1; i >= 0; i-- {
			if ls[i].sender == key && ls[i].from != "?" && ls[i].from != "Thành viên" && !ls[i].out {
				return ls[i].from
			}
		}
	}
	return ""
}
