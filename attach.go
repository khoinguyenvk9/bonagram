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
	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"
)

const (
	albumMax   = 10   // files in one Telegram album
	captionMax = 1024 // characters of a caption (2048 with Premium; the lower limit is used)
	btnW       = 11   // columns of the attach and send buttons inside the input box, 0 when the box is too narrow
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
	photo bool       // a small jpg/png goes as a photo
	video *videoMeta // an MP4/MOV whose boxes could be read goes as a video; anything else as a file
}

// media is a photo or a video: Telegram puts those together in one album, but never with files.
func (u upload) media() bool { return u.photo || u.video != nil }

// groups cuts the files into the messages that carry them: runs of the same kind, at most albumMax each,
// because Telegram does not mix photos/videos and files in one album.
func groups(us []upload) [][]upload {
	var out [][]upload
	for _, u := range us {
		if n := len(out); n > 0 && out[n-1][0].media() == u.media() && len(out[n-1]) < albumMax {
			out[n-1] = append(out[n-1], u)
		} else {
			out = append(out, []upload{u})
		}
	}
	return out
}

// fileMax is the largest file Telegram takes: 4000 parts of 512 KB (twice that with Premium).
func fileMax(premium bool) int64 {
	if premium {
		return 4000 << 20
	}
	return 2000 << 20
}

// sendFiles uploads paths in order. The caption goes on the first file. replyTo 0 = no reply; in a forum topic
// the caller passes the topic root as replyTo, which is how a message is posted into a topic.
// pr (may be nil) is told every part that reaches the server.
func sendFiles(ctx context.Context, api *tg.Client, p tg.InputPeerClass, paths []string, caption string, replyTo int, pr *xfer) error {
	us := make([]upload, len(paths))
	for i, path := range paths {
		st, err := checkFile(path)
		if err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(path), err)
		}
		ext := strings.ToLower(filepath.Ext(path))
		us[i] = upload{path: path, photo: photoExt[ext] && st.Size() <= 10<<20}
		if videoMIME[ext] != "" {
			if v, ok := probeVideo(path); ok {
				us[i].video = &v
			}
		}
	}
	up := uploader.NewUploader(api)
	if pr != nil {
		up = up.WithProgress(pr)
	}
	b := message.NewSender(api).WithUploader(up).To(p).CloneBuilder()
	thumbs := uploader.NewUploader(api) // covers are small and not counted in the progress
	if replyTo != 0 {
		b = b.Reply(replyTo)
	}
	for _, g := range groups(us) {
		var cap []styling.StyledTextOption
		if caption != "" {
			cap = []styling.StyledTextOption{styling.Plain(caption)}
			caption = "" // only the first message gets it
		}
		if err := sendGroup(ctx, b, thumbs, g, cap); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(g[0].path), err)
		}
	}
	return nil
}

// mediaOpt is what one file of a message becomes; gotd's photo, video and file builders are all of both kinds.
type mediaOpt interface {
	message.MediaOption
	message.MultiMediaOption
}

func sendGroup(ctx context.Context, b *message.Builder, thumbs *uploader.Uploader, g []upload, cap []styling.StyledTextOption) error {
	var media []mediaOpt
	for i, u := range g {
		f, err := b.Upload(message.FromPath(u.path)).AsInputFile(ctx)
		if err != nil {
			return err
		}
		var c []styling.StyledTextOption
		if i == 0 {
			c = cap
		}
		switch {
		case u.photo:
			media = append(media, message.UploadedPhoto(f, c...))
		case u.video != nil:
			media = append(media, videoOpt(ctx, thumbs, u, f, c))
		default:
			media = append(media, message.File(f, c...))
		}
	}
	if len(media) == 1 {
		_, err := b.Media(ctx, media[0])
		return err
	}
	rest := make([]message.MultiMediaOption, 0, len(media)-1)
	for _, o := range media[1:] {
		rest = append(rest, o)
	}
	_, err := b.Album(ctx, media[0], rest...)
	return err
}

// videoOpt sends u as a video that plays in Telegram: its name, type, duration, frame size, streaming flag and,
// when ffmpeg made one, a cover picture.
func videoOpt(ctx context.Context, thumbs *uploader.Uploader, u upload, f tg.InputFileClass, c []styling.StyledTextOption) mediaOpt {
	doc := message.UploadedDocument(f, c...).Filename(filepath.Base(u.path)).MIME(videoMIME[strings.ToLower(filepath.Ext(u.path))])
	if dir, err := cacheDir(); err == nil {
		if tmp, err := os.MkdirTemp(dir, "thumb-"); err == nil {
			if t := videoThumb(ctx, u.path, tmp); t != "" {
				if tf, err := thumbs.FromPath(ctx, t); err == nil {
					doc = doc.Thumb(tf)
				}
			}
			os.RemoveAll(tmp)
		}
	}
	v := doc.Video().Duration(u.video.dur).Resolution(u.video.w, u.video.h)
	if u.video.stream {
		v = v.SupportsStreaming()
	}
	return v
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
	// Sizes are checked before anything is sent: a file over Telegram's limit would otherwise upload for
	// minutes and then fail.
	var total int64
	limit := fileMax(m.cfg.premium)
	for _, path := range m.attach {
		st, err := checkFile(path)
		if err != nil {
			m.err = filepath.Base(path) + ": " + errText(err)
			return nil
		}
		if st.Size() > limit {
			m.err = fmt.Sprintf("%s nặng %s, Telegram chỉ nhận tệp tới %s", filepath.Base(path), humanSize(st.Size()), humanSize(limit))
			return nil
		}
		total += st.Size()
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
	pr := newXfer(label, total, false)
	ctx = m.startXfer(ctx, pr)
	work := func() tea.Msg {
		defer pr.stop()
		err := sendFiles(ctx, api, c.peer, paths, caption, rt, pr)
		pr.done.Store(true)
		return actMsg{kind: "file", text: label, key: c.key, err: err, up: pr}
	}
	return tea.Batch(work, xferTickCmd(pr))
}
