package main

// "Đang soạn tin…" and online status. Telegram sends a typing update every few seconds while someone types and
// drops it after 6 s without one; the app keeps who is typing per chat (m.typing) and shows it in place of the
// last message in the list and in the chat's title row. Our own typing is announced at most every 5 s.
// Online status comes with the users of the chat list and live as updateUserStatus.

import (
	"context"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/tg"
)

const (
	typingFor   = 6 * time.Second // how long one typing update lasts (Telegram's own timeout)
	typingEvery = 5 * time.Second // our own typing is sent at most this often per chat
)

// typingMsg: someone (peer key who; "" in a private chat) does something in chat key; stop = they stopped.
type typingMsg struct {
	key, who, name string
	topic          int
	action         string
	stop           bool
}

type typer struct {
	who, name, action string
	topic             int
	until             time.Time
}

type typingTick struct{}

// statusMsg: user key went online or offline.
type statusMsg struct {
	key string
	p   presence
}

// presence is when a user was last online: until > now = online now; seen = the time they left; kind is what
// Telegram says when it hides the time ("recently", "week", "month", "long").
type presence struct {
	until, seen int
	kind        string
}

func presenceOf(s tg.UserStatusClass) presence {
	switch v := s.(type) {
	case *tg.UserStatusOnline:
		return presence{until: v.Expires}
	case *tg.UserStatusOffline:
		return presence{seen: v.WasOnline}
	case *tg.UserStatusRecently:
		return presence{kind: "recently"}
	case *tg.UserStatusLastWeek:
		return presence{kind: "week"}
	case *tg.UserStatusLastMonth:
		return presence{kind: "month"}
	case *tg.UserStatusEmpty:
		return presence{kind: "long"}
	}
	return presence{}
}

// presenceText is the status under a person's name: "đang hoạt động", "hoạt động 5 phút trước", ...
func presenceText(p presence, now time.Time) string {
	n := int(now.Unix())
	if p.until > n {
		return "đang hoạt động"
	}
	seen := p.seen
	if seen == 0 {
		seen = p.until // an "online until" that passed is when they left
	}
	if seen > 0 {
		at := time.Unix(int64(seen), 0).In(now.Location())
		d := now.Sub(at)
		y, mo, dd := now.Date()
		today := time.Date(y, mo, dd, 0, 0, 0, 0, now.Location())
		switch {
		case d < time.Minute:
			return "vừa hoạt động"
		case d < time.Hour:
			return "hoạt động " + strconv.Itoa(int(d/time.Minute)) + " phút trước"
		case !at.Before(today):
			return "hoạt động lúc " + at.Format("15:04")
		case !at.Before(today.AddDate(0, 0, -1)):
			return "hoạt động hôm qua lúc " + at.Format("15:04")
		case at.Year() == now.Year():
			return "hoạt động ngày " + at.Format("02/01")
		}
		return "hoạt động ngày " + at.Format("02/01/2006")
	}
	switch p.kind {
	case "recently":
		return "hoạt động gần đây"
	case "week":
		return "hoạt động trong tuần"
	case "month":
		return "hoạt động trong tháng"
	case "long":
		return "đã lâu không hoạt động"
	}
	return ""
}

// actionText says what a typing update means; "" for the ones not shown (games, imports, bot drafts...).
func actionText(a tg.SendMessageActionClass) (text string, stop bool) {
	switch a.(type) {
	case *tg.SendMessageCancelAction:
		return "", true
	case *tg.SendMessageTypingAction:
		return "đang soạn tin", false
	case *tg.SendMessageRecordAudioAction:
		return "đang ghi âm", false
	case *tg.SendMessageRecordVideoAction, *tg.SendMessageRecordRoundAction:
		return "đang quay video", false
	case *tg.SendMessageUploadPhotoAction:
		return "đang gửi ảnh", false
	case *tg.SendMessageUploadVideoAction, *tg.SendMessageUploadRoundAction:
		return "đang gửi video", false
	case *tg.SendMessageUploadAudioAction:
		return "đang gửi tin thoại", false
	case *tg.SendMessageUploadDocumentAction:
		return "đang gửi tệp", false
	case *tg.SendMessageChooseStickerAction:
		return "đang chọn sticker", false
	case *tg.SendMessageGeoLocationAction:
		return "đang chọn vị trí", false
	case *tg.SendMessageChooseContactAction:
		return "đang chọn liên hệ", false
	}
	return "", false
}

// typingFrom builds the message for chat key from the typer's peer (nil in a private chat).
func typingFrom(e tg.Entities, key string, from tg.PeerClass, topic int, a tg.SendMessageActionClass) (typingMsg, bool) {
	text, stop := actionText(a)
	if key == "" || (text == "" && !stop) {
		return typingMsg{}, false
	}
	ev := typingMsg{key: key, topic: topic, action: text, stop: stop}
	if from != nil {
		ev.who = peerKey(from)
		if u, ok := from.(*tg.PeerUser); ok {
			if usr, ok := e.Users[u.UserID]; ok && usr != nil {
				ev.name = firstWord(clean(strings.TrimSpace(usr.FirstName + " " + usr.LastName)))
			}
		}
	}
	return ev, true
}

// onTyping keeps who is typing where; a ticker drops the ones that went quiet while any are left.
func (m *model) onTyping(ev typingMsg) tea.Cmd {
	cmds := []tea.Cmd{m.wait()}
	if ev.key == m.selfKey {
		return cmds[0]
	}
	ts := m.typing[ev.key]
	for i := range ts {
		if ts[i].who == ev.who {
			ts = append(ts[:i:i], ts[i+1:]...)
			break
		}
	}
	if !ev.stop {
		name := ev.name
		if name == "" && ev.who != "" {
			name = firstWord(m.nameOf(ev.who))
		}
		ts = append(ts, typer{who: ev.who, name: name, action: ev.action, topic: ev.topic, until: time.Now().Add(typingFor)})
	}
	if len(ts) == 0 {
		delete(m.typing, ev.key)
	} else {
		m.typing[ev.key] = ts
	}
	if len(m.typing) > 0 && !m.typingOn {
		m.typingOn = true
		cmds = append(cmds, typingTickCmd())
	}
	return tea.Batch(cmds...)
}

func typingTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return typingTick{} })
}

func (m *model) onTypingTick() tea.Cmd {
	m.pruneTyping(time.Now())
	if len(m.typing) == 0 {
		m.typingOn = false
		return nil
	}
	return typingTickCmd()
}

func (m *model) pruneTyping(now time.Time) {
	for k, ts := range m.typing {
		kept := ts[:0]
		for _, t := range ts {
			if now.Before(t.until) {
				kept = append(kept, t)
			}
		}
		if len(kept) == 0 {
			delete(m.typing, k)
		} else {
			m.typing[k] = kept
		}
	}
}

// typingDone forgets the typing of who in chat key: their message arrived.
func (m *model) typingDone(key, who string) {
	ts := m.typing[key]
	for i := range ts {
		if ts[i].who == who || (key[0] == 'u' && ts[i].who == key) {
			m.typing[key] = append(ts[:i:i], ts[i+1:]...)
			if len(m.typing[key]) == 0 {
				delete(m.typing, key)
			}
			return
		}
	}
}

// typingText is what chat key shows while someone types; topic >= 0 keeps only that forum topic (General = 1
// also takes updates without a topic), -1 takes all. "" when nobody types.
func (m *model) typingText(key string, topic int, now time.Time) string {
	var ts []typer
	for _, t := range m.typing[key] {
		if now.Before(t.until) && (topic < 0 || t.topic == topic || (topic <= 1 && t.topic == 0)) {
			ts = append(ts, t)
		}
	}
	switch {
	case len(ts) == 0:
		return ""
	case key[0] == 'u':
		return ts[0].action + "…"
	case len(ts) == 1:
		return nameOr(ts[0].name) + " " + ts[0].action + "…"
	case len(ts) == 2 && ts[0].action == ts[1].action:
		return nameOr(ts[0].name) + " và " + nameOr(ts[1].name) + " " + ts[0].action + "…"
	}
	return strconv.Itoa(len(ts)) + " người " + ts[0].action + "…"
}

func nameOr(n string) string {
	if n == "" {
		return "Ai đó"
	}
	return n
}

func (m *model) onStatus(ev statusMsg) tea.Cmd {
	if i := m.index(ev.key); i >= 0 {
		m.chats[i].seen = ev.p
	}
	return m.wait()
}

// typedCmd tells chat c that we are typing, at most every typingEvery (Telegram shows it for 6 s). Not in
// Saved Messages or in channels, where nobody would see it.
func (m *model) typedCmd(c *chat) tea.Cmd {
	if m.api == nil || c == nil || c.peer == nil || c.key == m.selfKey || c.broadcast {
		return nil
	}
	now := time.Now()
	if m.typedKey == c.key && now.Sub(m.typedAt) < typingEvery {
		return nil
	}
	m.typedKey, m.typedAt = c.key, now
	req := &tg.MessagesSetTypingRequest{Peer: c.peer, Action: &tg.SendMessageTypingAction{}}
	if c.forum && c.topic > 1 {
		req.SetTopMsgID(c.topic)
	}
	ctx, api := m.ctx, m.api
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, _ = api.MessagesSetTyping(ctx, req) // best effort: a missed one only hides "typing" for a moment
		return nil
	}
}
