package main

// Chat folders (Telegram's "Folders": Cá nhân, Công việc...). They are read once at start and again when they
// change on another device (updateDialogFilter*), shown as a row of tabs right above the chat list (the blank
// row there; nothing moves when there are none), switched by a click or Ctrl+L. A folder only filters the list;
// a search (Ctrl+K) looks through every chat.

import (
	"context"
	"errors"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

type folder struct {
	id     int
	title  string
	all    bool             // "Tất cả": every chat
	shared bool             // a shared folder (chat list): its chats are edited in Telegram, not here
	raw    *tg.DialogFilter // what the server sent, sent back changed to add or remove a chat; nil for all/shared
	// the chats named in the folder: always in (include, pinned) or always out (exclude)
	include, exclude map[string]bool
	// the kinds of chats it takes, and the ones it leaves out among those
	contacts, nonContacts, groups, broadcasts, bots bool
	exMuted, exRead                                 bool
}

type foldersMsg struct {
	list []folder
	err  error
}

// foldersChanged: the folders were changed on another device.
type foldersChanged struct{}

// inputKey is the chat key of a peer named by a folder; "" for the kinds a folder cannot hold.
func inputKey(p tg.InputPeerClass, self string) string {
	switch v := p.(type) {
	case *tg.InputPeerUser:
		return "u" + strconv.FormatInt(v.UserID, 10)
	case *tg.InputPeerChat:
		return "g" + strconv.FormatInt(v.ChatID, 10)
	case *tg.InputPeerChannel:
		return "c" + strconv.FormatInt(v.ChannelID, 10)
	case *tg.InputPeerSelf:
		return self
	}
	return ""
}

func keySet(ps []tg.InputPeerClass, self string) map[string]bool {
	out := map[string]bool{}
	for _, p := range ps {
		if k := inputKey(p, self); k != "" {
			out[k] = true
		}
	}
	return out
}

// toFolders keeps the server's order; "Tất cả" goes first when the server did not place it. Fewer than two
// folders means the user made none: no tabs.
func toFolders(fs []tg.DialogFilterClass, self string) []folder {
	var out []folder
	hasAll := false
	for _, f := range fs {
		switch v := f.(type) {
		case *tg.DialogFilterDefault:
			out, hasAll = append(out, folder{title: "Tất cả", all: true}), true
		case *tg.DialogFilter:
			in := keySet(v.IncludePeers, self)
			for k := range keySet(v.PinnedPeers, self) {
				in[k] = true
			}
			out = append(out, folder{
				id: v.ID, raw: v,
				title: clean(strings.TrimSpace(v.Title.Text)), include: in, exclude: keySet(v.ExcludePeers, self),
				contacts: v.Contacts, nonContacts: v.NonContacts, groups: v.Groups, broadcasts: v.Broadcasts, bots: v.Bots,
				exMuted: v.ExcludeMuted, exRead: v.ExcludeRead,
			})
		case *tg.DialogFilterChatlist:
			in := keySet(v.IncludePeers, self)
			for k := range keySet(v.PinnedPeers, self) {
				in[k] = true
			}
			out = append(out, folder{id: v.ID, shared: true, title: clean(strings.TrimSpace(v.Title.Text)), include: in, exclude: map[string]bool{}})
		}
	}
	if !hasAll {
		out = append([]folder{{title: "Tất cả", all: true}}, out...)
	}
	for i := range out {
		if out[i].title == "" {
			out[i].title = "Thư mục"
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// kindIn says whether the folder takes chats of c's kind (its flags), leaving the named chats aside.
func (f *folder) kindIn(c *chat) bool {
	g := *f
	g.include, g.exclude, g.exMuted, g.exRead = nil, nil, false, false
	return g.has(c)
}

// has says whether chat c is in folder f. Chats named in the folder are always in (or out); the others go by
// kind, then the muted/read exclusions.
func (f *folder) has(c *chat) bool {
	switch {
	case f.all:
		return true
	case f.exclude[c.key]:
		return false
	case f.include[c.key]:
		return true
	case f.exMuted && c.muted:
		return false
	case f.exRead && c.unread == 0 && c.reacts == 0:
		return false
	}
	switch c.key[0] {
	case 'u':
		switch {
		case c.bot:
			return f.bots
		case c.contact:
			return f.contacts
		}
		return f.nonContacts
	case 'g':
		return f.groups
	}
	if c.broadcast {
		return f.broadcasts
	}
	return f.groups
}

func (m model) foldersCmd() tea.Cmd {
	if m.api == nil {
		return nil
	}
	ctx, api, self := m.ctx, m.api, m.selfKey
	return func() tea.Msg {
		res, err := api.MessagesGetDialogFilters(ctx)
		if err != nil {
			return foldersMsg{err: err}
		}
		return foldersMsg{list: toFolders(res.Filters, self)}
	}
}

func (m *model) onFolders(msg foldersMsg) tea.Cmd {
	if msg.err != nil {
		return nil // best effort: without folders the list is just whole
	}
	cur := ""
	if m.folder < len(m.folders) {
		cur = m.folders[m.folder].title
	}
	m.folders, m.folder = msg.list, 0
	if m.tabMemo != nil {
		m.tabMemo.gen++
	}
	for i, f := range m.folders { // stay in the folder shown, if it is still there
		if f.title == cur {
			m.folder = i
		}
	}
	m.fitTabs()
	m.fixTop()
	return nil
}

// curFolder is the folder that filters the list; nil = every chat.
func (m *model) curFolder() *folder {
	if m.folder <= 0 || m.folder >= len(m.folders) || m.folders[m.folder].all {
		return nil
	}
	return &m.folders[m.folder]
}

// setFolder shows folder i (wrapping around) from the top of the list.
func (m *model) setFolder(i int) {
	if len(m.folders) == 0 {
		return
	}
	m.folder = (i + len(m.folders)) % len(m.folders)
	m.top, m.sel = 0, 0
	m.fitTabs()
}

// folderUnread counts the unread, not muted chats of folder f: the number on its tab.
func (m *model) folderUnread(f *folder) int {
	n := 0
	for i := range m.chats {
		if c := &m.chats[i]; c.unread > 0 && !c.muted && f.has(c) {
			n++
		}
	}
	return n
}

type folderSpan struct{ x0, x1, i int }

type tabMemo struct {
	gen   int // which reading of the folders
	sig   uint64
	row   string
	spans []folderSpan
}

// tabSig sums up everything the tabs show: the folders, the one shown, the width, and per chat what decides
// which folders it is in and whether it counts as unread (FNV-1a). Counting every folder's unread chats each
// frame cost ~0.1 ms with 1,000 chats and 8 folders; this pass is a few µs.
func (m *model) tabSig(w int) uint64 {
	h := uint64(14695981039346656037)
	mix := func(v uint64) { h = (h ^ v) * 1099511628211 }
	mix(uint64(len(m.folders)))
	mix(uint64(m.tabMemo.gen)) // bumped when the folders are read again
	mix(uint64(m.folder))
	mix(uint64(m.tabOff))
	mix(uint64(w))
	for i := range m.chats {
		c := &m.chats[i]
		for j := 0; j < len(c.key); j++ {
			mix(uint64(c.key[j]))
		}
		flags := uint64(0)
		for k, b := range []bool{c.muted, c.bot, c.contact, c.broadcast, c.reacts > 0} {
			if b {
				flags |= 1 << k
			}
		}
		mix(uint64(c.unread)<<8 | flags)
	}
	return h
}

// folderTabs draws the tabs in w cells and says where each one is, from the memo when nothing changed.
func (m *model) folderTabs(w int) (string, []folderSpan) {
	if m.tabMemo == nil {
		return m.drawTabs(w)
	}
	if sig := m.tabSig(w); sig != m.tabMemo.sig || m.tabMemo.row == "" {
		m.tabMemo.row, m.tabMemo.spans = m.drawTabs(w)
		m.tabMemo.sig = sig
	}
	return m.tabMemo.row, m.tabMemo.spans
}

// Spans of the scroll arrows in folderTabs (their i).
const (
	tabLeft  = -1
	tabRight = -2
)

// drawTabs draws the tabs from m.tabOff in w cells: "‹" at the left when tabs are hidden before, "›" at the right
// when some are hidden after; both are spans too (i = tabLeft / tabRight), so they can be clicked.
func (m *model) drawTabs(w int) (string, []folderSpan) {
	n := len(m.folders)
	if n == 0 {
		return "", nil
	}
	off := min(max(m.tabOff, 0), n-1)
	const gap = 2
	var b strings.Builder
	var spans []folderSpan
	if off > 0 {
		b.WriteString(ink(cAccent, "‹") + " ")
		spans = append(spans, folderSpan{0, 2, tabLeft})
	} else {
		b.WriteString("  ")
	}
	x, more := 2, false
	for i := off; i < n; i++ {
		f := &m.folders[i]
		l := cut(f.title, 16)
		if i == m.folder {
			l = inkB(cAccent, l)
		} else {
			l = ink(cMuted, l)
		}
		if c := m.folderUnread(f); c > 0 && !f.all {
			l += " " + ink(cMuted, strconv.Itoa(c))
		}
		lw := ansi.StringWidth(l)
		room := w
		if i < n-1 {
			room = w - 2 // keep " ›" free unless this is the last tab
		}
		if x+lw > room {
			more = true
			break
		}
		spans = append(spans, folderSpan{x, x + lw, i})
		b.WriteString(l + strings.Repeat(" ", gap))
		x += lw + gap
	}
	row := ansi.Truncate(b.String(), w, "")
	if more && w >= 4 {
		row = ansi.Truncate(row, w-2, "")
		row += strings.Repeat(" ", max(w-2-ansi.StringWidth(row), 0)) + " " + ink(cAccent, "›")
		spans = append(spans, folderSpan{w - 2, w, tabRight})
	}
	return row, spans
}

// tabShown says whether folder i has a tab in the row drawn from the current offset.
func (m *model) tabShown(i int) bool {
	_, spans := m.drawTabs(m.side)
	for _, s := range spans {
		if s.i == i {
			return true
		}
	}
	return false
}

// fitTabs scrolls the tabs so the folder shown has its tab: after Ctrl+L, a click, new folders or a new width.
func (m *model) fitTabs() {
	m.tabW = m.side
	if len(m.folders) == 0 {
		m.tabOff = 0
		return
	}
	m.tabOff = min(max(m.tabOff, 0), len(m.folders)-1)
	if m.folder < m.tabOff {
		m.tabOff = m.folder
	}
	for m.tabOff < m.folder && !m.tabShown(m.folder) {
		m.tabOff++
	}
	// room on the right (a wider list, or scrolled to the end): bring hidden tabs back in from the left
	hadRight := m.tabShown(tabRight)
	for m.tabOff > 0 {
		m.tabOff--
		if !m.tabShown(m.folder) || (!hadRight && m.tabShown(tabRight)) {
			m.tabOff++
			break
		}
	}
}

// scrollTabs moves the tabs by d (the wheel over the row, or a click on an arrow); not past the last one.
func (m *model) scrollTabs(d int) {
	if len(m.folders) == 0 {
		return
	}
	if d > 0 && !m.tabShown(tabRight) {
		return // the last tab is in view already
	}
	m.tabOff = min(max(m.tabOff+d, 0), len(m.folders)-1)
}

// clickFolder opens the folder whose tab is at column x.
func (m *model) clickFolder(x int) bool {
	_, spans := m.folderTabs(m.side)
	for _, s := range spans {
		if x >= s.x0 && x < s.x1 {
			switch s.i {
			case tabLeft:
				m.scrollTabs(-1)
			case tabRight:
				m.scrollTabs(1)
			default:
				m.setFolder(s.i)
			}
			return true
		}
	}
	return false
}

// foldersReload re-reads the folders after a change made elsewhere; it re-arms wait like every event handler.
func (m *model) foldersReload() tea.Cmd {
	return tea.Batch(m.wait(), m.foldersCmd())
}

// --- adding and removing chats ---------------------------------------------------------

// withChat is folder f's filter with chat c added (in = true) or taken out, as Telegram's apps do it: adding
// drops c from the excluded chats and names it among the included ones (a chat pinned in the folder stays
// pinned); removing drops it from the included and pinned ones and, when the folder would still take it by its
// kind (all groups, say), excludes it. The server's slices are never changed in place.
func withChat(f *folder, c *chat, in bool, self string) (*tg.DialogFilter, error) {
	if f.raw == nil || c.peer == nil {
		return nil, errors.New("không sửa được thư mục này")
	}
	if _, empty := c.peer.(*tg.InputPeerEmpty); empty {
		return nil, errors.New("không sửa được thư mục này")
	}
	nf := *f.raw
	drop := func(ps []tg.InputPeerClass) []tg.InputPeerClass {
		out := make([]tg.InputPeerClass, 0, len(ps))
		for _, p := range ps {
			if inputKey(p, self) != c.key {
				out = append(out, p)
			}
		}
		return out
	}
	named := func(ps []tg.InputPeerClass) bool { return len(drop(ps)) != len(ps) }
	nf.ExcludePeers = drop(f.raw.ExcludePeers)
	if in {
		if !named(f.raw.IncludePeers) && !named(f.raw.PinnedPeers) { // already named (or pinned): it stays as it is
			nf.IncludePeers = append(append([]tg.InputPeerClass(nil), f.raw.IncludePeers...), c.peer)
		}
		return &nf, nil
	}
	nf.IncludePeers, nf.PinnedPeers = drop(f.raw.IncludePeers), drop(f.raw.PinnedPeers)
	if f.kindIn(c) {
		nf.ExcludePeers = append(nf.ExcludePeers, c.peer)
	}
	return &nf, nil
}

// saveFolder sends the changed folder to Telegram; its refusals are said in Vietnamese.
func saveFolder(ctx context.Context, api *tg.Client, nf *tg.DialogFilter) error {
	req := &tg.MessagesUpdateDialogFilterRequest{ID: nf.ID}
	req.SetFilter(nf)
	_, err := api.MessagesUpdateDialogFilter(ctx, req)
	switch {
	case tgerr.Is(err, "FILTER_INCLUDE_EMPTY"):
		return errors.New("thư mục phải còn ít nhất một chat hoặc một loại chat")
	case tgerr.Is(err, "FILTER_INCLUDE_TOO_MUCH", "FILTER_EXCLUDE_TOO_MUCH", "DIALOG_FILTERS_TOO_MUCH", "CHATLISTS_TOO_MUCH"):
		return errors.New("thư mục đã đủ số chat tối đa Telegram cho phép (100, 200 với Premium)")
	}
	return err
}

// folderItems is the sub-menu of a chat's right-click menu: every folder the user made, ✓ on the ones that
// hold the chat; picking one adds the chat or takes it out.
func (m *model) folderItems(c chat) []menuItem {
	ctx, api, self := m.ctx, m.api, m.selfKey
	var items []menuItem
	for i := range m.folders {
		f := m.folders[i]
		if f.all {
			continue
		}
		in := f.has(&c)
		it := menuItem{icon: icFolder, label: cut(f.title, 24)}
		if in {
			it.hint = "✓"
		}
		switch {
		case f.shared:
			it.run = func(m *model) tea.Cmd { return m.toastCmd("Thư mục chia sẻ: thêm/bớt chat trong app Telegram") }
		default:
			it.run = func(m *model) tea.Cmd {
				nf, err := withChat(&f, &c, !in, self)
				if err != nil {
					m.err = err.Error()
					return nil
				}
				return func() tea.Msg {
					return actMsg{kind: "folder", key: c.key, text: c.title + "\x00" + f.title, rsOK: !in, folder: nf, err: saveFolder(ctx, api, nf)}
				}
			}
		}
		items = append(items, it)
	}
	return items
}

// onFolderSaved puts the folder Telegram accepted in place, so the tabs and the list follow at once.
func (m *model) onFolderSaved(a actMsg) tea.Cmd {
	who, where, _ := strings.Cut(a.text, "\x00")
	for i := range m.folders {
		if a.folder != nil && m.folders[i].id == a.folder.ID && !m.folders[i].shared && !m.folders[i].all {
			if fs := toFolders([]tg.DialogFilterClass{&tg.DialogFilterDefault{}, a.folder}, m.selfKey); len(fs) == 2 {
				m.folders[i] = fs[1]
			}
		}
	}
	if m.tabMemo != nil {
		m.tabMemo.gen++
	}
	m.fixTop()
	if a.rsOK {
		return m.toastCmd("Đã thêm " + who + " vào " + where)
	}
	return m.toastCmd("Đã bỏ " + who + " khỏi " + where)
}
