package main

// "Ai đã thả react": right-click a message › a sub-menu of who reacted with what, filtered by emoji, a click on
// a person opens the chat with them. Read only: nobody is told, nothing is marked read. In a private chat the
// answer is in the message itself (you and the other person); in groups it is one messages.getMessageReactionsList
// when the menu opens, and only when Telegram says the list can be seen (can_see_list: small groups). Channels
// hide who reacted.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

const reactorsMax = 100 // people asked for at once; more are counted in the title, not listed

type reactor struct {
	name, emoji string
	key         string
	peer        tg.InputPeerClass // to open the chat with them; nil for you
	mine        bool
}

// reactorsMsg answers the request for message id of view mk.
type reactorsMsg struct {
	mk    string
	id    int
	list  []reactor
	total int
	err   error
}

// reactionEmoji is how a reaction is drawn: the emoji, ✦ for a custom one, ⭐ for a paid one.
func reactionEmoji(r tg.ReactionClass) string {
	switch v := r.(type) {
	case *tg.ReactionEmoji:
		return clean(v.Emoticon)
	case *tg.ReactionCustomEmoji:
		return "✦"
	case *tg.ReactionPaid:
		return "⭐"
	}
	return ""
}

// canSeeReactors says whether the menu offers the list for l in chat c.
func canSeeReactors(c *chat, l line) bool {
	switch {
	case len(l.reacts) == 0 || l.id == 0 || c == nil:
		return false
	case c.key[0] == 'u':
		return true
	case c.broadcast:
		return false
	}
	return l.seeWho || (l.msg != nil && l.msg.Reactions.CanSeeList)
}

// privateReactors needs no request: in a private chat a reaction is yours, theirs, or (count 2) both.
func privateReactors(c *chat, rs []reaction) []reactor {
	var out []reactor
	for _, r := range rs {
		if r.mine {
			out = append(out, reactor{name: "Bạn", emoji: r.emoji, mine: true})
		}
		if !r.mine || r.count > 1 {
			out = append(out, reactor{name: c.title, emoji: r.emoji, key: c.key, peer: c.peer})
		}
	}
	return out
}

func loadReactors(ctx context.Context, api *tg.Client, p tg.InputPeerClass, id int) ([]reactor, int, error) {
	res, err := api.MessagesGetMessageReactionsList(ctx, &tg.MessagesGetMessageReactionsListRequest{Peer: p, ID: id, Limit: reactorsMax})
	if err != nil {
		return nil, 0, err
	}
	users, chans := map[int64]*tg.User{}, map[int64]*tg.Channel{}
	chats := map[int64]*tg.Chat{}
	for _, u := range res.Users {
		if v, ok := u.(*tg.User); ok && v != nil {
			users[v.ID] = v
		}
	}
	for _, c := range res.Chats {
		switch v := c.(type) {
		case *tg.Channel:
			chans[v.ID] = v
		case *tg.Chat:
			chats[v.ID] = v
		}
	}
	var out []reactor
	for _, r := range res.Reactions {
		e := reactionEmoji(r.Reaction)
		if e == "" || r.PeerID == nil {
			continue
		}
		rc := reactor{emoji: e, key: peerKey(r.PeerID), mine: r.My}
		switch v := r.PeerID.(type) {
		case *tg.PeerUser:
			if u, ok := users[v.UserID]; ok {
				rc.name = clean(strings.TrimSpace(u.FirstName + " " + u.LastName))
				if rc.name == "" {
					rc.name = clean(u.Username)
				}
				if !u.Self {
					rc.peer = &tg.InputPeerUser{UserID: u.ID, AccessHash: u.AccessHash}
				}
			}
		case *tg.PeerChannel:
			if ch, ok := chans[v.ChannelID]; ok {
				rc.name, rc.peer = clean(ch.Title), &tg.InputPeerChannel{ChannelID: ch.ID, AccessHash: ch.AccessHash}
			}
		case *tg.PeerChat:
			if ch, ok := chats[v.ChatID]; ok {
				rc.name, rc.peer = clean(ch.Title), &tg.InputPeerChat{ChatID: ch.ID}
			}
		}
		if r.My {
			rc.name, rc.peer = "Bạn", nil
		}
		if rc.name == "" {
			rc.name = "Ai đó"
		}
		out = append(out, rc)
	}
	total := len(out)
	if res.NextOffset != "" { // more than one page: Telegram's count says how many in all
		total = max(res.Count, len(out))
	}
	return out, total, nil
}

// reactorsItem is the message menu's entry; nil when the list cannot be seen.
func (m *model) reactorsItem(c *chat, l line) *menuItem {
	if !canSeeReactors(c, l) {
		return nil
	}
	return &menuItem{icon: icHeart, label: "Ai đã thả react", hint: "›", run: func(m *model) tea.Cmd {
		if c.key[0] == 'u' {
			m.reactorsMenu(m.mk(), l.id, privateReactors(c, l.reacts), 0, "")
			return nil
		}
		m.subMenu("Đang tải…", []menuItem{{icon: icCancel, label: "Huỷ", hint: "Esc", run: func(*model) tea.Cmd { return nil }}})
		m.menu.wait = l.id
		ctx, api, p, mk, id := m.ctx, m.api, c.peer, m.mk(), l.id
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			list, total, err := loadReactors(ctx, api, p, id)
			return reactorsMsg{mk: mk, id: id, list: list, total: total, err: err}
		}
	}}
}

// onReactors shows the list in the menu that waits for it; a menu closed meanwhile stays closed.
func (m *model) onReactors(r reactorsMsg) tea.Cmd {
	if m.menu == nil || m.menu.wait != r.id {
		return nil
	}
	if r.err != nil {
		m.menu = nil
		if d, ok := tgerr.AsFloodWait(r.err); ok {
			m.err = "Telegram bắt chờ: thử lại sau " + strconv.Itoa(int(d.Seconds())+1) + " giây"
		} else {
			m.err = "xem ai thả react lỗi: " + errText(r.err)
		}
		return nil
	}
	m.reactorsMenu(r.mk, r.id, r.list, r.total, "")
	return nil
}

// reactorsMenu fills the menu with the people who reacted (only those with emoji `only`, when set): first one
// row per emoji to filter by (when there are several), then the people, as many as the window holds.
func (m *model) reactorsMenu(mk string, id int, list []reactor, total int, only string) {
	var kinds []string
	count := map[string]int{}
	for _, r := range list {
		if count[r.emoji] == 0 {
			kinds = append(kinds, r.emoji)
		}
		count[r.emoji]++
	}
	var items []menuItem
	if len(kinds) > 1 {
		filter := func(e string) func(*model) tea.Cmd {
			return func(m *model) tea.Cmd { m.reactorsMenu(mk, id, list, total, e); return nil }
		}
		all := menuItem{label: "Tất cả · " + strconv.Itoa(len(list)), run: filter("")}
		if only == "" {
			all.hint = "✓"
		}
		items = append(items, all)
		for _, e := range kinds {
			it := menuItem{label: e + " · " + strconv.Itoa(count[e]), run: filter(e)}
			if e == only {
				it.hint = "✓"
			}
			items = append(items, it)
		}
		items = append(items, menuSep)
	}
	room := max(m.h-8-len(items), 3) // the box must fit in the window
	shown := 0
	for _, r := range list {
		if only != "" && r.emoji != only {
			continue
		}
		if shown == room {
			break
		}
		shown++
		it := menuItem{label: cut(r.name, 28), hint: r.emoji, run: func(*model) tea.Cmd { return nil }}
		if r.peer != nil {
			r := r
			it.run = func(m *model) tea.Cmd { return m.openPeer(r) }
		}
		items = append(items, it)
	}
	n := len(list)
	if only != "" {
		n = count[only]
	}
	title := "Ai đã thả react"
	switch {
	case total > len(list) && only == "":
		title += fmt.Sprintf(" (%d/%d)", shown, total)
	case shown < n:
		title += fmt.Sprintf(" (%d/%d)", shown, n)
	}
	m.subMenu(title, items)
	m.menu.wait = id
}

// openPeer opens the chat with someone from the list, adding it to the chat list when it is not there yet.
func (m *model) openPeer(r reactor) tea.Cmd {
	if i := m.index(r.key); i >= 0 {
		return m.open(i)
	}
	m.found = append(m.found[:0], chat{key: r.key, title: r.name, peer: r.peer})
	return m.open(len(m.chats))
}
