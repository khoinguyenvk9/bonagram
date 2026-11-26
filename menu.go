package main

// Right-click menus. A message: a row of quick reactions on top (one click reacts), then its actions (reply,
// copy, pin, forward, edit, delete, links, files), each with a Nerd Font icon and its browse-mode key. A chat
// in the list: pin, mute, delete or leave. Each item runs the same code as its key.

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Nerd Font (Font Awesome) icons, one cell each. BONAGRAM_ICONS=off leaves them out for fonts without them.
const (
	icReply    = ""
	icCopy     = ""
	icPin      = ""
	icForward  = ""
	icEdit     = ""
	icTrash    = ""
	icLink     = ""
	icOpen     = ""
	icImage    = ""
	icDownload = ""
	icBell     = ""
	icBellOff  = ""
	icLeave    = ""
	icCancel   = ""
	icFolder   = ""
	icHeart    = ""
)

// menuItem is one row; an item without run is a separator line between groups.
type menuItem struct {
	icon   string
	label  string
	hint   string // the key that does the same in browse mode, shown on the right
	danger bool   // drawn in the error colour (delete)
	run    func(m *model) tea.Cmd
}

var menuSep = menuItem{}

type ctxMenu struct {
	x, y   int    // top-left cell of the box on screen
	title  string // first row of a sub-menu
	reacts *line  // the message whose quick reactions make the top row; nil: no such row
	items  []menuItem
	wait   int // id of the message whose reactors this menu shows or waits for (reactors.go)
	sel    int // highlighted item; -1: the reaction row, where rsel is the emoji
	rsel   int
	icons  bool
}

// menuFor lists what can be done with l; items that would only answer "not possible" are left out.
func (m *model) menuFor(l line) []menuItem {
	c := m.curChat()
	if c == nil {
		return nil
	}
	key := func(s string) func(*model) tea.Cmd { // the browse-mode key s, on this message
		return func(m *model) tea.Cmd { cmd, _ := m.actionKey(s, l); return cmd }
	}
	it := []menuItem{{icon: icReply, label: "Trả lời", hint: "^R", run: func(m *model) tea.Cmd { m.setReply(&l); return nil }}}
	if _, p := photoOf(l.msg); p {
		it = append(it, menuItem{icon: icImage, label: "Xem ảnh", hint: "Enter", run: func(m *model) tea.Cmd { return m.mediaCmd(l) }})
	} else if d, ok := docOf(l.msg); ok {
		it = append(it, menuItem{icon: icDownload, label: "Tải tệp · " + humanSize(d.Size), hint: "Enter", run: func(m *model) tea.Cmd { return m.mediaCmd(l) }})
	}
	copyLabel := "Chép tin"
	if len(l.segs) > 0 {
		copyLabel = "Chép code"
	}
	it = append(it, menuItem{icon: icCopy, label: copyLabel, hint: "C", run: func(m *model) tea.Cmd { return m.copyFrom(l) }})
	if ri := m.reactorsItem(c, l); ri != nil {
		it = append(it, *ri)
	}
	if len(linksOf(l.msg)) > 0 {
		it = append(it, menuItem{icon: icOpen, label: "Mở link", hint: "O", run: key("o")}, menuItem{icon: icLink, label: "Chép link", hint: "L", run: key("l")})
	}
	if l.id != 0 {
		it = append(it, m.pinItem(*c, l), menuItem{icon: icForward, label: "Chuyển tiếp", hint: "F", run: key("f")})
	}
	if m.mine(l, c) && l.id != 0 {
		it = append(it,
			menuItem{icon: icEdit, label: "Sửa", hint: "E", run: key("e")},
			menuItem{icon: icTrash, label: "Xoá", hint: "D  ›", danger: true, run: func(m *model) tea.Cmd {
				m.subMenu("Xoá tin này?", []menuItem{
					{icon: icTrash, label: "Xoá với mọi người", danger: true, run: func(m *model) tea.Cmd { m.delID = l.id; return m.answerPrompt("y") }},
					{icon: icCancel, label: "Huỷ", hint: "Esc", run: func(*model) tea.Cmd { return nil }},
				})
				return nil
			}})
	}
	return it
}

// pinItem is "Bỏ ghim" for a pinned message, else a sub-menu with the two ways to pin it: in a group or
// channel with or without notifying the members, in a private chat for both people or for you only.
func (m *model) pinItem(c chat, l line) menuItem {
	mk, id := m.mk(), l.id
	pin := func(unpin, silent, oneSide bool) func(*model) tea.Cmd {
		return func(m *model) tea.Cmd {
			ctx, api := m.ctx, m.api
			return func() tea.Msg {
				err := pinMessage(ctx, api, c.peer, id, unpin, silent, oneSide)
				return actMsg{kind: "pin", mk: mk, key: c.key, id: id, rsOK: unpin, err: err}
			}
		}
	}
	for _, p := range m.pins[mk] {
		if p.id == id {
			return menuItem{icon: icPin, label: "Bỏ ghim", run: pin(true, true, false)}
		}
	}
	opts := []menuItem{
		{icon: icBell, label: "Ghim và báo cho mọi người", run: pin(false, false, false)},
		{icon: icBellOff, label: "Ghim im lặng", run: pin(false, true, false)},
	}
	if c.key[0] == 'u' {
		opts = []menuItem{
			{icon: icPin, label: "Ghim cho cả hai bên", run: pin(false, false, false)},
			{icon: icPin, label: "Chỉ ghim cho tôi", run: pin(false, true, true)},
		}
	}
	return menuItem{icon: icPin, label: "Ghim", hint: "›", run: func(m *model) tea.Cmd { m.subMenu("Ghim tin này", opts); return nil }}
}

// openChatMenu is the right-click menu of a chat in the list: pin, notifications on/off, delete or leave.
func (m *model) openChatMenu(c chat, x, y int) {
	key := c.key
	act := func(kind string, on bool, do func() error) tea.Cmd {
		return func() tea.Msg { return actMsg{kind: kind, key: key, rsOK: on, text: c.title, err: do()} }
	}
	ctx, api := m.ctx, m.api
	mute := menuItem{icon: icBellOff, label: "Tắt thông báo", run: func(*model) tea.Cmd {
		return act("mute", true, func() error { return setMuted(ctx, api, c.peer, true) })
	}}
	if c.muted {
		mute = menuItem{icon: icBell, label: "Bật thông báo", run: func(*model) tea.Cmd {
			return act("mute", false, func() error { return setMuted(ctx, api, c.peer, false) })
		}}
	}
	pin := menuItem{icon: icPin, label: "Ghim lên đầu", run: func(*model) tea.Cmd {
		return act("pinchat", true, func() error { return pinChat(ctx, api, c.peer, true) })
	}}
	if c.pinned {
		pin = menuItem{icon: icPin, label: "Bỏ ghim chat", run: func(*model) tea.Cmd {
			return act("pinchat", false, func() error { return pinChat(ctx, api, c.peer, false) })
		}}
	}
	items := []menuItem{pin, mute}
	if fi := m.folderItems(c); len(fi) > 0 {
		items = append(items, menuItem{icon: icFolder, label: "Thư mục", hint: "›", run: func(m *model) tea.Cmd {
			m.subMenu("Thư mục của "+cut(c.title, 24), append(fi, menuSep, menuItem{icon: icCancel, label: "Huỷ", hint: "Esc", run: func(*model) tea.Cmd { return nil }}))
			return nil
		}})
	}
	del := func(revoke bool) func(*model) tea.Cmd {
		return func(*model) tea.Cmd {
			return act("delchat", revoke, func() error { return deleteChat(ctx, api, c.peer, revoke) })
		}
	}
	cancel := menuItem{icon: icCancel, label: "Huỷ", hint: "Esc", run: func(*model) tea.Cmd { return nil }}
	switch {
	case key == m.selfKey: // Saved Messages holds your notes: not deleted from here
	case key[0] == 'u':
		items = append(items, menuSep, menuItem{icon: icTrash, label: "Xoá chat", hint: "›", danger: true, run: func(m *model) tea.Cmd {
			m.subMenu("Xoá chat với "+c.title+"?", []menuItem{
				{icon: icTrash, label: "Chỉ xoá phía tôi", run: del(false)},
				{icon: icTrash, label: "Xoá cho cả hai bên", danger: true, run: del(true)},
				cancel,
			})
			return nil
		}})
	default:
		items = append(items, menuSep, menuItem{icon: icLeave, label: "Rời nhóm", hint: "›", danger: true, run: func(m *model) tea.Cmd {
			m.subMenu("Rời "+c.title+"?", []menuItem{
				{icon: icLeave, label: "Rời và xoá khỏi danh sách", danger: true, run: del(false)},
				cancel,
			})
			return nil
		}})
	}
	m.menu = &ctxMenu{x: x, y: y, items: items, icons: m.cfg.icons}
	m.placeMenu()
}

// menuReact gives quick reaction n (0-based) to the menu's message; one you already gave is taken back.
func (m *model) menuReact(n int) tea.Cmd {
	l := m.menu.reacts
	if l == nil || n < 0 || n >= len(quickReactions) {
		return nil
	}
	m.menu = nil
	cp := *l
	m.react = &cp
	cmd := m.answerPrompt(strconv.Itoa(n + 1))
	if m.browse {
		m.render()
	}
	return cmd
}

// openMenu selects the message under the mouse (like a click) and shows its menu there.
func (m *model) openMenu(x, y int) {
	i, ok := m.msgIndexAt(y)
	if !ok {
		return
	}
	if m.searching {
		m.searching = false
		m.search.Blur()
	}
	m.delID, m.react = 0, nil
	m.setPrompt("")
	m.bsel, m.browse = i, true
	m.in.Blur()
	m.render()
	l := m.lines()[i]
	if it := m.menuFor(l); len(it) > 0 {
		m.menu = &ctxMenu{x: x, y: y, items: it, icons: m.cfg.icons}
		if l.id != 0 {
			m.menu.reacts = &l
		}
		m.placeMenu()
	}
}

// subMenu replaces the items in place (pin choices, delete confirmation).
func (m *model) subMenu(title string, it []menuItem) {
	m.menu = &ctxMenu{x: m.menu.x, y: m.menu.y, title: title, items: it, icons: m.menu.icons}
	m.placeMenu()
}

const (
	menuPad   = 2 // blank cells left and right inside the box
	menuGap   = 4 // at least this much between a label and its key
	reactSlot = 4 // cells per emoji in the reaction row
)

// head is the number of rows above the first item: a blank one, then the title or the reactions with a line.
func (mn *ctxMenu) head() int {
	h := 1
	if mn.title != "" {
		h += 2
	}
	if mn.reacts != nil {
		h += 2
	}
	return h
}

func (mn *ctxMenu) iconW() int {
	if mn.icons {
		return 3 // the icon and two spaces
	}
	return 0
}

// menuCols is the widest label and the widest key hint.
func (mn *ctxMenu) menuCols() (label, hint int) {
	for _, i := range mn.items {
		label, hint = max(label, ansi.StringWidth(i.label)), max(hint, ansi.StringWidth(i.hint))
	}
	return
}

// width of the whole box.
func (mn *ctxMenu) width() int {
	label, hint := mn.menuCols()
	w := menuPad + mn.iconW() + label + menuGap + hint + menuPad
	if mn.title != "" {
		w = max(w, ansi.StringWidth(mn.title)+2*menuPad)
	}
	if mn.reacts != nil {
		w = max(w, len(quickReactions)*reactSlot+2*menuPad)
	}
	return w
}

// placeMenu keeps the box on screen (above the key line), moving it left/up from the mouse when needed.
func (m *model) placeMenu() {
	mn := m.menu
	w, h := mn.width(), mn.head()+len(mn.items)+1
	mn.x = max(0, min(mn.x, m.w-w))
	mn.y = max(0, min(mn.y, m.h-1-h))
}

// menuHit is what is under screen cell (x, y): an item (-1 when none, or a separator) or, on the reaction
// row, an emoji (react, -1 when none).
func (m *model) menuHit(x, y int) (item, react int) {
	mn := m.menu
	item, react = -1, -1
	if x < mn.x || x >= mn.x+mn.width() {
		return
	}
	if mn.reacts != nil && y == mn.y+1 {
		if i := (x - mn.x - menuPad) / reactSlot; x >= mn.x+menuPad && i < len(quickReactions) {
			react = i
		}
		return
	}
	if r := y - mn.y - mn.head(); r >= 0 && r < len(mn.items) && mn.items[r].run != nil {
		item = r
	}
	return
}

// step moves the highlight d rows, over separators, wrapping around; the reaction row sits above the items.
func (mn *ctxMenu) step(d int) {
	n := len(mn.items)
	lo := 0
	if mn.reacts != nil {
		lo = -1
	}
	for i := 0; i < n+1; i++ {
		mn.sel += d
		switch {
		case mn.sel < lo:
			mn.sel = n - 1
		case mn.sel >= n:
			mn.sel = lo
		}
		if mn.sel == -1 || mn.items[mn.sel].run != nil {
			return
		}
	}
}

// pickMenu closes the menu and runs item i (which may open a sub-menu instead).
func (m *model) pickMenu(i int) tea.Cmd {
	if i < 0 || i >= len(m.menu.items) || m.menu.items[i].run == nil {
		return nil
	}
	mn := m.menu
	cmd := mn.items[i].run(m)
	if m.menu == mn { // not replaced by a sub-menu: done
		m.menu = nil
	}
	if m.menu == nil && m.browse { // an action that kept the message selected: redraw without the box
		m.render()
	}
	return cmd
}

func (m *model) menuKey(s string) tea.Cmd {
	mn := m.menu
	if n, err := strconv.Atoi(s); err == nil && mn.reacts != nil { // 1..7: that reaction at once
		return m.menuReact(n - 1)
	}
	switch s {
	case "up", "k":
		mn.step(-1)
	case "down", "j", "tab":
		mn.step(1)
	case "left", "h":
		if mn.sel == -1 {
			mn.rsel = (mn.rsel - 1 + len(quickReactions)) % len(quickReactions)
		}
	case "right", "l":
		if mn.sel == -1 {
			mn.rsel = (mn.rsel + 1) % len(quickReactions)
		}
	case "enter", " ":
		if mn.sel == -1 {
			return m.menuReact(mn.rsel)
		}
		return m.pickMenu(mn.sel)
	case "esc", "q":
		m.menu = nil
	}
	return nil
}

// menuMouse: hovering highlights, a left click on an item or an emoji picks it, any click elsewhere closes
// the menu (handled = false: a right click elsewhere then opens the menu of what is there).
func (m *model) menuMouse(e tea.MouseMsg) (tea.Cmd, bool) {
	r, react := m.menuHit(e.X, e.Y)
	switch {
	case e.Action == tea.MouseActionMotion:
		if r >= 0 {
			m.menu.sel = r
		} else if react >= 0 {
			m.menu.sel, m.menu.rsel = -1, react
		}
	case e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonLeft && react >= 0:
		return m.menuReact(react), true
	case e.Action == tea.MouseActionPress && e.Button == tea.MouseButtonLeft && r >= 0:
		return m.pickMenu(r), true
	case e.Action == tea.MouseActionPress:
		if m.inMenu(e.X, e.Y) && e.Button == tea.MouseButtonLeft {
			return nil, true // a click on padding or a line inside the box: nothing
		}
		m.menu = nil
		return nil, e.Button != tea.MouseButtonRight
	}
	return nil, true
}

// inMenu tells whether (x, y) is inside the box.
func (m *model) inMenu(x, y int) bool {
	mn := m.menu
	h := mn.head() + len(mn.items) + 1
	return x >= mn.x && x < mn.x+mn.width() && y >= mn.y && y < mn.y+h
}

// overlayMenu draws the box over the finished frame rows: no border, a lighter background, the reaction row
// or the sub-menu title on top, a thin line under it, icons, keys right-aligned and muted, the highlighted
// row in the selection colour and delete in red.
func (m *model) overlayMenu(rows []string) {
	mn := m.menu
	w := mn.width()
	_, hintW := mn.menuCols()
	pad := strings.Repeat(" ", menuPad)
	blank := paint("", w, cFg, cBoost)
	line := paint(pad+ink(cDivider, strings.Repeat("─", max(w-2*menuPad, 0))), w, cFg, cBoost)
	box := []string{blank}
	if mn.title != "" {
		box = append(box, paint(pad+inkB(cMuted, mn.title), w, cFg, cBoost), line)
	}
	if mn.reacts != nil {
		var b strings.Builder
		b.WriteString(paint(pad, menuPad, cFg, cBoost))
		for i, e := range quickReactions {
			bg := cBoost
			for _, r := range mn.reacts.reacts {
				if r.mine && r.emoji == e {
					bg = cWarm // already given: a click takes it back
				}
			}
			if mn.sel == -1 && i == mn.rsel {
				bg = cBorder
			}
			b.WriteString(paint(" "+e, reactSlot, cFg, bg))
		}
		box = append(box, paint(b.String(), w, cFg, cBoost), line)
	}
	for i, it := range mn.items {
		if it.run == nil {
			box = append(box, line)
			continue
		}
		bg, txt, iconCol := cBoost, cFg, cMuted
		if it.danger {
			txt, iconCol = cErr, cErr
		}
		label := ink(txt, it.label)
		if i == mn.sel {
			bg, label = cKey, inkB(txt, it.label)
			if !it.danger {
				iconCol = cAccent
			}
		}
		icon := ""
		if mn.icons && it.icon != "" {
			icon = ink(iconCol, it.icon) + "  "
		} else if mn.icons {
			icon = "   "
		}
		hint := ink(cMuted, strings.Repeat(" ", hintW-ansi.StringWidth(it.hint))+it.hint)
		gap := w - 2*menuPad - mn.iconW() - ansi.StringWidth(it.label) - hintW
		box = append(box, paint(pad+icon+label+strings.Repeat(" ", max(gap, 1))+hint, w, cFg, bg))
	}
	box = append(box, blank)
	overlayAt(rows, mn.x, mn.y, box, w)
}

// overlayAt draws box (rows of width w) over the finished frame rows, its top-left cell at (x, y).
func overlayAt(rows []string, x, y int, box []string, w int) {
	for i, b := range box {
		if y+i < 0 || y+i >= len(rows) {
			continue
		}
		// close a hyperlink the left part may have opened, so the box is not part of it
		rows[y+i] = ansi.Truncate(rows[y+i], x, "") + "\x1b]8;;\x1b\\" + b + ansi.TruncateLeft(rows[y+i], x+w, "")
	}
}

// msgIndexAt is the message drawn at screen row y of the message view.
func (m *model) msgIndexAt(y int) (int, bool) {
	top := m.msgY()
	if y < top || y >= top+m.vp.Height || len(m.lineAt) == 0 {
		return 0, false
	}
	row, i := m.vp.YOffset+y-top, 0
	for j, at := range m.lineAt {
		if at <= row {
			i = j
		}
	}
	return i, i < len(m.lines())
}
