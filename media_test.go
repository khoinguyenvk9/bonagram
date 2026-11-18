package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
	"github.com/gotd/td/tgerr"
)

func docMsg(id int, name, mime string, size int64, attrs ...tg.DocumentAttributeClass) *tg.Message {
	if name != "" {
		attrs = append(attrs, &tg.DocumentAttributeFilename{FileName: name})
	}
	return &tg.Message{ID: id, Media: &tg.MessageMediaDocument{Document: &tg.Document{
		ID: int64(id), MimeType: mime, Size: size, Attributes: attrs}}}
}

func TestMediaTagsSayWhatTheyAre(t *testing.T) {
	for _, c := range []struct {
		media tg.MessageMediaClass
		want  string
	}{
		{docMsg(1, "báo cáo.pdf", "application/pdf", 2400000).Media, "[file] báo cáo.pdf · 2.3 MB"},
		{docMsg(1, "a.zip", "application/zip", 500).Media, "[file] a.zip · 500 B"},
		{docMsg(1, "", "application/x-foo", 0).Media, "[file] file.bin (application/x-foo)"},
		{docMsg(1, "v.mp4", "video/mp4", 5<<20, &tg.DocumentAttributeVideo{Duration: 34}).Media, "[video] v.mp4 0:34 · 5.0 MB"},
		{docMsg(1, "", "audio/ogg", 9000, &tg.DocumentAttributeAudio{Voice: true, Duration: 12}).Media, "[voice] 0:12 · 8.8 KB"},
		{docMsg(1, "", "image/webp", 9000, &tg.DocumentAttributeSticker{Alt: "😀"}).Media, "[sticker] 😀"},
		{&tg.MessageMediaWebPage{}, ""},
		{&tg.MessageMediaGeo{Geo: &tg.GeoPoint{Lat: 10.77, Long: 106.69}}, "[location] 10.77000, 106.69000"},
		{&tg.MessageMediaContact{FirstName: "An", PhoneNumber: "+84123"}, "[contact] An  +84123"},
	} {
		got := strings.Join(strings.Fields(mediaTag(c.media)), " ")
		if want := strings.Join(strings.Fields(c.want), " "); got != want {
			t.Errorf("%T: got %q want %q", c.media, got, want)
		}
	}
}

func homeIn(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, k := range []string{"HOME", "USERPROFILE", "XDG_CACHE_HOME", "LOCALAPPDATA"} {
		t.Setenv(k, home)
	}
	return home
}

// fileModel has one message with a file, selected.
func fileModel(t *testing.T, msg *tg.Message) *actionModel {
	t.Helper()
	a := newActionModel(t)
	a.rec.file = []byte("12345")
	a.send(histMsg{mk: "u1", key: "u1", lines: []line{{id: msg.ID, from: "Đạt", sender: "u1", text: mediaTag(msg.Media), at: time.Now(), msg: msg}}})
	m := a.m()
	m.bsel = 0
	a.set(m)
	return a
}

func TestDownloadFileSavesToDownloadsAndOpens(t *testing.T) {
	home := homeIn(t)
	var opened, revealed string
	oldO, oldR := openURL, revealFile
	openURL, revealFile = func(p string) error { opened = p; return nil }, func(p string) error { revealed = p; return nil }
	defer func() { openURL, revealFile = oldO, oldR }()

	a := fileModel(t, docMsg(7, "báo cáo.pdf", "application/pdf", 5))
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	want := filepath.Join(home, "Downloads", "báo cáo.pdf")
	if b, err := os.ReadFile(want); err != nil || string(b) != "12345" {
		t.Fatalf("the file should be in Downloads: %v %q", err, b)
	}
	if opened != want || revealed != "" || a.m().err != "" || a.m().note != "" {
		t.Fatalf("opened=%q revealed=%q err=%q note=%q", opened, revealed, a.m().err, a.m().note)
	}
	// a type that is not known to be harmless is shown in the file manager, never run
	opened = ""
	b := fileModel(t, docMsg(8, "run.sh", "application/x-sh", 5))
	b.run(b.send(tea.KeyMsg{Type: tea.KeyEnter}))
	if opened != "" || revealed != filepath.Join(home, "Downloads", "run.sh") {
		t.Fatalf("opened=%q revealed=%q", opened, revealed)
	}
	// the same name with other content does not overwrite
	if got := savePath(filepath.Join(home, "Downloads"), "run.sh", 99); got != filepath.Join(home, "Downloads", "run (1).sh") {
		t.Fatalf("save path: %q", got)
	}
}

func TestDownloadErrorsStayOnScreen(t *testing.T) {
	homeIn(t)
	a := fileModel(t, docMsg(7, "a.zip", "application/zip", 5))
	a.rec.fileErrs = []error{tgerr.New(400, "FILE_PARTS_INVALID")}
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	if e := a.m().err; !strings.Contains(e, "tải tệp lỗi") || !strings.Contains(e, "FILE_PARTS_INVALID") {
		t.Fatalf("the error should stay: %q", e)
	}
	// a short file is an error, not a silent half download
	b := fileModel(t, docMsg(9, "b.zip", "application/zip", 99))
	b.run(b.send(tea.KeyMsg{Type: tea.KeyEnter}))
	if e := b.m().err; !strings.Contains(e, "thiếu") {
		t.Fatalf("a short download: %q", e)
	}
}

func TestDownloadRetriesWithFreshFileReference(t *testing.T) {
	homeIn(t)
	oldO := openURL
	openURL = func(string) error { return nil }
	defer func() { openURL = oldO }()
	msg := docMsg(7, "a.pdf", "application/pdf", 5)
	a := fileModel(t, msg)
	a.rec.fileErrs = []error{tgerr.New(400, "FILE_REFERENCE_EXPIRED")}
	fresh := docMsg(7, "a.pdf", "application/pdf", 5)
	fresh.FromID, fresh.PeerID = &tg.PeerUser{UserID: 1}, &tg.PeerUser{UserID: 1}
	a.rec.msgs = []tg.MessageClass{fresh}
	a.rec.users = []tg.UserClass{&tg.User{ID: 1, FirstName: "Đạt"}}
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	if a.m().err != "" {
		t.Fatalf("after reading the message again the file should download: %q", a.m().err)
	}
}

func TestClickOnFileDoesNotDownload(t *testing.T) {
	homeIn(t)
	a := fileModel(t, docMsg(7, "big.zip", "application/zip", 5<<30))
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	m := a.m()
	n := a.rec.count()
	a.send(tea.MouseMsg{X: m.side + 5, Y: m.msgY() + m.lineAt[0] - m.vp.YOffset, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if a.rec.count() != n || !strings.Contains(a.m().toast, "Enter để tải big.zip (5.0 GB)") {
		t.Fatalf("a click must only select; requests %d→%d toast=%q", n, a.rec.count(), a.m().toast)
	}
}

func TestFileIsDrawnAsACard(t *testing.T) {
	msg := docMsg(7, "Báo cáo quý 3.pdf", "application/pdf", 2400000)
	rows := fileCard(msg, 50)
	if len(rows) != 1 || ansi.StringWidth(rows[0]) > 50 {
		t.Fatalf("a card is one row within the width, got %d", len(rows))
	}
	w := ansi.StringWidth(rows[0])
	for _, r := range rows {
		if ansi.StringWidth(r) != w {
			t.Fatalf("every row of a card has the same width: %q", ansi.Strip(strings.Join(rows, "\n")))
		}
	}
	text := ansi.Strip(strings.Join(rows, "\n"))
	for _, want := range []string{"PDF", "Báo cáo quý 3.pdf", "2.3 MB", "tải"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if fileCard(&tg.Message{}, 50) != nil || fileCard(docMsg(1, "", "image/webp", 5, &tg.DocumentAttributeSticker{Alt: "😀"}), 50) != nil {
		t.Fatal("only files get a card")
	}
	// in the message view the card replaces the one-line label
	a := fileModel(t, msg)
	v := plainView(a.m())
	if !strings.Contains(v, " PDF ") || !strings.Contains(v, "Báo cáo quý 3.pdf") || strings.Contains(v, "[file]") {
		t.Fatalf("the view should show the card:\n%s", v)
	}
}
