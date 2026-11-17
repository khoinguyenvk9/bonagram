package main

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"

	"github.com/gotd/td/tg"
)

// show writes entities as kind[offset+length] so a test can compare them in one line.
func show(es []tg.MessageEntityClass) string {
	var out []string
	for _, e := range es {
		switch v := e.(type) {
		case *tg.MessageEntityBold:
			out = append(out, fmt.Sprintf("b[%d+%d]", v.Offset, v.Length))
		case *tg.MessageEntityItalic:
			out = append(out, fmt.Sprintf("i[%d+%d]", v.Offset, v.Length))
		case *tg.MessageEntityStrike:
			out = append(out, fmt.Sprintf("s[%d+%d]", v.Offset, v.Length))
		case *tg.MessageEntitySpoiler:
			out = append(out, fmt.Sprintf("sp[%d+%d]", v.Offset, v.Length))
		case *tg.MessageEntityCode:
			out = append(out, fmt.Sprintf("c[%d+%d]", v.Offset, v.Length))
		case *tg.MessageEntityPre:
			out = append(out, fmt.Sprintf("pre[%d+%d %s]", v.Offset, v.Length, v.Language))
		case *tg.MessageEntityTextURL:
			out = append(out, fmt.Sprintf("a[%d+%d %s]", v.Offset, v.Length, v.URL))
		}
	}
	return strings.Join(out, " ")
}

func TestParseMarkdown(t *testing.T) {
	for _, c := range []struct{ in, text, ents string }{
		{"chào **bạn** ơi", "chào bạn ơi", "b[5+3]"},
		{"__nghiêng__ và ~~gạch~~", "nghiêng và gạch", "i[0+7] s[11+4]"},
		{"**đậm __cả nghiêng__ nữa**", "đậm cả nghiêng nữa", "b[0+18] i[4+10]"},
		{"😀 **x**", "😀 x", "b[3+1]"}, // an emoji is 2 UTF-16 units
		{"dùng `go test` nhé", "dùng go test nhé", "c[5+7]"},
		{"```go\nx := 1\n```", "x := 1", "pre[0+6 go]"},
		{"xem [tài liệu](https://a.vn/x) đi", "xem tài liệu đi", "a[4+8 https://a.vn/x]"},
		{"||bí mật||", "bí mật", "sp[0+6]"},
		// stays as typed
		{"2*3*4 và snake_case_name", "2*3*4 và snake_case_name", ""},
		{"**không đóng", "**không đóng", ""},
		{"** cách **", "** cách **", ""},
		{"snake__case__x", "snake__case__x", ""},
		{"[x](javascript:alert(1))", "[x](javascript:alert(1))", ""},
		{`\**không đậm\**`, "**không đậm**", ""},
		{"C:\\temp\\file", "C:\\temp\\file", ""},
		{"``", "``", ""},
	} {
		text, es := parseMarkdown(c.in)
		if text != c.text || show(es) != c.ents {
			t.Errorf("%q\n got  %q %s\n want %q %s", c.in, text, show(es), c.text, c.ents)
		}
	}
}

func TestMarkdownRoundTrip(t *testing.T) {
	for _, in := range []string{
		"chào **bạn** ơi __nghiêng__ ~~gạch~~ ||kín||",
		"**đậm __cả nghiêng__ nữa**",
		"```go\nx := 1\n```",
		"dùng `go test` và [tài liệu](https://a.vn/x)",
		"😀 **x** `y`",
		`gõ \*\*chữ\*\* thì không đậm`,
		"C:\\temp và snake_case",
	} {
		text, es := parseMarkdown(in)
		back := toMarkdown(&tg.Message{Message: text, Entities: es})
		text2, es2 := parseMarkdown(back)
		if text2 != text || show(es2) != show(es) {
			t.Errorf("%q → %q %s → %q → %q %s", in, text, show(es), back, text2, show(es2))
		}
	}
	// a message from another app that merely contains marks must not be formatted by an edit
	m := &tg.Message{Message: "giá **5** nhé"}
	if text, es := parseMarkdown(toMarkdown(m)); text != m.Message || len(es) != 0 {
		t.Errorf("literal marks became formatting: %q %s", text, show(es))
	}
}

func TestSendingFormattedText(t *testing.T) {
	a := newActionModel(t)
	a.send(tea.KeyMsg{Type: tea.KeyEsc}) // leave browsing
	m := a.m()
	m.in.SetValue("chào **bạn** `x`")
	a.set(m)
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	req, ok := a.rec.last(t).(*tg.MessagesSendMessageRequest)
	if !ok || req.Message != "chào bạn x" || show(req.Entities) != "b[5+3] c[9+1]" {
		t.Fatalf("what was sent: %+v", a.rec.last(t))
	}
	ls := a.m().lines()
	last := ls[len(ls)-1]
	if last.text != "chào bạn x" {
		t.Fatalf("the line shows %q", last.text)
	}
	if !strings.Contains(a.m().vp.View(), "\x1b[1m") {
		t.Fatal("the bold word should be drawn bold at once")
	}
}

func TestEditKeepsFormatting(t *testing.T) {
	a := newActionModel(t)
	text := "xin chào bạn"
	msg := &tg.Message{ID: 8, Out: true, Message: text, Entities: []tg.MessageEntityClass{&tg.MessageEntityBold{Offset: 9, Length: 3}}}
	a.send(histMsg{mk: "u1", key: "u1", lines: []line{{id: 8, from: "Bạn", out: true, text: text, at: time.Now(), msg: msg}}})
	m := a.m()
	m.bsel = 0
	a.set(m)
	a.key("e")
	if got := a.m().in.Value(); got != "xin chào **bạn**" {
		t.Fatalf("the edit box should hold the formatting as marks, got %q", got)
	}
	m = a.m()
	m.in.SetValue("xin chào **các** bạn")
	a.set(m)
	a.run(a.send(tea.KeyMsg{Type: tea.KeyEnter}))
	req, ok := a.rec.last(t).(*tg.MessagesEditMessageRequest)
	if !ok || req.Message != "xin chào các bạn" || show(req.Entities) != "b[9+3]" {
		t.Fatalf("what was sent: %+v", a.rec.last(t))
	}
	if l := a.m().lines()[0]; l.text != "xin chào các bạn" || l.msg == nil || show(l.msg.Entities) != "b[9+3]" {
		t.Fatalf("the line after the edit: %q %+v", l.text, l.msg)
	}
}

func TestReceivedFormattingIsDrawn(t *testing.T) {
	msg := &tg.Message{ID: 4, Message: "đậm nghiêng gạch xem docs ở đây", Entities: []tg.MessageEntityClass{
		&tg.MessageEntityBold{Offset: 0, Length: 3},
		&tg.MessageEntityItalic{Offset: 4, Length: 7},
		&tg.MessageEntityStrike{Offset: 12, Length: 4},
		&tg.MessageEntityBold{Offset: 21, Length: 4}, // inside the link: both apply
		&tg.MessageEntityTextURL{Offset: 21, Length: 4, URL: "https://d.vn"},
	}}
	s := inline(msg, 0, 100, "")
	if got := ansi.Strip(s); got != "đậm nghiêng gạch xem docs (https://d.vn) ở đây" {
		t.Fatalf("visible text: %q", got)
	}
	for _, want := range []string{"\x1b[1m", "\x1b[3m", "\x1b[9m", osc8 + "id=b4_0;https://d.vn"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %q", want, s)
		}
	}
	// every wrapped row stands alone: whatever it opens it closes
	for i, r := range carry(strings.Split(ansi.Wrap(s, 12, ""), "\n")) {
		if again := carry([]string{r})[0]; again != r { // nothing is left open at the end of the row
			t.Errorf("row %d leaks: %q", i, r)
		}
	}
}
