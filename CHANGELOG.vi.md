# Nhật ký thay đổi

English: [CHANGELOG.md](CHANGELOG.md)

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
