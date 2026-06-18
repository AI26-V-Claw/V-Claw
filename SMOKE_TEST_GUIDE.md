# Smoke Test Guide — Hardening Orchestration State & Failure Path

Tất cả lệnh chạy từ thư mục gốc `D:\\01_learning\\ai_ml\\AI20K_VINUNI\\V-Claw`.
Flag `-json` in full `AgentResponse` JSON — dùng để verify `failureReason` field.

---

## Chuẩn bị

```powershell
# Build trước để đảm bảo không có lỗi compile
go build -o vclaw.exe ./cmd/vclaw

# Verify app khởi động được
.\vclaw.exe help
```

---

## Scenario 1 — Happy Path (failureReason phải empty)

**Mục tiêu:** `status = completed`, `failureReason = ""` (không xuất hiện trong JSON do omitempty)

```powershell
.\vclaw.exe agent -prompt "Xin chào, bạn là ai?" -session "smoke-happy" -json
```

**Verify trong JSON output:**
- `"status": "completed"` ✓
- `"failureReason"` không xuất hiện trong JSON (omitempty khi empty) ✓

---

## Scenario 2 — Max Iteration (failureReason = max_iteration)

**Mục tiêu:** `status = max_iterations_reached`, `failureReason = "max_iteration"`

```powershell
.\vclaw.exe agent -prompt "Hãy đếm từ 1 đến 1000, mỗi số trên một dòng, không bỏ qua số nào" -session "smoke-maxiter" -max-iterations 2 -json
```

**Verify trong JSON output:**
- `"status": "max_iterations_reached"` ✓
- `"failureReason": "max_iteration"` ✓

---

## Scenario 3 — Provider Error (failureReason = provider_error)

**Mục tiêu:** `status = failed`, `failureReason = "provider_error"`

```powershell
# Dùng API key sai để trigger provider error
$env:OPENAI_API_KEY = "sk-invalid-key-for-smoke-test"
.\vclaw.exe agent -prompt "Hello" -session "smoke-provider-err" -json
```

**Restore lại sau khi test:**
```powershell
# Load lại .env bằng cách restart shell hoặc:
Remove-Item Env:OPENAI_API_KEY
.\vclaw.exe agent -prompt "test" -session "smoke-check" -json
```

**Verify trong JSON output:**
- `"status": "failed"` ✓
- `"failureReason": "provider_error"` hoặc `"provider_unavailable"` ✓

---

## Scenario 4 — Cancellation (failureReason = canceled)

**Mục tiêu:** `status = failed`, `failureReason = "canceled"`

```powershell
# Chạy lệnh dài rồi bấm Ctrl+C giữa chừng
.\vclaw.exe agent -prompt "Hãy phân tích toàn bộ lịch sử triết học từ Hy Lạp cổ đại đến hiện đại, viết chi tiết 10000 từ" -session "smoke-cancel" -json
# Bấm Ctrl+C sau khoảng 2-3 giây
```

**Verify trong JSON output (nếu kịp in trước khi cancel):**
- `"status": "failed"` ✓
- `"failureReason": "canceled"` ✓

> **Lưu ý:** Nếu cancel quá nhanh app chưa in JSON, kiểm tra log file tại `./data/logs/` hoặc `$LOG_DIR`.

---

## Scenario 5 — Approval Required + Approve (failureReason = empty khi hoàn thành)

**Mục tiêu:** Agent yêu cầu approval, sau khi approve tool tiếp tục chạy, `failureReason = ""`

```powershell
# Bước 1: Khởi động interactive chat
.\vclaw.exe agent chat -session "smoke-approval" -json

# Bước 2: Gõ lệnh cần approval (ví dụ tạo calendar event)
You> Tạo lịch họp "Test HITL" vào ngày mai lúc 10:00 sáng

# App sẽ in ApprovalRequest với Approval ID
# Ví dụ output:
# Approval ID: appr_xxx
# Expires At: 2026-06-16T15:45:00+07:00
# Reply with: approve, reject, revise <comment>

# Bước 3: Approve
You> approve

# Verify JSON output có:
# "status": "completed"
# "failureReason" không xuất hiện (empty)
```

---

## Scenario 6 — Approval Rejected (failureReason = approval_rejected)

**Mục tiêu:** `status = failed`, `failureReason = "approval_rejected"`

```powershell
.\vclaw.exe agent chat -session "smoke-reject" -json

You> Gửi email đến test@example.com với nội dung "Test reject"

# Sau khi thấy Approval ID:
You> reject

# Verify JSON output:
# "status": "failed"
# "failureReason": "approval_rejected"
```

---

## Scenario 7 — Approval Expired (failureReason = approval_expired)

**Mục tiêu:** `status = failed`, `failureReason = "approval_expired"`

> **Lưu ý:** TTL mặc định là 10 phút. Để test nhanh, có thể tìm constant `approvalTTL` trong `runtime_approval_expiry.go` và tạm thời đổi thành `30 * time.Second` rồi rebuild.

```powershell
# Tùy chọn A: Chờ 10 phút thật
.\vclaw.exe agent chat -session "smoke-expired" -json
You> Xóa file test.txt
# Thấy Approval ID → KHÔNG làm gì → chờ 10 phút
# Gửi thêm 1 tin nhắn bất kỳ để trigger clearExpiredApprovalsForSession
You> xin chào
# Verify JSON output có failureReason: "approval_expired"

# Tùy chọn B: Override TTL (khuyến nghị cho test)
# 1. Sửa runtime_approval_expiry.go: đổi TTL constant thành 30s
# 2. go build -o vclaw.exe ./cmd/vclaw
# 3. Chạy chat, trigger approval, chờ 30 giây, gửi tin nhắn tiếp
```

---

## Scenario 8 — Tool Error (failureReason = tool_error)

**Mục tiêu:** `status = failed`, `failureReason = "tool_error"`

```powershell
# Trigger tool failure bằng cách yêu cầu download attachment từ message ID không tồn tại
.\vclaw.exe agent -prompt "Download attachment từ Gmail message ID 'invalid-msg-id-999' vào thư mục ./tmp" -session "smoke-tool-err" -json -google-tools auto
```

**Verify trong JSON output:**
- `"status": "failed"` ✓
- `"failureReason": "tool_error"` ✓

---

## Scenario 9 — Revise (kiểm tra context không mất)

**Mục tiêu:** Sau khi revise, agent nhớ context và thực hiện đúng task đã sửa

```powershell
.\vclaw.exe agent chat -session "smoke-revise" -json

You> Tạo lịch họp "Sprint Review" vào ngày mai lúc 9:00 sáng
# Thấy Approval ID

You> revise đổi giờ sang 14:00 chiều
# Agent xử lý lại với giờ mới, hỏi approval lần 2

You> approve
# Verify: lịch được tạo đúng lúc 14:00
# "status": "completed"
# "failureReason" không xuất hiện
```

---

## Checklist Verify FailureReason

Sau mỗi scenario, kiểm tra JSON output:

| Scenario | Expected status | Expected failureReason |
|---|---|---|
| 1. Happy path | `completed` | *(không xuất hiện)* |
| 2. Max iteration | `max_iterations_reached` | `max_iteration` |
| 3. Provider error | `failed` | `provider_error` |
| 4. Cancellation | `failed` | `canceled` |
| 5. Approve + complete | `completed` | *(không xuất hiện)* |
| 6. Reject | `failed` | `approval_rejected` |
| 7. Expired | `failed` | `approval_expired` |
| 8. Tool error | `failed` | `tool_error` |
| 9. Revise + complete | `completed` | *(không xuất hiện)* |

---

## Xem Log Chi Tiết

```powershell
# Xem log file sau khi chạy
Get-Content .\data\logs\vclaw.log | Select-Object -Last 50

# Filter theo failure_reason
Get-Content .\data\logs\vclaw.log | Select-String "failureReason|failure_reason"
```

Paste file và chạy `go build -o vclaw.exe ./cmd/vclaw` để confirm build clean trước khi bắt đầu test.
