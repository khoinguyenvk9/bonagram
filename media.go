package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/tg"
	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// kitty: the terminal can show images through unicode placeholders (Kitty, Ghostty). Anywhere else
// (WezTerm, Terminal.app, tmux...) images fall back to coloured half-blocks.
var kitty = os.Getenv("TMUX") == "" && (os.Getenv("TERM") == "xterm-kitty" || os.Getenv("TERM") == "xterm-ghostty" ||
	os.Getenv("TERM_PROGRAM") == "ghostty" || os.Getenv("KITTY_WINDOW_ID") != "")

var lastImg atomic.Uint32

// outMu serialises every write to the terminal: Bubble Tea's frames (through lockedOut) and our own
// escape sequences (images, bell, notifications), so one can never land in the middle of the other.
var outMu sync.Mutex

type lockedOut struct{ f *os.File }

// realCursor is where the real terminal cursor is put after every frame (nil = hidden). Bubble Tea hides it and
// draws its own, but an input method (Vietnamese Telex/VNI, CJK) shows its half-typed text at the real cursor.
var realCursor atomic.Pointer[cursorPos]

type cursorPos struct{ row, col int } // 0-based screen cell

func setCursor(row, col int) {
	if row < 0 {
		realCursor.Store(nil)
		return
	}
	realCursor.Store(&cursorPos{row, col})
}

func (o lockedOut) Write(b []byte) (int, error) {
	outMu.Lock()
	defer outMu.Unlock()
	if bytes.Contains(b, []byte("\x1b[?1049l")) { // leaving the alternate screen: leave the cursor alone
		return o.f.Write(b)
	}
	tail := "\x1b[?25l"
	if p := realCursor.Load(); p != nil {
		tail = fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", p.row+1, p.col+1)
	}
	// Autowrap is off while a frame is drawn: if the terminal measures some character wider than we did, the
	// overflow is clipped instead of wrapping onto the next line and shifting (and overlapping) the rest of
	// the screen. It is back on right after, so a crash can not leave the shell without it.
	if _, err := o.f.Write(append(append([]byte("\x1b[?7l"), b...), tail+"\x1b[?7h"...)); err != nil {
		return 0, err
	}
	return len(b), nil
}
func (o lockedOut) Read(b []byte) (int, error) { return o.f.Read(b) }
func (o lockedOut) Fd() uintptr                { return o.f.Fd() }
func (o lockedOut) Close() error               { return nil } // bubbletea.term.File needs it; stdout stays open

// termWrite sends our own escape sequences (images, bell, notifications) without a cursor move.
func termWrite(s string) {
	outMu.Lock()
	defer outMu.Unlock()
	os.Stdout.WriteString(s)
}

// cacheDir is where downloaded photos and files go: inside the user's own cache directory, not a shared
// /tmp, where on a multi-user machine somebody else could create the folder first.
func cacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	d := filepath.Join(base, "bonagram")
	return d, os.MkdirAll(d, 0o700)
}

func photoOf(m *tg.Message) (*tg.Photo, bool) {
	if m == nil {
		return nil, false
	}
	mm, ok := m.Media.(*tg.MessageMediaPhoto)
	if !ok {
		return nil, false
	}
	p, ok := mm.Photo.(*tg.Photo)
	return p, ok
}

func docOf(m *tg.Message) (*tg.Document, bool) {
	if m == nil {
		return nil, false
	}
	mm, ok := m.Media.(*tg.MessageMediaDocument)
	if !ok {
		return nil, false
	}
	d, ok := mm.Document.(*tg.Document)
	return d, ok
}

// pickSize returns the photo size type to download: the largest, or for a thumbnail the ~320px "m" one.
func pickSize(p *tg.Photo, thumb bool) string {
	best, bestArea := "", -1
	have := map[string]bool{}
	for _, s := range p.Sizes {
		var t string
		var w, h int
		switch v := s.(type) {
		case *tg.PhotoSize:
			t, w, h = v.Type, v.W, v.H
		case *tg.PhotoSizeProgressive:
			t, w, h = v.Type, v.W, v.H
		default:
			continue // stripped / cached / path sizes are not downloadable
		}
		have[t] = true
		if w*h > bestArea {
			best, bestArea = t, w*h
		}
	}
	if thumb {
		for _, t := range []string{"m", "x"} {
			if have[t] {
				return t
			}
		}
	}
	return best
}

func fetch(ctx context.Context, api *tg.Client, loc tg.InputFileLocationClass, path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	tmp := path + ".part"
	if _, err := downloader.NewDownloader().Download(api, loc).ToPath(ctx, tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return path, os.Rename(tmp, path)
}

func downloadPhoto(ctx context.Context, api *tg.Client, p *tg.Photo, thumb bool) (string, error) {
	typ := pickSize(p, thumb)
	if typ == "" {
		return "", errors.New("ảnh không có kích thước tải được")
	}
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	loc := &tg.InputPhotoFileLocation{ID: p.ID, AccessHash: p.AccessHash, FileReference: p.FileReference, ThumbSize: typ}
	return fetch(ctx, api, loc, filepath.Join(dir, fmt.Sprintf("p%d-%s.jpg", p.ID, typ)))
}

func docName(d *tg.Document) string {
	for _, a := range d.Attributes {
		if f, ok := a.(*tg.DocumentAttributeFilename); ok {
			if n := filepath.Base(strings.ReplaceAll(f.FileName, "\x00", "")); n != "." && n != ".." && n != string(filepath.Separator) {
				return n
			}
		}
	}
	ext := ".bin"
	if exts, _ := mime.ExtensionsByType(d.MimeType); len(exts) > 0 {
		ext = exts[0]
	}
	return "file" + ext
}

func downloadDoc(ctx context.Context, api *tg.Client, d *tg.Document) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	loc := &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference}
	return fetch(ctx, api, loc, filepath.Join(dir, fmt.Sprintf("d%d-%s", d.ID, docName(d))))
}

func openExternal(path string) error {
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	return exec.Command(opener, path).Start()
}

// --- drawing ------------------------------------------------------------------

type picture struct {
	rows []string
	id   uint32 // kitty image id, 0 when drawn with half-blocks
}

func loadImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

// fitCells sizes a w x h pixel image into at most maxC x maxR cells (a cell is about twice as tall as wide).
func fitCells(w, h, maxC, maxR int) (int, int) {
	c := max(maxC, 1)
	r := max((h*c+w)/(2*w), 1)
	if r > maxR {
		r = max(maxR, 1)
		c = max(min((2*w*r+h/2)/h, maxC), 1)
	}
	return c, r
}

// renderImage draws the image at path into at most maxC x maxR cells.
func renderImage(path string, maxC, maxR int) (picture, error) {
	img, err := loadImage(path)
	if err != nil {
		return picture{}, err
	}
	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return picture{}, errors.New("ảnh rỗng")
	}
	c, r := fitCells(b.Dx(), b.Dy(), min(maxC, len(diacritics)), min(maxR, len(diacritics)))
	if !kitty {
		return picture{rows: halfblocks(img, c, r)}, nil
	}
	pw := min(b.Dx(), c*20, 1600)
	dst := image.NewRGBA(image.Rect(0, 0, pw, max(pw*b.Dy()/b.Dx(), 1)))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, draw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return picture{}, err
	}
	id := lastImg.Add(1) + 1000
	// the PNG goes inline in 4 KiB chunks (works on every kitty-protocol terminal); q=2: no reply on stdin
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	var seq strings.Builder
	for first := true; len(data) > 0; first = false {
		n := min(4096, len(data))
		more := 0
		if n < len(data) {
			more = 1
		}
		if first {
			fmt.Fprintf(&seq, "\x1b_Ga=T,U=1,f=100,i=%d,c=%d,r=%d,q=2,m=%d;%s\x1b\\", id, c, r, more, data[:n])
		} else {
			fmt.Fprintf(&seq, "\x1b_Gm=%d,q=2;%s\x1b\\", more, data[:n])
		}
		data = data[n:]
	}
	termWrite(seq.String())
	return picture{rows: placeholders(id, c, r), id: id}, nil
}

// placeholders prints the cells an image id occupies: U+10EEEE + row + column marks, coloured with the id.
func placeholders(id uint32, c, r int) []string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&255, id>>8&255, id&255)
	rows := make([]string, r)
	for y := range rows {
		var b strings.Builder
		b.WriteString(fg)
		for x := 0; x < c; x++ {
			b.WriteRune(0x10EEEE)
			b.WriteRune(diacritics[y])
			b.WriteRune(diacritics[x])
		}
		b.WriteString("\x1b[39m")
		rows[y] = b.String()
	}
	return rows
}

func halfblocks(img image.Image, c, r int) []string {
	dst := image.NewRGBA(image.Rect(0, 0, c, 2*r))
	draw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)
	rows := make([]string, r)
	for y := range rows {
		var b strings.Builder
		for x := 0; x < c; x++ {
			t, u := dst.RGBAAt(x, 2*y), dst.RGBAAt(x, 2*y+1)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%d;48;2;%d;%d;%dm▀", t.R, t.G, t.B, u.R, u.G, u.B)
		}
		b.WriteString("\x1b[0m")
		rows[y] = b.String()
	}
	return rows
}

func dropImage(id uint32) {
	if id != 0 {
		termWrite(fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id))
	}
}

func dropAllImages() {
	if kitty && lastImg.Load() > 0 {
		termWrite("\x1b_Ga=d,d=A,q=2\x1b\\")
	}
}
