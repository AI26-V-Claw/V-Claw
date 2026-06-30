# V-Claw Windows Quickstart

Tài liệu này dành cho người dùng Windows muốn chạy V-Claw bản đầy đủ.

Bản phát hành này phù hợp cho nội bộ, demo hoặc power-user. Đây chưa phải bản consumer one-click hoàn toàn tự động.

---

## 1. V-Claw là gì?

V-Claw là một trợ lý AI local-first hỗ trợ:

- Đọc và tóm tắt Gmail
- Xem lịch Google Calendar
- Làm việc với Google Drive, Docs, Sheets
- Chạy bot Telegram cho một người dùng sở hữu
- Kiểm tra trạng thái hệ thống, approval và logs
- Hỗ trợ một số luồng vận hành nội bộ hoặc demo

---

## 2. Bạn cần chuẩn bị gì?

### Bắt buộc

- Máy Windows
- Kết nối internet
- OpenAI API key
- Thư mục V-Claw đã được giải nén hoàn toàn

### Nếu dùng Telegram bot

Bạn cần thêm:

- Telegram bot token lấy từ BotFather
- Telegram user ID được phép dùng bot

### Nếu dùng Google Workspace

Bạn cần thêm:

- File `credentials.json`
- Tài khoản Google có quyền thật trên Gmail, Calendar, Drive, Docs, Sheets hoặc Chat mà bạn muốn dùng
- Đăng nhập Google qua OAuth ở lần chạy đầu tiên

### Nếu muốn dùng đầy đủ `status`, `logs`, `approvals`, sandbox

Bạn nên có thêm:

- Docker Desktop
- Docker Compose hoạt động bình thường
- PostgreSQL chạy được qua `docker compose`
- `Setup.exe` sẽ tự build sandbox image

---

## 3. Giải nén đúng cách

Không chạy file trực tiếp bên trong file `.zip`.

Hãy giải nén toàn bộ thư mục `VClaw` ra một vị trí riêng, ví dụ:

```text
C:\VClaw\
```

Sau khi giải nén, bạn nên thấy các file và thư mục như:

```text
vclaw.exe
.env.example
setup.bat
start.bat
status.bat
google-auth.bat
docker-compose.yml
sandbox-docker\
configs\google\
```

---

## 4. Cấu hình lúc cài đặt

### Trong Setup.exe

Ngay trong `VClaw-Setup.exe`, installer sẽ hỏi bạn:

- `OPENAI_API_KEY`
- `TELEGRAM_BOT_TOKEN`
- `ALLOWED_TELEGRAM_USER_ID`

Installer sẽ tự tạo file `.env` cho bạn.

### Nếu muốn chỉnh thêm sau khi cài

Sau khi cài xong, bạn có thể mở:

```text
Edit V-Claw Config
```

để sửa file `.env` bằng Notepad nếu cần.

### Cấu hình mẫu đầy đủ hơn

```env
OPENAI_API_KEY=your_openai_api_key_here
OPENAI_MODEL=gpt-5.4-mini

TELEGRAM_BOT_TOKEN=your_telegram_bot_token_here
ALLOWED_TELEGRAM_USER_ID=123456789

VCLAW_GOOGLE_CREDENTIALS_PATH=configs/google/credentials.json
VCLAW_GOOGLE_TOKEN_PATH=configs/google/token.json
VCLAW_GOOGLE_TOOLS_MODE=auto

VCLAW_WEB_TOOLS_MODE=auto
TAVILY_API_KEY=

DATA_DIR=./data
LOG_DIR=./logs

DATABASE_URL=postgres://vclaw:vclaw@localhost:5433/vclaw?sslmode=disable
```

---

## 5. Ý nghĩa các biến quan trọng

### Luôn nên có

| Biến | Ý nghĩa |
|---|---|
| `OPENAI_API_KEY` | API key để agent hoạt động |
| `OPENAI_MODEL` | Model dùng cho runtime |

### Nếu chạy Telegram bot

| Biến | Ý nghĩa |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Token bot Telegram |
| `ALLOWED_TELEGRAM_USER_ID` | Chỉ cho phép user ID này dùng bot |

### Nếu dùng Google Workspace

| Biến | Ý nghĩa |
|---|---|
| `VCLAW_GOOGLE_CREDENTIALS_PATH` | Đường dẫn tới file `credentials.json` |
| `VCLAW_GOOGLE_TOKEN_PATH` | Đường dẫn lưu file `token.json` sau OAuth |
| `VCLAW_GOOGLE_TOOLS_MODE` | Chế độ bật/tắt Google tools, thường để `auto` |

### Nếu dùng monitoring / approvals / logs đầy đủ

| Biến | Ý nghĩa |
|---|---|
| `DATABASE_URL` | Chuỗi kết nối PostgreSQL |

### Nếu dùng sandbox shell / Python / PDF

Không cần thêm biến riêng. `Setup.exe` sẽ tự build Docker image `vclaw-sandbox:latest`.

---

## 6. Cấu hình Google Workspace

### Lưu ý quan trọng

File `credentials.json` không đủ để dùng một mình.

Bạn vẫn cần:

- Tài khoản Google thật
- Tài khoản đó có quyền với dữ liệu cần dùng
- Chạy OAuth để cấp quyền lần đầu

### Các bước cấu hình

Đặt file `credentials.json` vào:

```text
configs/google/credentials.json
```

Sau đó double-click:

```text
google-auth.bat
```

Trình duyệt sẽ mở ra để bạn đăng nhập Google.

Sau khi đăng nhập và cấp quyền thành công, V-Claw sẽ tạo file:

```text
configs/google/token.json
```

### Nếu công ty bạn dùng Google Workspace

Người dùng thường cần:

- Tài khoản thuộc tổ chức, hoặc tài khoản đã được cấp quyền phù hợp
- Quyền truy cập vào Gmail, Calendar, Drive, Docs, Sheets hoặc Chat tương ứng
- Google OAuth app được cấu hình đúng nếu tổ chức có chính sách bảo mật riêng

---

## 7. Cấu hình PostgreSQL bằng Docker

Nếu bạn chỉ muốn chạy mức cơ bản, có thể bỏ qua bước này.

Nếu bạn muốn dùng đầy đủ:

- `status`
- `logs`
- `approvals`
- monitoring liên quan DB

thì nên chạy PostgreSQL.

### Cách chạy

Mở Command Prompt hoặc PowerShell trong thư mục `VClaw`, rồi chạy:

```bash
docker compose up -d postgres
```

Kiểm tra container:

```bash
docker compose ps
```

Bạn nên thấy service `postgres` đang chạy.

---

## 8. Cấu hình Docker sandbox

Docker trong bản phát hành này có 2 vai trò:

- Chạy PostgreSQL cho `status`, `logs`, `approvals`
- Chạy sandbox cô lập cho `sandbox.runPython`, `sandbox.runShell`, `sandbox.extractPDF`

### Build sandbox image

`VClaw-Setup.exe` sẽ tự build image `vclaw-sandbox:latest` trong lúc cài đặt.

### Khi nào cần bước này

Bạn cần để Docker Desktop chạy sẵn trong lúc cài đặt nếu muốn dùng các tính năng:

- chạy Python an toàn
- chạy shell an toàn
- trích xuất PDF trong sandbox

Nếu bước build image bị lỗi, app chính vẫn có thể chạy, nhưng các tool sandbox sẽ không đầy đủ.

---

## 9. Khởi chạy V-Claw

### Thứ tự khuyên dùng

1. Nhập cấu hình ngay trong `VClaw-Setup.exe`
2. Nếu dùng Google, chạy `google-auth.bat`
3. Nếu dùng Postgres, chạy `docker compose up -d postgres`
4. Chạy `start.bat`

### Chạy runtime chính

Double-click:

```text
start.bat
```

File này sẽ chạy Telegram runtime với Google tools và web tools ở chế độ `auto`.

### Lưu ý

Giữ cửa sổ terminal mở trong suốt thời gian bot đang chạy.

Nếu đóng cửa sổ này, runtime sẽ dừng.

---

## 9. Kiểm tra trạng thái hệ thống

Sau khi runtime đã chạy, double-click:

```text
status.bat
```

Kết quả tốt thường có các trạng thái như:

```text
llm_provider ok
channel ok
tool_registry ok
```

Nếu dùng DB đầy đủ thì nên thấy:

```text
postgres ok
```

Nếu dùng Google và đã auth thành công thì nên thấy:

```text
google_oauth ok
```

---

## 10. Smoke test nhanh

Smoke test dùng để kiểm tra bản cài đặt có chạy được các luồng cơ bản hay không.

### Smoke test 1: Kiểm tra khởi động

Chạy:

```text
start.bat
```

Kỳ vọng:

- App không bị crash ngay khi mở
- Không báo thiếu `.env`
- Không báo thiếu `OPENAI_API_KEY`

### Smoke test 2: Kiểm tra status

Chạy:

```text
status.bat
```

Kỳ vọng:

- Các thành phần chính có trạng thái hợp lý
- Không có lỗi nghiêm trọng

### Smoke test 3: Kiểm tra Google auth

Chạy:

```text
google-auth.bat
```

Kỳ vọng:

- Đăng nhập Google thành công
- File sau được tạo:

```text
configs/google/token.json
```

### Smoke test 4: Kiểm tra Telegram bot

Mở Telegram và gửi một tin nhắn đơn giản cho bot, ví dụ:

```text
hôm nay tôi có lịch gì
```

Kỳ vọng:

- Bot phản hồi
- Không báo lỗi token hoặc quyền user

### Smoke test 5: Kiểm tra Gmail / Calendar

Sau khi Google auth thành công, thử các câu như:

```text
liệt kê 10 email gần đây
```

hoặc:

```text
hôm nay tôi có lịch gì
```

---

## 11. Checklist demo / bàn giao

Trước khi demo hoặc gửi cho người dùng, kiểm tra:

- [ ] `vclaw.exe` chạy được
- [ ] `.env` đã điền đúng giá trị cần thiết
- [ ] `OPENAI_API_KEY` hoạt động
- [ ] Telegram bot token đúng
- [ ] `ALLOWED_TELEGRAM_USER_ID` đúng
- [ ] Nếu dùng Google: `credentials.json` đúng chỗ
- [ ] Nếu dùng Google: OAuth đăng nhập thành công
- [ ] Nếu dùng DB: Docker Desktop chạy tốt
- [ ] Nếu dùng DB: `docker compose up -d postgres` thành công
- [ ] `status.bat` không báo lỗi nghiêm trọng
- [ ] Bot trả lời được một yêu cầu đơn giản
- [ ] Nếu demo write action: approval flow hoạt động đúng
- [ ] Không phát hành kèm secret thật

---

## 12. Cảnh báo an toàn

### Không chia sẻ các file bí mật

Không chia sẻ hoặc commit công khai các file sau:

```text
.env
configs/google/credentials.json
configs/google/token.json
```

### Không chia sẻ các secret thật

Không commit hoặc gửi công khai:

- OpenAI API key
- Telegram bot token
- Google OAuth token
- Database URL thật
- Bất kỳ credential nội bộ nào khác

### Lưu ý vận hành

Các hành động có side effect cần được kiểm tra cẩn thận, ví dụ:

- Gửi email
- Sửa dữ liệu
- Tạo event
- Ghi vào Google Docs / Sheets
- Gửi tin nhắn qua bot
- Chạy shell / sandbox

Không nên dùng bản này như production app cho số đông nếu chưa test kỹ.

Với file upload, shell, sandbox hoặc write action, luôn coi đây là vùng nhạy cảm.

---

## 13. Các lỗi thường gặp và cách xử lý

### Lỗi 1: App mở lên rồi tắt ngay

Nguyên nhân thường gặp:

- Thiếu `OPENAI_API_KEY`
- Chưa tạo `.env`
- `.env` đặt sai chỗ
- Token Telegram sai
- Runtime lỗi khi khởi động

Cách xử lý:

1. Kiểm tra đã copy `.env.example` thành `.env`
2. Kiểm tra `OPENAI_API_KEY`
3. Kiểm tra `TELEGRAM_BOT_TOKEN`
4. Chạy lại `start.bat`
5. Đọc lỗi hiển thị trên màn hình terminal

---

### Lỗi 2: `status.bat` báo không kết nối được

Nguyên nhân thường gặp:

- Chưa chạy `start.bat`
- Runtime đã bị tắt
- Monitoring server chưa được mở
- Port hoặc service liên quan chưa sẵn sàng

Cách xử lý:

1. Chạy `start.bat` trước
2. Giữ cửa sổ runtime mở
3. Chạy lại `status.bat`

---

### Lỗi 3: Google không hoạt động

Nguyên nhân thường gặp:

- Chưa có `credentials.json`
- Đặt sai vị trí `credentials.json`
- Chưa chạy `google-auth.bat`
- Tài khoản Google không có quyền dữ liệu
- Token cũ bị lỗi hoặc hết hạn

Cách xử lý:

1. Đặt đúng file vào:

```text
configs/google/credentials.json
```

2. Chạy lại:

```text
google-auth.bat
```

3. Nếu vẫn lỗi, xóa file sau rồi auth lại:

```text
configs/google/token.json
```

---

### Lỗi 4: Telegram bot không phản hồi

Nguyên nhân thường gặp:

- `TELEGRAM_BOT_TOKEN` sai
- `ALLOWED_TELEGRAM_USER_ID` sai
- Bot chưa chạy
- Bạn đang nhắn bằng tài khoản Telegram khác
- Runtime đã bị tắt

Cách xử lý:

1. Kiểm tra token bot
2. Kiểm tra đúng Telegram user ID
3. Chắc chắn `start.bat` đang chạy
4. Thử nhắn lại từ đúng tài khoản owner

---

### Lỗi 5: Docker / PostgreSQL không chạy

Nguyên nhân thường gặp:

- Chưa cài Docker Desktop
- Docker Desktop chưa được start
- Cổng đang bị chiếm
- Container PostgreSQL chưa lên

Cách xử lý:

1. Mở Docker Desktop
2. Chạy:

```bash
docker compose up -d postgres
```

3. Kiểm tra:

```bash
docker compose ps
```

---

### Lỗi 6: Có `DATABASE_URL` nhưng status / logs / approvals vẫn lỗi

Nguyên nhân thường gặp:

- PostgreSQL chưa chạy thật
- DB URL sai port
- Schema chưa được apply
- Runtime không đọc đúng `.env`
- Container chưa healthy

Cách xử lý:

1. Kiểm tra lại `DATABASE_URL`
2. Kiểm tra container PostgreSQL:

```bash
docker compose ps
```

3. Nếu cần, apply schema theo hướng dẫn dev của nhóm phát hành
4. Khởi động lại runtime bằng `start.bat`

---

### Lỗi 7: Bot trả lời lỗi model / provider

Nguyên nhân thường gặp:

- OpenAI API key sai
- Model không tồn tại
- Tài khoản không được cấp quyền dùng model đó
- Hết quota
- Billing issue

Cách xử lý:

1. Kiểm tra `OPENAI_API_KEY`
2. Thử model ổn định hơn
3. Kiểm tra quota và billing của tài khoản API
4. Chạy lại runtime

---

## 14. Khuyến nghị sử dụng thực tế

### Nếu bạn là người dùng mới

Nên làm theo thứ tự:

1. Cấu hình `.env`
2. Chạy `google-auth.bat` nếu cần Google
3. Chạy `start.bat`
4. Chạy `status.bat`
5. Thử một yêu cầu đơn giản trước

Ví dụ:

```text
hôm nay tôi có lịch gì
```

hoặc:

```text
liệt kê 10 email gần đây
```

### Nếu bạn chỉ demo

Nên chuẩn bị trước:

- `.env` đã điền sẵn
- Docker Desktop đã mở
- PostgreSQL đã chạy nếu cần DB
- Google auth đã hoàn tất
- Telegram bot đã test trước
- Approval flow đã test nếu demo write action

Nên test ít nhất một lần trước giờ demo.

---

## 15. Khi nào cần hỗ trợ thêm?

Liên hệ người phát hành nếu:

- Bạn không có `credentials.json`
- Bạn không có quyền Google Workspace cần thiết
- Bot không lên dù `.env` đã đúng
- Docker / PostgreSQL không chạy được
- Approval flow hoạt động sai
- Runtime trả lỗi lặp lại dù đã kiểm tra API key và OAuth
- Không rõ nên cấu hình Google OAuth như thế nào

---

## 16. Ghi chú phát hành

Bản Windows này nên được phát hành kèm:

```text
vclaw.exe
.env.example
start.bat
status.bat
google-auth.bat
docker-compose.yml
configs/google/
README hoặc WINDOWS_QUICKSTART.md
```

Không phát hành kèm:

```text
.env
configs/google/credentials.json
configs/google/token.json
logs/
data/telegram_offset.txt
```

Các file secret nên do người dùng tự tạo hoặc được cấp riêng qua kênh an toàn.
