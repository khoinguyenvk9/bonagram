package main

// Benchmarks of drawing a chat of 500 messages: go test -run TestPerfHeap -v -bench . -benchmem

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/tg"
)

func perfModel(b *testing.B) tea.Model {
	t := &testing.T{}
	var mm tea.Model = newTestModel(t)
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	mm, _ = mm.Update(chatsMsg{chats: []chat{{key: "u1", title: "Đạt", peer: &tg.InputPeerUser{UserID: 1}}}})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	var ls []line
	now := time.Now()
	for i := 1; i <= 500; i++ {
		text := fmt.Sprintf("tin số %d: xem https://example.com/a/%d nhé, **đậm** và `code` cho dài thêm một chút để xuống dòng", i, i)
		msg := &tg.Message{ID: i, Message: text, Entities: []tg.MessageEntityClass{&tg.MessageEntityURL{Offset: 12, Length: 22}}}
		ls = append(ls, line{id: i, from: []string{"Đạt", "Lan", "Bình"}[i%3], sender: fmt.Sprint("u", i%3), text: text, at: now.Add(time.Duration(i) * time.Minute), msg: msg, replyTo: max(i-3, 0)})
	}
	mm, _ = mm.Update(histMsg{mk: "u1", key: "u1", lines: ls})
	return mm
}

func BenchmarkRender500(b *testing.B) {
	mm := perfModel(b)
	m := mm.(model)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.render()
	}
}

func BenchmarkView(b *testing.B) {
	mm := perfModel(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mm.View()
	}
}

func BenchmarkWheel(b *testing.B) {
	mm := perfModel(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		btn := tea.MouseButtonWheelUp
		if i%2 == 0 {
			btn = tea.MouseButtonWheelDown
		}
		mm, _ = mm.Update(tea.MouseMsg{X: 60, Y: 20, Button: btn, Action: tea.MouseActionPress})
		_ = mm.View()
	}
}

func BenchmarkHoverMove(b *testing.B) {
	mm := perfModel(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mm, _ = mm.Update(tea.MouseMsg{X: 40 + i%40, Y: 20, Button: tea.MouseButtonNone, Action: tea.MouseActionMotion})
		_ = mm.View()
	}
}

func TestPerfHeap(t *testing.T) {
	runtime.GC()
	var a, z runtime.MemStats
	runtime.ReadMemStats(&a)
	mm := perfModel(nil)
	runtime.GC()
	runtime.ReadMemStats(&z)
	_ = mm.View()
	runtime.GC()
	runtime.ReadMemStats(&z)
	runtime.KeepAlive(mm)
	t.Logf("HEAP in use with 500 messages: %.2f MB (before %.2f MB)", float64(z.HeapInuse)/1e6, float64(a.HeapInuse)/1e6)
}

// BenchmarkTypeInLongDraft is one key typed into a 5,500-character draft, with the frame drawn after it.
func BenchmarkTypeInLongDraft(b *testing.B) {
	m := newModel(b.Context(), nil, make(chan tea.Msg), config{maxMsgs: 500}, sideDef)
	var mm tea.Model = m
	mm, _ = mm.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm, _ = mm.Update(chatsMsg{chats: []chat{{key: "u1", title: "Đạt"}}})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune(strings.Repeat("chào buổi sáng cả nhà ", 250))})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mm, _ = mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		_ = mm.View()
	}
}
