<!-- Historical demo note: this file records Sprint 2 manual/demo scenarios. Prefer the top-level README, docs/README.md, docs/runbook.md, and SMOKE_TEST_GUIDE.md for current install/start commands. -->

# Kịch Bản Demo Test Thật — Tùng (self-creation98)

> Ngày: 2026-06-17

---

## Tùng đã implement gì?

### Backlog Tasks (N3 - Tools & Connectors)

| # | Task | Priority | DoD tóm tắt |
|---|---|---|---|
| 1 | **Chuẩn hóa và mở rộng `tool registry`** | high | Tool definition thống nhất name/description/schema/group/capability/risk/timeout/enabled/approval; các entrypoint dùng cùng registration path |
| 2 | **Chuẩn hóa Web Search và internal tools** | medium | Tavily ổn định; sandbox.runPython/runShell hỗ trợ lọc file, gom thư mục; internal/file tools hoạt động; tools gom vào groups |

### Git Commits (không tính merge)

| Ngày | Commit | Nội dung |
|---|---|---|
| 30/05 | `f17db1a` | Calendar connector + tools (events CRUD, types, conflict detection) |
| 01/06 | `f288e95` | Kết nối API Google Calendar (connector client) |
| 01/06 | `a44be84` | Calendar tool theo contract (adapter pattern) |
| 09/06 | `7666244` | Thêm `Group` field vào tool registry + CLI `tools list` command |
| 10/06 | `22b2a8b` | Filesystem tools (listDir, readFile, fileInfo, writeFile) + PathGuard |

### Files đã code/chạm vào

```
internal/tools/registry.go              ← Tool registry core + Group field
internal/tools/registry_test.go
internal/tools/builtin.go               ← Cập nhật builtin tools
cmd/vclaw/tools.go                      ← CLI: vclaw tools list
cmd/vclaw/main.go                       ← Entry point wiring

internal/connectors/google/calendar/    ← Calendar connector (client, events, types)
internal/tools/office/calendar/         ← Calendar tool (tool, adapter, tests)

internal/tools/os/filesystem/           ← Filesystem tools (tool, guard, tests)

internal/tools/web/tool.go              ← Cập nhật Group field
internal/tools/office/gmail/tool.go     ← Cập nhật Group field
internal/tools/office/chat/tool.go      ← Cập nhật Group field
internal/tools/office/people/tool.go    ← Cập nhật Group field
internal/tools/system/sandbox/tool.go   ← Cập nhật Group field
```

---

## Chuẩn Bị Môi Trường

Trước khi test, đảm bảo các điều kiện sau:

### Bắt buộc
```bash
# 1. Build thành công
go build ./cmd/vclaw

# 2. File .env có ít nhất
OPENAI_API_KEY=sk-...
```

### Cho Calendar test
```bash
# Google OAuth đã setup
VCLAW_GOOGLE_CREDENTIALS_PATH=configs/google/credentials.json
VCLAW_GOOGLE_TOKEN_PATH=configs/google/token.json

# Chạy auth nếu chưa có token
vclaw google auth
```

### Cho Web Search test
```bash
TAVILY_API_KEY=tvly-...
```

### Cho Telegram test (nếu test qua channel)
```bash
TELEGRAM_BOT_TOKEN=...
ALLOWED_TELEGRAM_USER_ID=...
```

---

## Kịch Bản 1: Tool Registry & CLI `tools list`

> **Mục đích**: Kiểm tra tool registry hoạt động — tất cả tools đã đăng ký đúng metadata (name, group, risk, approval)
> **Commit liên quan**: `7666244`

### Bước test

**1.1. Chạy CLI liệt kê tools:**
```bash
vclaw tools list
```

**Kỳ vọng**: Hiện danh sách tất cả tools, mỗi tool có:
- Name (vd: `gmail.listEmails`, `calendar.createEvent`, `filesystem.readFile`)
- Group (vd: `google_workspace`, `filesystem`, `web`, `builtin`, `sandbox`)
- Risk Level (vd: `safe_read`, `external_write`, `code_execution`)
- Requires Approval (yes/no)

**1.2. Lọc theo group:**
```bash
vclaw tools list --group filesystem
vclaw tools list --group google_workspace
vclaw tools list --group web
vclaw tools list --group sandbox
vclaw tools list --group builtin
```

### Checklist

- [ ] `tools list` chạy không lỗi, hiện danh sách
- [ ] Mỗi tool có đủ: name, group, risk level, approval requirement
- [ ] `--group filesystem` chỉ hiện filesystem tools
- [ ] `--group google_workspace` hiện Gmail/Calendar/Chat/Drive/Docs/Sheets/People
- [ ] `--group web` hiện web.search, web.fetch
- [ ] `--group builtin` hiện get_current_time, calculator
- [ ] `--group sandbox` hiện sandbox tools
- [ ] Không có tool nào bị thiếu group hoặc group sai

---

## Kịch Bản 2: Filesystem Tools

> **Mục đích**: Test listDir, readFile, fileInfo, writeFile hoạt động thật + PathGuard bảo vệ đúng
> **Commit liên quan**: `22b2a8b`, `7f2da5f`

### Test qua Telegram (khuyến nghị)

Khởi động bot:
```bash
vclaw telegram run
```

**2.1. listDir — Liệt kê thư mục**

Gửi trên Telegram:
```
Liệt kê các file trong thư mục workspace hiện tại
```

**Kỳ vọng**:
- Agent gọi tool `filesystem.listDir` hoặc tương đương
- Risk level = `safe_read` → **không cần approval** (auto-allow)
- Trả về danh sách files/folders

---

**2.2. readFile — Đọc file**

Gửi:
```
Đọc nội dung file [tên file] trong workspace
```

**Kỳ vọng**:
- Agent gọi `filesystem.readFile`
- Risk = `safe_read` → auto-allow
- Trả về nội dung file

---

**2.3. fileInfo — Thông tin file**

Gửi:
```
Cho tôi biết thông tin chi tiết file go.mod (kích thước, ngày sửa)
```

**Kỳ vọng**:
- Agent gọi `filesystem.fileInfo`
- Trả về metadata: size, modified date, permissions

---

**2.4. writeFile — Ghi file (CẦN APPROVAL)**

Gửi:
```
Tạo file test-output.txt với nội dung "Hello from V-Claw"
```

**Kỳ vọng**:
- Agent gọi `filesystem.writeFile`
- Risk = `local_write` → **BẮT BUỘC approval**
- Hiện preview: tên file, nội dung, path
- Nhấn **Approve** → file được tạo
- Kiểm tra file thật tồn tại trên disk

---

**2.5. PathGuard — Chặn truy cập ngoài workspace**

Gửi:
```
Đọc nội dung file C:\Windows\System32\config\system
```

**Kỳ vọng**:
- PathGuard chặn → trả lỗi, không đọc file hệ thống
- Không có approval request (bị block ngay)

### Checklist

- [ ] `listDir` hoạt động, trả kết quả đúng
- [ ] `readFile` đọc được nội dung file trong workspace
- [ ] `fileInfo` trả metadata đúng
- [ ] `writeFile` **bắt buộc approval** trước khi ghi
- [ ] Approve → file được tạo thật trên disk
- [ ] Reject → file KHÔNG được tạo
- [ ] PathGuard chặn path ngoài workspace (C:\Windows, C:\Users\...\AppData, v.v.)
- [ ] Tool result format nhất quán cho LLM

---

## Kịch Bản 3: Calendar Connector & Tools

> **Mục đích**: Test Google Calendar integration end-to-end
> **Commit liên quan**: `f17db1a`, `f288e95`, `a44be84`
> **Yêu cầu**: Google OAuth đã setup với Calendar scope

### Test qua CLI (nhanh, không cần Telegram)

**3.1. Liệt kê sự kiện:**
```bash
vclaw google drive list
# Nếu có calendar CLI:
vclaw agent -prompt "Liệt kê lịch của tôi trong tuần này"
```

### Test qua Telegram (full flow)

Khởi động bot:
```bash
vclaw telegram run --google-tools auto --web-tools auto
```

**3.2. listEvents — Đọc lịch (safe_read)**

Gửi:
```
Xem lịch của tôi ngày mai có gì không
```

**Kỳ vọng**:
- Agent gọi `calendar.listEvents`
- Risk = `safe_read` → **auto-allow**, không cần approval
- Trả về danh sách events hoặc "không có lịch"

---

**3.3. createEvent — Tạo lịch (APPROVAL REQUIRED)**

Gửi:
```
Tạo lịch họp "V-Claw Demo Test" lúc 4h chiều mai, khoảng 30 phút
```

**Kỳ vọng**:
- Agent có thể hỏi lại (clarify) nếu thiếu info
- Agent gọi `calendar.listEvents` kiểm tra trùng (auto-allow)
- Agent đề xuất `calendar.createEvent` → Risk = `external_write` → **APPROVAL**
- Preview hiện: title, time, duration
- **Approve** → Event được tạo thật trên Google Calendar
- **Mở Google Calendar kiểm tra** event có xuất hiện

---

**3.4. createEvent — Reject flow**

Gửi lại yêu cầu tạo lịch, nhưng lần này nhấn **Reject**.

**Kỳ vọng**:
- Event KHÔNG được tạo
- Agent thông báo đã hủy

---

**3.5. Conflict detection**

Tạo 1 event thật trên Google Calendar ở khung giờ cụ thể, rồi gửi:
```
Tạo lịch họp lúc [giờ đã có event] mai
```

**Kỳ vọng**:
- Agent phát hiện conflict
- Gợi ý slot khác hoặc hỏi xác nhận

---

**3.6. deleteEvent (nếu có)**

Gửi:
```
Xóa lịch họp "V-Claw Demo Test" vừa tạo
```

**Kỳ vọng**:
- Risk = `destructive` hoặc `external_write` → **APPROVAL**
- Approve → Event bị xóa thật

### Checklist

- [ ] `calendar.listEvents` auto-allow, trả kết quả đúng
- [ ] `calendar.createEvent` bắt buộc approval
- [ ] Approval preview có đủ: title, time, duration, attendees
- [ ] Approve → event tạo thật trên Google Calendar
- [ ] Reject → event KHÔNG tạo
- [ ] Conflict detection hoạt động (phát hiện trùng giờ)
- [ ] Clarification hoạt động khi thiếu thông tin
- [ ] OAuth error handling (thử với token expired)
- [ ] `calendar.deleteEvent` cần approval

---

## Kịch Bản 4: Web Search & Fetch (Tavily)

> **Mục đích**: Test web tools hoạt động ổn định
> **Commit liên quan**: `7666244` (Group field update)
> **Yêu cầu**: `TAVILY_API_KEY` đã set

### Test qua Telegram

```bash
vclaw telegram run --google-tools auto --web-tools auto
```

**4.1. web.search — Tìm kiếm web**

Gửi:
```
Messi đã ghi bao nhiêu bàn thắng ở WC 2026
```

**Kỳ vọng**:
- Agent gọi `web.search`
- Risk = `safe_read` → auto-allow
- Trả về kết quả tìm kiếm với tiêu đề, URL, snippet

---

**4.2. web.fetch — Đọc nội dung web**

Gửi:
```
Đọc nội dung trang https://go.dev/doc/ và tóm tắt cho tôi
```

**Kỳ vọng**:
- Agent gọi `web.fetch`
- Risk = `safe_read` → auto-allow
- Trả về nội dung trang web đã parse

---

**4.3. Error handling — Thiếu API key**

Tắt `TAVILY_API_KEY`, khởi động lại với `--web-tools auto`:

**Kỳ vọng**:
- Web tools không được đăng ký (mode=auto, key missing → skip)
- Gửi yêu cầu tìm kiếm → Agent trả lời không có tool web, không crash

### Checklist

- [ ] `web.search` hoạt động, trả kết quả đúng format
- [ ] `web.fetch` đọc được nội dung trang web
- [ ] Result shape nhất quán (có title, content, URL)
- [ ] Auto-allow (safe_read), không cần approval
- [ ] Thiếu Tavily key + mode=auto → tools bị skip, không crash
- [ ] Thiếu Tavily key + mode=required → startup fail (đúng hành vi)

---

## Kịch Bản 5: Sandbox runShell / runPython

> **Mục đích**: Test sandbox execution tools
> **Commit liên quan**: `7666244` (Group field)
> **Lưu ý**: Sandbox tools cần Docker nếu dùng Docker runner, hoặc chạy local

### Test qua Telegram

```bash
vclaw telegram run --google-tools auto --web-tools auto
```

**5.1. runShell — Chạy lệnh đơn giản (APPROVAL)**

Gửi:
```
Chạy lệnh "dir" để xem thư mục hiện tại
```

**Kỳ vọng**:
- Agent đề xuất `sandbox.runShell`
- Risk = `code_execution` → **BẮT BUỘC approval**
- Preview hiện: command sẽ chạy, working directory
- Approve → lệnh chạy, trả stdout
- Reject → không chạy

---

**5.2. runPython — Chạy script Python (APPROVAL)**

Gửi:
```
Viết và chạy script Python in ra "Hello World" và tính 2+2
```

**Kỳ vọng**:
- Agent đề xuất `sandbox.runPython`
- Risk = `code_execution` → **APPROVAL**
- Preview hiện: script sẽ chạy
- Approve → script chạy → trả output "Hello World\n4"

---

**5.3. Dangerous command — Block**

Gửi:
```
Chạy lệnh xóa tất cả file: rm -rf /
```

**Kỳ vọng**:
- Safety detector / sandbox policy **BLOCK** ngay
- Không tạo approval request (bị chặn ở policy layer)
- Risk = `destructive` → always_block
- Trả thông báo bị chặn

---

**5.4. Timeout handling**

Gửi:
```
Chạy script Python: import time; time.sleep(300)
```

**Kỳ vọng**:
- Approve → script chạy → bị timeout
- Trả lỗi timeout rõ ràng

### Checklist

- [ ] `sandbox.runShell` bắt buộc approval
- [ ] `sandbox.runPython` bắt buộc approval
- [ ] Approve → command chạy, trả stdout/stderr đúng
- [ ] Reject → KHÔNG chạy
- [ ] Dangerous commands (rm -rf, crontab, etc.) bị block ngay bởi policy
- [ ] Timeout được xử lý → trả lỗi rõ
- [ ] Workspace isolation — không truy cập file ngoài sandbox
- [ ] Audit log ghi nhận execution result

---

## Tổng Hợp: Checklist Pass/Fail Cho Nhóm Trưởng

| # | Kịch bản | Số test case | Kết quả |
|---|---|---|---|
| 1 | Tool Registry & CLI | 8 | ☐ Pass / ☐ Fail |
| 2 | Filesystem Tools | 8 | ☐ Pass / ☐ Fail |
| 3 | Calendar Tools | 9 | ☐ Pass / ☐ Fail |
| 4 | Web Search & Fetch | 6 | ☐ Pass / ☐ Fail |
| 5 | Sandbox Execution | 8 | ☐ Pass / ☐ Fail |
| **Tổng** | | **39 test cases** | |

> [!TIP]
> **Thứ tự test khuyến nghị**: 1 → 2 → 4 → 3 → 5
> - Kịch bản 1 (CLI) nhanh nhất, chạy trước kiểm tra tools registry
> - Kịch bản 2 (Filesystem) không cần API key ngoài
> - Kịch bản 4 (Web) chỉ cần Tavily key
> - Kịch bản 3 (Calendar) cần Google OAuth setup
> - Kịch bản 5 (Sandbox) cần Docker hoặc local runner

> [!IMPORTANT]
> **Ghi lại bằng chứng**: Với mỗi test case, chụp screenshot Telegram hoặc copy terminal output để nhóm trưởng tổng hợp chọn lọc.
