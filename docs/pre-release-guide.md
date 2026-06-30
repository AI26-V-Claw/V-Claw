# Hướng Dẫn Trước Release

Tài liệu này dành cho người chuẩn bị demo, kiểm tra bản chạy thử, hoặc bàn giao cho người dùng nội bộ. Nội dung được viết đơn giản, ưu tiên thao tác thực tế hơn là chi tiết kỹ thuật.

## Đọc File Nào Trước

Nếu bạn chỉ có 10 phút, đọc theo thứ tự này:

1. File này: [pre-release-guide.md](/home/nxhai/V_Claw/docs/pre-release-guide.md:1)
2. Danh sách sẵn sàng trước release: [release-readiness.md](/home/nxhai/V_Claw/docs/release-readiness.md:1)
3. Checklist cần tick trước khi demo hoặc bàn giao: [release-checklist.md](/home/nxhai/V_Claw/docs/release-checklist.md:1)
4. Nếu cần demo trước lớp/khách hàng: [demo-checklist.md](/home/nxhai/V_Claw/docs/demo-checklist.md:1)
5. Nếu cần biết điều gì không nên làm: [safety-guide.md](/home/nxhai/V_Claw/docs/safety-guide.md:1)

## V-Claw Là Gì

V-Claw là trợ lý AI hỗ trợ công việc văn phòng như:

- đọc và tóm tắt Gmail
- xem và tạo lịch trên Google Calendar
- đọc và gửi tin trên Google Chat
- làm việc với Google Drive, Docs, Sheets
- xử lý file cục bộ trong môi trường có kiểm soát

Điểm quan trọng là các hành động có rủi ro như gửi email, tạo lịch, sửa dữ liệu, chạy code hoặc đụng file thường phải qua bước xác nhận.

## Trước Khi Chạy

Bạn cần chuẩn bị:

- máy có Go
- file `.env`
- Google OAuth nếu muốn dùng Gmail, Calendar, Drive, Docs, Sheets, Chat
- PostgreSQL nếu muốn dùng log, approval history, health/status đầy đủ
- Telegram token nếu muốn chạy bot Telegram

Nếu bạn chưa chắc đã cấu hình đủ chưa, xem:

- [docs/runbook.md](/home/nxhai/V_Claw/docs/runbook.md:1)
- [configs/google/README.md](/home/nxhai/V_Claw/configs/google/README.md:1)

## Khởi Chạy Nhanh

### Chạy local một lệnh agent

```bash
rtk go run ./cmd/vclaw agent --prompt "ping" --session dev --channel dev-cli
```

### Chạy bot Telegram

```bash
rtk go run ./cmd/vclaw telegram run --google-tools auto --web-tools auto
```

### Kiểm tra trạng thái hệ thống

```bash
rtk go run ./cmd/vclaw status
```

### Xem approval đang chờ

```bash
rtk go run ./cmd/vclaw approvals --status pending
```

## Smoke Test Nhanh

Nếu cần kiểm tra nhanh trước demo:

1. Chạy `vclaw status`
2. Kiểm tra `postgres`, `llm_provider`, `tool_registry`
3. Nếu dùng Google Workspace, kiểm tra `google_oauth`
4. Chạy 1 usecase đơn giản trong `testing-e2e/usecases`
5. Nếu cần approval flow, chạy case reject:
   [calendar-create-event-reject.json](/home/nxhai/V_Claw/testing-e2e/usecases/calendar-create-event-reject.json:1)
6. Nếu cần negative/error flow, chạy case file not found:
   [read-file-not-found.json](/home/nxhai/V_Claw/testing-e2e/usecases/read-file-not-found.json:1)

Ví dụ:

```bash
rtk .venv/bin/python testing-e2e/scripts/run_agent_usecase.py --usecase testing-e2e/usecases/calendar-create-event-reject.json
rtk .venv/bin/python testing-e2e/scripts/run_agent_usecase.py --usecase testing-e2e/usecases/read-file-not-found.json
```

## Cách Biết Bản Này Có Sẵn Sàng Không

Đừng chỉ nhìn xem chương trình có chạy hay không. Hãy nhìn 4 thứ:

1. `status` có khỏe không
2. usecase E2E có pass không
3. approval có hoạt động không
4. kết quả trả về có dễ hiểu với người dùng không

Trạng thái hiện tại của từng mảng được ghi ở:

- [release-readiness.md](/home/nxhai/V_Claw/docs/release-readiness.md:1)

## Nếu Có Lỗi

Các lỗi hay gặp nhất:

- thiếu quyền Google OAuth
- PostgreSQL chưa chạy
- Telegram token sai hoặc thiếu
- provider AI chưa cấu hình
- usecase âm tính trả lỗi đúng nghiệp vụ nhưng harness cũ chưa hiểu

Xem cách xử lý ở:

- [docs/runbook.md](/home/nxhai/V_Claw/docs/runbook.md:1)
- [release-checklist.md](/home/nxhai/V_Claw/docs/release-checklist.md:1)

## Cảnh Báo Rõ Ràng

## CHƯA CÓ BẢN ĐÁNH GIÁ BENCHMARK CHUẨN

Hiện repo chưa có bộ benchmark chính thức để so sánh tốc độ, chi phí, độ chính xác, độ ổn định giữa các model hay giữa các lần release.

Điều này có nghĩa là:

- chưa có con số chuẩn để nói bản này nhanh hơn bao nhiêu
- chưa có ngưỡng pass/fail về chất lượng model ở mức benchmark
- việc đánh giá vẫn đang dựa nhiều vào usecase E2E, smoke test và review thủ công

## CHƯA NÊN COI ĐÂY LÀ BẢN PRODUCTION HOÀN CHỈNH

Từ trạng thái code và tài liệu hiện tại, đây là bản gần demo / gần internal release hơn là production release cho số đông.

## CHƯA PHẢI MỌI MẢNG ĐỀU CÓ E2E ỔN ĐỊNH NHƯ NHAU

Một số flow đã có test khá rõ. Một số flow vẫn có thể bị ảnh hưởng bởi:

- quyền truy cập Google thật
- trạng thái provider
- dữ liệu bên ngoài thay đổi
- hạ tầng local như DB hoặc network

## Khi Nào Nên Dừng Và Báo Nhóm

Dừng và báo nhóm ngay nếu:

- `vclaw status` báo `unhealthy` ở `postgres` hoặc `llm_provider`
- approval không hiện ra dù hành động đáng ra phải cần xác nhận
- agent tạo/sửa/gửi thật mà không qua approval đúng chỗ
- file safety cho ra kết quả mơ hồ hoặc đọc nhầm file nguy hiểm như file an toàn
- output người dùng nhìn thấy khó hiểu, quá kỹ thuật, hoặc không nhất quán
