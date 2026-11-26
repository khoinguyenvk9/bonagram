# Nhật ký thay đổi

English: [CHANGELOG.md](CHANGELOG.md)

## v0.4.0

**Tệp và video**
- Gửi và tải tệp hiện tiến trình (phần trăm và dung lượng) ở dòng tiêu đề; `Ctrl+X` huỷ lượt gửi/tải mới nhất. Tệp vượt giới hạn của Telegram (2 GB, 4 GB với Premium) bị chặn trước khi tải lên, và gửi lỗi giờ có báo lý do.
- Tệp mp4/mov/m4v được gửi dạng video xem được ngay trong Telegram (thời lượng, kích thước và phát trực tiếp đọc từ tệp; có ảnh bìa nếu máy cài `ffmpeg`).
- Ảnh gửi "dạng tệp" (không nén) có ảnh xem trước dưới thẻ tệp.

**Chat**
- Thư mục chat thành các tab trên danh sách (`Ctrl+L` hoặc bấm để đổi, số chưa đọc từng tab); hàng tab cuộn ngang bằng ‹ › hoặc con lăn khi không vừa.
- Chuột phải vào chat › Thư mục để thêm vào hoặc bỏ khỏi thư mục (lưu lên Telegram, thiết bị khác cũng đổi theo).
- "Đang soạn tin…" (và đang ghi âm, đang gửi ảnh, …) trong danh sách và tiêu đề chat; người khác cũng thấy khi bạn gõ. Trạng thái online ("đang hoạt động", "hoạt động 5 phút trước") dưới tên người.
- ✓/✓✓ cho tin cuối của bạn trong danh sách chat.

**Đọc tin**
- Chuột phải vào tin › Ai đã thả react: ai thả gì, lọc theo emoji; bấm vào người để mở chat với họ. Chỉ đọc; trong nhóm chỉ khi Telegram cho xem, kênh thì không.
- ←/→/Backspace/Delete trong ô soạn đi qua trọn một ký tự (dấu gõ tách rời, emoji ghép).

**Tài liệu**
- Hướng dẫn cài trên Linux (bản dựng sẵn đã thử trên Ubuntu, Fedora và Alpine); thêm năm ảnh minh hoạ (menu, reaction, thư mục, gửi tệp, cột thu gọn).

## v0.3.0

**Giao diện mới** (theo bản thiết kế)
- Tông graphite với một màu nhấn hổ phách; "Bonagram" và số chat chưa đọc phía trên ô tìm không viền; tiêu đề chat một dòng.
- Tin của bạn nằm trong khối nền ấm, giờ và ✓✓ ở dưới; mỗi ngày bắt đầu bằng dòng "─── Hôm nay ───"; trích dẫn có vạch theo màu tên người; reaction dạng ô; thẻ tệp và thẻ vị trí còn một dòng.
- Ô soạn khung bo tròn có nút kẹp giấy và nút gửi; dòng dưới cùng là các ô phím của chế độ đang dùng, ghi theo kiểu macOS hoặc Windows/Linux, thay cho thanh phím tắt tiếng Anh.
- Cửa sổ quá hẹp cho danh sách (dưới 76 cột với độ rộng mặc định), hoặc kéo thanh dọc hẳn sang trái, thì danh sách chat thu thành cột ô chữ viết tắt có màu, số chưa đọc, rê chuột để xem tên.

**Nhắn tin**
- Dấu đã đọc: `✓` khi đã gửi, `✓✓` khi đã được đọc, cập nhật trực tiếp.
- Ô soạn nhiều dòng: `Alt+Enter`/`Ctrl+J` xuống dòng, dán văn bản giữ nguyên các dòng, ô cao dần tới 6 dòng, `↑` `↓` lên xuống dòng, click để đặt con trỏ, `Ctrl+Z` hoàn tác. Con trỏ thật đi theo để bộ gõ tiếng Việt hiện đúng chỗ.
- Gợi ý emoji: gõ `:` + tên (`:haha`, `:camon`), chèn bằng `Tab`/`Enter`.
- Chuột phải vào tin: hàng reaction ở trên cùng (một click, hoặc phím 1–7), bên dưới là trả lời, chép, ghim, chuyển tiếp, sửa, xoá, link và tệp, mỗi mục có biểu tượng Nerd Font và phím tắt (`BONAGRAM_ICONS=off` để bỏ biểu tượng).
- Ghim và bỏ ghim tin (nhóm: báo cho mọi người hoặc im lặng; chat riêng: cho cả hai hoặc chỉ cho tôi).

**Chat**
- `Ctrl+K` tìm cả trên Telegram (người, nhóm, kênh theo tên hoặc `@username`) khi gõ từ 3 chữ, để nhắn cho người chưa có trong danh sách.
- Chuột phải vào chat: ghim hoặc bỏ ghim, tắt hoặc bật thông báo, xoá chat (chỉ phía tôi hoặc cho cả hai) hoặc rời nhóm.
- Đọc trên máy khác thì số chưa đọc trong app giảm theo ngay.

**Đọc tin**
- Click vào trích dẫn để nhảy tới tin gốc; click dòng Ghim để nhảy tới tin ghim, click tiếp để sang tin ghim cũ hơn (`Ghim 1/3`). Tin chưa tải thì mở cùng các tin quanh nó.

**Hiệu năng**
- Nhanh hơn v0.2.1 dù giao diện nhiều chi tiết hơn: vẽ lại chat 500 tin 1,15 ms (v0.2.1: 2,3 ms), một khung hình 0,10 ms (0,21 ms), gõ một phím trong bản nháp 5.500 ký tự 0,29 ms.

**Tài liệu**
- Viết lại README cả hai thứ tiếng: điểm nổi bật, bắt đầu nhanh, mục lục, hướng dẫn chia theo việc, bảng phím chia nhóm, và sửa các chỗ sai.

## v0.2.1

- Vẽ lại nhanh hơn: nội dung từng tin và dòng trích dẫn được vẽ một lần rồi dùng lại cho tới khi thay đổi. Vẽ lại một chat 500 tin mất gần bằng v0.1.0 (2,3 ms so với 2,1 ms trong benchmark), dù giờ mọi tin trả lời đều có trích dẫn và chữ có định dạng.
- `bench_test.go`: benchmark vẽ chat 500 tin (`go test -run TestPerfHeap -v -bench . -benchmem`).
- Đo lại trên tài khoản thật (README, mục "Bộ nhớ, CPU và dung lượng"): CPU lúc để yên và khi cuộn như v0.1.0; khi bật ảnh xem trước, RSS khoảng 60 MB sau khi mở chat có ảnh (bộ nhớ vật lý 18 đến 20 MB). Không còn tiến trình thừa sau Ctrl+Q trong 30 lần thử.
- Thêm nhật ký thay đổi này.

## v0.2.0 (so với v0.1.0)

**Đọc tin**
- Mọi tin trả lời đều hiện đang trả lời ai, nội dung gì (`↪ Tên: "nội dung"`); tin được trả lời cũ hơn các tin đã tải thì được tra một lần cho cả trang.
- Trong nhóm, tin mới đến mà thiếu thông tin người gửi không còn hiện tên nhóm: app lấy tên đã thấy ở tin trước, hoặc tải lại chat.
- Tin nhận về hiện định dạng: đậm, nghiêng, gạch chân, gạch ngang, chữ ẩn (mờ), code, mention, hashtag.
- Link màu xanh, click để mở, gạch chân khi rê chuột, là hyperlink OSC 8 (Cmd/Ctrl+click trên terminal hỗ trợ). Link ẩn sau chữ hiện kèm địa chỉ.
- Tệp hiện thành thẻ có nhãn màu theo loại, tên và dung lượng; video, âm thanh có thời lượng. Nhãn ở các chỗ khác nói rõ là gì (`[file] a.pdf · 2.3 MB`, `[voice] 0:12`, `[location]`, `[contact]`, `[poll]`); bản xem trước link không còn thêm `[media]`.
- Sticker tĩnh được vẽ ngay trong khung tin.
- Vị trí hiện thành thẻ có link Google Maps.
- Tin bị người khác xoá vẫn nằm trên màn hình tới khi mở lại app (cố ý).

**Viết tin**
- Gõ `@` trong nhóm để gợi ý thành viên (chỉ hỏi server khi gõ `@`); người không có username được mention bằng entity.
- Định dạng bằng ký hiệu: `**đậm**`, `__nghiêng__`, `~~gạch~~`, `||ẩn||`, `` `code` ``, khối ```` ``` ````, `[chữ](https://url)`. Sửa tin thì định dạng được đổi lại thành ký hiệu.
- `/position vĩ-độ, kinh-độ` (hoặc `/vitri`, hoặc dán link Google Maps) để gửi vị trí.

**Tìm và chép**
- `Ctrl+F` tìm trong nội dung tin của chat hoặc topic đang mở; `Enter` trên kết quả mở các tin quanh đó.
- Kéo chuột qua tin để bôi đen, thả chuột là chép.
- `L` trên một tin để chép các link của nó.

**Tệp, ảnh, video**
- `Enter` trên tệp lưu vào `~/Downloads` (không ghi đè), mở các loại an toàn, loại khác chỉ chỉ ra trong trình quản lý tệp. Lỗi nằm lại trên màn hình; liên kết tệp hết hạn thì app tự làm mới rồi tải lại. Click vào tệp chỉ chọn tin.
- Khung xem ảnh toàn màn hình chạy thêm giao thức iTerm2 (iTerm2, WezTerm, mintty, Rio, VS Code) và Sixel (foot, Windows Terminal 1.22+, Konsole, mlterm, contour). `BONAGRAM_IMAGES` nhận `kitty`, `iterm`, `sixel`, `blocks`.
- Video và GIF phát ngay trong terminal bằng mpv nếu có (đồ hoạ Kitty, Sixel hoặc ô chữ màu), không thì mở bằng trình phát hệ thống.

**Sửa lỗi**
- Reaction vừa thả hiện ngay; click vào tin là chọn được để thao tác, không cần Ctrl+B; đang chọn tin mà click ô nhập thì quay lại gõ.
- Windows: mở tệp và link bằng `rundll32` (trước đó gọi nhầm `xdg-open`).

**Giới hạn**: xem mục "Giới hạn" trong README. Chưa thử trên tài khoản thật: khung xem ảnh iTerm2/Sixel, phát video bằng mpv, gửi vị trí, gửi định dạng, Ctrl+F, tải tệp.
