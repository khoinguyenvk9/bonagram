package main

// Sending files: the [Tệp] button / Ctrl+O (a system file dialog), "/file a b c", one caption for the lot,
// several files as albums. The paths are first staged (m.attach, shown in the bar above the input); the next
// Enter sends them with whatever is typed as the caption.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/message/styling"
	"github.com/gotd/td/tg"
)

const (
	albumMax   = 10   // files in one Telegram album
	captionMax = 1024 // characters of a caption (2048 with Premium; the lower limit is used)
	btnW       = 7    // columns of the [Tệp] button inside the input box, 0 when the box is too narrow
)

var photoExt = map[string]bool{".jpg": true, ".jpeg": true, ".png": true}

// checkFile accepts only a non-empty regular file.
func checkFile(path string) (os.FileInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("không phải tệp thường")
	}
	if st.Size() == 0 {
		return nil, errors.New("tệp rỗng")
	}
	return st, nil
}

// tokens splits what the terminal pastes for dropped files (each quoted, or with backslash-escaped spaces)
// into the file names. Backslashes are not escapes on Windows, where they separate folders.
func tokens(s string) []string {
	var out []string
	var b strings.Builder
	var q byte
	has := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case q != 0 && c == q:
			q = 0
		case q == 0 && (c == '\'' || c == '"'):
			q, has = c, true
		case c == '\\' && q != '\'' && i+1 < len(s) && runtime.GOOS != "windows":
			i++
			b.WriteByte(s[i])
			has = true
		case q == 0 && (c == ' ' || c == '\t' || c == '\n' || c == '\r'):
			if has {
				out = append(out, b.String())
				b.Reset()
				has = false
			}
		default:
			b.WriteByte(c)
			has = true
		}
	}
	if has {
		out = append(out, b.String())
	}
	return out
}

// parsePaths is the argument of "/file": one path (also when it has spaces that were not escaped) or several.
func parsePaths(s string) []string {
	if one := shellPath(s); one != "" {
		if _, err := os.Stat(one); err == nil {
			return []string{one}
		}
	}
	var out []string
	for _, t := range tokens(s) {
		out = append(out, shellPath(t))
	}
	return out
}

type upload struct {
	path  string
	photo bool // a small jpg/png goes as a photo, anything else as a file
}

// groups cuts the files into the messages that carry them: runs of the same kind, at most albumMax each,
// because Telegram does not mix photos and files in one album.
func groups(us []upload) [][]upload {
	var out [][]upload
	for _, u := range us {
		if n := len(out); n > 0 && out[n-1][0].photo == u.photo && len(out[n-1]) < albumMax {
			out[n-1] = append(out[n-1], u)
		} else {
			out = append(out, []upload{u})
		}
	}
	return out
}

// sendFiles uploads paths in order. The caption goes on the first file. replyTo 0 = no reply; in a forum topic
// the caller passes the topic root as replyTo, which is how a message is posted into a topic.
func sendFiles(ctx context.Context, api *tg.Client, p tg.InputPeerClass, paths []string, caption string, replyTo int) error {
	us := make([]upload, len(paths))
	for i, path := range paths {
		st, err := checkFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		us[i] = upload{path, photoExt[strings.ToLower(filepath.Ext(path))] && st.Size() <= 10<<20}
	}
	b := message.NewSender(api).To(p).CloneBuilder()
	if replyTo != 0 {
		b = b.Reply(replyTo)
	}
	for _, g := range groups(us) {
		var cap []styling.StyledTextOption
		if caption != "" {
			cap = []styling.StyledTextOption{styling.Plain(caption)}
			caption = "" // only the first message gets it
		}
		if err := sendGroup(ctx, b, g, cap); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(g[0].path), err)
		}
	}
	return nil
}

func sendGroup(ctx context.Context, b *message.Builder, g []upload, cap []styling.StyledTextOption) error {
	if len(g) == 1 {
		up := b.Upload(message.FromPath(g[0].path))
		var err error
		if g[0].photo {
			_, err = up.Photo(ctx, cap...)
		} else {
			_, err = up.File(ctx, cap...)
		}
		return err
	}
	var media []message.MultiMediaOption
	for i, u := range g {
		f, err := b.Upload(message.FromPath(u.path)).AsInputFile(ctx)
		if err != nil {
			return err
		}
		var c []styling.StyledTextOption
		if i == 0 {
			c = cap
		}
		if u.photo {
			media = append(media, message.UploadedPhoto(f, c...))
		} else {
			media = append(media, message.File(f, c...))
		}
	}
	_, err := b.Album(ctx, media[0], media[1:]...)
	return err
}

// --- the file dialog ----------------------------------------------------------------

// pickFiles shows the system's open dialog (several files can be chosen). nil, nil = cancelled.
func pickFiles(ctx context.Context) ([]string, error) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "osascript",
			"-e", `set out to ""`,
			"-e", `repeat with f in (choose file with multiple selections allowed)`,
			"-e", `set out to out & POSIX path of f & linefeed`,
			"-e", `end repeat`,
			"-e", `return out`)
	case "windows":
		cmd = exec.CommandContext(ctx, "powershell", "-NoProfile", "-STA", "-Command",
			`Add-Type -AssemblyName System.Windows.Forms; $d = New-Object System.Windows.Forms.OpenFileDialog; $d.Multiselect = $true; if ($d.ShowDialog() -eq 'OK') { $d.FileNames }`)
	default:
		if _, err := exec.LookPath("zenity"); err == nil {
			cmd = exec.CommandContext(ctx, "zenity", "--file-selection", "--multiple", "--separator=\n")
		} else if _, err := exec.LookPath("kdialog"); err == nil {
			cmd = exec.CommandContext(ctx, "kdialog", "--getopenfilename", ".", "--multiple", "--separate-output")
		} else {
			return nil, errors.New("cần cài zenity hoặc kdialog, hoặc dùng /file")
		}
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && (ee.ExitCode() == 1) { // Cancel: osascript, zenity and kdialog all exit 1
			return nil, nil
		}
		return nil, fmt.Errorf("%w %s", err, strings.TrimSpace(errb.String()))
	}
	var paths []string
	for _, l := range strings.Split(out.String(), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			paths = append(paths, l)
		}
	}
	return paths, nil
}

type pickedMsg struct {
	paths []string
	err   error
}

func (m *model) pickCmd() tea.Cmd {
	if m.curChat() == nil || m.api == nil || m.picking {
		return nil
	}
	m.picking = true
	ctx := m.ctx
	return tea.Batch(m.toastCmd("Chọn tệp trong hộp thoại vừa mở…"), func() tea.Msg {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
		defer cancel()
		p, err := pickFiles(ctx)
		return pickedMsg{p, err}
	})
}

// --- staging and sending ---------------------------------------------------------------

// stage adds files to the ones waiting for a caption. Nothing is added when one of them is not usable.
func (m *model) stage(paths []string) {
	for _, p := range paths {
		if _, err := checkFile(p); err != nil {
			m.err = "gửi tệp lỗi: " + filepath.Base(p) + ": " + errText(err)
			return
		}
	}
	m.err = ""
next:
	for _, p := range paths {
		for _, q := range m.attach {
			if p == q {
				continue next
			}
		}
		m.attach = append(m.attach, p)
	}
	m.in.SetValue("")
	m.layout()
	m.render()
}

func attachBar(paths []string) string {
	names := make([]string, len(paths))
	for i, p := range paths {
		names[i] = filepath.Base(p)
	}
	n := ""
	if len(paths) > 1 {
		n = fmt.Sprintf(" (%d)", len(paths))
	}
	return "+ Đính kèm" + n + ": " + strings.Join(names, ", ") + "   (gõ chú thích · Enter gửi · Esc huỷ)"
}

// sendAttached sends the staged files with caption (may be empty), as a reply when one is chosen.
func (m *model) sendAttached(c chat, caption string) tea.Cmd {
	if utf8.RuneCountInString(caption) > captionMax {
		m.err = fmt.Sprintf("chú thích dài quá %d ký tự", captionMax)
		return nil
	}
	m.err = ""
	rt := 0
	switch {
	case m.reply != nil:
		rt = m.reply.id
	case c.forum && c.topic > 1:
		rt = c.topic // posting into a topic = replying to its root
	}
	paths, ctx, api := m.attach, m.ctx, m.api
	m.attach, m.reply = nil, nil
	m.in.SetValue("")
	m.layout()
	m.render()
	label := filepath.Base(paths[0])
	if len(paths) > 1 {
		label = fmt.Sprintf("%d tệp", len(paths))
	}
	work := func() tea.Msg {
		return actMsg{kind: "file", text: label, key: c.key, err: sendFiles(ctx, api, c.peer, paths, caption, rt)}
	}
	return tea.Batch(m.toastCmd("Đang gửi "+label+"…"), work)
}
