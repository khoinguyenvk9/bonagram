package main

// Text formatting in the input box. Telegram's own apps use a small markdown: the same marks are read when
// sending or editing, and written back when a message is loaded for editing, so formatting survives an edit.
//
//	**bold**  __italic__  ~~strike~~  ||spoiler||  `code`  ```lang\ncode```  [words](https://address)
//
// A mark that has no partner, or is followed by a space, stays as typed. A backslash before a mark types it as is.
// Single * and _ are not marks, so snake_case and 2*3 are safe.

import (
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/gotd/td/tg"
)

type mdParser struct {
	src  []rune
	out  strings.Builder
	n    int // UTF-16 units written
	ents []tg.MessageEntityClass
}

var mdMarks = []string{"**", "__", "~~", "||"}

func (p *mdParser) emit(r rune) {
	p.out.WriteRune(r)
	p.n += len(utf16.Encode([]rune{r}))
}

func (p *mdParser) emitStr(s string) {
	for _, r := range s {
		p.emit(r)
	}
}

func (p *mdParser) at(i int, s string) bool {
	rs := []rune(s)
	if i < 0 || i+len(rs) > len(p.src) {
		return false
	}
	for k, r := range rs {
		if p.src[i+k] != r {
			return false
		}
	}
	return true
}

func (p *mdParser) find(from int, s string) int {
	for i := from; i < len(p.src); i++ {
		if p.at(i, s) {
			return i
		}
	}
	return -1
}

func isWord(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' }

func mark(m string, off, length int) tg.MessageEntityClass {
	switch m {
	case "**":
		return &tg.MessageEntityBold{Offset: off, Length: length}
	case "__":
		return &tg.MessageEntityItalic{Offset: off, Length: length}
	case "~~":
		return &tg.MessageEntityStrike{Offset: off, Length: length}
	}
	return &tg.MessageEntitySpoiler{Offset: off, Length: length}
}

// parse reads from i until the closing mark `end` ("" = the end of the text) and returns the index after it,
// or -1 when the closing mark is missing.
func (p *mdParser) parse(i int, end string) int {
	for i < len(p.src) {
		c := p.src[i]
		switch {
		case end != "" && p.at(i, end) && i > 0 && !unicode.IsSpace(p.src[i-1]):
			return i + len(end)
		case c == '\\' && i+1 < len(p.src) && strings.ContainsRune("*_~|`[\\", p.src[i+1]):
			p.emit(p.src[i+1])
			i += 2
		case p.at(i, "```"):
			if j := p.find(i+3, "```"); j > i+3 {
				body := string(p.src[i+3 : j])
				lang := ""
				if nl := strings.IndexByte(body, '\n'); nl > 0 && !strings.ContainsAny(body[:nl], " \t") {
					lang, body = body[:nl], body[nl+1:]
				} else if nl == 0 {
					body = body[1:]
				}
				body = strings.TrimRight(body, "\n")
				if body != "" {
					off := p.n
					p.emitStr(body)
					p.ents = append(p.ents, &tg.MessageEntityPre{Offset: off, Length: p.n - off, Language: lang})
					i = j + 3
					continue
				}
			}
			p.emitStr("```")
			i += 3
		case c == '`':
			if j := p.find(i+1, "`"); j > i+1 && !strings.ContainsRune(string(p.src[i+1:j]), '\n') {
				off := p.n
				p.emitStr(string(p.src[i+1 : j]))
				p.ents = append(p.ents, &tg.MessageEntityCode{Offset: off, Length: p.n - off})
				i = j + 1
				continue
			}
			p.emit(c)
			i++
		case c == '[':
			if j := p.find(i+1, "]("); j > i+1 {
				if k := p.find(j+2, ")"); k > j+2 {
					if url, ok := webURL(string(p.src[j+2 : k])); ok && !strings.ContainsAny(string(p.src[j+2:k]), " \n") {
						off := p.n
						p.emitStr(string(p.src[i+1 : j]))
						p.ents = append(p.ents, &tg.MessageEntityTextURL{Offset: off, Length: p.n - off, URL: url})
						i = k + 1
						continue
					}
				}
			}
			p.emit(c)
			i++
		default:
			if m := p.opener(i); m != "" {
				outLen, n, ne := p.out.Len(), p.n, len(p.ents)
				saved := p.out.String()
				if j := p.parse(i+2, m); j >= 0 && p.n > n {
					p.ents = append(p.ents[:ne:ne], append([]tg.MessageEntityClass{mark(m, n, p.n-n)}, p.ents[ne:]...)...)
					i = j
					continue
				}
				// no partner: the mark is plain text; drop what the attempt wrote and go on
				p.out.Reset()
				p.out.WriteString(saved[:outLen])
				p.n, p.ents = n, p.ents[:ne]
				p.emitStr(m)
				i += 2
				continue
			}
			p.emit(c)
			i++
		}
	}
	if end != "" {
		return -1
	}
	return i
}

// opener is the mark that opens at i, "" when none: it must be followed by something that is not a space, and
// "__" must not stick to the end of a word (snake__case).
func (p *mdParser) opener(i int) string {
	for _, m := range mdMarks {
		if p.at(i, m) && i+2 < len(p.src) && !unicode.IsSpace(p.src[i+2]) {
			if m == "__" && i > 0 && isWord(p.src[i-1]) {
				continue
			}
			return m
		}
	}
	return ""
}

// parseMarkdown turns the typed text into the text to send and its entities.
func parseMarkdown(s string) (string, []tg.MessageEntityClass) {
	p := &mdParser{src: []rune(s)}
	p.parse(0, "")
	return p.out.String(), p.ents
}

// toMarkdown is the reverse of parseMarkdown for the entities it understands: the text of a message as it
// should be typed to produce the same message again. Marks that appear in the text itself are escaped.
func toMarkdown(m *tg.Message) string {
	u := utf16.Encode([]rune(m.Message))
	open, closing := map[int][]string{}, map[int][]string{}
	for _, e := range m.Entities {
		a, z := e.GetOffset(), e.GetOffset()+e.GetLength()
		if a < 0 || z > len(u) || a >= z {
			continue
		}
		var o, c string
		switch v := e.(type) {
		case *tg.MessageEntityBold:
			o, c = "**", "**"
		case *tg.MessageEntityItalic:
			o, c = "__", "__"
		case *tg.MessageEntityStrike:
			o, c = "~~", "~~"
		case *tg.MessageEntitySpoiler:
			o, c = "||", "||"
		case *tg.MessageEntityCode:
			o, c = "`", "`"
		case *tg.MessageEntityPre:
			o, c = "```"+v.Language+"\n", "\n```"
		case *tg.MessageEntityTextURL:
			o, c = "[", "]("+v.URL+")"
		default:
			continue
		}
		open[a] = append(open[a], o)
		closing[z] = append([]string{c}, closing[z]...)
	}
	rs := utf16.Decode(u)
	at := make([]int, len(rs)+1) // UTF-16 offset of every rune
	for k, r := range rs {
		at[k+1] = at[k] + len(utf16.Encode([]rune{r}))
	}
	var b strings.Builder
	code := 0 // inside `code` or a block: nothing is escaped there
	const marks = "*_~|`[\\"
	for k := 0; k <= len(rs); k++ {
		for _, c := range closing[at[k]] {
			b.WriteString(c)
			if c == "`" || c == "\n```" {
				code--
			}
		}
		for _, o := range open[at[k]] {
			b.WriteString(o)
			if o == "`" || strings.HasPrefix(o, "```") {
				code++
			}
		}
		if k == len(rs) {
			break
		}
		r := rs[k]
		if code == 0 {
			var prev, next rune
			if k > 0 {
				prev = rs[k-1]
			}
			if k+1 < len(rs) {
				next = rs[k+1]
			}
			switch {
			case strings.ContainsRune("*_~|", r) && (next == r || prev == r),
				r == '`',
				r == '[' && strings.Contains(string(rs[k:]), "]("),
				r == '\\' && strings.ContainsRune(marks, next):
				b.WriteByte('\\')
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}
