# Demo Telegram: User Setting, `/status`, `/history` - Sprint 2 (Hai)

Kịch bản này mô tả luồng thao tác ngay trên Telegram cho 3 phần đã có trong Sprint 2:

- user setting qua `/policy`
- xem trạng thái run bằng `/status`
- xem lịch sử run bằng `/history`

Mục tiêu của demo là cho thấy người dùng có thể:

1. Tự cấu hình mức rủi ro nào được auto-allow.
2. Theo dõi run gần nhất với thời gian, chi phí, kết quả và ref lỗi.
3. Xem danh sách lịch sử gọn, có phân loại theo category và mở chi tiết từng run.
4. Dev có thể mở Langfuse để soi trace chi tiết, tool call, metadata và error ref.

---

## Prerequisites

- Bot đang chạy: `go run ./cmd/vclaw telegram run --google-tools required`
- Google OAuth đã sẵn sàng nếu demo có dùng Google Workspace tools.
- PostgreSQL đang chạy.
- User đang dùng đúng Telegram account đã được allow.

---

## S1 - User setting qua `/policy`

**Mục tiêu**: kiểm tra policy menu trong Telegram và xác nhận user setting ảnh hưởng đến việc auto-allow hay yêu cầu phê duyệt.

### Bước

1. Nhắn vào bot:
   ```text
   /policy
   ```
2. Trong menu policy:
   - để `safe_read` ở nhóm auto-allow
   - để `safe_compute` ở nhóm auto-allow
   - giữ `sensitive_read` ở nhóm cần phê duyệt
   - giữ `external_write` ở nhóm cần phê duyệt
   - giữ `local_write` ở nhóm cần phê duyệt
   - giữ `code_execution` ở nhóm cần phê duyệt
   - giữ `destructive` ở nhóm luôn chặn
3. Bấm `Lưu`.

### Kết quả kỳ vọng

- Bot lưu policy thành công.
- Telegram phản hồi đúng trạng thái từng nhóm rủi ro.
- Không có thay đổi ngoài policy đã chọn.

### Gợi ý kiểm tra nhanh

Sau khi lưu policy, thử một lệnh read an toàn:

```text
xem lịch ngày mai của tôi
```

Kỳ vọng:

- Nếu tool thuộc nhóm `safe_read`, bot tự chạy không cần hỏi approve.
- Nếu tool thuộc nhóm `sensitive_read` hoặc cao hơn, bot vẫn dừng ở bước phê duyệt.

---

## S2 - `/status` cho run gần nhất

**Mục tiêu**: kiểm tra bot có trả về trạng thái lệnh gần nhất với đầy đủ thông tin mà người dùng cần đọc ngay.

### Bước

1. Chạy một lệnh bất kỳ, ví dụ:
   ```text
   xem lịch ngày mai của tôi
   ```
2. Khi bot trả lời xong, nhắn:
   ```text
   /status
   ```

### Kết quả kỳ vọng

- Bot trả về khối trạng thái run gần nhất.
- Phần hiển thị có:
  - thời gian chạy
  - thời gian xử lý
  - chi phí
  - yêu cầu
  - các bước kết quả
  - trạng thái cuối cùng có emoji
- Nếu run thất bại, có thêm dòng `🔍 Ref: ...`.

### Điểm cần chú ý khi demo

- Nếu run là thành công, trạng thái cuối phải là:
  ```text
  Trạng thái: ✅ Hoàn thành
  ```
- Nếu run là thất bại, trạng thái cuối phải là:
  ```text
  Trạng thái: ❌ Thất bại
  ```
- Chi phí chỉ nên hiển thị giá trị thật, không nên là số 0 giả.

---

## S3 - `/history` cho danh sách run

**Mục tiêu**: kiểm tra danh sách run gần nhất, cách rút gọn nhãn, biểu tượng category và thời gian thông minh.

### Bước

1. Nhắn:
   ```text
   /history
   ```
2. Quan sát danh sách 10 run gần nhất.
3. Nhắn tiếp:
   ```text
   /history 2
   ```

### Kết quả kỳ vọng

- `/history` hiển thị danh sách gọn theo thứ tự gần nhất.
- Mỗi dòng có:
  - số thứ tự
  - thời gian thông minh
  - icon category
  - short label đã rút gọn
  - trạng thái thành công/thất bại
  - thời lượng run
- Với run cũ hơn trong ngày, lịch sử phải hiện ngày, không chỉ hiện giờ.
- `/history 2` mở chi tiết run thứ 2 trong danh sách.

### Ví dụ quan sát

- `📧` cho email
- `📅` cho calendar
- `📁` cho drive
- `📄` cho docs
- `🔍` cho search
- `💬` cho các category còn lại

---

## S4 - Kịch bản demo ngắn trên Telegram

Nếu muốn trình diễn nhanh trên sân khấu hoặc trong video, đi theo chuỗi này:

1. `/policy`
2. Bật auto-allow cho `safe_read` và `safe_compute`
3. Gửi:
   ```text
   xem lịch ngày mai của tôi
   ```
4. Gửi:
   ```text
   /status
   ```
5. Gửi:
   ```text
   /history
   ```

Kết quả mong đợi:

- Người xem thấy ngay user setting ảnh hưởng tới việc bot có hỏi phê duyệt hay không.
- Người xem thấy `/status` cho run gần nhất có thời gian, chi phí và kết quả rõ ràng.
- Người xem thấy `/history` là bản tóm tắt ngắn gọn, còn `/status` là bản chi tiết.

---

## S5 - Langfuse cho dev giám sát chi tiết

**Mục tiêu**: xác nhận mỗi run có thể được dev theo dõi lại ở Langfuse bằng trace URL, metadata và error ref.

### Bước

1. Chạy một lệnh có tool call, ví dụ:
   ```text
   xem lịch ngày mai của tôi
   ```
2. Nếu lệnh thất bại, ghi lại ref lỗi từ Telegram:
   ```text
   🔍 Ref: A8F3C2
   ```
3. Mở log terminal hoặc dashboard monitoring và lấy `traceUrl=` của run đó.
4. Mở trace trong Langfuse.
5. Tìm trace bằng một trong các cách:
   - `traceUrl`
   - `run_id`
   - `error_ref`

### Kết quả kỳ vọng

- Dev thấy toàn bộ trace của run, gồm:
  - input/output của tool call
  - timing
  - status
  - metadata của run
- Nếu run thất bại, `error_ref` phải xuất hiện trong Langfuse để tìm lại nhanh.
- `traceUrl` trong log hoặc `/status` là đường dẫn trực tiếp để mở trace.

### Điểm demo nên nói rõ

- Telegram là bề mặt cho người dùng cuối.
- Langfuse là bề mặt cho dev quan sát chi tiết.
- Hai luồng này bổ sung nhau: user xem `/status` và `/history`, dev mở Langfuse để soi trace sâu hơn.

---

## Ghi chú cho người demo

- Không nhắc người xem về chi tiết kỹ thuật nội bộ như trace, DB schema, hoặc policy object.
- Chỉ nói theo ngôn ngữ người dùng nhìn thấy trong Telegram.
- Nếu cần minh họa run thất bại, dùng một lệnh an toàn nhưng thiếu dữ liệu hoặc một trường hợp connector lỗi sẵn có trong môi trường test.
