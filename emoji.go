package main

// Emoji suggestions: ":" at the start of a word followed by two or more letters shows matching emoji in the
// row above the input (like @mentions); Tab/Enter puts the emoji in place of ":word". Keywords are English
// and accent-free Vietnamese written as one word ("camon"), since a space ends the suggestion. Only emoji that terminals draw two cells wide without a variation selector.

import (
	"strings"
	"unicode"
)

type emoji struct{ e, words string }

var emojis = []emoji{
	{"😀", "cuoi vui smile grin happy"}, {"😂", "haha cuoi lol joy"}, {"🤣", "rofl lan haha"},
	{"😊", "hihi ngai blush"}, {"😉", "wink nhay"}, {"😍", "me yeu love"}, {"🥰", "thuong yeu love"},
	{"😘", "hon kiss"}, {"😎", "ngau cool"}, {"🤔", "nghi hmm think"}, {"😅", "sweat ngai"},
	{"🥲", "cuoibuon"}, {"😭", "khoc cry sob"}, {"😢", "buon sad"}, {"😡", "gian tuc angry"},
	{"😱", "so hoang scream"}, {"😳", "ngai flushed"}, {"🤯", "soc mind"}, {"😴", "ngu sleep"},
	{"🤗", "om hug"}, {"🙄", "chan roll"}, {"🤦", "facepalm"}, {"🤷", "shrug biet"},
	{"🙏", "camon thanks xin pray"}, {"👍", "like ok dongy thumbs"}, {"👎", "dislike khong"},
	{"👏", "votay clap"}, {"🙌", "hooray"}, {"💪", "manh co strong"}, {"👌", "ok hand"},
	{"👋", "chao hi bye wave"}, {"🤝", "battay deal handshake"}, {"👀", "nhin xem eyes"},
	{"✅", "xong done check"}, {"❌", "sai no cross"}, {"🔥", "fire chay hot"}, {"💯", "tuyet 100 hundred"},
	{"🎉", "chucmung party tada"}, {"🎂", "sinhnhat birthday cake"}, {"🎁", "qua gift"},
	{"💖", "tim heart love"}, {"💔", "tanvo broken"}, {"⭐", "sao star"}, {"☕", "cafe coffee"},
	{"🍻", "bia nhau beer"}, {"🍜", "pho mi noodle"}, {"🚀", "rocket nhanh"}, {"💡", "ytuong idea"},
	{"📌", "ghim pin"}, {"⏰", "baothuc alarm"}, {"🐶", "cho dog"}, {"🐱", "meo cat"}, {"🌹", "hoa rose flower"},
}

const emojiShow = 6

// emojiToken finds the ":word" right before the cursor (start = rune index of ":").
func emojiToken(val string, pos int) (start int, q string, ok bool) {
	rs := []rune(val)
	pos = min(pos, len(rs))
	for i := pos - 1; i >= 0; i-- {
		switch r := rs[i]; {
		case r == ':':
			if (i == 0 || unicode.IsSpace(rs[i-1])) && pos-i-1 >= 2 {
				return i, string(rs[i+1 : pos]), true
			}
			return 0, "", false
		case !unicode.IsLetter(r) && !unicode.IsDigit(r):
			return 0, "", false
		}
	}
	return 0, "", false
}

// findEmoji lists the emoji with a keyword starting with q (accents ignored): those whose main (first)
// keyword matches come first, so ":ha" puts 😂 "haha" before 😀 "happy".
func findEmoji(q string) []emoji {
	q = fold(q)
	var main, other []emoji
	for _, e := range emojis {
		for i, w := range strings.Fields(e.words) {
			if strings.HasPrefix(w, q) {
				if i == 0 {
					main = append(main, e)
				} else {
					other = append(other, e)
				}
				break
			}
		}
	}
	return append(main, other...)
}

// emojiCheck runs after the input changed or the cursor moved: shows, updates or hides the suggestions.
func (m *model) emojiCheck() {
	had := len(m.ec) > 0
	m.ec = nil
	if start, q, ok := emojiToken(m.in.Value(), m.in.Position()); ok && m.in.Focused() {
		m.ec, m.estart = findEmoji(q), start
	}
	m.esel = min(m.esel, max(len(m.ec)-1, 0))
	if had != (len(m.ec) > 0) {
		m.layout()
	}
}

func (m *model) emojiKey(s string) bool {
	switch s {
	case "left", "up":
		m.esel = (m.esel + len(m.ec) - 1) % len(m.ec)
	case "right", "down":
		m.esel = (m.esel + 1) % len(m.ec)
	case "tab", "enter":
		rs := []rune(m.in.Value())
		pos := min(m.in.Position(), len(rs))
		out := string(rs[:m.estart]) + m.ec[m.esel].e
		m.in.SetValue(out + string(rs[pos:]))
		m.in.SetCursor(len([]rune(out)))
		m.ec, m.esel = nil, 0
		m.layout()
	case "esc":
		m.ec = nil
		m.layout()
	default:
		return false
	}
	return true
}

func emojiBar(es []emoji, sel int) string {
	from := max(0, min(sel-2, len(es)-emojiShow))
	var b strings.Builder
	for i := from; i < min(from+emojiShow, len(es)); i++ {
		w := strings.Fields(es[i].words)[0]
		item := es[i].e + " " + w
		if i == sel {
			item = "▸" + item + "◂"
		}
		if i > from {
			b.WriteString("  ")
		}
		b.WriteString(item)
	}
	return b.String() + "   (←→ chọn · Tab/Enter chèn · Esc đóng)"
}
