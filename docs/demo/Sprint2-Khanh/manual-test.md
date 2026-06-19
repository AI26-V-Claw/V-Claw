Dưới đây là tài liệu hướng dẫn **Manual Test (Kiểm thử thủ công)** chi tiết giúp bạn xác nhận toàn bộ hệ thống tích hợp Google Workspace (Drive, Docs, Sheets), cơ chế kiểm soát rủi ro HITL (Human-in-the-Loop) và định dạng dữ liệu (ToolResult) hoạt động chuẩn xác theo thiết kế.

---

### **A. Chuẩn bị môi trường & Cấu hình**

1. **Khởi chạy bot (Telegram):**
   Đảm bảo bạn đã cấu hình đầy đủ token trong file `.env` hoặc truyền tham số khi chạy:
   ```bash
   go run ./cmd/vclaw telegram run
   ```
2. **Cấu hình Policy (`data/user-policy.json`):**
   Đảm bảo file cấu hình policy của bạn đang thiết lập đúng phân nhóm rủi ro:
   * **`safe_read` / `safe_compute`**: nằm trong `auto_allow` (Tự động chạy).
   * **`external_write` / `sensitive_read`**: nằm trong `require_approval` (Cần xác nhận qua HITL).
   * **`destructive`**: nằm trong `always_block` (Luôn chặn).

---

### **B. Kịch bản Manual Test chi tiết**

#### **1. Kiểm thử Nhóm Read-First (Auto-Allow / Safe Read)**
> **Mục tiêu:** Xác minh các tác vụ đọc dữ liệu chạy trực tiếp, nhanh chóng, không bị chặn hay yêu cầu xác nhận.

* **Test Case 1.1: Liệt kê danh sách file trên Drive (`drive.listFiles`)**
  * **Hành động:** Gửi tin nhắn cho bot: *"Hãy liệt kê cho tôi 5 file gần đây nhất trong Drive của tôi."*
  * **Kỳ vọng:**
    * Bot **không** hiện nút Approve/Reject.
    * Bot tự động thực thi và trả về danh sách file ngay lập tức.
    * *Đặc biệt:* Kiểm tra log xem LLM có nhận được danh sách file rút gọn (đã lọc các trường thừa - `compactDriveListFilesForLLM`) để tiết kiệm token không.
* **Test Case 1.2: Xem nội dung tài liệu Docs (`docs.getDocument`)**
  * **Hành động:** Gửi tin nhắn: *"Đọc nội dung tài liệu có ID là `<ID_của_file_Docs>` giúp tôi."*
  * **Kỳ vọng:** Nội dung tài liệu được hiển thị thẳng lên khung chat Telegram mà không qua bước duyệt.
* **Test Case 1.3: Đọc dữ liệu Sheets (`sheets.readValues`)**
  * **Hành động:** Gửi tin nhắn: *"Đọc dữ liệu của sheet từ ô A1 đến C10 trong file Spreadsheet `<ID_của_file_Sheets>`"*
  * **Kỳ vọng:** Bot đọc dữ liệu và trả kết quả hiển thị dạng bảng hoặc văn bản ngay lập tức.

---

#### **2. Kiểm thử Nhóm Mutation & Share (Yêu cầu duyệt HITL)**
> **Mục tiêu:** Xác minh các công cụ ghi/sửa đổi dữ liệu đều kích hoạt cơ chế yêu cầu người dùng xác nhận trước khi chạy.

* **Test Case 2.1: Chấp nhận thay đổi (Approve flow)**
  * **Hành động:** Gửi tin nhắn yêu cầu di chuyển file: *"Hãy di chuyển file 'Thuật toán binary search' vào thư mục 'Nhập môn lập trình' giúp tôi."* (sử dụng `drive.moveFile`) hoặc yêu cầu viết thêm văn bản: *"Hãy ghi thêm dòng 'Cập nhật ngày 12/06' vào tài liệu Docs `<ID>`"* (sử dụng `docs.appendText`).
  * **Kỳ vọng:**
    * Bot **không** thực thi ngay lập tức.
    * Xuất hiện thông điệp yêu cầu phê duyệt kèm các nút **[Approve]** và **[Reject]**.
    * Nhấn nút **[Approve]**.
    * Bot thực hiện hành động thành công, thông báo kết quả và cập nhật trạng thái đã phê duyệt.
* **Test Case 2.2: Từ chối thay đổi (Reject flow)**
  * **Hành động:** Gửi tin nhắn: *"Hãy chia sẻ file `<ID>` này cho email `test@gmail.com` với quyền xem."* (sử dụng `drive.shareFile`).
  * **Kỳ vọng:**
    * Bot hiển thị form yêu cầu phê duyệt (HITL).
    * Nhấn nút **[Reject]**.
    * Bot dừng thực thi ngay lập tức, báo trạng thái đã từ chối và gửi phản hồi thông báo thao tác bị từ chối bởi người dùng.

---

#### **3. Kiểm thử Nhóm Destructive (Always Blocked - Luôn chặn)**
> **Mục tiêu:** Xác minh các tác vụ nguy hiểm bị chặn tuyệt đối mà không cần hỏi ý kiến.

* **Test Case 3.1: Xóa file hoặc xóa sheet (`drive.trashFile` hoặc `sheets.deleteSheet`)**
  * **Hành động:** Gửi tin nhắn: *"Hãy xóa vĩnh viễn file `<ID>` trên Drive của tôi"* hoặc *"Xóa trang tính 'Data_2025' trong file Sheets `<Spreadsheet_ID>` giúp tôi"*.
  * **Kỳ vọng:**
    * Bot phản hồi ngay lập tức rằng hành động này bị cấm bởi chính sách hệ thống (Policy Blocked).
    * Không gửi bất kỳ yêu cầu duyệt nào, không thực thi cuộc gọi API hủy hoại này lên Google.

---

#### **4. Kiểm thử Định dạng đầu ra (ToolResult Shape), Artifact Ref & Truncation**
> **Mục tiêu:** Xác minh định dạng dữ liệu trả về cho LLM & User sạch sẽ, đầy đủ siêu dữ liệu (metadata), liên kết tài liệu (artifact) và xử lý tràn dung lượng.

* **Test Case 4.1: Kiểm tra Artifact Reference**
  * **Hành động:** Yêu cầu bot: *"Hãy tạo một file tài liệu Docs mới có tiêu đề 'Báo cáo kiểm thử'."*
  * **Kỳ vọng:**
    * Sau khi duyệt (Approve) và tạo thành công, kiểm tra log hệ thống xem cấu trúc `ToolResult` có trả về đúng `ArtifactRef` dạng:
      ```json
      "artifactRef": {
        "kind": "file",
        "label": "Báo cáo kiểm thử",
        "uri": "https://docs.google.com/document/d/.../edit",
        "id": "documentId-12345"
      }
      ```
    * Bot hiển thị link liên kết đến tài liệu mới tạo trên giao diện chat Telegram để bạn có thể bấm trực tiếp vào.
* **Test Case 4.2: Xử lý Truncation (Giới hạn độ dài)**
  * **Hành động:** Yêu cầu bot tải xuống một file văn bản hoặc file log cực kỳ lớn (hoặc đọc tài liệu Docs có dung lượng hàng trăm trang).
  * **Kỳ vọng:**
    * Kết quả trả về cho LLM được đánh dấu `Truncated: true` để tránh làm tràn ngữ cảnh (context limit).
    * User nhận được thông điệp báo hiệu dữ liệu bị cắt ngắn kèm tùy chọn/hướng dẫn để đọc các phần tiếp theo cụ thể hơn.

---

#### **5. Kiểm thử Bảo mật (Redaction)**
> **Mục tiêu:** Đảm bảo các thông tin nhạy cảm không bị rò rỉ vào log hoặc nội dung gửi tới LLM.

* **Test Case 5.1: Ẩn thông tin nhạy cảm**
  * **Hành động:** Gửi một tin nhắn chứa thông tin nhạy cảm (như OAuth Token, API Key hoặc mật khẩu giả lập) trong yêu cầu.
  * **Kỳ vọng:**
    * Kiểm tra log hoặc `ContentForLLM` xem các thông tin nhạy cảm có được che đi (ví dụ: thay bằng `[REDACTED]`) hay không.
    * `ContentForUser` vẫn hiển thị đúng để người dùng kiểm soát thông tin của mình.

---

#### **6. Kiểm thử Xử lý lỗi (Error Handling) & OAuth Scopes**
> **Mục tiêu:** Đảm bảo hệ thống xử lý lỗi mượt mà khi gặp sự cố phân quyền hoặc tham số không hợp lệ.

* **Test Case 6.1: Thiếu quyền truy cập (OAuth Scope Error)**
  * **Hành động:** Tạo một tài khoản Google test nhưng không cấp quyền ghi (chỉ cấp quyền đọc) hoặc xóa file token cũ đi và xác thực lại nhưng bỏ chọn một số quyền. Sau đó chạy một công cụ ghi dữ liệu (ví dụ: `sheets.updateValues`).
  * **Kỳ vọng:** Bot không bị crash, hiển thị thông báo lỗi thân thiện bằng tiếng Việt (yêu cầu cấp lại quyền hoặc báo lỗi phân quyền cụ thể).
* **Test Case 6.2: Tham số không hợp lệ**
  * **Hành động:** Yêu cầu đọc một file Docs có ID không tồn tại.
  * **Kỳ vọng:** Bot trả về lỗi hợp lệ (`TOOL_INPUT_INVALID` hoặc `INTERNAL_ERROR`), thông báo lỗi bằng tiếng Việt, không bị crash bot.