package main

// Locations: a received one is a card with a link to the map; /position sends one.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

// mapURL opens a point in Google Maps (the map most people here use; it opens in any browser).
func mapURL(lat, long float64) string {
	return fmt.Sprintf("https://www.google.com/maps?q=%.6f,%.6f", lat, long)
}

// geoOf is the point a message carries (a location, a live location or a place) and the place's name.
func geoOf(m *tg.Message) (lat, long float64, title, address string, live, ok bool) {
	if m == nil {
		return
	}
	var g tg.GeoPointClass
	switch v := m.Media.(type) {
	case *tg.MessageMediaGeo:
		g = v.Geo
	case *tg.MessageMediaGeoLive:
		g, live = v.Geo, true
	case *tg.MessageMediaVenue:
		g, title, address = v.Geo, clean(v.Title), clean(v.Address)
	}
	p, ok := g.(*tg.GeoPoint)
	if !ok {
		return 0, 0, "", "", false, false
	}
	return p.Lat, p.Long, title, address, live, true
}

// geoCard draws a location as a card whose second row is a link to the map; nil when the message has none.
func geoCard(m *tg.Message, maxW int, hot string) []string {
	lat, long, title, address, live, ok := geoOf(m)
	if !ok {
		return nil
	}
	coords := fmt.Sprintf("%.5f, %.5f", lat, long)
	if title == "" {
		title = coords
	} else if address != "" {
		coords = address
	}
	badge := "VỊ TRÍ"
	if live {
		badge = "TRỰC TIẾP"
	}
	url := mapURL(lat, long)
	label := "Mở bản đồ ↗"
	w := min(maxW, cardMax, max(cardMin, ansi.StringWidth(badge)+ansi.StringWidth(title)+7))
	bw := ansi.StringWidth(badge) + 2
	field := w - 4 - bw - 1
	if field < 6 {
		return nil
	}
	link := fgSeq(cLink)
	if url == hot {
		link += "\x1b[4m"
	}
	edge := fg(cBorder)
	line := func(inner string) string { return edge.Render("│") + " " + inner + " " + edge.Render("│") }
	pad := strings.Repeat(" ", bw+1)
	rows := []string{
		edge.Render("╭" + strings.Repeat("─", w-2) + "╮"),
		line(sgr(cBg, badgeColours["sheet"]) + "\x1b[1m " + badge + " \x1b[0m " + fg(cFg).Bold(true).Render(fit(title, field))),
	}
	if coords != title {
		rows = append(rows, line(pad+fg(cMuted).Render(fit(coords, field))))
	}
	rows = append(rows,
		line(pad+linkSeq("g"+strconv.Itoa(m.ID), url)+link+label+"\x1b[0m"+linkEnd+strings.Repeat(" ", max(field-ansi.StringWidth(label), 0))),
		edge.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return rows
}

var coordRe = regexp.MustCompile(`(-?\d{1,3}(?:\.\d+)?)\s*[,; ]\s*(-?\d{1,3}(?:\.\d+)?)`)

// parsePosition reads "10.77, 106.69", "10.77 106.69" or a map link (Google Maps "@lat,long" or "q=lat,long").
func parsePosition(s string) (float64, float64, error) {
	s = strings.TrimSpace(s)
	for _, key := range []string{"@", "q=", "ll=", "query="} {
		if i := strings.Index(s, key); i >= 0 && strings.Contains(s, "://") {
			s = s[i+len(key):]
			break
		}
	}
	m := coordRe.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, errors.New("cần vĩ độ, kinh độ, ví dụ /position 10.7769, 106.7009")
	}
	lat, _ := strconv.ParseFloat(m[1], 64)
	long, _ := strconv.ParseFloat(m[2], 64)
	if lat < -90 || lat > 90 || long < -180 || long > 180 {
		return 0, 0, errors.New("vĩ độ phải trong -90..90 và kinh độ trong -180..180")
	}
	return lat, long, nil
}

// sendGeo sends a location, as a reply or into a forum topic like sendText.
func sendGeo(ctx context.Context, api *tg.Client, p tg.InputPeerClass, lat, long float64, replyTo, tp int) error {
	rid, err := randID()
	if err != nil {
		return err
	}
	req := &tg.MessagesSendMediaRequest{Peer: p, RandomID: rid, Media: &tg.InputMediaGeoPoint{GeoPoint: &tg.InputGeoPoint{Lat: lat, Long: long}}}
	switch {
	case replyTo != 0 && tp > 1:
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: replyTo, TopMsgID: tp}
	case replyTo != 0:
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: replyTo}
	case tp > 1:
		req.ReplyTo = &tg.InputReplyToMessage{ReplyToMsgID: tp}
	}
	_, err = api.MessagesSendMedia(ctx, req)
	return err
}

// positionCmd is /position: send the point; the chat is read again afterwards to show it (our own sends are not
// announced back).
func (m *model) positionCmd(c chat, arg string) tea.Cmd {
	lat, long, err := parsePosition(arg)
	if err != nil {
		return m.toastCmd(err.Error())
	}
	rt, tp := 0, 0
	if m.reply != nil {
		rt = m.reply.id
	}
	if c.forum && c.topic > 0 {
		tp = c.topic
	}
	m.in.SetValue("")
	m.setReply(nil)
	ctx, api, mk := m.ctx, m.api, m.mk()
	text := fmt.Sprintf("vị trí %.5f, %.5f", lat, long)
	return func() tea.Msg {
		return actMsg{kind: "geo", mk: mk, text: text, key: c.key, err: sendGeo(ctx, api, c.peer, lat, long, rt, tp)}
	}
}
