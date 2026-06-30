# Checklist Trước Release

Tài liệu này để nhóm tick nhanh trước khi demo, gửi cho mentor, hoặc đóng gói bản trải nghiệm.

## Mức Ưu Tiên

- `BẮT BUỘC`: không đạt thì chưa nên release/demo
- `NÊN CÓ`: thiếu vẫn có thể demo, nhưng rủi ro cao hơn
- `TỐT NẾU CÓ`: giúp bản chạy ổn hơn hoặc dễ giải thích hơn

## 1. Cấu Hình

### BẮT BUỘC

- [ ] Có file `.env` đúng môi trường chạy
- [ ] Nếu dùng Google tools, đã có OAuth credentials và token hợp lệ
- [ ] Nếu dùng `status`, `logs`, `approvals`, đã có `DATABASE_URL`
- [ ] Nếu demo Telegram, đã có `TELEGRAM_BOT_TOKEN` và `ALLOWED_TELEGRAM_USER_ID`
- [ ] Máy chạy được `go run ./cmd/vclaw ...`

### NÊN CÓ

- [ ] Có file cấu hình mẫu rõ ràng cho người khác copy
- [ ] Có tài khoản Google demo riêng thay vì dùng tài khoản cá nhân

## 2. Khởi Chạy

### BẮT BUỘC

- [ ] Chạy được `rtk go run ./cmd/vclaw status`
- [ ] Nếu demo bot, chạy được `rtk go run ./cmd/vclaw telegram run --google-tools auto --web-tools auto`
- [ ] Không có lỗi startup ngay khi mở runtime

### NÊN CÓ

- [ ] Đã thử restart runtime ít nhất 1 lần
- [ ] Đã kiểm tra cổng monitoring đang đúng

## 3. Health Check

### BẮT BUỘC

- [ ] `llm_provider` là `ok`
- [ ] `tool_registry` có tool

### NÊN CÓ

- [ ] `postgres` là `ok` nếu bạn dùng logs/approvals/history
- [ ] `google_oauth` là `ok` nếu bạn demo Google Workspace
- [ ] `channel` đúng với cách bạn đang demo

## 4. Smoke Test

### BẮT BUỘC

- [ ] Chạy được 1 happy path đơn giản
- [ ] Chạy được 1 approval flow
- [ ] Chạy được 1 reject flow
- [ ] Chạy được 1 negative case

Gợi ý các case nên chạy:

- happy / read: [drive-list-files-happy.json](/home/nxhai/V_Claw/testing-e2e/usecases/drive-list-files-happy.json:1)
- approval + reject: [calendar-create-event-reject.json](/home/nxhai/V_Claw/testing-e2e/usecases/calendar-create-event-reject.json:1)
- negative: [read-file-not-found.json](/home/nxhai/V_Claw/testing-e2e/usecases/read-file-not-found.json:1)
- multi-step: [chat-to-calendar-and-docs.json](/home/nxhai/V_Claw/testing-e2e/usecases/chat-to-calendar-and-docs.json:1)

## 5. Safety

### BẮT BUỘC

- [ ] Hành động có rủi ro phải dừng ở approval đúng chỗ
- [ ] Reject phải không thực thi tool
- [ ] Agent không được bịa nội dung khi file/tài nguyên không tồn tại
- [ ] File safety case trả kết quả dễ hiểu với người dùng

### NÊN CÓ

- [ ] Đã thử ít nhất một case prompt injection
- [ ] Đã thử ít nhất một case fake file / sai định dạng

## 6. Tài Liệu

### BẮT BUỘC

- [ ] Có hướng dẫn người mới bắt đầu
- [ ] Có runbook
- [ ] Có checklist release
- [ ] Có cảnh báo an toàn

### NÊN CÓ

- [ ] Có checklist demo riêng
- [ ] Có bảng readiness ngắn để ai cũng biết cái gì sẵn sàng

## 7. Những Chỗ Cần Nhìn Thẳng Và Ghi Rõ

## CHƯA CÓ BENCHMARK CHÍNH THỨC

- [ ] Đã ghi rõ với người xem rằng hiện chưa có benchmark chuẩn về tốc độ, chi phí, độ chính xác

## CHƯA PHẢI BẢN PRODUCTION HOÀN CHỈNH

- [ ] Đã nói rõ đây là bản demo / internal release nếu đó là sự thật

## TELEGRAM VÀ MỘT SỐ PROVIDER CÒN PHỤ THUỘC MÔI TRƯỜNG

- [ ] Đã có phương án fallback nếu Telegram, Google, hoặc DB có vấn đề

## 8. Quyết Định Cuối

Chỉ nên chốt `READY` nếu:

- các mục `BẮT BUỘC` đều đạt
- smoke test chính pass
- approval flow pass
- negative case chính pass
- không có lỗi nghiêm trọng về safety

Nếu chưa đạt, ghi rõ là:

- `READY FOR DEMO`
- `READY FOR INTERNAL TRIAL`
- `NOT READY FOR RELEASE`
