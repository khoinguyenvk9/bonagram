package main

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/tg"
	"golang.org/x/text/unicode/norm"
)

const (
	page        = 40   // messages per request
	maxChats    = 1000 // main folder
	maxArchived = 500
)

type topic struct {
	id     int
	title  string
	unread int
}

type chat struct {
	key      string // "u<id>" user, "g<id>" group, "c<id>" channel/supergroup
	title    string
	username string
	peer     tg.InputPeerClass
	last     string
	date     int
	unread   int
	muted    bool
	pinned   bool // pinned in the chat list: stays above the others, whatever their activity
	forum    bool
	topics   []topic // nil until loaded (forum only)
	topic    int     // selected topic id, 0 = not a forum
}

type line struct {
	id        int // 0 = sent by us, server id not known yet
	from      string
	sender    string // peer key of the author, "" for ours; used to group follow-ups
	text      string
	out       bool
	at        time.Time
	mentioned bool
	replyTo   int
	quote     string // "name: text" of the message replied to when it is not among the loaded ones
	topic     int
	reacts    []reaction
	segs      []seg       // code blocks / inline code of the text; nil when it has none
	msg       *tg.Message // media lives here; nil for service messages and our own pending lines
	event     bool        // a service message (someone joined, a title changed...): text says what happened
}

// reaction is one emoji with how many people used it; mine = you used it too.
type reaction struct {
	emoji string
	count int
	mine  bool
}

// toReactions keeps what can be shown without more requests: plain emoji with their counts. Custom emoji
// and paid reactions only arrive as ids, so they get a stand-in symbol. When Min is set (a partial update)
// Telegram does not say which ones are yours.
func toReactions(r tg.MessageReactions) []reaction {
	var out []reaction
	for _, rc := range r.Results {
		var e string
		switch v := rc.Reaction.(type) {
		case *tg.ReactionEmoji:
			e = clean(v.Emoticon)
		case *tg.ReactionCustomEmoji:
			e = "✦"
		case *tg.ReactionPaid:
			e = "⭐"
		}
		if e == "" || rc.Count <= 0 {
			continue
		}
		_, chosen := rc.GetChosenOrder()
		out = append(out, reaction{e, rc.Count, chosen && !r.Min})
	}
	return out
}

func peerKey(p tg.PeerClass) string {
	switch v := p.(type) {
	case *tg.PeerUser:
		return "u" + strconv.FormatInt(v.UserID, 10)
	case *tg.PeerChat:
		return "g" + strconv.FormatInt(v.ChatID, 10)
	case *tg.PeerChannel:
		return "c" + strconv.FormatInt(v.ChannelID, 10)
	}
	return ""
}

// clean drops what could break the screen: control and private-use characters (names and texts come from
// outside and could inject escape sequences or fake image placeholders), invisible format characters
// (ZWJ, bidi marks, zero-width spaces), variation selectors and skin tones, which make terminals disagree
// on how wide an emoji is, and line/paragraph separators.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t' || unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r):
			return ' '
		case unicode.IsControl(r) || unicode.Is(unicode.Co, r) || unicode.Is(unicode.Cf, r),
			r >= 0xFE00 && r <= 0xFE0F,   // variation selectors
			r >= 0x1F3FB && r <= 0x1F3FF: // skin tones: joined to the emoji before, or wide on their own

			return -1
		}
		return r
	}, s)
}

func peerTitle(e peer.Entities, p tg.PeerClass) string { return clean(rawTitle(e, p)) }

func rawTitle(e peer.Entities, p tg.PeerClass) string {
	switch v := p.(type) {
	case *tg.PeerUser:
		if u, ok := e.User(v.UserID); ok {
			if n := strings.TrimSpace(u.FirstName + " " + u.LastName); n != "" {
				return n
			}
			if u.Username != "" {
				return u.Username
			}
		}
	case *tg.PeerChat:
		if c, ok := e.Chat(v.ChatID); ok {
			return c.Title
		}
	case *tg.PeerChannel:
		if c, ok := e.Channel(v.ChannelID); ok {
			return c.Title
		}
	}
	return "?"
}

// peerExtra returns the username and forum flag of a chat, "" / false when unknown.
func peerExtra(e peer.Entities, p tg.PeerClass) (username string, forum bool) {
	switch v := p.(type) {
	case *tg.PeerUser:
		if u, ok := e.User(v.UserID); ok {
			username = u.Username
		}
	case *tg.PeerChannel:
		if c, ok := e.Channel(v.ChannelID); ok {
			username, forum = c.Username, c.Forum
		}
	}
	return
}

// humanSize writes a byte count the way file managers do: 512 B, 2.3 MB.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return strconv.FormatInt(n, 10) + " B"
	}
	v, u := float64(n), 0
	for ; v >= unit && u < 3; u++ {
		v /= unit
	}
	return strconv.FormatFloat(v, 'f', 1, 64) + " " + [...]string{"KB", "MB", "GB", "TB"}[u-1]
}

// docInfo is what a document says about itself.
type docInfo struct {
	kind    string // file, video, gif, audio, voice, sticker
	name    string // the file name, "" when it has none
	length  string // 0:34 for video and audio
	artist  string // "title - performer" of a song
	alt     string // the emoji of a sticker
	voice   bool
	hasName bool
}

func infoOf(d *tg.Document) docInfo {
	in := docInfo{kind: "file"}
	for _, a := range d.Attributes {
		switch v := a.(type) {
		case *tg.DocumentAttributeFilename:
			in.hasName = true
		case *tg.DocumentAttributeAnimated:
			in.kind = "gif"
		case *tg.DocumentAttributeVideo:
			if in.kind != "gif" {
				in.kind = "video"
			}
			in.length = clock(int(v.Duration))
		case *tg.DocumentAttributeAudio:
			in.kind, in.length = "audio", clock(v.Duration)
			if v.Voice {
				in.kind, in.voice = "voice", true
			} else if t := strings.Trim(strings.TrimSpace(v.Title+" - "+v.Performer), " -"); t != "" {
				in.artist = t
			}
		case *tg.DocumentAttributeSticker:
			in.kind, in.alt = "sticker", v.Alt
		}
	}
	if in.hasName {
		in.name = docName(d)
	}
	return in
}

// docTag describes a document in one line: what kind it is, its name or length, and its size.
func docTag(d *tg.Document) string {
	in := infoOf(d)
	var detail string
	switch {
	case in.kind == "sticker":
		detail = in.alt
	case in.kind == "file" || in.hasName:
		detail = strings.TrimSpace(docName(d) + " " + in.length)
		if !in.hasName {
			detail += " (" + d.MimeType + ")"
		}
	case in.artist != "":
		detail = in.artist + " · " + in.length
	default:
		detail = in.length
	}
	tag := "[" + in.kind + "]"
	if detail != "" {
		tag += " " + detail
	}
	if d.Size > 0 && in.kind != "sticker" {
		tag += " · " + humanSize(d.Size)
	}
	return clean(tag)
}

// mediaTag is the label shown for what a message carries: [photo], [file] name · size, [video] 0:34 · 5.1 MB...
func mediaTag(m tg.MessageMediaClass) string {
	switch v := m.(type) {
	case nil, *tg.MessageMediaEmpty, *tg.MessageMediaWebPage: // a link preview adds nothing: the link is in the text
		return ""
	case *tg.MessageMediaPhoto:
		return "[photo]"
	case *tg.MessageMediaDocument:
		if d, ok := v.Document.(*tg.Document); ok {
			return docTag(d)
		}
		return "[file]"
	case *tg.MessageMediaGeo:
		if g, ok := v.Geo.(*tg.GeoPoint); ok {
			return fmt.Sprintf("[location] %.5f, %.5f", g.Lat, g.Long)
		}
	case *tg.MessageMediaGeoLive:
		if g, ok := v.Geo.(*tg.GeoPoint); ok {
			return fmt.Sprintf("[live location] %.5f, %.5f", g.Lat, g.Long)
		}
	case *tg.MessageMediaVenue:
		return clean("[location] " + v.Title)
	case *tg.MessageMediaContact:
		return clean("[contact] " + strings.TrimSpace(v.FirstName+" "+v.LastName+" "+v.PhoneNumber))
	case *tg.MessageMediaPoll:
		return clean("[poll] " + v.Poll.Question.Text)
	}
	return "[media]"
}

// replyInfo reads the reply header: the message replied to (0 = none) and the forum topic (1 = General).
// In a forum a plain topic message points at the topic root, which is not a reply.
func replyInfo(h tg.MessageReplyHeaderClass) (replyTo, topic int) {
	r, ok := h.(*tg.MessageReplyHeader)
	if !ok {
		return 0, 1
	}
	topic = 1
	if r.ForumTopic {
		if topic = r.ReplyToTopID; topic == 0 {
			topic = r.ReplyToMsgID
		}
		if topic == 0 {
			topic = 1
		}
		if r.ReplyToTopID == 0 {
			return 0, topic
		}
	}
	return r.ReplyToMsgID, topic
}

func author(e peer.Entities, from, chatPeer tg.PeerClass, out bool) (name, key string) {
	if out {
		return "Bạn", ""
	}
	src := chatPeer
	if from != nil {
		src = from
	}
	return peerTitle(e, src), peerKey(src)
}

// toLine converts a message or service message; ok is false for anything else (empty/deleted).
func toLine(mc tg.NotEmptyMessage, e peer.Entities) (line, bool) {
	switch m := mc.(type) {
	case *tg.Message:
		from, key := author(e, m.FromID, m.PeerID, m.Out)
		rt, tp := replyInfo(m.ReplyTo)
		return line{
			id: m.ID, from: from, sender: key, out: m.Out, at: time.Unix(int64(m.Date), 0),
			text:      strings.TrimSpace(clean(m.Message) + " " + mediaTag(m.Media)),
			mentioned: m.Mentioned, replyTo: rt, topic: tp, reacts: toReactions(m.Reactions), segs: segments(m.Message, m.Entities), msg: m,
		}, true
	case *tg.MessageService:
		from, key := author(e, m.FromID, m.PeerID, m.Out)
		_, tp := replyInfo(m.ReplyTo)
		return line{
			id: m.ID, from: from, sender: key, out: m.Out, at: time.Unix(int64(m.Date), 0), topic: tp,
			text: "[" + eventText(e, m.FromID, m.Action) + "]", event: true,
		}, true
	}
	return line{}, false
}

// addLine appends l unless it is already there. A line we sent (id 0) is matched
// with the server's echo of it, so it works whether or not the echo ever arrives.
func addLine(ls []line, l line) []line {
	for i := range ls {
		if l.id != 0 && ls[i].id == l.id {
			if ls[i].msg == nil || ls[i].msg.ID == 0 { // the line was numbered (or drawn from what we typed) from the send reply: now it gets its full content
				ls[i].msg, ls[i].segs = l.msg, l.segs
			}
			return ls
		}
		if l.out && l.id != 0 && ls[i].id == 0 && ls[i].text == l.text {
			ls[i].id, ls[i].msg, ls[i].segs = l.id, l.msg, l.segs
			return ls
		}
	}
	return append(ls, l)
}

// sumUnread is the unread count of a forum: the sum over its topics. The count the server keeps for the
// forum as a whole is not updated when a topic is read, so it can stay stuck on a number nobody can clear.
func sumUnread(ts []topic) int {
	n := 0
	for _, t := range ts {
		n += t.unread
	}
	return n
}

func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// fold lowercases and strips accents so "nguyen dat" finds "Nguyễn Đạt".
func fold(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.Is(unicode.Mn, r):
			return -1
		case r == 'đ':
			return 'd'
		}
		return r
	}, norm.NFD.String(strings.ToLower(s)))
}

var weekdays = [7]string{"CN", "Thứ 2", "Thứ 3", "Thứ 4", "Thứ 5", "Thứ 6", "Thứ 7"}

// shortTime is the chat-list time: HH:MM today, "Hôm qua", weekday within a week, else dd/mm.
func shortTime(date int, now time.Time) string {
	if date == 0 {
		return ""
	}
	t := time.Unix(int64(date), 0).In(now.Location())
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, x.Location()) }
	days := int(day(now).Sub(day(t)).Hours()/24 + 0.5)
	switch {
	case days <= 0:
		return t.Format("15:04")
	case days == 1:
		return "Hôm qua"
	case days < 7:
		return weekdays[t.Weekday()]
	}
	return t.Format("02/01")
}

func lastPreview(m *tg.Message, key string, e peer.Entities) string {
	body := flat(clean(m.Message))
	if body == "" {
		body = mediaTag(m.Media)
	}
	switch {
	case m.Out:
		return "Bạn: " + body
	case key[0] != 'u' && m.FromID != nil:
		if who := strings.Fields(peerTitle(e, m.FromID)); len(who) > 0 && who[0] != "?" {
			return who[0] + ": " + body
		}
	}
	return body
}

// chanTrack is what the updates manager needs to follow a channel/supergroup from now on.
type chanTrack struct {
	id, hash int64
	pts      int
}

// loadChats returns the main-folder chats, the set of archived chat keys (which stay hidden) and the
// channels to register with the updates manager: it only follows channels it was told about, and
// otherwise learns of one from its next update, after the message that matters was already missed.
func loadChats(ctx context.Context, api *tg.Client) ([]chat, map[string]bool, []chanTrack, error) {
	// FolderID(0) is "not set" for gotd, and then Telegram returns every dialog, archived ones included:
	// each dialog says which folder it is in, so the archived ones are filtered out below.
	it := query.GetDialogs(api).BatchSize(100).Iter()
	archived := map[string]bool{}
	var out []chat
	var tracks []chanTrack
	now := int(time.Now().Unix())
	for len(out) < maxChats && it.Next(ctx) {
		el := it.Value()
		d, ok := el.Dialog.(*tg.Dialog)
		if !ok {
			continue
		}
		key := peerKey(d.Peer)
		if _, empty := el.Peer.(*tg.InputPeerEmpty); key == "" || el.Peer == nil || empty {
			continue
		}
		if d.FolderID != 0 { // in the Archive folder: stays hidden, like in Telegram
			archived[key] = true
			continue
		}
		if ch, ok := el.Peer.(*tg.InputPeerChannel); ok && d.Pts > 0 {
			tracks = append(tracks, chanTrack{ch.ChannelID, ch.AccessHash, d.Pts})
		}
		user, forum := peerExtra(el.Entities, d.Peer)
		c := chat{
			key: key, title: peerTitle(el.Entities, d.Peer), username: user, peer: el.Peer, forum: forum,
			unread: d.UnreadCount, muted: d.NotifySettings.MuteUntil > now, pinned: d.Pinned,
		}
		if m, ok := el.Last.(*tg.Message); ok {
			c.last, c.date = lastPreview(m, key, el.Entities), m.Date
		} else if s, ok := el.Last.(*tg.MessageService); ok {
			c.date = s.Date
			if l, ok := toLine(s, el.Entities); ok {
				c.last = l.text
				if key[0] != 'u' && !l.out {
					c.last = firstWord(l.from) + " " + l.text
				}
			}
		}
		out = append(out, c)
	}
	if err := it.Err(); err != nil {
		return nil, nil, nil, err
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].pinned && !out[j].pinned }) // pinned first, order kept
	loadForumTopics(ctx, api, out)
	ai := query.GetDialogs(api).FolderID(1).BatchSize(100).Iter()
	for n := 0; n < maxArchived && ai.Next(ctx); n++ { // best effort: only used to keep these chats hidden
		if d, ok := ai.Value().Dialog.(*tg.Dialog); ok {
			archived[peerKey(d.Peer)] = true
		}
	}
	return out, archived, tracks, nil
}

// loadMessages returns up to n lines older than offsetID (0 = newest), oldest first, and whether more may exist.
// topic > 0 reads that forum topic instead of the whole chat.
func loadMessages(ctx context.Context, api *tg.Client, p tg.InputPeerClass, topic, offsetID, n int) ([]line, bool, error) {
	var out []line
	collect := func(next func(context.Context) bool, msg func() (tg.NotEmptyMessage, peer.Entities), errf func() error) error {
		for len(out) < n && next(ctx) {
			mc, e := msg()
			if l, ok := toLine(mc, e); ok {
				out = append(out, l)
			}
		}
		return errf()
	}
	var err error
	if topic > 0 {
		it := query.Messages(api).GetReplies(p).MsgID(topic).OffsetID(offsetID).BatchSize(n).Iter()
		err = collect(it.Next, func() (tg.NotEmptyMessage, peer.Entities) { v := it.Value(); return v.Msg, v.Entities }, it.Err)
	} else {
		it := query.Messages(api).GetHistory(p).OffsetID(offsetID).BatchSize(n).Iter()
		err = collect(it.Next, func() (tg.NotEmptyMessage, peer.Entities) { v := it.Value(); return v.Msg, v.Entities }, it.Err)
	}
	fillQuotes(ctx, api, p, out)
	// newest first from the server; show oldest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, len(out) >= n, err
}

func loadTopics(ctx context.Context, api *tg.Client, p tg.InputPeerClass) ([]topic, error) {
	res, err := api.MessagesGetForumTopics(ctx, &tg.MessagesGetForumTopicsRequest{Peer: p, Limit: 100})
	if err != nil {
		return nil, err
	}
	var out []topic
	for _, t := range res.Topics {
		if ft, ok := t.(*tg.ForumTopic); ok && ft.Title != "" { // deleted topics carry no title
			out = append(out, topic{ft.ID, clean(ft.Title), ft.UnreadCount})
		}
	}
	return out, nil
}

// markRead reads the whole chat, or only forum topic `tp` (> 0) up to message `upto`.
func markRead(ctx context.Context, api *tg.Client, p tg.InputPeerClass, tp, upto int) error {
	switch ch := p.(type) {
	case *tg.InputPeerChannel:
		if tp > 0 {
			_, err := api.MessagesReadDiscussion(ctx, &tg.MessagesReadDiscussionRequest{Peer: p, MsgID: tp, ReadMaxID: upto})
			return err
		}
		_, err := api.ChannelsReadHistory(ctx, &tg.ChannelsReadHistoryRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash},
		})
		return err
	}
	_, err := api.MessagesReadHistory(ctx, &tg.MessagesReadHistoryRequest{Peer: p})
	return err
}

// sendText posts text; replyTo (0 = none) is the message answered, topic (> 1) the forum topic to post into.
// It returns the id the server gave the message (0 if the answer does not say).
func sendText(ctx context.Context, api *tg.Client, p tg.InputPeerClass, text string, replyTo, tp int, ents []tg.MessageEntityClass) (int, error) {
	rid, err := randID()
	if err != nil {
		return 0, err
	}
	req := &tg.MessagesSendMessageRequest{Peer: p, Message: text, RandomID: rid, Entities: ents}
	switch {
	case replyTo != 0 && tp > 1:
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: replyTo, TopMsgID: tp}
	case replyTo != 0:
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: replyTo}
	case tp > 1: // posting into a topic = replying to its root
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: tp}
	}
	upd, err := api.MessagesSendMessage(ctx, req)
	return sentID(upd), err
}

// loadForumTopics fills in the topics of every forum chat (4 requests at a time) and replaces the forum's
// own unread count by the sum over its topics. A failed request leaves that chat as it was.
func loadForumTopics(ctx context.Context, api *tg.Client, cs []chat) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i := range cs {
		if !cs[i].forum {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(c *chat) {
			defer func() { <-sem; wg.Done() }()
			ts, err := loadTopics(ctx, api, c.peer)
			if err != nil {
				return
			}
			c.topics = append([]topic{}, ts...) // non-nil: loaded, even if empty
			c.unread = sumUnread(ts)
			if len(ts) > 0 {
				c.topic = ts[0].id
			}
		}(&cs[i])
	}
	wg.Wait()
}

// quoteText is how a replied-to message is shown above the reply.
func quoteText(from, text string) string { return from + `: "` + cut(text, 60) + `"` }

// fillQuotes looks up, in one request, the messages that lines reply to but that are not among the lines
// themselves (older ones), so the reply can still show who and what it answers. Best effort: on any error
// the reply is shown without it.
func fillQuotes(ctx context.Context, api *tg.Client, p tg.InputPeerClass, ls []line) {
	have := make(map[int]bool, len(ls))
	for _, l := range ls {
		have[l.id] = true
	}
	var ids []tg.InputMessageClass
	for _, l := range ls {
		if l.replyTo != 0 && !have[l.replyTo] && len(ids) < 100 {
			have[l.replyTo] = true
			ids = append(ids, &tg.InputMessageID{ID: l.replyTo})
		}
	}
	if len(ids) == 0 {
		return
	}
	res, err := getMessages(ctx, api, p, ids)
	found := map[int]string{}
	for _, t := range linesOf(res) {
		if !t.event {
			found[t.id] = quoteText(t.from, t.text)
		}
	}
	if err != nil {
		return
	}
	for i := range ls {
		if ls[i].replyTo != 0 {
			ls[i].quote = found[ls[i].replyTo]
		}
	}
}

// getMessages reads messages of a chat by id.
func getMessages(ctx context.Context, api *tg.Client, p tg.InputPeerClass, ids []tg.InputMessageClass) (tg.MessagesMessagesClass, error) {
	if ch, ok := p.(*tg.InputPeerChannel); ok {
		return api.ChannelsGetMessages(ctx, &tg.ChannelsGetMessagesRequest{
			Channel: &tg.InputChannel{ChannelID: ch.ChannelID, AccessHash: ch.AccessHash}, ID: ids})
	}
	return api.MessagesGetMessages(ctx, ids)
}

// linesOf converts the messages of an answer to lines, in the order the server gave them (newest first).
func linesOf(res tg.MessagesMessagesClass) []line {
	mm, ok := res.(tg.ModifiedMessagesMessages)
	if !ok {
		return nil
	}
	chats := tg.ChatClassArray(mm.GetChats())
	ent := peer.NewEntities(tg.UserClassArray(mm.GetUsers()).UserToMap(), chats.ChatToMap(), chats.ChannelToMap())
	var out []line
	for _, mc := range mm.GetMessages() {
		if nm, ok := mc.AsNotEmpty(); ok {
			if l, ok := toLine(nm, ent); ok {
				out = append(out, l)
			}
		}
	}
	return out
}
