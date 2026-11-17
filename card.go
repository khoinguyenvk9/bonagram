package main

// A file in a message is drawn as a small card: a coloured badge with the type, the name, and the size.
//
//	╭────────────────────────────────╮
//	│  PDF   Báo cáo quý 3.pdf       │
//	│        2.3 MB                  │
//	╰────────────────────────────────╯

import (
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/gotd/td/tg"
)

const (
	badgeW  = 7  // cells of the type badge
	cardMax = 56 // widest card
	cardMin = 30
)

// badgeColours by kind of file; the badge is dark text on that background.
var badgeColours = map[string]string{
	"doc": "#6a9fe0", "sheet": "#62b883", "slide": "#e09a66", "zip": "#d9b34f",
	"image": "#9b86e0", "audio": "#cd78bd", "video": "#e07a7a", "code": "#5fb8b0", "other": "#8d867a",
}

var extKinds = map[string]string{
	"pdf": "doc", "doc": "doc", "docx": "doc", "txt": "doc", "md": "doc", "rtf": "doc", "odt": "doc", "epub": "doc",
	"xls": "sheet", "xlsx": "sheet", "csv": "sheet", "ods": "sheet",
	"ppt": "slide", "pptx": "slide", "key": "slide",
	"zip": "zip", "rar": "zip", "7z": "zip", "tar": "zip", "gz": "zip", "tgz": "zip", "bz2": "zip", "xz": "zip",
	"jpg": "image", "jpeg": "image", "png": "image", "gif": "image", "webp": "image", "heic": "image", "svg": "image",
	"mp3": "audio", "m4a": "audio", "ogg": "audio", "oga": "audio", "opus": "audio", "wav": "audio", "flac": "audio",
	"mp4": "video", "mov": "video", "mkv": "video", "webm": "video", "avi": "video", "m4v": "video",
	"go": "code", "py": "code", "js": "code", "ts": "code", "json": "code", "html": "code", "css": "code",
	"java": "code", "c": "code", "cpp": "code", "rs": "code", "sh": "code", "sql": "code", "yml": "code", "yaml": "code", "xml": "code",
}

// badgeOf is the text and the colour of the badge: the extension of the file, or the kind when it has none.
func badgeOf(in docInfo, d *tg.Document) (string, string) {
	ext := strings.ToUpper(strings.TrimPrefix(filepath.Ext(in.name), "."))
	text, kind := ext, extKinds[strings.ToLower(ext)]
	switch in.kind {
	case "video", "gif":
		if kind == "" || kind == "other" {
			kind = "video"
		}
		if ext == "" {
			text = strings.ToUpper(in.kind)
		}
	case "audio", "voice":
		kind = "audio"
		if ext == "" {
			text = strings.ToUpper(in.kind)
		}
	}
	if text == "" || len(text) > 5 {
		text = "FILE"
	}
	if kind == "" {
		kind = "other"
	}
	return text, badgeColours[kind]
}

// mediaCard is the card of what a message carries: a file or a location; nil for anything else.
func mediaCard(m *tg.Message, maxW int, hot string) []string {
	if c := geoCard(m, maxW, hot); c != nil {
		return c
	}
	return fileCard(m, maxW)
}

// fileCard draws the document of a message as a card; nil when the message has none (or it is a sticker).
func fileCard(m *tg.Message, maxW int) []string {
	d, ok := docOf(m)
	if !ok {
		return nil
	}
	in := infoOf(d)
	if in.kind == "sticker" {
		return nil
	}
	title := clean(in.name)
	switch {
	case in.voice:
		title = "Tin thoại"
	case title == "" && in.artist != "":
		title = clean(in.artist)
	case title == "":
		title = map[string]string{"video": "Video", "gif": "GIF", "audio": "Âm thanh"}[in.kind]
		if title == "" {
			title = clean(d.MimeType)
		}
	}
	var parts []string
	if in.length != "" {
		parts = append(parts, in.length)
	}
	if d.Size > 0 {
		parts = append(parts, humanSize(d.Size))
	}
	detail := strings.Join(parts, " · ")
	if in.artist != "" && in.name != "" {
		detail = strings.Join(append([]string{clean(in.artist)}, parts...), " · ")
	}

	w := min(maxW, cardMax, max(cardMin, badgeW+ansi.StringWidth(title)+5))
	if w < badgeW+8 { // no room for a card: a line of text
		return []string{docTag(d)}
	}
	text, colour := badgeOf(in, d)
	pad := badgeW - ansi.StringWidth(text)
	badge := sgr(cBg, colour) + "\x1b[1m" + strings.Repeat(" ", pad/2) + text + strings.Repeat(" ", pad-pad/2) + "\x1b[0m"
	field := w - 4 - badgeW - 1 // the room left for the name
	edge := fg(cBorder)
	line := func(inner string) string { return edge.Render("│") + " " + inner + " " + edge.Render("│") }
	blank := strings.Repeat(" ", badgeW+1)
	return []string{
		edge.Render("╭" + strings.Repeat("─", w-2) + "╮"),
		line(badge + " " + fg(cFg).Bold(true).Render(fit(title, field))),
		line(blank + fg(cMuted).Render(fit(detail, field))),
		edge.Render("╰" + strings.Repeat("─", w-2) + "╯"),
	}
}
