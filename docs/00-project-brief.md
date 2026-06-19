# V-Claw: AI Agent Tự Động Hóa Công Việc & Điều Khiển Máy Tính An Toàn

> Tài liệu tổng quan dự án — Project Brief

---

## 1. Mô Tả Dự Án

**V-Claw** là một trợ lý AI Agent toàn diện dành cho cá nhân. Trợ lý này kết nối trực tiếp với Email, Lịch họp và Chat để quản lý công việc văn phòng (Google Workspace), đồng thời có khả năng can thiệp vào hệ điều hành bằng Python/Shell để xử lý file, dữ liệu và điều khiển máy tính.

---

## 2. Cơ Chế HITL (Human-in-the-Loop)

Hệ thống sẽ phân loại hành động theo mức độ rủi ro:

- **Hành động an toàn** (đọc thông tin, truy vấn): thực thi ngay lập tức.
- **Hành động nguy hiểm / thay đổi dữ liệu** (gửi email cho đối tác, xóa/sửa lịch họp, xóa file, chạy lệnh hệ thống sâu): **bắt buộc phải kích hoạt vòng lặp chờ duyệt**.

Khi gặp hành động nguy hiểm, AI sẽ:
1. **Dừng lại** và không tự ý thực thi.
2. **Hiển thị rõ ràng** những gì nó định làm (bằng tiếng Việt).
3. **Chờ người dùng xác nhận** bằng nút bấm mới tiến hành thực thi.

---

## 3. Mục Tiêu Dự Án

- Tạo ra một bản thử nghiệm có khả năng liên thông đa tác vụ giữa công việc văn phòng và hệ thống máy tính.
- Thử nghiệm mô hình bảo mật và an toàn AI trong thực tế.

---

## 4. Lộ Trình Phát Triển (Roadmap)

### Sprint 1 — Kết Nối Các Cổng External & Ổn Định Agent Routing

| Mục tiêu | Mô tả |
|---|---|
| **G1** | Kết nối xong API cơ bản của Gmail, Calendar và Chat (bộ Google Workspace). |
| **G2** | Thiết lập môi trường (Sandbox/Docker) để chuẩn bị cho việc chạy code Python. |
| **G3** | Ổn định luồng agent routing theo kiểu GoClaw: chat an toàn đi no-tool, tác vụ cần dữ liệu/công cụ đi tool-enabled agent loop, prompt injection bị chặn. Clarify chỉ xảy ra khi agent/tool schema thật sự thiếu thông tin bắt buộc; risk/approval do tool policy quyết định. |
| **G4** | Kết nối các phương thức Telegram/Slack để giao tiếp với Agent. |
| **G5** | Agent loop, tool call, multi-step planning. |

---

### Sprint 2 — Hoàn Thiện Nền Tảng Agent, Tool, HITL & Memory

| Mục tiêu | Mô tả |
|---|---|
| **G1** | Hoàn thiện agent loop nhiều bước để AI có thể suy nghĩ, gọi công cụ, đọc kết quả và tiếp tục xử lý cho đến khi hoàn thành yêu cầu. |
| **G2** | AI chạy được Python/Shell trong môi trường sandbox để xử lý file, dữ liệu, Excel/Word và các tác vụ máy tính cơ bản một cách an toàn. |
| **G3** | Hoàn thiện HITL: mọi hành động rủi ro như gửi mail, sửa lịch, ghi file hoặc chạy lệnh hệ thống đều phải dừng lại và chờ người dùng duyệt bằng tiếng Việt rõ ràng. |
| **G4** | Kết nối các luồng công việc hằng ngày giữa Gmail, Calendar, Chat, Drive/Docs/Sheets và Web Search theo hướng đọc trước, đề xuất sau, chỉ ghi/sửa khi được duyệt. |
| **G5** | Cải thiện bộ nhớ ngắn hạn, lưu trạng thái phiên làm việc, lịch sử tool call, approval và log để agent không mất ngữ cảnh khi chạy nhiều bước. |

---

### Sprint 3 — Memory Dài Hạn, Luồng Thực Tế & Bản Trải Nghiệm Gần Production

| Mục tiêu | Mô tả |
|---|---|
| **G1** | Tích hợp bộ nhớ dài hạn ở mức dễ kiểm soát: AI nhớ người quen, dự án, tài liệu quan trọng, thói quen làm việc và các ghi chú ổn định của người dùng. |
| **G2** | Bổ sung lớp liên kết thông tin đơn giản để AI hiểu quan hệ giữa người, dự án, cuộc họp và tài liệu mà không làm hệ thống quá phức tạp. |
| **G3** | Hoàn thiện log, health check và hướng dẫn vận hành để người dùng xem lại được hành động nào đã chạy, hành động nào được duyệt, bị từ chối hoặc gặp lỗi. |
| **G4** | Tối ưu các luồng phối hợp nhiều công cụ cùng lúc, đặc biệt là các thao tác đọc an toàn có thể chạy song song để phản hồi nhanh hơn. |
| **G5** | Hoàn thành kiểm thử và demo hybrid nổi bật: AI xử lý việc văn phòng qua Gmail/Calendar/Chat/Drive, đồng thời xử lý file hoặc lệnh máy tính trong sandbox, mọi hành động rủi ro đều có HITL tiếng Việt. |
| **G6** | Đóng gói bản gần production để user trải nghiệm: cấu hình rõ ràng, checklist khởi chạy, smoke test, runbook và kịch bản demo cuối sprint. |

---

## 5. Cơ Cấu Nhóm

| Nhóm | Số thành viên | Trách nhiệm |
|---|---|---|
| **Memory & Context** | 2 người | Phụ trách bộ nhớ ngắn hạn/dài hạn, ghi chú người dùng, liên kết người-dự án-tài liệu và cách agent dùng context đúng lúc. |
| **Agent Flow** | 2 người | Phụ trách agent loop, chạy nhiều bước, tiếp tục sau khi người dùng duyệt/từ chối và tối ưu phối hợp nhiều công cụ. |
| **Tools & Workspace** | 2 người | Phụ trách Google Workspace, Web Search, sandbox/file tools và đảm bảo công cụ có mô tả/risk rõ ràng để agent dùng an toàn. |
| **Safety & Release** | 2 người | Phụ trách HITL, log, monitoring, channel UX, runbook, smoke test và đóng gói bản trải nghiệm gần production. |

---
