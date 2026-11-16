package main

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestCLI(t *testing.T) {
	var out, errOut bytes.Buffer
	if done, code := cli(nil, &out, &errOut); done || code != 0 {
		t.Fatal("no arguments must start the app")
	}
	for _, a := range []string{"--version", "-v", "version"} {
		out.Reset()
		if done, code := cli([]string{a}, &out, &errOut); !done || code != 0 || !strings.HasPrefix(out.String(), "bonagram ") || !strings.Contains(out.String(), "go1") {
			t.Fatalf("%s: done=%v code=%d out=%q", a, done, code, out.String())
		}
	}
	for _, a := range []string{"--help", "-h", "help"} {
		out.Reset()
		if done, code := cli([]string{a}, &out, &errOut); !done || code != 0 || !strings.Contains(out.String(), "--version") || !strings.Contains(out.String(), "F1") {
			t.Fatalf("%s: done=%v code=%d", a, done, code)
		}
	}
	out.Reset()
	errOut.Reset()
	if done, code := cli([]string{"--bậy"}, &out, &errOut); !done || code != 2 || out.Len() != 0 || !strings.Contains(errOut.String(), "--bậy") || !strings.Contains(errOut.String(), "--help") {
		t.Fatalf("unknown option: done=%v code=%d err=%q", done, code, errOut.String())
	}
}

func frameOK(t *testing.T, m model) {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.h {
		t.Fatalf("%dx%d: %d lines", m.w, m.h, len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != m.w {
			t.Fatalf("%dx%d: line %d is %d cells", m.w, m.h, i, w)
		}
	}
}

func TestHelpScreen(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browse
	a.send(tea.KeyMsg{Type: tea.KeyF1})
	m := a.m()
	if !m.help {
		t.Fatal("F1 should open the guide")
	}
	frameOK(t, *m)
	if v := m.View(); !strings.Contains(v, "hướng dẫn") || !strings.Contains(v, "Ctrl+K") {
		t.Fatal("the guide should list the keys")
	}
	a.send(tea.KeyMsg{Type: tea.KeyDown})
	a.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // typing must not reach the message box
	if m = a.m(); m.helpTop != 1 || m.in.Value() != "" {
		t.Fatalf("helpTop=%d input=%q", m.helpTop, m.in.Value())
	}
	a.send(tea.KeyMsg{Type: tea.KeyEnd})
	if m = a.m(); m.helpTop != m.helpMax() || m.helpTop == 0 {
		t.Fatalf("End should reach the bottom: %d of %d", m.helpTop, m.helpMax())
	}
	frameOK(t, *m)
	if !strings.Contains(m.View(), "README.md") {
		t.Fatal("the last lines of the guide should be reachable")
	}
	a.send(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if a.m().helpTop >= m.helpTop {
		t.Fatal("the wheel should scroll the guide")
	}
	a.send(tea.KeyMsg{Type: tea.KeyEsc})
	if m = a.m(); m.help || m.cur != "u1" {
		t.Fatal("Esc closes the guide and leaves the chat as it was")
	}
	a.send(tea.KeyMsg{Type: tea.KeyF1})
	a.send(tea.KeyMsg{Type: tea.KeyF1})
	if a.m().help {
		t.Fatal("F1 again closes it")
	}
	a.send(tea.KeyMsg{Type: tea.KeyF1})
	if _, quit := a.send(tea.KeyMsg{Type: tea.KeyCtrlQ})().(tea.QuitMsg); !quit {
		t.Fatal("Ctrl+Q must still quit from the guide")
	}
}

func TestHelpInTinyWindows(t *testing.T) {
	for _, sz := range [][2]int{{1, 1}, {10, 3}, {40, 8}, {80, 24}, {130, 46}} {
		a := newActionModel(t)
		a.send(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		a.send(tea.KeyMsg{Type: tea.KeyEsc})
		a.send(tea.KeyMsg{Type: tea.KeyF1})
		a.send(tea.KeyMsg{Type: tea.KeyEnd})
		a.send(tea.KeyMsg{Type: tea.KeyPgUp})
		frameOK(t, *a.m())
	}
}
