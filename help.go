package main

// The command line (--help, --version) and the in-app guide (F1).

import (
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// version is set at build time: go build -ldflags="-s -w -X main.version=0.1.0"
var version = "dev"

func versionLine() string {
	return fmt.Sprintf("bonagram %s (%s %s/%s)", version, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}

const usage = `bonagram: Telegram trong terminal.

Cách dùng:
  bonagram              mở app (lần đầu sẽ hỏi api_id, api_hash rồi đăng nhập)
  bonagram --version    in phiên bản
  bonagram --help       in hướng dẫn này

Trong app: F1 mở hướng dẫn phím tắt, Ctrl+Q thoát.
Cấu hình và phiên đăng nhập nằm ở ~/.config/bonagram/. Các tuỳ chọn BONAGRAM_* và
cách lấy api_id, api_hash: xem README.md.
`

// cli handles the command-line arguments. done is true when the program should exit with code instead of
// starting the app.
func cli(args []string, out, errOut io.Writer) (done bool, code int) {
	if len(args) == 0 {
		return false, 0
	}
	switch args[0] {
	case "-v", "--version", "version":
		fmt.Fprintln(out, versionLine())
		return true, 0
	case "-h", "--help", "help":
		fmt.Fprint(out, usage)
		return true, 0
	}
	fmt.Fprintf(errOut, "bonagram: không hiểu tuỳ chọn %q\n\n%s", args[0], usage)
	return true, 2
}

type helpSection struct {
	head string
	rows [][2]string // key, what it does
}

var helpDoc = []helpSection{
	{"Chat", [][2]string{
		{"Ctrl+K", "Tìm chat (gõ không dấu cũng ra), Enter mở, Esc huỷ"},
		{"Ctrl+N / Ctrl+P", "Chat kế tiếp / chat trước"},
		{"Ctrl+G", "Nhảy tới chat có tin chưa đọc"},
		{"Ctrl+T / Ctrl+Y", "Topic kế tiếp / trước (nhóm có topic)"},
	}},
	{"Gõ và gửi", [][2]string{
		{"Enter", "Gửi tin"},
		{"Ctrl+U / Ctrl+W", "Xoá chữ trước con trỏ / xoá từng từ"},
		{"End", "Đưa con trỏ về cuối dòng (rồi Ctrl+U để xoá hết)"},
		{"Esc", "Huỷ trả lời hoặc sửa tin"},
		{"Ctrl+O / [Tệp]", "Mở hộp chọn tệp (chọn được nhiều tệp)"},
		{"/file đường-dẫn…", "Đính kèm một hoặc nhiều tệp (kéo thả vào terminal)"},
		{"Chú thích", "Gõ chú thích rồi Enter để gửi; Esc bỏ đính kèm"},
	}},
	{"Chọn tin (Ctrl+B, hoặc click một tin)", [][2]string{
		{"↑ ↓", "Di chuyển; ↑ ở tin đầu tiên tải thêm tin cũ"},
		{"Enter / Ctrl+R", "Trả lời tin; ảnh hoặc tệp thì mở (Esc/Q đóng, O mở ngoài)"},
		{"C", "Chép khối code (bấm lại: khối kế); không có code: cả tin"},
		{"E / D", "Sửa / xoá tin của bạn (xoá: bấm Y để xác nhận)"},
		{"R", "Thả reaction: bấm số 1..7, bấm lại để bỏ"},
		{"F", "Chuyển tiếp: chọn chat đích bên trái rồi Enter"},
		{"O", "Mở link trong tin (nhiều link: bấm tiếp để sang link kế)"},
		{"M", "Nhảy lên @mention gần nhất"},
		{"Esc", "Thoát chế độ chọn tin"},
	}},
	{"Chuột", [][2]string{
		{"Click", "Mở chat, chọn topic, chọn tin (rồi e/d/r/f/o), [copy] của code"},
		{"Cuộn", "Cuộn danh sách chat hoặc tin; lên đầu thì tải tin cũ"},
		{"Kéo thanh dọc", "Đổi độ rộng danh sách chat (được nhớ)"},
		{"Shift + kéo", "Bôi đen chữ để sao chép"},
	}},
	{"Khác", [][2]string{
		{"F1", "Mở / đóng hướng dẫn này"},
		{"Ctrl+Q", "Thoát"},
	}},
}

// helpLines is the guide as styled lines (the same for every window width; long ones are cut when drawn).
func helpLines() []string {
	const keyW = 18
	out := []string{"", "  " + fg(cAccent).Bold(true).Render("bonagram") + fg(cMuted).Render("  "+version+"  ·  hướng dẫn nhanh"), ""}
	for _, s := range helpDoc {
		out = append(out, "  "+fg(cFg).Bold(true).Render(s.head))
		for _, r := range s.rows {
			out = append(out, "    "+fg(cAccent).Render(r[0])+strings.Repeat(" ", max(keyW-ansi.StringWidth(r[0]), 1))+fg(cFg).Render(r[1]))
		}
		out = append(out, "")
	}
	return append(out, "  "+fg(cMuted).Render("Cấu hình, giới hạn và xử lý sự cố: xem README.md"), "")
}

// helpMax is how far the guide can be scrolled in a window of the current height.
func (m *model) helpMax() int { return max(len(helpLines())-max(m.h-1, 1), 0) }

func (m model) helpView() string {
	lines := helpLines()
	rows := max(m.h-1, 0)
	top := min(max(m.helpTop, 0), m.helpMax())
	var b strings.Builder
	for i := 0; i < rows; i++ {
		l := ""
		if top+i < len(lines) {
			l = lines[top+i]
		}
		b.WriteString(paint(l, m.w, cFg, cSurface) + "\n")
	}
	hint := "↑ ↓ cuộn · Esc hoặc F1 đóng"
	b.WriteString(paint("  "+fg(cMuted).Render(hint), m.w, cFg, cBg))
	return b.String()
}
