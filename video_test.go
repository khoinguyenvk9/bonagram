package main

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

func box(typ string, body ...[]byte) []byte {
	n := 8
	for _, b := range body {
		n += len(b)
	}
	out := binary.BigEndian.AppendUint32(nil, uint32(n))
	out = append(out, typ...)
	for _, b := range body {
		out = append(out, b...)
	}
	return out
}

// mvhd version 0: flags, two times, timescale, duration, then padding.
func mvhd(scale, dur uint32) []byte {
	b := make([]byte, 100)
	binary.BigEndian.PutUint32(b[12:], scale)
	binary.BigEndian.PutUint32(b[16:], dur)
	return box("mvhd", b)
}

// tkhd version 0 with a matrix (a, b) and a 16.16 width/height.
func tkhd(w, h int, a, b int32) []byte {
	body := make([]byte, 84)
	binary.BigEndian.PutUint32(body[40:], uint32(a))
	binary.BigEndian.PutUint32(body[44:], uint32(b))
	binary.BigEndian.PutUint32(body[76:], uint32(w)<<16)
	binary.BigEndian.PutUint32(body[80:], uint32(h)<<16)
	return box("tkhd", body)
}

func writeVideo(t *testing.T, name string, parts ...[]byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	var all []byte
	for _, b := range parts {
		all = append(all, b...)
	}
	if err := os.WriteFile(p, all, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProbeVideo(t *testing.T) {
	ftyp, mdat := box("ftyp", []byte("isom0000")), box("mdat", make([]byte, 64))
	audio := box("trak", tkhd(0, 0, 1<<16, 0))
	moov := box("moov", mvhd(1000, 3500), audio, box("trak", tkhd(640, 360, 1<<16, 0)))

	v, ok := probeVideo(writeVideo(t, "a.mp4", ftyp, mdat, moov))
	if !ok || v.dur != 3500*time.Millisecond || v.w != 640 || v.h != 360 || v.stream {
		t.Fatalf("moov at the end: %+v %v", v, ok)
	}
	if v, ok = probeVideo(writeVideo(t, "b.mp4", ftyp, moov, mdat)); !ok || !v.stream {
		t.Fatalf("moov first = streamable: %+v", v)
	}
	rot := box("moov", mvhd(600, 600), box("trak", tkhd(1920, 1080, 0, 1<<16)))
	if v, ok = probeVideo(writeVideo(t, "c.mov", ftyp, rot, mdat)); !ok || v.w != 1080 || v.h != 1920 {
		t.Fatalf("a phone's portrait video (rotated 90°) must be tall: %+v", v)
	}
	for name, parts := range map[string][][]byte{
		"no picture": {ftyp, box("moov", mvhd(1, 1), audio), mdat},
		"no moov":    {ftyp, mdat},
		"truncated":  {ftyp, []byte{0, 0, 1, 0, 'm', 'o', 'o', 'v', 1, 2}},
		"not mp4":    {[]byte("hello, this is not a video at all")},
		"empty":      {},
	} {
		if _, ok := probeVideo(writeVideo(t, "x.mp4", parts...)); ok {
			t.Errorf("%s: must fall back to a file", name)
		}
	}
	if _, ok := probeVideo(filepath.Join(t.TempDir(), "missing.mp4")); ok {
		t.Error("missing file")
	}
}

// An MP4 goes as a video document with its attributes; a photo and a video share one album; a .mkv stays a file.
func TestSendVideo(t *testing.T) {
	old := ffmpegPath
	ffmpegPath = func() string { return "" }
	t.Cleanup(func() { ffmpegPath = old })

	moov := box("moov", mvhd(1000, 2000), box("trak", tkhd(720, 1280, 1<<16, 0)))
	clip := writeVideo(t, "clip.mp4", box("ftyp", []byte("isom0000")), moov, box("mdat", make([]byte, 64)))
	rec := &recorder{}
	api := tg.NewClient(rec)
	if err := sendFiles(context.Background(), api, &tg.InputPeerUser{UserID: 1}, []string{clip}, "xem", 0, nil); err != nil {
		t.Fatal(err)
	}
	req, ok := rec.last(t).(*tg.MessagesSendMediaRequest)
	if !ok {
		t.Fatalf("want sendMedia, got %T", rec.last(t))
	}
	doc, ok := req.Media.(*tg.InputMediaUploadedDocument)
	if !ok || doc.ForceFile || doc.MimeType != "video/mp4" {
		t.Fatalf("not a video document: %+v", req.Media)
	}
	var v *tg.DocumentAttributeVideo
	var name string
	for _, a := range doc.Attributes {
		switch a := a.(type) {
		case *tg.DocumentAttributeVideo:
			v = a
		case *tg.DocumentAttributeFilename:
			name = a.FileName
		}
	}
	if v == nil || v.Duration != 2 || v.W != 720 || v.H != 1280 || !v.SupportsStreaming || name != "clip.mp4" {
		t.Fatalf("video attributes: %+v name=%q", v, name)
	}

	photo := filepath.Join(t.TempDir(), "a.png")
	os.WriteFile(photo, []byte("img"), 0o600)
	if err := sendFiles(context.Background(), api, &tg.InputPeerUser{UserID: 1}, []string{photo, clip}, "", 0, nil); err != nil {
		t.Fatal(err)
	}
	if al, ok := rec.last(t).(*tg.MessagesSendMultiMediaRequest); !ok || len(al.MultiMedia) != 2 {
		t.Fatalf("a photo and a video go as one album: %T", rec.last(t))
	}

	mkv := writeVideo(t, "x.mkv", []byte("matroska"))
	if err := sendFiles(context.Background(), api, &tg.InputPeerUser{UserID: 1}, []string{mkv}, "", 0, nil); err != nil {
		t.Fatal(err)
	}
	if d, ok := rec.last(t).(*tg.MessagesSendMediaRequest).Media.(*tg.InputMediaUploadedDocument); !ok || !d.ForceFile {
		t.Fatalf("an mkv is sent as a file: %+v", rec.last(t))
	}
}

// Ctrl+X stops the send in progress; its answer is a notice, not an error, and the chat is read again.
func TestCancelUpload(t *testing.T) {
	var mm tea.Model = newTestModel(t)
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	mm, _ = mm.Update(chatsMsg{chats: []chat{{key: "u1", title: "Đạt", peer: &tg.InputPeerUser{UserID: 1}}}})
	m := mm.(model)
	path := filepath.Join(t.TempDir(), "clip.mp4")
	os.WriteFile(path, make([]byte, 4096), 0o600)
	m.attach = []string{path}
	m.sendAttached(m.chats[0], "")
	if len(m.xfers) != 1 {
		t.Fatal("no transfer")
	}
	up := m.xfers[0]
	if !strings.Contains(ansi.Strip(m.View()), "huỷ gửi tệp") {
		t.Fatal("the Ctrl+X hint is missing while sending")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = mm.(model)
	if !up.stopped || !strings.Contains(m.note, "Đang huỷ") {
		t.Fatalf("Ctrl+X: stopped=%v note=%q", up.stopped, m.note)
	}
	m.onAct(actMsg{kind: "file", text: "clip.mp4", key: "u1", err: errors.Join(errors.New("upload"), context.Canceled), up: up})
	if m.err != "" || len(m.xfers) != 0 || !strings.Contains(m.toast, "Đã huỷ gửi clip.mp4") {
		t.Fatalf("after cancel: err=%q toast=%q", m.err, m.toast)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlX}) // nothing is being sent: nothing happens
	if mm.(model).err != "" {
		t.Fatal("Ctrl+X with no send must do nothing")
	}
}
