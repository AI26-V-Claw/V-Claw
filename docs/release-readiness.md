# Trạng Thái Sẵn Sàng Trước Release

Tài liệu này trả lời câu hỏi: "Feature nào đã sẵn sàng, feature nào còn rủi ro, và chỗ nào chưa nên hứa với người dùng?"

## Cách Đọc

- `SẴN SÀNG TƯƠNG ĐỐI`: có code, có bằng chứng test/usecase đủ dùng cho demo hoặc internal release
- `DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN`: có thể chạy, nhưng còn phụ thuộc cấu hình/quyền/hạ tầng
- `CHƯA ỔN ĐỊNH`: không nên hứa mạnh với người dùng cuối
- `CHƯA CÓ`: hiện chưa có bằng chứng hoặc chưa nên coi là xong

## Tổng Quan Nhanh

| Mảng | Trạng thái | Ghi chú ngắn |
| --- | --- | --- |
| Agent runtime cơ bản | SẴN SÀNG TƯƠNG ĐỐI | Có unit/integration proof theo `TEST_MATRIX.md` |
| HITL approval | DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN | Có approve, revise, reject; E2E mới ở mức partial |
| Gmail tools | SẴN SÀNG TƯƠNG ĐỐI | Có nhiều usecase và test, nhưng phụ thuộc OAuth thật |
| Calendar tools | SẴN SÀNG TƯƠNG ĐỐI | Đã có happy và reject flow |
| Chat tools | DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN | Có usecase nhưng channel end-user vẫn còn phụ thuộc runtime thực |
| Drive / Docs / Sheets | DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN | Có usecase khá nhiều, nhưng dễ bị ảnh hưởng bởi quyền và dữ liệu thật |
| File safety | DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN | Có usecase, nhưng nên review kỹ output user-facing trước demo |
| Memory / seeded recall | DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN | Có seed-based usecase, chưa nên hứa quá mạnh |
| Telegram channel | CHƯA ỔN ĐỊNH | Theo `TEST_MATRIX.md` còn `in_progress` |
| Logs / audit / monitoring | DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN | Phụ thuộc Postgres và setup DB |
| Benchmark / đánh giá chuẩn | CHƯA CÓ | Chưa có benchmark chính thức |

## Chi Tiết Theo Mảng

### 1. Agent Runtime

Trạng thái: `SẴN SÀNG TƯƠNG ĐỐI`

Bằng chứng:

- [docs/TEST_MATRIX.md](/home/nxhai/V_Claw/docs/TEST_MATRIX.md:15)
- test trong `internal/agent/*`

Ý nghĩa với người dùng:

- có thể nhận yêu cầu, gọi tool, dừng ở approval, rồi trả kết quả

### 2. HITL Approval

Trạng thái: `DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN`

Bằng chứng:

- [calendar-create-event-reject.json](/home/nxhai/V_Claw/testing-e2e/usecases/calendar-create-event-reject.json:1)
- [approval-revise-then-approve.json](/home/nxhai/V_Claw/testing-e2e/usecases/approval-revise-then-approve.json:1)
- [gmail-send-clarify-then-approve.json](/home/nxhai/V_Claw/testing-e2e/usecases/gmail-send-clarify-then-approve.json:1)

Điểm tốt:

- đã có approve
- đã có revise
- đã có reject/hủy

Điểm cần cẩn thận:

- E2E theo `TEST_MATRIX.md` vẫn là `partial`
- trải nghiệm channel thực tế có thể khác CLI

### 3. Google Workspace

Trạng thái: `DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN`

Bao gồm:

- Gmail
- Calendar
- Drive
- Docs
- Sheets
- Chat

Điểm tốt:

- có nhiều usecase thật trong `testing-e2e/usecases`
- có smoke flow trong CLI Google commands

Điểm cần cẩn thận:

- phụ thuộc Google OAuth thật
- phụ thuộc quyền tài khoản thật
- phụ thuộc dữ liệu thật trên Drive/Gmail/Calendar

### 4. Memory

Trạng thái: `DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN`

Bằng chứng:

- [memory-seeded-recall.json](/home/nxhai/V_Claw/testing-e2e/usecases/memory-seeded-recall.json:1)
- [calendar-summary-email-seeded-followup.json](/home/nxhai/V_Claw/testing-e2e/usecases/calendar-summary-email-seeded-followup.json:1)
- [drive-folder-summary-chat-seeded-followup.json](/home/nxhai/V_Claw/testing-e2e/usecases/drive-folder-summary-chat-seeded-followup.json:1)

## CHƯA CÓ THƯỚC ĐO ỔN ĐỊNH CỦA MEMORY THEO THỜI GIAN

Hiện có usecase seeded, nhưng chưa có benchmark hoặc báo cáo dài hạn để kết luận memory luôn nhớ đúng, nhớ đủ, và không nhớ sai.

### 5. File Safety

Trạng thái: `DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN`

Bằng chứng:

- [uploaded-file-prompt-injection-safe.json](/home/nxhai/V_Claw/testing-e2e/usecases/uploaded-file-prompt-injection-safe.json:1)
- [uploaded-file-safety-check.json](/home/nxhai/V_Claw/testing-e2e/usecases/uploaded-file-safety-check.json:1)
- [read-file-not-found.json](/home/nxhai/V_Claw/testing-e2e/usecases/read-file-not-found.json:1)

## CHƯA CÓ BỘ ĐÁNH GIÁ CHUẨN CHO FILE SAFETY

Chưa có benchmark chính thức kiểu:

- tỷ lệ chặn prompt injection
- tỷ lệ nhận diện file giả / file sai định dạng
- tỷ lệ false positive / false negative

### 6. Telegram

Trạng thái: `CHƯA ỔN ĐỊNH`

Bằng chứng:

- [docs/TEST_MATRIX.md](/home/nxhai/V_Claw/docs/TEST_MATRIX.md:23)

Ý nghĩa:

- có code và tài liệu setup
- nhưng chưa nên coi đây là phần hoàn thiện nhất để cam kết với người dùng cuối

### 7. Health / Logs / Evaluate

Trạng thái: `DÙNG ĐƯỢC NHƯNG CẦN CẨN THẬN`

Điểm tốt:

- có `vclaw status`
- có `vclaw approvals`
- có `vclaw logs`
- có `testing-e2e/scripts/evaluate_agent_expectations.py`

Điểm cần cẩn thận:

- nhiều phần phụ thuộc Postgres
- evaluate hiện là LLM-based review, không phải benchmark chuẩn

## CHƯA CÓ BẢNG BENCHMARK CHÍNH THỨC

Hiện chưa có:

- benchmark tốc độ
- benchmark chi phí
- benchmark độ chính xác theo bộ chuẩn
- benchmark stability qua nhiều lần chạy

Nếu cần release nghiêm túc hơn, đây là khoảng trống nên bổ sung sớm.

## Kết Luận Gọn

Nếu release nội bộ hoặc demo:

- có thể làm được
- nhưng cần chạy checklist trước

Nếu release cho người dùng rộng hơn:

- chưa nên xem là production-ready hoàn chỉnh
- cần bổ sung benchmark, readiness tracking rõ hơn, và xác nhận kỹ channel/runtime thật
