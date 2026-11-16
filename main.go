package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"golang.org/x/term"
)

var stdin = bufio.NewReader(os.Stdin)

func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "bonagram")
}

func readEnv(path string) map[string]string {
	kv := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return kv
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if ok && !strings.HasPrefix(k, "#") {
			k = strings.TrimSpace(strings.TrimPrefix(k, "export ")) // shell-style files write `export KEY=...`
			kv[k] = strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return kv
}

func prompt(label string) (string, error) {
	fmt.Print(label)
	s, err := stdin.ReadString('\n')
	return strings.TrimSpace(s), err
}

// loadCreds reads TG_API_ID / TG_API_HASH from the env file, then the process env; otherwise it asks
// for them and saves them.
func loadCreds() (int, string, error) {
	envFile := filepath.Join(configDir(), "env")
	kv := readEnv(envFile)
	if id, err := strconv.Atoi(kv["TG_API_ID"]); err == nil && kv["TG_API_HASH"] != "" {
		return id, kv["TG_API_HASH"], nil
	}
	if id, err := strconv.Atoi(os.Getenv("TG_API_ID")); err == nil && os.Getenv("TG_API_HASH") != "" {
		return id, os.Getenv("TG_API_HASH"), nil
	}
	fmt.Println("Lấy api_id và api_hash ở https://my.telegram.org (API development tools).")
	idStr, err := prompt("api_id: ")
	if err != nil {
		return 0, "", err
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, "", errors.New("api_id phải là một số")
	}
	hash, err := prompt("api_hash: ")
	if err != nil || hash == "" {
		return 0, "", errors.New("thiếu api_hash")
	}
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return 0, "", err
	}
	return id, hash, saveCreds(envFile, id, hash)
}

// saveCreds writes the two credentials and keeps every other line of an existing env file.
func saveCreds(path string, id int, hash string) error {
	var keep []string
	if old, err := os.ReadFile(path); err == nil {
		for _, l := range strings.Split(string(old), "\n") {
			k, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimSpace(l), "export "), "=")
			if l != "" && strings.TrimSpace(k) != "TG_API_ID" && strings.TrimSpace(k) != "TG_API_HASH" {
				keep = append(keep, l)
			}
		}
	}
	keep = append(keep, fmt.Sprintf("TG_API_ID=%d", id), "TG_API_HASH="+hash)
	return os.WriteFile(path, []byte(strings.Join(keep, "\n")+"\n"), 0o600)
}

// normalizePhone turns a local Vietnamese "0372..." number into "+84372...".
func normalizePhone(raw string) string {
	s := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "").Replace(raw)
	switch {
	case strings.HasPrefix(s, "+"):
		return s
	case strings.HasPrefix(s, "0"):
		return "+84" + s[1:]
	}
	return "+" + s
}

type termAuth struct{}

func (termAuth) Phone(context.Context) (string, error) {
	s, err := prompt("Số điện thoại: ")
	return normalizePhone(s), err
}

func (termAuth) Code(context.Context, *tg.AuthSentCode) (string, error) {
	return prompt("Mã Telegram: ")
}

func (termAuth) Password(context.Context) (string, error) {
	fmt.Print("Mật khẩu 2 bước: ")
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	return string(b), err
}

func (termAuth) AcceptTermsOfService(context.Context, tg.HelpTermsOfService) error {
	return errors.New("hãy đăng ký tài khoản bằng app Telegram chính thức trước")
}

func (termAuth) SignUp(context.Context) (auth.UserInfo, error) {
	return auth.UserInfo{}, errors.New("số này chưa có tài khoản Telegram")
}

func peerOf(mc tg.NotEmptyMessage) tg.PeerClass {
	switch m := mc.(type) {
	case *tg.Message:
		return m.PeerID
	case *tg.MessageService:
		return m.PeerID
	}
	return nil
}

func toNewMsg(e tg.Entities, msg tg.MessageClass) tea.Msg {
	mc, ok := msg.AsNotEmpty()
	if !ok {
		return nil
	}
	pid := peerOf(mc)
	key := peerKey(pid)
	if key == "" {
		return nil
	}
	ent := peer.EntitiesFromUpdate(e)
	l, ok := toLine(mc, ent)
	if !ok {
		return nil
	}
	in, err := ent.ExtractPeer(pid)
	if err != nil {
		in = nil
	}
	user, forum := peerExtra(ent, pid)
	return newMsg{key: key, title: peerTitle(ent, pid), username: user, forum: forum, peer: in, line: l}
}

// toEdit turns "message was edited" into an editMsg: bots stream an answer by editing one message again and again.
func toEdit(e tg.Entities, msg tg.MessageClass) tea.Msg {
	mc, ok := msg.AsNotEmpty()
	if !ok {
		return nil
	}
	key := peerKey(peerOf(mc))
	l, ok := toLine(mc, peer.EntitiesFromUpdate(e))
	if key == "" || !ok || l.msg == nil {
		return nil
	}
	return editMsg{key: key, line: l}
}

// loadConfig reads the optional BONAGRAM_* settings (env file first, then the environment);
// bad or out-of-range values fall back to the default or the nearest bound.
func loadConfig() config {
	kv := readEnv(filepath.Join(configDir(), "env"))
	get := func(k string) string {
		if v, ok := kv[k]; ok {
			return v
		}
		return os.Getenv(k)
	}
	num := func(k string, def, lo, hi int) int {
		n, err := strconv.Atoi(get(k))
		if err != nil {
			return def
		}
		return max(lo, min(hi, n))
	}
	return config{
		maxMsgs:     num("BONAGRAM_MAX_MESSAGES", 500, 40, 5000),
		previews:    get("BONAGRAM_PREVIEWS") == "1",
		previewRows: num("BONAGRAM_PREVIEW_ROWS", 8, 0, 30),
		previewCnt:  num("BONAGRAM_PREVIEW_COUNT", 8, 0, 50),
		images:      get("BONAGRAM_IMAGES"),
		fps:         num("BONAGRAM_FPS", 60, 10, 120),
	}
}

func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return err != nil
}

func loadSide() int {
	b, err := os.ReadFile(filepath.Join(configDir(), "width"))
	n, err2 := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || err2 != nil {
		return sideDef
	}
	return max(sideMin, min(sideMax, n))
}

func saveSide(n int) {
	_ = os.WriteFile(filepath.Join(configDir(), "width"), []byte(strconv.Itoa(n)), 0o600) // a nicety: ignore failure
}

func main() {
	if done, code := cli(os.Args[1:], os.Stdout, os.Stderr); done {
		os.Exit(code)
	}
	// closing the terminal sends SIGHUP: shut down like for Ctrl+C instead of lingering with the session open
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "bonagram:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	id, hash, err := loadCreds()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(configDir(), 0o700); err != nil {
		return err
	}

	events := make(chan tea.Msg, 256)
	push := func(msg tea.Msg) {
		if msg == nil {
			return
		}
		select {
		case events <- msg:
		case <-ctx.Done():
		}
	}
	d := tg.NewUpdateDispatcher()
	d.OnNewMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		push(toNewMsg(e, u.Message))
		return nil
	})
	d.OnMessageReactions(func(_ context.Context, _ tg.Entities, u *tg.UpdateMessageReactions) error {
		if key := peerKey(u.Peer); key != "" {
			push(reactMsg{key: key, id: u.MsgID, rs: toReactions(u.Reactions)})
		}
		return nil
	})
	d.OnEditMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateEditMessage) error {
		push(toEdit(e, u.Message))
		return nil
	})
	d.OnEditChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateEditChannelMessage) error {
		push(toEdit(e, u.Message))
		return nil
	})
	d.OnNewChannelMessage(func(_ context.Context, e tg.Entities, u *tg.UpdateNewChannelMessage) error {
		push(toNewMsg(e, u.Message))
		return nil
	})

	// the manager fetches what was missed after a gap or a reconnect and hands updates to the dispatcher in order
	gaps := updates.New(updates.Config{Handler: d})
	client := telegram.NewClient(id, hash, telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{Path: filepath.Join(configDir(), "session.json")},
		UpdateHandler:  gaps,
	})
	return client.Run(ctx, func(ctx context.Context) error {
		if err := client.Auth().IfNecessary(ctx, auth.NewFlow(termAuth{}, auth.SendCodeOptions{})); err != nil {
			return err
		}
		self, err := client.Self(ctx)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithCancel(ctx)
		defer cancel()
		cfg := loadConfig()
		switch cfg.images { // BONAGRAM_IMAGES=kitty|blocks overrides the terminal guess
		case "kitty":
			kitty = true
		case "blocks":
			kitty = false
		}
		mdl := newModel(ctx, client.API(), events, cfg, loadSide())
		if marker := filepath.Join(configDir(), "help-seen"); fileMissing(marker) {
			mdl.help = true // the very first launch opens the guide; F1 brings it back
			_ = os.WriteFile(marker, nil, 0o600)
		}
		mdl.selfKey = "u" + strconv.FormatInt(self.ID, 10)
		mdl.track = func(ctx context.Context, ts []chanTrack) {
			for _, t := range ts {
				_ = gaps.TrackChannel(ctx, t.id, t.hash, t.pts) // errors only mean "not tracked": updates still arrive on next message
			}
		}
		// The renderer wakes up once per frame even when idle: idle CPU was 0.67% at 60 fps, 0.41% at 30.
		// 60 is the default because lower rates felt slower to type and scroll with; BONAGRAM_FPS changes it.
		// Once quitting starts (Ctrl+Q, or a closed terminal / signal), nothing may keep the process alive for
		// more than 3 s: a leftover process would keep the account's session open.
		forceExit := func() { time.AfterFunc(3*time.Second, func() { os.Exit(0) }) }
		go func() { <-ctx.Done(); forceExit() }()
		leak := &mouseLeak{}
		filter := func(m tea.Model, msg tea.Msg) tea.Msg {
			if _, quit := msg.(tea.QuitMsg); quit {
				forceExit()
			}
			return leak.filter(m, msg)
		}
		p := tea.NewProgram(mdl, tea.WithFPS(cfg.fps), tea.WithFilter(filter),
			tea.WithOutput(lockedOut{os.Stdout}), tea.WithAltScreen(), tea.WithMouseCellMotion(),
			tea.WithReportFocus(), tea.WithContext(ctx))
		done := make(chan error, 1)
		go func() {
			_, err := p.Run()
			dropAllImages()
			cancel() // the UI ended: stop the updates manager too
			done <- err
		}()
		gerr := gaps.Run(ctx, client.API(), self.ID, updates.AuthOptions{})
		uerr := <-done // wait until the terminal is restored
		switch {
		case uerr != nil && !errors.Is(uerr, tea.ErrProgramKilled) && !errors.Is(uerr, context.Canceled):
			return uerr
		case gerr != nil && !errors.Is(gerr, context.Canceled):
			return gerr
		}
		return nil
	})
}
