# bonagram

[English](README.md) | **Tiếng Việt**

Telegram chạy ngay trong một tab terminal: nhận tin nhắn có chuông, thông báo và số chưa đọc trên tiêu đề tab, trả lời thẳng tại chỗ mà không phải chuyển sang ứng dụng khác.

Viết bằng Go với [gotd/td](https://github.com/gotd/td) (kết nối Telegram) và [Bubble Tea](https://github.com/charmbracelet/bubbletea) (giao diện terminal). Đây là client **không chính thức**, đăng nhập bằng tài khoản Telegram của chính bạn. Chỉ là một file chạy duy nhất, không cần cài thêm gì.

![Giao diện bonagram](docs/screenshot.png)

*Ảnh minh hoạ: dữ liệu mẫu (tên và tin nhắn giả), dựng từ chính giao diện của app. Màu và font thật phụ thuộc terminal của bạn.*

## Tính năng

- Ô tìm chat và tên chat đang mở nằm cùng một dải trên cùng (dòng phụ cho biết loại chat: `@username`, nhóm, số topic, tắt tiếng). Ô tìm và ô nhắn tin cùng kiểu viền, viền đổi màu khi con trỏ ở trong ô.
- Danh sách chat bên trái (click được), mỗi chat hai dòng: tên kèm số chưa đọc và giờ, tin cuối kèm tên người gửi trong nhóm.
- Tìm chat bằng `Ctrl+K` hoặc click ô tìm chat: không cần gõ dấu ("dat" ra "Đạt"), tìm cả theo username.
- Chuyển nhanh giữa các chat bằng phím, nhảy tới chat có tin chưa đọc.
- Tin mới đến hiện ngay: chuông, thông báo nhỏ ở góc trên, thông báo desktop khi tab không được chọn, `(3) bonagram` trên tiêu đề tab. Chat tắt tiếng không rung chuông. Tin lỡ khi mất mạng được bù lại khi nối lại.
- Trả lời tin bất kỳ. Tin bạn gửi hiện ngay.
- Cuộn lên đầu để tải tin cũ hơn (có giới hạn, xem phần Cấu hình).
- Nhóm có Topics (forum): mỗi topic là một tab, đếm chưa đọc riêng từng topic.
- Ảnh: xem to toàn màn hình, tuỳ chọn thumbnail ngay trong khung chat. Video, file, gif mở bằng ứng dụng của hệ điều hành.
- Khối code có tiêu đề ngôn ngữ và nút **copy**, code trong dòng có nền riêng.
- Phản ứng (reaction) của người khác hiện dưới tin, phản ứng của bạn được tô nổi. Bạn cũng thả được reaction.
- Sửa và xoá tin của mình, chuyển tiếp tin, mở link, gửi ảnh hoặc tệp (nút, `Ctrl+O` hoặc `/file`), tin ghim hiện ở đầu chat, chữ đang soạn dở được giữ riêng cho từng chat. Xem phần "Thao tác trên tin nhắn".
- Tin hệ thống hiện chi tiết bằng chữ: ai đó tham gia hoặc rời nhóm, được thêm hoặc bị xoá, đổi tên nhóm, ghim tin, tạo topic, cuộc gọi (nhỡ hoặc thời lượng), tin tự xoá. Loại sự kiện hiếm gặp hiện `[sự kiện: Tên]`.
- Bot stream câu trả lời bằng cách sửa dần một tin được hiển thị trực tiếp.
- Chat lưu trữ (Archived) bị ẩn, giống Telegram. Chat ghim luôn nằm trên cùng.
- Kéo thanh dọc giữa danh sách chat và khung tin để đổi độ rộng (được nhớ cho lần sau).
- Gõ tiếng Việt (Telex/VNI) hiện ngay trong ô nhập.

## Yêu cầu và checklist trước khi chạy

Đánh dấu từng mục; mục nào chưa đạt thì làm theo cột bên phải.

| ☐ | Cần có | Kiểm tra bằng | Nếu chưa đạt |
|---|---|---|---|
| ☐ | **Go 1.26 trở lên** (chỉ để build) | `go version` | Cài từ <https://go.dev/dl>. Go 1.21 trở lên tự tải đúng phiên bản khi build, nhưng cần mạng |
| ☐ | **Mạng truy cập được Telegram** (và tải thư viện lần build đầu) | Mở được app Telegram chính thức trên cùng mạng | Dùng VPN. App **chưa hỗ trợ proxy** (SOCKS, MTProxy) |
| ☐ | **Tài khoản Telegram** cùng `api_id`, `api_hash` | Xem mục "Lấy `api_id` và `api_hash`" | Tạo ở <https://my.telegram.org> |
| ☐ | **Terminal hỗ trợ màu 24-bit** | `echo $COLORTERM` ra `truecolor` hoặc `24bit` | Dùng Ghostty, Kitty, WezTerm, iTerm2, Alacritty... Terminal.app của macOS không hỗ trợ: màu sẽ sai |
| ☐ | **Terminal hỗ trợ chuột** | Click được vào chat sau khi mở app | Bật báo cáo chuột (mouse reporting) trong terminal |
| ☐ | **Locale UTF-8** (để hiện tiếng Việt, emoji) | `locale` hoặc `echo $LANG` có `UTF-8` | `export LANG=en_US.UTF-8` (hoặc `vi_VN.UTF-8`) |
| ☐ | **Cửa sổ từ 80x24 trở lên** | `tput cols; tput lines` | Phóng to cửa sổ. Nhỏ hơn vẫn chạy, không lỗi, nhưng chật |
| ☐ | (Linux) công cụ clipboard và mở file | `command -v xclip wl-copy xdg-open` | Cài `xclip` (X11) hoặc `wl-clipboard` (Wayland), và `xdg-open`. Thiếu thì nút copy chỉ dùng OSC 52, không mở được tệp bằng ứng dụng ngoài |
| ☐ | (tmux) bật các tuỳ chọn | Xem mục "Dùng trong tmux" | Thêm vào `~/.tmux.conf` |

**Nền tảng:** macOS đã dùng thực tế (Apple Silicon). Linux (amd64, arm64) và Windows được build và chạy test trên GitHub Actions (máy Linux, Windows, macOS), nhưng chưa ai dùng app với tài khoản thật ở đó. Windows không mở được tệp bằng ứng dụng ngoài.

**Terminal khuyến nghị:** Ghostty hoặc Kitty (ảnh thật, thông báo desktop, clipboard). Các terminal khác dùng được, với vài tính năng hạn chế (xem "Giới hạn").

## Cài đặt

**File chạy dựng sẵn (không cần cài Go):** tải gói cho hệ điều hành của bạn ở [trang Releases](https://github.com/khoinguyenvk9/bonagram/releases) (`darwin` = macOS, `arm64` = Apple Silicon / ARM, `amd64` = Intel/AMD), giải nén rồi chạy `bonagram` (`bonagram.exe` trên Windows). Có thể đối chiếu bản tải về với `checksums.txt`. File chưa được ký số nên lần chạy đầu có thể bị chặn:
- macOS: `xattr -d com.apple.quarantine ./bonagram` (hoặc chuột phải > Open trong Finder).
- Windows: SmartScreen báo "unknown publisher": chọn More info > Run anyway.

**Từ mã nguồn:** trong thư mục mã nguồn:

```bash
go build -ldflags="-s -w" -o bonagram .
./bonagram
```

Muốn gắn số phiên bản vào file chạy (để người dùng báo lỗi đúng bản): `go build -ldflags="-s -w -X main.version=0.1.0" -o bonagram .`

Dòng lệnh:

| Lệnh | Tác dụng |
|---|---|
| `bonagram` | Mở app |
| `bonagram --version` (hoặc `-v`) | In phiên bản, bản Go và nền tảng |
| `bonagram --help` (hoặc `-h`) | In cách dùng ngắn |

Tuỳ chọn không hợp lệ thì app báo lỗi và thoát với mã 2, không mở app.

Muốn gọi từ mọi thư mục thì chép file `bonagram` vào một thư mục trong `PATH`, ví dụ `~/.local/bin`.

## Lấy `api_id` và `api_hash`

1. Vào <https://my.telegram.org> và đăng nhập bằng số điện thoại Telegram của bạn.
2. Chọn **API development tools**, tạo một ứng dụng (tên và mô tả điền tuỳ ý).
3. Chép lại **App api_id** (một số) và **App api_hash** (chuỗi dài).

`api_id` và `api_hash` là của riêng bạn, không chia sẻ và không đưa lên git.

## Đăng nhập và chạy

```bash
./bonagram
```

Lần đầu: nhập `api_id` và `api_hash`, rồi số điện thoại (số bắt đầu bằng `0` được hiểu là số **Việt Nam** và đổi thành `+84...`; số của nước khác phải gõ đủ dạng `+<mã quốc gia>...`), mã Telegram gửi về và mật khẩu hai bước nếu có. Sau đó app mở luôn. Các lần sau chạy thẳng, không hỏi lại.

Mở trong một tab terminal riêng và để đó. Thoát bằng `Ctrl+Q`.

Cấu hình và phiên đăng nhập nằm trong `~/.config/bonagram/`:

| File | Nội dung |
|---|---|
| `env` | `api_id`, `api_hash` và các tuỳ chọn |
| `session.json` | Phiên đăng nhập |
| `width` | Độ rộng danh sách chat |
| `help-seen` | File rỗng, đánh dấu app đã tự mở hướng dẫn ở lần chạy đầu (xoá đi thì lần sau hiện lại) |

## Phím tắt

| Phím | Tác dụng |
|---|---|
| `Ctrl+K` | Tìm chat (`↑` `↓` chọn kết quả, `Enter` mở, `Esc` huỷ) |
| `Ctrl+N` / `Ctrl+P` | Chat kế tiếp / trước đó |
| `Ctrl+G` | Nhảy tới chat có tin chưa đọc |
| `Ctrl+T` / `Ctrl+Y` | Topic kế tiếp / trước đó (nhóm forum) |
| `Ctrl+B` | Chọn tin trong khung tin nhắn (`↑` `↓` di chuyển, `Esc` thoát) |
| `Enter` trên tin có ảnh hoặc tệp | Xem ảnh to (`Esc` hoặc `Q` đóng, `O` mở bằng ứng dụng hệ thống) |
| `Enter` hoặc `Ctrl+R` trên tin thường | Trả lời tin đó (`Ctrl+R` khi chưa chọn tin: trả lời tin mới nhất) |
| `C` (khi đang chọn tin) | Chép khối code của tin; bấm lần nữa để sang khối kế tiếp. Tin không có code thì chép cả tin |
| `E` `D` `R` `F` `O` `M` (khi đang chọn tin) | Sửa, xoá, reaction, chuyển tiếp, mở link, nhảy tới @mention. Xem phần dưới |
| `Ctrl+U` / `Ctrl+W` | Xoá chữ phía trước con trỏ (cuối dòng thì xoá hết ô) / xoá từng từ. Con trỏ ở giữa dòng: bấm `End` trước |
| `Esc` | Huỷ trả lời hoặc sửa tin |
| `↑` ở tin đầu tiên, `PgUp`, cuộn chuột lên | Tải thêm tin cũ |
| `F1` | Mở / đóng hướng dẫn phím tắt ngay trong app (`↑` `↓` cuộn, `Esc` đóng). Lần chạy đầu tiên app tự mở hướng dẫn này một lần |
| `Ctrl+Q` | Thoát |

Chuột:

- Click chat để mở, click ô tìm chat để tìm, click tab topic để chuyển topic.
- Click một tin để chọn: click ảnh hoặc tệp thì mở ngay, tin thường thì được tô sáng để dùng ngay `E`/`D`/`R`/`F`/`O` (`Enter` để trả lời tin đó).
- Click `[copy]` trên khối code để chép code.
- Cuộn chuột trên danh sách chat hoặc khung tin; kéo thanh dọc giữa hai khung để đổi độ rộng.
- Vì app bật chế độ chuột, muốn bôi đen chữ bằng chuột hãy giữ `Shift` khi kéo.

## Thao tác trên tin nhắn

Bấm `Ctrl+B` (hoặc click một tin) để chọn tin, rồi:

| Phím | Tác dụng |
|---|---|
| `E` | **Sửa** tin của bạn: nội dung đưa vào ô nhập, sửa xong bấm `Enter` để lưu, `Esc` để huỷ |
| `D` | **Xoá** tin của bạn với mọi người. App hỏi lại, chỉ `Y` mới xoá, phím khác là giữ |
| `R` | **Thả reaction**: bấm số `1`..`7` (👍 👎 ❤ 🔥 🎉 😁 😢). Bấm lại đúng reaction đã thả để bỏ |
| `F` | **Chuyển tiếp**: chọn chat đích trong danh sách bên trái (gõ để tìm), `Enter` để gửi, `Esc` để huỷ. Tin vào chat đích, nếu đích là nhóm forum thì vào topic General |
| `O` | **Mở link** trong tin bằng trình duyệt (chỉ http/https). Tin có nhiều link: bấm `O` tiếp để sang link kế |
| `M` | Nhảy lên @mention gần nhất trong các tin đã tải |

Các việc khác:

- **Gửi ảnh hoặc tệp**: bấm nút `[Tệp]` ở bên phải ô nhập (hoặc `Ctrl+O`) để mở hộp thoại chọn tệp của hệ điều hành, chọn một hoặc nhiều tệp; hoặc gõ `/file ` rồi các đường dẫn, hoặc kéo tệp từ Finder thả vào terminal để terminal dán đường dẫn. Ví dụ `/file ~/Downloads/bao-cao.pdf ~/Pictures/a.png`. Các tệp sẽ hiện ở dòng phía trên ô nhập: gõ chú thích (không bắt buộc) rồi `Enter` để gửi, hoặc `Esc` để bỏ. Ảnh jpg/png dưới 10 MB gửi dạng ảnh, còn lại gửi dạng tệp; nhiều ảnh (hoặc nhiều tệp) gửi thành một album tối đa 10, chú thích nằm ở tệp đầu. Đang trả lời một tin thì tệp gửi kèm làm trả lời tin đó. Trên Linux hộp thoại cần `zenity` hoặc `kdialog`; macOS và Windows có sẵn.
- **Tin ghim**: dòng `▍ Ghim` ở đầu chat là tin được ghim mới nhất (với nhóm forum, của topic đang xem). Lấy bằng một yêu cầu khi mở chat.
- **Nháp**: chữ đang gõ dở được giữ riêng cho từng chat và từng topic khi bạn chuyển đi chỗ khác, quay lại là còn. Nháp chỉ nằm trong bộ nhớ, thoát app là mất.

Những thao tác này chỉ chạy khi bạn bấm, mỗi lần là một yêu cầu tới Telegram, không có việc nào chạy nền.

## Ảnh và tệp

Terminal Kitty và Ghostty hiện ảnh thật; terminal khác (và tmux) hiện bằng khối màu nửa ô. Nếu ảnh không hiện đúng trên terminal của bạn, đặt `BONAGRAM_IMAGES=blocks` (luôn dùng khối màu) hoặc `BONAGRAM_IMAGES=kitty` (ép dùng giao thức Kitty).

Tệp ngoài ảnh chỉ được mở bằng ứng dụng hệ thống nếu thuộc loại quen thuộc (ảnh, video, âm thanh, PDF, văn bản, tài liệu Office). Loại khác chỉ được lưu vào thư mục cache và báo đường dẫn, vì mở bừa một tệp nhận từ chat có thể chạy mã.

## Khối code và copy

Nút `[copy]` chép nguyên văn khối code vào clipboard qua chuỗi OSC 52 (Ghostty, Kitty, WezTerm, iTerm2 đều hỗ trợ, dùng được cả khi qua SSH) và gọi thêm `pbcopy` (macOS), `wl-copy` hoặc `xclip` nếu có. Terminal.app của macOS không hỗ trợ OSC 52 nên chỉ nhờ `pbcopy`. Trong tmux cần bật `set -g set-clipboard on`.

## Cấu hình tuỳ chọn

Đặt trong `~/.config/bonagram/env` (mỗi dòng `KHOÁ=giá trị`) hoặc bằng biến môi trường. File `env` thắng biến môi trường cùng tên. Giá trị sai thì dùng mặc định, ngoài khoảng thì lấy cận gần nhất.

```
TG_API_ID=1234567
TG_API_HASH=<api_hash 32 ký tự của bạn>

# Tuỳ chọn (đây là giá trị mặc định)
BONAGRAM_MAX_MESSAGES=500
BONAGRAM_PREVIEWS=0
BONAGRAM_PREVIEW_ROWS=8
BONAGRAM_PREVIEW_COUNT=8
BONAGRAM_FPS=60
```

| Khoá | Mặc định | Ý nghĩa |
|---|---|---|
| `TG_API_ID`, `TG_API_HASH` | bắt buộc | Thông tin API ở trên |
| `BONAGRAM_MAX_MESSAGES` | `500` (40 đến 5000) | Số tin tối đa được tải khi cuộn lên xem lịch sử. Càng lớn càng tốn bộ nhớ |
| `BONAGRAM_PREVIEWS` | `0` (0 hoặc 1) | `1` hiện thumbnail ảnh ngay trong khung chat |
| `BONAGRAM_PREVIEW_ROWS` | `8` (0 đến 30) | Chỉ có tác dụng khi `BONAGRAM_PREVIEWS=1`: chiều cao tối đa của thumbnail, tính bằng dòng |
| `BONAGRAM_PREVIEW_COUNT` | `8` (0 đến 50) | Chỉ có tác dụng khi `BONAGRAM_PREVIEWS=1`: chỉ N ảnh mới nhất trong chat có thumbnail |
| `BONAGRAM_IMAGES` | tự nhận | `kitty` hoặc `blocks`: ép cách vẽ ảnh |
| `BONAGRAM_FPS` | `60` (10 đến 120) | Số khung hình tối đa mỗi giây. Thấp hơn thì tốn ít CPU hơn lúc để yên nhưng gõ và cuộn kém mượt hơn |

## Bộ nhớ, CPU và dung lượng

Số đo thực tế trên macOS (Apple Silicon), tài khoản thật có 117 chat. Chạy trong tmux nên ảnh vẽ bằng khối màu.

**Bộ nhớ**

| Tình huống | RSS | Bộ nhớ vật lý (`footprint`) |
|---|---|---|
| Vừa mở, danh sách chat đã tải | 26,5 MB | 15 MB |
| Mở một chat (40 tin) | 26,8 MB | 15 MB |
| Đã cuộn tới giới hạn 500 tin | 28,7 MB | 16 MB |
| Dùng một lúc rồi để yên | khoảng 25 MB | — |

RSS có tính cả thư viện dùng chung của hệ thống; cột bộ nhớ vật lý là phần hệ điều hành tính riêng cho app, ổn định hơn. Bộ nhớ gần như không tăng khi tải thêm tin vì app chỉ giữ dữ liệu tin, không tạo thành phần giao diện cho từng tin.

**CPU** (phần trăm của một nhân)

| Tình huống | CPU |
|---|---|
| Khởi động (kết nối và tải 117 chat) | khoảng 0,1 giây CPU |
| Để yên | khoảng 0,7% (0,4% với `BONAGRAM_FPS=30`) |
| Cuộn chuột liên tục (1.000 sự kiện trong 10 giây, mức cuộn rất nhanh) | khoảng 8% |
| 15 tin nhắn đến dồn dập | khoảng 1,0% |

App vẽ lại tối đa 60 khung hình mỗi giây. Hạ xuống bằng `BONAGRAM_FPS` nếu muốn tiết kiệm CPU hơn: 30 khung hình cho khoảng 0,4% lúc để yên, 20 khung hình khoảng 0,3%, đổi lại gõ chữ và cuộn có thể thấy chậm hơn một chút (độ trễ vẽ tối đa 33 ms ở 30 khung hình, 50 ms ở 20).

**Dung lượng**

| Thành phần | Dung lượng |
|---|---|
| File chạy `bonagram` (build với `-s -w`) | khoảng 17 MB |
| `~/.config/bonagram/` | vài chục KB |
| Cache ảnh và tệp đã tải (`~/Library/Caches/bonagram/` trên macOS, `~/.cache/bonagram/` trên Linux) | tuỳ số ảnh bạn xem, vài trăm KB đến vài MB mỗi ảnh |

Cache không tự xoá: bạn xoá tay thư mục `bonagram/` đó bất cứ lúc nào, app tải lại khi cần.

Ghi chú về số đo: mỗi tình huống đo một lần (các con số lệch vài phần trăm giữa các lần đo), chưa đo khi hiện ảnh bằng giao thức Kitty trên Ghostty, chưa đo với tài khoản có hàng nghìn chat. Dùng thực tế nên tính khoảng 25 đến 35 MB RSS.

## Dùng trong tmux

Thêm vào `~/.tmux.conf` rồi tải lại (`tmux source ~/.tmux.conf`):

```
set -g default-terminal "tmux-256color"
set -ga terminal-overrides ",*:RGB"   # màu 24-bit
set -g set-clipboard on               # nút copy
set -g focus-events on                # thông báo và đánh dấu đã đọc đúng lúc
set -g mouse on
```

Trong tmux ảnh luôn hiện bằng khối màu (không dùng giao thức Kitty).

## Dữ liệu và bảo mật

- `~/.config/bonagram/session.json` **là quyền truy cập đầy đủ vào tài khoản Telegram của bạn**. Ai có file này đọc và gửi tin được như bạn. Không chia sẻ, không đưa lên git.
- `~/.config/bonagram/env` chứa `api_hash`, nên để quyền `600` (app tạo file với quyền này).
- App chỉ nói chuyện với server Telegram, không gửi dữ liệu đi nơi khác.
- Mở chat trong app là đánh dấu đã đọc, giống app Telegram chính thức.
- Tên và nội dung nhận từ Telegram được lọc bỏ ký tự điều khiển trước khi hiển thị, để người gửi không chèn được lệnh vào terminal của bạn.

## Xử lý sự cố

| Triệu chứng | Cách xử lý |
|---|---|
| Báo `API_ID_INVALID` | `api_id`/`api_hash` sai. Sửa trong `~/.config/bonagram/env` hoặc xoá file đó để nhập lại |
| Kẹt khi kết nối | Kiểm tra mạng và VPN. App chưa hỗ trợ proxy |
| Gõ số điện thoại báo sai | Số bắt đầu bằng `0` bị đổi thành `+84...` (Việt Nam). Số nước khác hãy gõ đủ dạng `+<mã quốc gia>...` |
| Không thấy chat đã lưu trữ | Chủ ý: chat Archived bị ẩn |
| Ảnh không hiện hoặc méo | Đặt `BONAGRAM_IMAGES=blocks`. Trong tmux ảnh luôn hiện bằng khối màu |
| Màu sai hoặc xám xịt | Terminal không hỗ trợ màu 24-bit: đổi terminal hoặc kiểm tra `echo $COLORTERM` |
| Tiếng Việt hoặc emoji hiện ô vuông, lệch | Kiểm tra locale UTF-8 và phông chữ có hỗ trợ ký tự đó |
| `[copy]` không chép được | Terminal của bạn có thể không hỗ trợ OSC 52; trong tmux bật `set -g set-clipboard on` |
| Chữ đang soạn của bộ gõ tiếng Việt hiện sai chỗ | Báo lỗi kèm tên bộ gõ và terminal đang dùng |
| Thấy tiến trình `bonagram` còn chạy sau khi đã thoát | Tắt bằng `pkill bonagram` (tiến trình sót vẫn giữ phiên đăng nhập mở). Nếu gặp, báo lỗi kèm cách bạn đã thoát app |
| Muốn đăng xuất | Xoá `~/.config/bonagram/session.json`, hoặc chấm dứt phiên trong Telegram, mục Settings, Devices |
| Muốn làm lại từ đầu | Xoá cả thư mục `~/.config/bonagram/` |

## Giới hạn

**Tài khoản và mạng**
- Một tài khoản mỗi lần chạy (đổi tài khoản: xoá `~/.config/bonagram/session.json`).
- Chưa hỗ trợ proxy. Nơi chặn Telegram cần VPN.
- App hiện thành một thiết bị mới trong Telegram, mục Settings, Devices.
- Đăng nhập nhiều lần liên tiếp có thể bị Telegram tạm khoá gửi mã (FLOOD_WAIT): chờ rồi thử lại.
- Không dùng để gửi tin hàng loạt, Telegram có thể giới hạn hoặc khoá tài khoản.

**Dữ liệu được tải**
- Tối đa 1.000 chat chính; chat lưu trữ bị ẩn.
- Mỗi lần mở chat tải 40 tin gần nhất, cuộn lên tải thêm, tối đa `BONAGRAM_MAX_MESSAGES` tin (mặc định 500, tối đa 5.000).
- Tối đa 100 topic mỗi nhóm forum.
- Tin bị sửa bởi người khác (kể cả bot stream câu trả lời bằng cách sửa dần một tin) cập nhật trực tiếp, vẽ lại tối đa 4 lần mỗi giây. Tin bị xoá bởi người khác chỉ biến mất sau khi mở lại app.
- Tin ghim chỉ cập nhật khi mở chat; ghim hay bỏ ghim lúc đang xem chỉ hiện sau khi mở lại chat.
- Chat lưu trữ nhận tin mới (và được Telegram tự bỏ lưu trữ) chỉ hiện lại sau khi mở lại app.

**Tính năng chưa có**
- Gửi: văn bản thuần, ảnh và tệp (một hoặc nhiều, kèm chú thích). Chưa có giọng nói, định dạng chữ. Telegram giới hạn 4.096 ký tự mỗi tin, vượt quá sẽ báo lỗi gửi.
- Reaction: chỉ 7 emoji cố định; nếu chat không cho dùng emoji đó (ví dụ Saved Messages) Telegram từ chối và app báo lỗi. Sửa, xoá chỉ áp dụng cho tin của bạn (trong Saved Messages là mọi tin). Xoá không hoàn tác được.
- Chuyển tiếp: từng tin một, giữ tên người gửi gốc, vào topic General nếu đích là nhóm forum.
- Tin bạn gửi dạng tệp hoặc chuyển tiếp hiện sau vài giây, khi app tải lại các tin mới nhất của chat đó.
- Hiển thị: in đậm, nghiêng, liên kết bấm được bằng chuột (dùng phím `O`), tô màu cú pháp trong khối code, hình của emoji tuỳ chỉnh, xem ai đã thả phản ứng. Sticker, tin thoại, bình chọn, vị trí... hiện dưới dạng nhãn như `[file]`, `[media]`.
- Chat bí mật (secret chat), cuộc gọi, story: không hỗ trợ.
- Giao diện chỉ có tiếng Việt, một bảng màu tối cố định.
- Chat có tin mới nhảy lên đầu nhóm chat thường; chat ghim luôn nằm trên cùng và giữ vị trí. Ghim hoặc bỏ ghim chat lúc đang chạy chỉ hiện sau khi mở lại app.

**Theo terminal**
- Ảnh thật chỉ có trên Kitty và Ghostty; còn lại là khối màu.
- Thông báo desktop và đánh dấu đã đọc đúng lúc cần terminal báo trạng thái cửa sổ (focus). Terminal không báo thì app coi như cửa sổ luôn đang được xem.
- Nút copy dùng OSC 52 (Ghostty, Kitty, WezTerm, iTerm2) và các công cụ `pbcopy`, `wl-copy`, `xclip`; Terminal.app chỉ chạy được nhờ `pbcopy`.
- Chữ đang soạn của bộ gõ tiếng Việt chỉ hiện đúng chỗ nếu terminal vẽ nó tại vị trí con trỏ. Chưa kiểm tra trên mọi terminal.
- Vì app bật chế độ chuột, muốn bôi đen chữ phải giữ `Shift` khi kéo.

## Phát triển

```bash
go vet ./... && go test -race ./...    # test không kết nối Telegram thật
go test -run TestModelFlow ./...       # chạy một test
```

Bộ test dùng dữ liệu giả: gửi tin vào mô hình giao diện rồi kiểm tra khung hình. Phần gọi API Telegram chỉ kiểm chứng được bằng cách chạy app với tài khoản thật.

## Giấy phép

[MIT](LICENSE). Các thư viện mà dự án dùng có giấy phép riêng, đều là loại cho phép tự do (MIT, BSD, ISC, Apache-2.0).

## Lưu ý

Đây là dự án cá nhân, dùng API của Telegram qua tài khoản người dùng. Chỉ dùng với tài khoản của bạn, đừng dùng để gửi tin hàng loạt: Telegram có thể giới hạn hoặc khoá tài khoản vi phạm điều khoản sử dụng.
