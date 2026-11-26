package main

// Transfers that take a while: sending files and downloading them. Each shows how far it got in the title bar
// (m.note, refreshed by xferTick) and Ctrl+X stops the newest one.

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/telegram/uploader"
)

// xfer counts the bytes of one send or download. The uploader reports per file (by upload ID, from several
// goroutines); a download reports one running total. total is the size of everything moved.
type xfer struct {
	label   string
	total   int64
	down    bool
	done    atomic.Bool
	stop    context.CancelFunc // set by startXfer; nil in tests that build one by hand
	stopped bool               // Ctrl+X was pressed; only the UI goroutine reads/writes it
	mu      sync.Mutex
	parts   map[int64]int64 // upload ID (0 for a download) → bytes moved
}

func newXfer(label string, total int64, down bool) *xfer {
	return &xfer{label: label, total: total, down: down, parts: map[int64]int64{}}
}

// Chunk is the uploader's progress callback.
func (p *xfer) Chunk(_ context.Context, s uploader.ProgressState) error {
	p.mu.Lock()
	p.parts[s.ID] = max(p.parts[s.ID], s.Uploaded)
	p.mu.Unlock()
	return nil
}

// got is the downloader's progress callback.
func (p *xfer) got(done, _ int64) {
	p.mu.Lock()
	p.parts[0] = max(p.parts[0], done)
	p.mu.Unlock()
}

func (p *xfer) moved() int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	var n int64
	for _, v := range p.parts {
		n += v
	}
	return min(n, max(p.total, 0))
}

// text is the note shown meanwhile: "⬆ 42% · 120.0 MB / 350.0 MB · Đang gửi a.mp4" (or ⬇ … Đang tải). The
// percent comes first because the note box is cut at half the title row: a long file name loses its end, not
// the progress. After the last byte of a send the server still has to make the message: that is said too.
func (p *xfer) text() string {
	verb, arrow := "gửi", "⬆"
	if p.down {
		verb, arrow = "tải", "⬇"
	}
	if p.stopped {
		return "Đang huỷ " + verb + " " + p.label + "…"
	}
	n := p.moved()
	switch {
	case p.total <= 0:
		return arrow + " Đang " + verb + " " + p.label + "…"
	case n >= p.total && p.down:
		return "⬇ Đã tải " + p.label
	case n >= p.total:
		return "⬆ Đã tải lên " + p.label + ", chờ Telegram xử lý…"
	}
	return fmt.Sprintf("%s %d%% · %s / %s · Đang %s %s", arrow, n*100/p.total, humanSize(n), humanSize(p.total), verb, p.label)
}

// startXfer shows p and returns the context its work runs in, which Ctrl+X cancels.
func (m *model) startXfer(ctx context.Context, p *xfer) context.Context {
	ctx, p.stop = context.WithCancel(ctx)
	m.xfers = append(m.xfers, p)
	m.note, m.toast = p.text(), ""
	return ctx
}

// endXfer forgets p; the note goes to the transfer still running, if any.
func (m *model) endXfer(p *xfer) {
	for i, q := range m.xfers {
		if q == p {
			m.xfers = append(m.xfers[:i:i], m.xfers[i+1:]...)
			m.note = ""
			if n := len(m.xfers); n > 0 {
				m.note = m.xfers[n-1].text()
			}
			return
		}
	}
}

// running is the newest transfer that was not stopped: the one Ctrl+X stops and the hint names.
func (m *model) running() *xfer {
	for i := len(m.xfers) - 1; i >= 0; i-- {
		if !m.xfers[i].stopped {
			return m.xfers[i]
		}
	}
	return nil
}

// cancelXfer stops the newest transfer (Ctrl+X). Files of an earlier album of the same send that already went
// stay sent; a send's answer (actMsg with context.Canceled) refetches the chat to show what landed, a
// download's half-written file is removed.
func (m *model) cancelXfer() bool {
	p := m.running()
	if p == nil {
		return false
	}
	p.stopped = true
	if p.stop != nil {
		p.stop()
	}
	m.note = p.text()
	return true
}

type xferTick struct{ p *xfer }

func xferTickCmd(p *xfer) tea.Cmd {
	return tea.Tick(400*time.Millisecond, func(time.Time) tea.Msg { return xferTick{p} })
}

// onXferTick refreshes the note of the newest transfer; a finished one is forgotten here too, for the
// answers that do not name it (a picture opening in the viewer, a video playing).
func (m *model) onXferTick(p *xfer) tea.Cmd {
	if p.done.Load() {
		m.endXfer(p)
		return nil
	}
	if n := len(m.xfers); n > 0 && m.xfers[n-1] == p {
		m.note = p.text()
	}
	return xferTickCmd(p)
}
