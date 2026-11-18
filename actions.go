package main

// Things the user asks for one at a time: edit, delete, react, forward, upload a file, read the pinned
// message, open a link. None of them runs in the background; each is one request (the pinned message is
// one more request when a chat is opened).

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf16"

	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

// quickReactions are the emoji offered by the "r" key (1..7). Chats can restrict which ones they accept;
// the server's refusal is shown as an error.
var quickReactions = []string{"👍", "👎", "❤", "🔥", "🎉", "😁", "😢"}

// errText is an error as the user should read it: Telegram's own code (REACTION_INVALID, MESSAGE_ID_INVALID...)
// without the library's wrapping.
func errText(err error) string {
	if e, ok := tgerr.As(err); ok {
		return e.Message
	}
	return err.Error()
}

func randID() (int64, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(b[:])), nil
}

// sentID digs the id of the message just sent out of the server's answer.
func sentID(u tg.UpdatesClass) int {
	var ups []tg.UpdateClass
	switch v := u.(type) {
	case *tg.UpdateShortSentMessage:
		return v.ID
	case *tg.Updates:
		ups = v.Updates
	case *tg.UpdatesCombined:
		ups = v.Updates
	}
	for _, x := range ups {
		if m, ok := x.(*tg.UpdateMessageID); ok {
			return m.ID
		}
	}
	return 0
}

func channelOf(p tg.InputPeerClass) (*tg.InputChannel, bool) {
	if ch, ok := p.(*tg.InputPeerChannel); ok {
		return &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}, true
	}
	return nil, false
}

func editText(ctx context.Context, api *tg.Client, p tg.InputPeerClass, id int, text string) error {
	plain, ents := parseMarkdown(text)
	_, err := api.MessagesEditMessage(ctx, &tg.MessagesEditMessageRequest{Peer: p, ID: id, Message: plain, Entities: ents})
	return err
}

// deleteMsg deletes for everybody (revoke) where the chat allows it.
func deleteMsg(ctx context.Context, api *tg.Client, p tg.InputPeerClass, id int) error {
	if ch, ok := channelOf(p); ok {
		_, err := api.ChannelsDeleteMessages(ctx, &tg.ChannelsDeleteMessagesRequest{Channel: ch, ID: []int{id}})
		return err
	}
	_, err := api.MessagesDeleteMessages(ctx, &tg.MessagesDeleteMessagesRequest{ID: []int{id}, Revoke: true})
	return err
}

// sendReaction puts emoji on a message; an empty emoji takes your reaction back. The server does not announce
// the result of our own reaction as an update, so the new reactions are read out of its answer (ok=false when
// it carried none).
func sendReaction(ctx context.Context, api *tg.Client, p tg.InputPeerClass, id int, emoji string) (rs []reaction, ok bool, err error) {
	req := &tg.MessagesSendReactionRequest{Peer: p, MsgID: id}
	if emoji != "" {
		req.Reaction = []tg.ReactionClass{&tg.ReactionEmoji{Emoticon: emoji}}
	}
	u, err := api.MessagesSendReaction(ctx, req)
	if err != nil {
		return nil, false, err
	}
	var ups []tg.UpdateClass
	switch v := u.(type) {
	case *tg.Updates:
		ups = v.Updates
	case *tg.UpdatesCombined:
		ups = v.Updates
	}
	for _, x := range ups {
		if r, is := x.(*tg.UpdateMessageReactions); is && r.MsgID == id {
			return toReactions(r.Reactions), true, nil
		}
	}
	return nil, false, nil
}

// forwardMsg forwards one message; into a forum it lands in the General topic.
func forwardMsg(ctx context.Context, api *tg.Client, from, to tg.InputPeerClass, id int) error {
	r, err := randID()
	if err != nil {
		return err
	}
	_, err = api.MessagesForwardMessages(ctx, &tg.MessagesForwardMessagesRequest{
		FromPeer: from, ToPeer: to, ID: []int{id}, RandomID: []int64{r},
	})
	return err
}

// shellPath turns what a terminal pastes when a file is dropped on it (quoted, or with backslash-escaped
// spaces) into a plain path.
func shellPath(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		s = s[1 : len(s)-1]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && runtime.GOOS != "windows" {
			i++
		}
		b.WriteByte(s[i])
	}
	s = b.String()
	if strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, s[2:])
		}
	}
	return s
}

// loadPinned returns the newest pinned message of the chat (or forum topic tp > 1) as one line, "" if none.
// pinned is one pinned message: its id (to jump to it) and its text as one line.
type pinned struct {
	id   int
	text string
}

const maxPins = 20

// loadPinned reads the pinned messages of a chat (of forum topic tp > 1), newest first.
func loadPinned(ctx context.Context, api *tg.Client, p tg.InputPeerClass, tp int) ([]pinned, error) {
	req := &tg.MessagesSearchRequest{Peer: p, Filter: &tg.InputMessagesFilterPinned{}, Limit: maxPins}
	if tp > 1 {
		req.TopMsgID = tp
	}
	res, err := api.MessagesSearch(ctx, req)
	if err != nil {
		return nil, err
	}
	mm, ok := res.(tg.ModifiedMessagesMessages)
	if !ok {
		return nil, nil
	}
	var out []pinned
	for _, x := range mm.GetMessages() {
		if m, ok := x.(*tg.Message); ok {
			out = append(out, pinned{m.ID, flat(clean(m.Message) + " " + mediaTag(m.Media))})
		}
	}
	return out, nil
}

// webURL returns s as an http(s) address, adding "https://" to one without a scheme; ok is false for anything
// else (tg://, file:, javascript:, mailto:), which is not opened from a chat.
func webURL(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	if !strings.Contains(s, "://") {
		// a scheme without // (javascript:, mailto:, tel:) is not a web address; host:port is
		if i := strings.IndexByte(s, ':'); i > 0 && !strings.Contains(s[:i], "/") && (i+1 >= len(s) || s[i+1] < '0' || s[i+1] > '9') {
			return "", false
		}
		s = "https://" + s
	}
	if l := strings.ToLower(s); strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://") {
		return s, true
	}
	return "", false
}

// linkEnt is a link of a message: the words it covers (UTF-16 units [off, end), only when they lie inside the
// text), its address, and whether the words hide that address (a "text link" rather than the address itself).
type linkEnt struct {
	off, end int
	url      string
	hidden   bool
}

// linkEntities lists the http(s) links of a message in entity order.
func linkEntities(m *tg.Message) []linkEnt {
	if m == nil {
		return nil
	}
	u := utf16.Encode([]rune(m.Message))
	var out []linkEnt
	for _, e := range m.Entities {
		var l linkEnt
		var raw string
		switch v := e.(type) {
		case *tg.MessageEntityURL:
			if v.Offset >= 0 && v.Length > 0 && v.Offset+v.Length <= len(u) {
				raw = string(utf16.Decode(u[v.Offset : v.Offset+v.Length]))
				l.off, l.end = v.Offset, v.Offset+v.Length
			}
		case *tg.MessageEntityTextURL:
			raw, l.hidden = v.URL, true
			if v.Offset >= 0 && v.Length > 0 && v.Offset+v.Length <= len(u) {
				l.off, l.end = v.Offset, v.Offset+v.Length
			}
		}
		if url, ok := webURL(raw); ok {
			l.url = url
			out = append(out, l)
		}
	}
	return out
}

// linksOf lists the http(s) addresses of a message, in order; a location adds its map link.
func linksOf(m *tg.Message) []string {
	var out []string
	for _, l := range linkEntities(m) {
		out = append(out, l.url)
	}
	if lat, long, _, _, _, ok := geoOf(m); ok {
		out = append(out, mapURL(lat, long))
	}
	return out
}

// pinMessage pins or unpins message id. silent: the members get no notification; oneSide: in a private chat,
// pinned for you only.
func pinMessage(ctx context.Context, api *tg.Client, p tg.InputPeerClass, id int, unpin, silent, oneSide bool) error {
	_, err := api.MessagesUpdatePinnedMessage(ctx, &tg.MessagesUpdatePinnedMessageRequest{
		Peer: p, ID: id, Unpin: unpin, Silent: silent, PmOneside: oneSide,
	})
	return err
}

// setMuted turns the notifications of a chat off for good, or back on.
func setMuted(ctx context.Context, api *tg.Client, p tg.InputPeerClass, muted bool) error {
	until := 0
	if muted {
		until = math.MaxInt32 // "forever", as the official apps send it
	}
	_, err := api.AccountUpdateNotifySettings(ctx, &tg.AccountUpdateNotifySettingsRequest{
		Peer: &tg.InputNotifyPeer{Peer: p}, Settings: tg.InputPeerNotifySettings{MuteUntil: until},
	})
	return err
}

// deleteChat takes a chat out of your list. A private chat loses its history, on your side only or for both
// people (revoke); a supergroup or channel is left; a basic group is left and its history dropped on your side.
func deleteChat(ctx context.Context, api *tg.Client, p tg.InputPeerClass, revoke bool) error {
	switch v := p.(type) {
	case *tg.InputPeerChannel:
		_, err := api.ChannelsLeaveChannel(ctx, &tg.InputChannel{ChannelID: v.ChannelID, AccessHash: v.AccessHash})
		return err
	case *tg.InputPeerChat:
		if _, err := api.MessagesDeleteChatUser(ctx, &tg.MessagesDeleteChatUserRequest{ChatID: v.ChatID, UserID: &tg.InputUserSelf{}}); err != nil {
			return err
		}
		_ = clearHistory(ctx, api, p, false) // already out of the group: the leftover dialog is best effort
		return nil
	}
	return clearHistory(ctx, api, p, revoke)
}

// clearHistory deletes the whole history; the server works in batches and says where it stopped.
func clearHistory(ctx context.Context, api *tg.Client, p tg.InputPeerClass, revoke bool) error {
	for range 100 {
		res, err := api.MessagesDeleteHistory(ctx, &tg.MessagesDeleteHistoryRequest{Peer: p, Revoke: revoke})
		if err != nil {
			return err
		}
		if res.Offset <= 0 {
			return nil
		}
	}
	return nil
}

// pinChat pins a chat at the top of the list, or unpins it.
func pinChat(ctx context.Context, api *tg.Client, p tg.InputPeerClass, pinned bool) error {
	_, err := api.MessagesToggleDialogPin(ctx, &tg.MessagesToggleDialogPinRequest{Pinned: pinned, Peer: &tg.InputDialogPeer{Peer: p}})
	if tgerr.Is(err, "PINNED_DIALOGS_TOO_MUCH") {
		return errors.New("đã ghim tối đa số chat Telegram cho phép (5 với tài khoản thường), bỏ ghim một chat khác trước")
	}
	return err
}
