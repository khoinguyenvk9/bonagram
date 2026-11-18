package main

import (
	"context"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestInitials(t *testing.T) {
	for in, want := range map[string]string{
		"Nhóm Dev Go Việt Nam": "ND",
		"Đạt":                  "ĐẠ",
		"Bố":                   "BỐ",
		"Lan Phương":           "LP",
		"🎉 Party":              "PA",
		"   ":                  "?",
		"小明 同学":                "小",
	} {
		if got := initials(in); got != want {
			t.Errorf("initials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNarrowWindowFoldsTheList(t *testing.T) {
	a := newActionModel(t) // two chats, Đạt (u1) open
	m := a.m()
	m.chats[1].unread = 3
	a.set(m)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.send(tea.WindowSizeMsg{Width: 80, Height: 30})
	if a.m().rail {
		t.Fatal("80 columns still fit the list (36) beside the messages (40): no fold")
	}
	a.send(tea.WindowSizeMsg{Width: 70, Height: 30})
	m = a.m()
	if !m.rail || m.side != railW {
		t.Fatalf("a window of 70 columns folds the list: rail=%v side=%d", m.rail, m.side)
	}
	v := ansi.Strip(a.mm.View())
	if !strings.Contains(v, "ĐẠ") || !strings.Contains(v, "LA 3") {
		t.Fatalf("the rail shows initials and unread counts:\n%s", v)
	}
	frameOK(t, *m)

	a.send(tea.MouseMsg{X: 3, Y: listY0 + itemRows, Action: tea.MouseActionMotion, Button: tea.MouseButtonNone})
	if v := ansi.Strip(a.mm.View()); !strings.Contains(v, "Lan") || !strings.Contains(v, "3 chưa đọc") {
		t.Fatal("hovering a tile shows the chat's name")
	}
	frameOK(t, *a.m())

	a.run(a.send(tea.MouseMsg{X: 3, Y: listY0 + itemRows, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}))
	if m = a.m(); m.cur != "u3" || !m.rail {
		t.Fatalf("a click on a tile opens the chat and the list stays folded: cur=%q", m.cur)
	}

	a.send(tea.KeyMsg{Type: tea.KeyCtrlK})
	if m = a.m(); m.rail || m.side <= railW {
		t.Fatal("searching opens the full list")
	}
	frameOK(t, *m)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	if m = a.m(); !m.rail || m.side != railW {
		t.Fatal("closing the search folds it again")
	}

	a.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	if m = a.m(); m.rail || m.side != sideDef {
		t.Fatalf("a wide window brings the list back at its width: side=%d", m.side)
	}
	frameOK(t, *m)
}

func TestDragDividerFoldsTheList(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	a := newActionModel(t) // 120 columns: the full list
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	drag := func(x int, act tea.MouseAction) {
		a.send(tea.MouseMsg{X: x, Y: 10, Action: act, Button: tea.MouseButtonLeft})
	}
	drag(a.m().side, tea.MouseActionPress)
	drag(30, tea.MouseActionMotion)
	if m := a.m(); m.rail || m.side != 30 {
		t.Fatalf("a short drag only narrows the list: side=%d", m.side)
	}
	drag(10, tea.MouseActionMotion)
	if m := a.m(); !m.rail || m.side != railW {
		t.Fatalf("dragging past the narrowest list folds it, even in a wide window: side=%d", m.side)
	}
	frameOK(t, *a.m())
	drag(10, tea.MouseActionRelease)
	if loadSide() != railW {
		t.Fatal("the fold is remembered")
	}
	if m := newModel(context.Background(), nil, make(chan tea.Msg), config{}, loadSide()); !m.folded || m.sideWant != sideDef {
		t.Fatal("the next start comes back folded, and unfolds to the default width")
	}

	drag(railW, tea.MouseActionPress) // the rail's own edge drags too
	drag(44, tea.MouseActionMotion)
	drag(44, tea.MouseActionRelease)
	if m := a.m(); m.rail || m.side != 44 || loadSide() != 44 {
		t.Fatalf("dragging back out unfolds it: side=%d", m.side)
	}
}

func TestNarrowRailDoesNotSaveAFold(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	saveSide(36)
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	a.send(tea.WindowSizeMsg{Width: 70, Height: 30}) // folded by the window, not by the user
	a.send(tea.MouseMsg{X: railW, Y: 10, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	a.send(tea.MouseMsg{X: 12, Y: 10, Action: tea.MouseActionMotion, Button: tea.MouseButtonLeft})
	a.send(tea.MouseMsg{X: 12, Y: 10, Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if m := a.m(); m.folded || loadSide() != 36 {
		t.Fatalf("a drag on a rail the window folded must not save a fold: folded=%v saved=%d", m.folded, loadSide())
	}
	a.send(tea.WindowSizeMsg{Width: 120, Height: 30})
	if m := a.m(); m.rail || m.side != 36 {
		t.Fatal("a wide window shows the full list again")
	}
}
