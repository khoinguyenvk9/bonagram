package main

// Sending a video so that it plays inside Telegram: an MP4/MOV goes as a video document with its duration and
// frame size (read from the file's own boxes, no tool needed) and, when ffmpeg is installed, a cover picture.
// Without these the other side only gets a file to download.

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// videoMIME is the type sent for each extension sent as a video; anything else stays a plain file.
var videoMIME = map[string]string{".mp4": "video/mp4", ".m4v": "video/mp4", ".mov": "video/quicktime"}

type videoMeta struct {
	dur    time.Duration
	w, h   int
	stream bool // the index (moov) is before the data, so it can be played while it downloads
}

// probeVideo reads an MP4/QuickTime file's duration (mvhd), frame size (tkhd of the first track that has one,
// turned when the track is rotated by 90°) and whether moov comes before mdat. ok is false when the file is not
// one it understands or has no picture, and the caller then sends it as a file.
func probeVideo(path string) (videoMeta, bool) {
	f, err := os.Open(path)
	if err != nil {
		return videoMeta{}, false
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return videoMeta{}, false
	}
	var v videoMeta
	var sawMoov, sawMdat bool
	err = walkBoxes(f, 0, st.Size(), func(typ string, off, size int64) error {
		switch typ {
		case "mdat":
			sawMdat = true
		case "moov":
			v.stream = !sawMdat
			sawMoov = true
			return walkBoxes(f, off, size, func(typ string, off, size int64) error {
				switch typ {
				case "mvhd":
					v.dur = readMvhd(f, off, size)
				case "trak":
					if v.w > 0 {
						return nil
					}
					return walkBoxes(f, off, size, func(typ string, off, size int64) error {
						if typ == "tkhd" && v.w == 0 {
							v.w, v.h = readTkhd(f, off, size)
						}
						return nil
					})
				}
				return nil
			})
		}
		return nil
	})
	if err != nil || !sawMoov || v.w <= 0 || v.h <= 0 {
		return videoMeta{}, false
	}
	return v, true
}

// walkBoxes calls fn for each box in [off, off+size): its type and its body (after the header).
func walkBoxes(r io.ReaderAt, off, size int64, fn func(typ string, body, bodySize int64) error) error {
	end := off + size
	var hdr [16]byte
	for off+8 <= end {
		if _, err := r.ReadAt(hdr[:8], off); err != nil {
			return err
		}
		n, head := int64(binary.BigEndian.Uint32(hdr[:4])), int64(8)
		typ := string(hdr[4:8])
		switch n {
		case 1: // 64-bit size follows
			if _, err := r.ReadAt(hdr[8:16], off+8); err != nil {
				return err
			}
			n, head = int64(binary.BigEndian.Uint64(hdr[8:16])), 16
		case 0: // to the end
			n = end - off
		}
		if n < head || off+n > end {
			return errors.New("bad box")
		}
		if err := fn(typ, off+head, n-head); err != nil {
			return err
		}
		off += n
	}
	return nil
}

func readMvhd(r io.ReaderAt, off, size int64) time.Duration {
	var b [32]byte
	if size < 20 {
		return 0
	}
	if _, err := r.ReadAt(b[:min(int(size), 32)], off); err != nil && !errors.Is(err, io.EOF) {
		return 0
	}
	var scale, dur uint64
	if b[0] == 1 { // version 1: 64-bit times
		if size < 32 {
			return 0
		}
		scale, dur = uint64(binary.BigEndian.Uint32(b[20:24])), binary.BigEndian.Uint64(b[24:32])
	} else {
		scale, dur = uint64(binary.BigEndian.Uint32(b[12:16])), uint64(binary.BigEndian.Uint32(b[16:20]))
	}
	if scale == 0 {
		return 0
	}
	return time.Duration(float64(dur) / float64(scale) * float64(time.Second))
}

// readTkhd returns the track's display size (16.16 fixed point at the end of the box), swapped when the
// matrix turns it by 90° (phones store portrait videos as landscape plus a rotation).
func readTkhd(r io.ReaderAt, off, size int64) (int, int) {
	var ver [1]byte
	if _, err := r.ReadAt(ver[:], off); err != nil {
		return 0, 0
	}
	mtx := int64(40) // version 0: flags 4, times 8, track 4, reserved 4, duration 4, reserved 8, layer/group/volume/reserved 8
	if ver[0] == 1 {
		mtx = 52
	}
	if size < mtx+36+8 {
		return 0, 0
	}
	var b [44]byte
	if _, err := r.ReadAt(b[:], off+mtx); err != nil {
		return 0, 0
	}
	a, c := int32(binary.BigEndian.Uint32(b[0:4])), int32(binary.BigEndian.Uint32(b[4:8]))
	w, h := int(binary.BigEndian.Uint32(b[36:40])>>16), int(binary.BigEndian.Uint32(b[40:44])>>16)
	if a == 0 && (c == 1<<16 || c == -1<<16) {
		w, h = h, w
	}
	return w, h
}

// ffmpegPath finds ffmpeg, used only for a video's cover picture; a variable so tests decide.
var ffmpegPath = func() string {
	p, _ := exec.LookPath("ffmpeg")
	return p
}

// videoThumb makes a cover picture (JPEG, at most 320 px, a second into the video or its first frame) in dir.
// "" when ffmpeg is missing or fails: the video is still sent, only without a cover.
func videoThumb(ctx context.Context, path, dir string) string {
	ff := ffmpegPath()
	if ff == "" {
		return ""
	}
	out := filepath.Join(dir, strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))+"-thumb.jpg")
	for _, at := range []string{"1", "0"} {
		ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		err := exec.CommandContext(ctx, ff, "-v", "error", "-y", "-ss", at, "-i", path, "-frames:v", "1",
			"-vf", "scale='if(gt(iw,ih),min(320,iw),-2)':'if(gt(iw,ih),-2,min(320,ih))'", "-q:v", "5", out).Run()
		cancel()
		if st, e := os.Stat(out); err == nil && e == nil && st.Size() > 0 && st.Size() <= 200<<10 {
			return out
		}
	}
	return ""
}
