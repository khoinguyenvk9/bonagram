package main

// @mention suggestions in groups. Nothing is loaded in advance: typing "@" (at the start of a word) asks
// the server for matching members, one request per pause in typing; basic groups return all their members
// in one request, which is kept for filtering locally. Tab/Enter puts the choice in the input.

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/tg"
)

const (
	mentionLimit = 20                     // members asked for in one search
	mentionShow  = 5                      // names shown at once in the bar
	mentionPause = 150 * time.Millisecond // typing pause before asking the server
)

type member struct {
	id       int64
	hash     int64
	name     string
	username string
}

type (
	mentionTick struct{ seq int }
	mentionMsg  struct {
		seq  int
		key  string
		all  []member // basic group: everybody (the answer to the one request)
		some []member // supergroup: the matches of the search
		err  error
	}
)

func toMember(u tg.UserClass) (member, bool) {
	v, ok := u.(*tg.User)
	if !ok || v.Self || v.Deleted {
		return member{}, false
	}
	name := clean(strings.TrimSpace(v.FirstName + " " + v.LastName))
	if name == "" {
		name = clean(v.Username)
	}
	return member{v.ID, v.AccessHash, name, clean(v.Username)}, name != ""
}

func toMembers(us []tg.UserClass) []member {
	var out []member
	for _, u := range us {
		if m, ok := toMember(u); ok {
			out = append(out, m)
		}
	}
	return out
}

// loadMembers asks for the members matching q in a supergroup (some) or for the whole basic group (all).
func loadMembers(ctx context.Context, api *tg.Client, p tg.InputPeerClass, q string) (all, some []member, err error) {
	switch v := p.(type) {
	case *tg.InputPeerChannel:
		var f tg.ChannelParticipantsFilterClass = &tg.ChannelParticipantsRecent{}
		if q != "" {
			f = &tg.ChannelParticipantsSearch{Q: q}
		}
		res, err := api.ChannelsGetParticipants(ctx, &tg.ChannelsGetParticipantsRequest{
			Channel: &tg.InputChannel{ChannelID: v.ChannelID, AccessHash: v.AccessHash}, Filter: f, Limit: mentionLimit,
		})
		if err != nil {
			return nil, nil, err
		}
		cp, ok := res.(*tg.ChannelsChannelParticipants)
		if !ok {
			return nil, nil, nil
		}
		return nil, toMembers(cp.Users), nil
	case *tg.InputPeerChat:
		full, err := api.MessagesGetFullChat(ctx, v.ChatID)
		if err != nil {
			return nil, nil, err
		}
		return toMembers(full.Users), nil, nil
	}
	return nil, nil, errors.New("không phải nhóm")
}

// filterMembers keeps the members whose name or username contains q (accents and case ignored).
func filterMembers(all []member, q string) []member {
	q = fold(q)
	var out []member
	for _, u := range all {
		if q == "" || strings.Contains(fold(u.name+" "+u.username), q) {
			if out = append(out, u); len(out) == mentionLimit {
				break
			}
		}
	}
	return out
}

// mentionToken finds the "@word" being typed right before the cursor: its start (a rune index) and the
// text after the "@". ok=false when the cursor is not in one.
func mentionToken(val string, pos int) (start int, q string, ok bool) {
	rs := []rune(val)
	pos = min(pos, len(rs))
	for i := pos - 1; i >= 0; i-- {
		switch {
		case rs[i] == '@':
			if i == 0 || unicode.IsSpace(rs[i-1]) {
				return i, string(rs[i+1 : pos]), true
			}
			return 0, "", false
		case unicode.IsSpace(rs[i]):
			return 0, "", false
		}
	}
	return 0, "", false
}

func (m *model) mentionOff() {
	if m.mactive || len(m.mc) > 0 {
		m.mactive, m.mc = false, nil
		m.layout()
		m.render()
	}
}

// mentionCheck runs after the input changed: starts, updates or ends the suggestions.
func (m *model) mentionCheck() tea.Cmd {
	c := m.curChat()
	if c == nil || m.api == nil || c.key[0] == 'u' {
		return nil
	}
	start, q, ok := mentionToken(m.in.Value(), m.in.Position())
	if !ok {
		m.mentionOff()
		return nil
	}
	if m.mactive && start == m.mstart && q == m.mq {
		return nil
	}
	m.mactive, m.mstart, m.mq = true, start, q
	m.mseq++
	seq := m.mseq
	if all, cached := m.members[c.key]; cached {
		m.setCandidates(filterMembers(all, q))
		return nil
	}
	return tea.Tick(mentionPause, func(time.Time) tea.Msg { return mentionTick{seq} })
}

func (m *model) setCandidates(us []member) {
	had := len(m.mc) > 0
	m.mc, m.msel = us, 0
	if had != (len(us) > 0) {
		m.layout()
	}
	m.render()
}

func (m *model) onMentionTick(t mentionTick) tea.Cmd {
	c := m.curChat()
	if t.seq != m.mseq || !m.mactive || c == nil {
		return nil
	}
	ctx, api, p, q, key := m.ctx, m.api, c.peer, m.mq, c.key
	return func() tea.Msg {
		all, some, err := loadMembers(ctx, api, p, q)
		return mentionMsg{t.seq, key, all, some, err}
	}
}

func (m *model) onMention(r mentionMsg) tea.Cmd {
	if r.err != nil {
		m.mentionOff()
		return m.toastCmd("không xem được thành viên: " + errText(r.err))
	}
	if r.all != nil {
		if m.members == nil {
			m.members = map[string][]member{}
		}
		m.members[r.key] = r.all
	}
	if r.seq != m.mseq || !m.mactive {
		return nil
	}
	if c := m.curChat(); c == nil || c.key != r.key {
		return nil
	}
	if r.all != nil {
		m.setCandidates(filterMembers(r.all, m.mq))
	} else {
		m.setCandidates(r.some)
	}
	return nil
}

// mentionKey handles the keys of the open suggestions.
func (m *model) mentionKey(s string) bool {
	switch s {
	case "up":
		m.msel = (m.msel + len(m.mc) - 1) % len(m.mc)
	case "down":
		m.msel = (m.msel + 1) % len(m.mc)
	case "tab", "enter":
		m.pickMention(m.mc[m.msel])
		return true
	case "esc":
		m.mentionOff()
		return true
	default:
		return false
	}
	m.render()
	return true
}

// pickMention replaces the "@word" before the cursor by the member. Somebody without a username can only be
// mentioned through an entity, which is attached when the message is sent (see mentionEnts).
func (m *model) pickMention(u member) {
	rs := []rune(m.in.Value())
	pos := min(m.in.Position(), len(rs))
	tok := "@" + u.username
	if u.username == "" {
		tok = "@" + u.name
		m.pend = append(m.pend, u)
	}
	out := string(rs[:m.mstart]) + tok + " "
	n := len([]rune(out))
	m.in.SetValue(out + string(rs[pos:]))
	m.in.SetCursor(n)
	m.mactive, m.mc = false, nil
	m.layout()
	m.render()
}

// mentionEnts turns the picked members without a username into mention entities of text.
func (m *model) mentionEnts(text string) []tg.MessageEntityClass {
	var out []tg.MessageEntityClass
	from := 0
	for _, u := range m.pend {
		tok := "@" + u.name
		i := strings.Index(text[from:], tok)
		if i < 0 {
			continue
		}
		i += from
		from = i + len(tok)
		out = append(out, &tg.InputMessageEntityMentionName{
			Offset: len(utf16.Encode([]rune(text[:i]))), Length: len(utf16.Encode([]rune(tok))),
			UserID: &tg.InputUser{UserID: u.id, AccessHash: u.hash},
		})
	}
	m.pend = nil
	return out
}

// mentionBar is the row above the input while suggestions are open: a window of names with the choice marked.
func mentionBar(us []member, sel int) string {
	from := max(0, min(sel-2, len(us)-mentionShow))
	var b strings.Builder
	b.WriteString("@ ")
	for i := from; i < min(from+mentionShow, len(us)); i++ {
		name := us[i].name
		if us[i].username != "" {
			name += " (@" + us[i].username + ")"
		}
		if i == sel {
			name = "▸" + name + "◂"
		}
		if i > from {
			b.WriteString(" · ")
		}
		b.WriteString(name)
	}
	return b.String() + "   (↑↓ chọn · Tab/Enter chèn · Esc đóng)"
}
