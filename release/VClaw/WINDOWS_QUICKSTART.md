# V-Claw Windows Quickstart

Tài liệu này dành cho người dùng Windows muốn chạy V-Claw bản đầy đủ.

## 1. V-Claw là gì

V-Claw là một trợ lý AI local-first hỗ trợ:
- đọc và tóm tắt Gmail
- xem lịch Google Calendar
- làm việc với Google Drive, Docs, Sheets
- chạy bot Telegram cho một người dùng sở hữu
- kiểm tra trạng thái hệ thống, approval, logs

Bản phát hành này là bản đầy đủ theo hướng nội bộ / demo / power-user, không phải bản consumer one-click hoàn toàn tự động.

## 2. Bạn cần chuẩn bị gì

### Bắt buộc
- Windows
- Kết nối internet
- OpenAI API key
- Thư mục V-Claw đã được giải nén hoàn toàn

### Nếu dùng Telegram bot
- Telegram bot token từ BotFather
- Telegram user ID được phép dùng bot

### Nếu dùng Google Workspace
- file `credentials.json`
- tài khoản Google có quyền thật trên Gmail / Calendar / Drive / Docs / Sheets / Chat mà bạn muốn dùng
- đăng nhập Google qua OAuth lần đầu

### Nếu muốn dùng đầy đủ `status`, `logs`, `approvals`
- Docker Desktop
- Docker Compose hoạt động bình thường
- PostgreSQL chạy được qua `docker compose`

## 3. Giải nén đúng cách

- Không chạy file trực tiếp bên trong `.zip`
- Hãy giải nén toàn bộ thư mục `VClaw` ra một chỗ riêng, ví dụ:

```text
C:\VClaw\
