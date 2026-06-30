# Checklist Demo

Tài liệu này dành cho người sắp demo trước mentor, khách mời, hoặc đồng đội không chuyên kỹ thuật.

## Mục Tiêu Demo

Người xem nên thấy được 4 điều:

1. V-Claw hiểu yêu cầu văn phòng bằng tiếng Việt
2. V-Claw biết đọc công cụ Google hoặc file khi cần
3. V-Claw dừng lại ở bước xác nhận khi hành động có rủi ro
4. V-Claw xử lý được cả trường hợp lỗi đơn giản mà không bịa kết quả

## Kịch Bản Demo Khuyên Dùng

### Demo 1: Approval + Reject

Usecase:

- [calendar-create-event-reject.json](/home/nxhai/V_Claw/testing-e2e/usecases/calendar-create-event-reject.json:1)

Người xem sẽ thấy:

- bot/agent hiểu yêu cầu tạo lịch
- agent dừng ở approval
- người dùng bấm hoặc trả lời reject
- agent xác nhận đã hủy, không tạo lịch

### Demo 2: Negative Case "Không Có File"

Usecase:

- [read-file-not-found.json](/home/nxhai/V_Claw/testing-e2e/usecases/read-file-not-found.json:1)

Người xem sẽ thấy:

- agent không bịa nội dung file
- khi file không tồn tại, agent trả lỗi đúng ý nghĩa

### Demo 3: Multi-step Flow

Usecase:

- [chat-to-calendar-and-docs.json](/home/nxhai/V_Claw/testing-e2e/usecases/chat-to-calendar-and-docs.json:1)

Người xem sẽ thấy:

- agent biết hỏi làm rõ
- agent biết chia nhiều bước
- agent biết tạo lịch rồi tạo docs sau khi được duyệt

## Trước Giờ Demo 15 Phút

- [ ] Mở terminal đúng thư mục repo
- [ ] Kiểm tra `.env`
- [ ] Kiểm tra `vclaw status`
- [ ] Kiểm tra DB nếu cần logs/approvals
- [ ] Kiểm tra token Google nếu demo Google Workspace
- [ ] Nếu demo Telegram, mở bot trước

## Câu Nói Nên Dùng Khi Demo

Bạn có thể nói đơn giản như sau:

- "Đây là trợ lý AI hỗ trợ công việc văn phòng."
- "Các hành động nhạy cảm như gửi email hoặc tạo lịch sẽ không tự chạy ngay."
- "Hệ thống sẽ hỏi xác nhận trước."
- "Nếu dữ liệu không có hoặc file không tồn tại, trợ lý không được phép bịa."

## Nếu Demo Bị Lỗi

### Lỗi nhẹ

Ví dụ:

- OAuth hết hạn
- provider trả chậm
- một usecase phụ fail

Cách nói:

- "Phần cấu hình môi trường đang có vấn đề, nhưng luồng chính của hệ thống là như thế này."

### Lỗi nặng

Ví dụ:

- approval không hiện ra
- agent tự làm hành động đáng ra phải dừng
- status báo unhealthy ở phần cốt lõi

Cách xử lý:

- dừng demo đó
- chuyển sang case an toàn hơn
- không cố giải thích vòng vo

## Case Dự Phòng

Nếu một case chính fail, nên chuyển sang:

- [calendar-create-event-reject.json](/home/nxhai/V_Claw/testing-e2e/usecases/calendar-create-event-reject.json:1)
- [read-file-not-found.json](/home/nxhai/V_Claw/testing-e2e/usecases/read-file-not-found.json:1)

Hai case này ngắn, dễ hiểu, và thể hiện rõ safety.

## Cảnh Báo Rõ Ràng Khi Demo

## CHƯA CÓ BENCHMARK CHÍNH THỨC

Nếu bị hỏi "độ chính xác bao nhiêu phần trăm" hoặc "nhanh hơn bao nhiêu", hiện chưa có bảng benchmark chính thức để trả lời bằng số liệu chuẩn.

## CHƯA NÊN NÓI ĐÂY LÀ SẢN PHẨM PRODUCTION HOÀN THIỆN

Nên gọi đây là:

- bản demo
- bản thử nghiệm nội bộ
- bản gần release nội bộ

Không nên nói quá rằng:

- mọi feature đã hoàn chỉnh
- mọi môi trường đều chạy ổn như nhau
- Telegram/channel/runtime đã hoàn toàn production-ready
