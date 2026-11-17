package main

// Images in as many terminals as possible. In the message list an image must be text the frame can hold, so only
// Kitty's unicode placeholders (Kitty, Ghostty) or coloured half-blocks are used there. The full-screen viewer can
// also use the protocols that paint pixels at the cursor: iTerm2's inline images (iTerm2, WezTerm, mintty, Rio,
// VS Code) and Sixel (foot, Windows Terminal, Konsole, mlterm, contour, xterm started with sixel). The picture is
// painted once, right after the frame that cleared room for it (see lockedOut.Write).

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	cpalette "image/color/palette"
	"image/png"
	"os"
	"strings"
	"sync/atomic"

	xdraw "golang.org/x/image/draw"
	"image/draw"
)

// rawProto is "iterm" or "sixel" when the viewer paints pixels that way, "" otherwise.
var rawProto = detectRaw(os.Getenv)

// detectRaw guesses from the environment whether the terminal speaks iTerm2's protocol or Sixel. Inside tmux
// neither is used: tmux would need passthrough and its own sixel support.
func detectRaw(env func(string) string) string {
	if env("TMUX") != "" {
		return ""
	}
	term, prog := env("TERM"), env("TERM_PROGRAM")
	switch {
	case prog == "iTerm.app", env("LC_TERMINAL") == "iTerm2", prog == "WezTerm", prog == "mintty", prog == "rio",
		prog == "vscode":
		return "iterm"
	case env("WT_SESSION") != "", env("KONSOLE_VERSION") != "", prog == "contour",
		strings.HasPrefix(term, "foot"), strings.HasPrefix(term, "mlterm"), strings.HasSuffix(term, "-sixel"):
		return "sixel"
	}
	return ""
}

// cellPixels is the size of one cell in pixels, a guess when the terminal does not say.
func cellPixels() (int, int) {
	if w, h := termCellPixels(); w > 0 && h > 0 {
		return w, h
	}
	return 10, 20
}

// drawReq is a picture waiting to be painted at a screen cell after the next frame.
type drawReq struct {
	row, col int
	seq      string
	id       uint32
}

var (
	pendingDraw atomic.Pointer[drawReq]
	drawnID     atomic.Uint32 // the picture already on screen; 0 = paint it again
)

// queueDraw asks for the picture to be painted after the next frame, unless it is already on screen.
func queueDraw(row, col int, p picture) {
	if p.raw == "" || drawnID.Load() == p.id {
		return
	}
	pendingDraw.Store(&drawReq{row, col, p.raw, p.id})
}

// takeDraw returns the escape sequence that paints the waiting picture ("" when none) and marks it drawn.
func takeDraw() string {
	r := pendingDraw.Swap(nil)
	if r == nil {
		return ""
	}
	drawnID.Store(r.id)
	return fmt.Sprintf("\x1b[%d;%dH", r.row+1, r.col+1) + r.seq
}

// renderViewerImage draws an image for the full-screen viewer with the best protocol the terminal has.
func renderViewerImage(path string, maxC, maxR int) (picture, error) {
	if rawProto == "" || kitty {
		return renderImage(path, maxC, maxR)
	}
	img, err := loadImage(path)
	if err != nil {
		return picture{}, err
	}
	b := img.Bounds()
	if b.Dx() < 1 || b.Dy() < 1 {
		return picture{}, fmt.Errorf("ảnh rỗng")
	}
	cw, ch := cellPixels()
	// cells are not square: fit the image by pixels, then round to whole cells
	c, r := maxC, max((b.Dy()*maxC*cw)/(b.Dx()*ch), 1)
	if r > maxR {
		r = maxR
		c = max(min((b.Dx()*maxR*ch)/(b.Dy()*cw), maxC), 1)
	}
	id := lastImg.Add(1) + 1000
	blank := make([]string, r)
	for i := range blank {
		blank[i] = strings.Repeat(" ", c)
	}
	var seq string
	if rawProto == "iterm" {
		seq, err = itermImage(img, c, r, cw)
	} else {
		seq = sixelImage(onBackground(img, c*cw, r*ch))
	}
	return picture{rows: blank, id: id, raw: seq}, err
}

// itermImage is iTerm2's inline image: a PNG the terminal scales into c x r cells.
func itermImage(img image.Image, c, r, cw int) (string, error) {
	b := img.Bounds()
	pw := min(b.Dx(), c*cw*2, 2000)
	dst := image.NewRGBA(image.Rect(0, 0, pw, max(pw*b.Dy()/b.Dx(), 1)))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return "", err
	}
	return fmt.Sprintf("\x1b]1337;File=inline=1;size=%d;width=%d;height=%d;preserveAspectRatio=1:%s\a",
		buf.Len(), c, r, base64.StdEncoding.EncodeToString(buf.Bytes())), nil
}

// onBackground scales the image to fit w x h pixels (keeping its shape) over the app's background colour, so
// transparent parts look right in a format without transparency.
func onBackground(img image.Image, w, h int) *image.RGBA {
	b := img.Bounds()
	pw, ph := w, max(w*b.Dy()/b.Dx(), 1)
	if ph > h {
		ph, pw = h, max(h*b.Dx()/b.Dy(), 1)
	}
	dst := image.NewRGBA(image.Rect(0, 0, pw, ph))
	r, g, bl := rgb(cBg)
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.RGBA{uint8(r), uint8(g), uint8(bl), 255}}, image.Point{}, draw.Src)
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), img, b, xdraw.Over, nil)
	return dst
}

// sixelImage encodes an image as Sixel: 256 colours (Plan 9 palette, error diffusion), six pixel rows per band,
// each colour of a band as one run-length coded line.
func sixelImage(src image.Image) string {
	b := src.Bounds()
	pal := image.NewPaletted(image.Rect(0, 0, b.Dx(), b.Dy()), cpalette.Plan9)
	draw.FloydSteinberg.Draw(pal, pal.Bounds(), src, b.Min)
	w, h := pal.Rect.Dx(), pal.Rect.Dy()
	var s strings.Builder
	fmt.Fprintf(&s, "\x1bP0;1;0q\"1;1;%d;%d", w, h)
	used := make([]bool, len(cpalette.Plan9))
	for _, i := range pal.Pix {
		used[i] = true
	}
	for i, c := range cpalette.Plan9 {
		if used[i] {
			r, g, bl, _ := c.RGBA()
			fmt.Fprintf(&s, "#%d;2;%d;%d;%d", i, r*100/0xffff, g*100/0xffff, bl*100/0xffff)
		}
	}
	row := make([]byte, w)
	for y0 := 0; y0 < h; y0 += 6 {
		var inBand [256]bool
		for y := y0; y < min(y0+6, h); y++ {
			for x := 0; x < w; x++ {
				inBand[pal.Pix[y*pal.Stride+x]] = true
			}
		}
		first := true
		for ci := range inBand {
			if !inBand[ci] {
				continue
			}
			for x := 0; x < w; x++ {
				var bits byte
				for k := 0; k < 6 && y0+k < h; k++ {
					if int(pal.Pix[(y0+k)*pal.Stride+x]) == ci {
						bits |= 1 << k
					}
				}
				row[x] = 63 + bits
			}
			if !first {
				s.WriteByte('$') // back to the start of the band for the next colour
			}
			first = false
			fmt.Fprintf(&s, "#%d", ci)
			for x := 0; x < w; {
				n := 1
				for x+n < w && row[x+n] == row[x] {
					n++
				}
				if n > 3 {
					fmt.Fprintf(&s, "!%d%c", n, row[x])
				} else {
					s.Write(bytes.Repeat([]byte{row[x]}, n))
				}
				x += n
			}
		}
		s.WriteByte('-')
	}
	s.WriteString("\x1b\\")
	return s.String()
}
