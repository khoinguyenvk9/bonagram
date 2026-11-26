package main

import (
	"encoding/base64"
	"image"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

func TestDetectImageProtocol(t *testing.T) {
	for _, c := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"TERM_PROGRAM": "iTerm.app"}, "iterm"},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, "iterm"},
		{map[string]string{"WT_SESSION": "x"}, "sixel"},
		{map[string]string{"TERM": "foot"}, "sixel"},
		{map[string]string{"KONSOLE_VERSION": "230800"}, "sixel"},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, ""},
		{map[string]string{"TERM_PROGRAM": "iTerm.app", "TMUX": "/tmp/x"}, ""},
	} {
		if got := detectRaw(func(k string) string { return c.env[k] }); got != c.want {
			t.Errorf("%v: got %q want %q", c.env, got, c.want)
		}
	}
}

func TestSixelEncoding(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 4, 7)) // two bands: 6 rows + 1
	for y := 0; y < 7; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	s := sixelImage(img)
	if !strings.HasPrefix(s, "\x1bP0;1;0q\"1;1;4;7") || !strings.HasSuffix(s, "-\x1b\\") || strings.Count(s, "-") != 2 {
		t.Fatalf("not a two-band sixel image: %q", s)
	}
	if !strings.Contains(s, "!4~") { // a full band of one colour, 4 wide, run-length coded
		t.Fatalf("expected a run of full sixels: %q", s)
	}
	if seq, err := itermImage(img, 3, 2, 10); err != nil || !strings.HasPrefix(seq, "\x1b]1337;File=inline=1;") || !strings.Contains(seq, "width=3;height=2") {
		t.Fatalf("iterm sequence: %q %v", seq[:min(len(seq), 60)], err)
	}
}

func TestRawPicturePaintedOnce(t *testing.T) {
	drawnID.Store(0)
	p := picture{rows: []string{"  "}, id: 4242, raw: "<img>"}
	queueDraw(3, 5, p)
	if got := takeDraw(); got != "\x1b[4;6H<img>" {
		t.Fatalf("first frame should paint at row 4 col 6: %q", got)
	}
	queueDraw(3, 5, p)
	if got := takeDraw(); got != "" {
		t.Fatalf("an image already on screen is not painted again: %q", got)
	}
	drawnID.Store(0) // a resize
	queueDraw(3, 5, p)
	if takeDraw() == "" {
		t.Fatal("after a resize it is painted again")
	}
}

func TestParsePosition(t *testing.T) {
	for _, c := range []struct {
		in        string
		lat, long float64
		bad       bool
	}{
		{"10.7769, 106.7009", 10.7769, 106.7009, false},
		{" 21.0285 105.8542", 21.0285, 105.8542, false},
		{"https://www.google.com/maps/@10.77,106.69,15z", 10.77, 106.69, false},
		{"https://maps.google.com/?q=-33.86,151.2", -33.86, 151.2, false},
		{"95, 10", 0, 0, true},
		{"nhà tôi", 0, 0, true},
	} {
		lat, long, err := parsePosition(c.in)
		if (err != nil) != c.bad || !c.bad && (lat != c.lat || long != c.long) {
			t.Errorf("%q: %v %v %v", c.in, lat, long, err)
		}
	}
}

func TestSendPosition(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	m.in.SetValue("/position 10.7769, 106.7009")
	a.set(m)
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	found := false
	for _, c := range a.rec.calls {
		if r, ok := c.(*tg.MessagesSendMediaRequest); ok {
			g := r.Media.(*tg.InputMediaGeoPoint).GeoPoint.(*tg.InputGeoPoint)
			found = g.Lat == 10.7769 && g.Long == 106.7009
		}
	}
	if !found || a.m().in.Value() != "" || a.m().err != "" {
		t.Fatalf("a location should be sent: calls=%d err=%q", len(a.rec.calls), a.m().err)
	}
	m = a.m()
	m.in.SetValue("/position chỗ cũ")
	a.set(m)
	n := len(a.rec.calls)
	a.send(tea.KeyMsg{Type: tea.KeyEnter})
	if len(a.rec.calls) != n || !strings.Contains(a.m().toast, "vĩ độ") {
		t.Fatalf("a bad position is explained, not sent: %q", a.m().toast)
	}
}

func TestLocationCardLinksToTheMap(t *testing.T) {
	msg := &tg.Message{ID: 3, Media: &tg.MessageMediaGeo{Geo: &tg.GeoPoint{Lat: 10.77, Long: 106.69}}}
	rows := geoCard(msg, 50, "")
	text := ansi.Strip(strings.Join(rows, "\n"))
	if !strings.Contains(text, "VỊ TRÍ") || !strings.Contains(text, "10.77000, 106.69000") || !strings.Contains(text, "Mở bản đồ") {
		t.Fatalf("card:\n%s", text)
	}
	for _, r := range rows {
		if ansi.StringWidth(r) != ansi.StringWidth(rows[0]) {
			t.Fatalf("uneven card:\n%s", text)
		}
	}
	url := "https://www.google.com/maps?q=10.770000,106.690000"
	if len(rows) != 1 {
		t.Fatalf("a location is one row: %d", len(rows))
	}
	if sp := linkSpans(0, rows[0]); len(sp) != 1 || sp[0].url != url {
		t.Fatalf("the map row should be a link: %+v", sp)
	}
	if ls := linksOf(msg); len(ls) != 1 || ls[0] != url {
		t.Fatalf("o and L should find the map link: %v", ls)
	}
}

const tinyWebp = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

func TestStaticStickerIsDrawn(t *testing.T) {
	homeIn(t)
	a := newActionModel(t)
	a.rec.file, _ = base64.StdEncoding.DecodeString(tinyWebp)
	st := docMsg(11, "", "image/webp", 26, &tg.DocumentAttributeSticker{Alt: "😀"})
	anim := docMsg(12, "", "application/x-tgsticker", 26, &tg.DocumentAttributeSticker{Alt: "🎉"})
	a.runAll(a.send(histMsg{mk: "u1", key: "u1", lines: []line{
		{id: 11, from: "Đạt", text: mediaTag(st.Media), at: time.Now(), msg: st},
		{id: 12, from: "Đạt", text: mediaTag(anim.Media), at: time.Now(), msg: anim},
	}}))
	m := a.m()
	if th := m.thumbs[11]; th == nil || !th.done || len(th.rows) != stickerRows {
		t.Fatalf("the static sticker should be drawn: %+v", m.thumbs[11])
	}
	if m.thumbs[12] != nil {
		t.Fatal("an animated sticker stays a label")
	}
}

func TestVideoPlaysInTerminalWithMpv(t *testing.T) {
	homeIn(t)
	old := mpvPath
	mpvPath = func() string { return "/usr/bin/mpv" }
	defer func() { mpvPath = old }()
	a := fileModel(t, docMsg(7, "v.mp4", "video/mp4", 5, &tg.DocumentAttributeVideo{Duration: 3}))
	cmd := a.send(tea.KeyMsg{Type: tea.KeyEnter})
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok { // the download and its progress ticks
		msg = b[0]()
	}
	if _, ok := msg.(playMsg); !ok {
		t.Fatal("a video should be handed to mpv")
	}
	oldK, oldR := kitty, rawProto
	kitty, rawProto = false, "sixel"
	defer func() { kitty, rawProto = oldK, oldR }()
	if args := mpvArgs("/x.mp4"); args[0] != "--vo=sixel" || args[len(args)-1] != "/x.mp4" {
		t.Fatalf("args: %v", args)
	}
}
