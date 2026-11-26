package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
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

// drawing is set by the first frame written to the terminal (never in tests, which only call View).
var drawing atomic.Bool

// setCursor records where the real cursor goes. A move that leaves the frame unchanged (←, Home, a click in
// the box) makes Bubble Tea skip the frame and with it the tail of lockedOut.Write, so the move is written
// here too: otherwise the IME draws its half-typed word where the cursor was before.
func setCursor(row, col int) {
	var p *cursorPos
	if row >= 0 {
		p = &cursorPos{row, col}
	}
	old := realCursor.Swap(p)
	if drawing.Load() && ((old == nil) != (p == nil) || old != nil && p != nil && *old != *p) {
		termWrite(cursorSeq(p))
	}
}

func cursorSeq(p *cursorPos) string {
	if p == nil {
		return "\x1b[?25l"
	}
	return fmt.Sprintf("\x1b[%d;%dH\x1b[?25h", p.row+1, p.col+1)
}

func (o lockedOut) Write(b []byte) (int, error) {
	outMu.Lock()
	defer outMu.Unlock()
	if bytes.Contains(b, []byte("\x1b[?1049l")) { // leaving the alternate screen: leave the cursor alone
		drawing.Store(false)
		return o.f.Write(b)
	}
	drawing.Store(true)
	tail := cursorSeq(realCursor.Load())
	// Autowrap is off while a frame is drawn: if the terminal measures some character wider than we did, the
	// overflow is clipped instead of wrapping onto the next line and shifting (and overlapping) the rest of
	// the screen. It is back on right after, so a crash can not leave the shell without it.
	if _, err := o.f.Write(append(append([]byte("\x1b[?7l"), b...), takeDraw()+tail+"\x1b[?7h"...)); err != nil {
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
	return pickOf(p.Sizes, thumb)
}

// pickOf is pickSize over any list of sizes (a photo's, or the thumbnails of a document).
func pickOf(sizes []tg.PhotoSizeClass, thumb bool) string {
	best, bestArea := "", -1
	have := map[string]bool{}
	for _, s := range sizes {
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
	return fetchP(ctx, api, loc, path, nil)
}

// fetchP is fetch telling pr (may be nil) how many bytes arrived. The size is not given to the downloader: with
// it, a download the server cut short came out padded to the size instead of failing the size check.
func fetchP(ctx context.Context, api *tg.Client, loc tg.InputFileLocationClass, path string, pr *xfer) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	tmp := path + ".part"
	b := downloader.NewDownloader().Download(api, loc)
	if pr != nil {
		b = b.WithProgress(pr.got)
	}
	if _, err := b.ToPath(ctx, tmp); err != nil {
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

// imageDocOf is a picture sent as a file ("without compression"): a document of an image type that is not a
// sticker or GIF and has a thumbnail the server made for it. It returns the thumbnail size to fetch, so the view
// shows a preview without downloading the whole original (which can be tens of MB).
func imageDocOf(m *tg.Message) (*tg.Document, string, bool) {
	d, ok := docOf(m)
	if !ok || d == nil || !strings.HasPrefix(d.MimeType, "image/") || infoOf(d).kind != "file" {
		return nil, "", false
	}
	typ := pickOf(d.Thumbs, true)
	return d, typ, typ != ""
}

// downloadDocThumb keeps the server's thumbnail of a document in the cache folder (thumbnails are JPEG).
func downloadDocThumb(ctx context.Context, api *tg.Client, d *tg.Document, typ string) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	loc := &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference, ThumbSize: typ}
	return fetch(ctx, api, loc, filepath.Join(dir, fmt.Sprintf("d%d-%s.jpg", d.ID, typ)))
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

// mpvPath finds mpv, which plays a video inside the terminal; a variable so tests decide.
var mpvPath = func() string {
	p, _ := exec.LookPath("mpv")
	return p
}

// mpvArgs plays the file with the picture drawn the way this terminal can: Kitty graphics, Sixel, or coloured
// text cells everywhere else. q quits and gives the screen back to the app.
func mpvArgs(path string) []string {
	vo := "--vo=tct"
	switch {
	case kitty:
		vo = "--vo=kitty"
	case rawProto == "sixel":
		vo = "--vo=sixel"
	}
	return []string{vo, "--really-quiet", "--keep-open=no", "--loop-file=no", path}
}

// staticSticker is the document of a sticker that is a still picture (webp); animated ones (tgs, webm) are not.
func staticSticker(m *tg.Message) (*tg.Document, bool) {
	d, ok := docOf(m)
	if !ok || d.MimeType != "image/webp" {
		return nil, false
	}
	return d, infoOf(d).kind == "sticker"
}

// downloadSticker keeps a sticker in the cache folder (not in Downloads: it is only shown).
func downloadSticker(ctx context.Context, api *tg.Client, d *tg.Document) (string, error) {
	dir, err := cacheDir()
	if err != nil {
		return "", err
	}
	loc := &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference}
	return fetch(ctx, api, loc, filepath.Join(dir, fmt.Sprintf("s%d.webp", d.ID)))
}

// downloadsDir is where received files are saved: the user's Downloads folder, so they can be found.
func downloadsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return cacheDir()
	}
	d := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(d, 0o755); err != nil {
		return cacheDir()
	}
	return d, nil
}

// savePath picks the file to write: the name of the file in dir, or "name (1).ext" when another file of that
// name is already there. A file of the same name and size is taken to be this one downloaded before.
func savePath(dir, name string, size int64) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	p := filepath.Join(dir, name)
	for n := 1; n < 1000; n++ {
		st, err := os.Stat(p)
		if err != nil || (size > 0 && st.Size() == size) {
			return p
		}
		p = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, n, ext))
	}
	return p
}

// downloadDoc saves a document into the Downloads folder and checks that all of it arrived.
func downloadDoc(ctx context.Context, api *tg.Client, d *tg.Document, pr *xfer) (string, error) {
	dir, err := downloadsDir()
	if err != nil {
		return "", err
	}
	loc := &tg.InputDocumentFileLocation{ID: d.ID, AccessHash: d.AccessHash, FileReference: d.FileReference}
	path, err := fetchP(ctx, api, loc, savePath(dir, docName(d), d.Size), pr)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(path); err == nil && d.Size > 0 && st.Size() != d.Size {
		os.Remove(path)
		return "", fmt.Errorf("tệp tải về thiếu (%s / %s)", humanSize(st.Size()), humanSize(d.Size))
	}
	return path, nil
}

// revealFile shows a file in the file manager; a variable so tests do not start programs.
var revealFile = func(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", "-R", path).Start()
	case "windows":
		return exec.Command("explorer", "/select,"+path).Start()
	}
	return exec.Command("xdg-open", filepath.Dir(path)).Start()
}

func openExternal(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows": // there is no xdg-open: this is what "start" does, without a shell to quote for
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

// --- drawing ------------------------------------------------------------------

type picture struct {
	rows []string
	id   uint32 // kitty image id, 0 when drawn with half-blocks
	raw  string // iTerm2 or Sixel sequence painted over the blank rows (viewer only)
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
	br, bg, bb := rgb(cBg) // transparent parts (stickers) take the app's background
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.RGBA{uint8(br), uint8(bg), uint8(bb), 255}}, image.Point{}, draw.Src)
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
