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
| **G3** | Ổn định luồng agent routing theo kiểu GoClaw: chat an toàn đi no-tool, tác vụ cần dữ liệu/công cụ đi tool-enabled agent loop, prompt injection bị chặn. Clarify chỉ xảy ra khi agent/tool schema thật sự thiếu thông tin bắt buộc; risk/approval do tool policy quyết định, không do intent classifier. |
| **G4** | Kết nối các phương thức Telegram/Slack để giao tiếp với Agent. |
| **G5** | Agent loop, tool call, multi-step planning. |

---

### Sprint 2 — Phát Triển Kỹ Năng Hệ Thống & HITL

| Mục tiêu | Mô tả |
|---|---|
| **G1** | AI tự viết và chạy được code Python/Shell trong môi trường sandbox để thực hiện các tác vụ như: lọc file, gom thư mục, đọc file Excel, tạo và sửa file Word. |
| **G2** | Hoàn thiện tính năng HITL: Khi AI định gửi mail hoặc chạy lệnh hệ thống, agent loop sẽ dừng lại, yêu cầu sự đồng ý của người dùng kèm lời giải thích rõ ràng bằng tiếng Việt. |
| **G3** | Kết nối luồng liên thông: AI đọc Email → Calendar → Chat để đưa ra các đề xuất và xử lý công việc hàng ngày. |
| **G4** | Tích hợp bộ nhớ ngắn hạn để AI nhớ được chuỗi hội thoại hiện tại. *(Ví dụ: "Nãy giờ về dự án AI Agent, tôi và bạn trao đổi gì — note lại giúp tôi.")* |

---

### Sprint 3 — Đồng Bộ Bộ Nhớ Dài Hạn, Test & Release

| Mục tiêu | Mô tả |
|---|---|
| **G1** | Tích hợp bộ nhớ dài hạn, giúp AI nhớ được thói quen làm việc và danh sách những người quen thuộc của người dùng để xử lý thông minh hơn. |
| **G2** | Xây dựng hệ thống log chi tiết: ghi lại từng hành động, lệnh nào đã được người dùng duyệt, lệnh nào bị từ chối. |
| **G3** | Tối ưu hóa tốc độ phản hồi của AI khi phối hợp nhiều công cụ cùng lúc. |
| **G4** | Hoàn thành kiểm thử với các kịch bản kết hợp thực tế. *(Ví dụ: "Kiểm tra mail xem có ai hẹn không, nếu có thì xếp vào lịch trống và gom các tài liệu họ gửi vào một thư mục riêng trên máy.")* |

---

## 5. Cơ Cấu Nhóm

| Nhóm | Số thành viên | Trách nhiệm |
|---|---|---|
| **Kiến trúc hệ thống** | Toàn bộ | Ngồi lại thảo luận về kiến trúc hệ thống, cơ sở dữ liệu, lên diagram cho các luồng chính của dự án. |
| **Tích hợp API** | 2 người | Tập trung làm việc với API của Mail, Calendar, Chat và đồng bộ dữ liệu về database. |
| **Hệ thống & Sandbox** | 2 người | Chịu trách nhiệm về môi trường cô lập, xử lý để AI chạy lệnh Shell/Python an toàn khi tương tác với máy tính. |
| **Agent & Bộ nhớ** | 2 người | Viết prompt, xây dựng turn router/tool-enabled agent loop, xử lý policy boundary cho hành động cần HITL và xây dựng bộ nhớ dài hạn (Knowledge Graph đơn giản). |
| **Phương thức kết nối** | 2 người | Kết nối với Telegram/Slack để giao tiếp với Agent. |

---
