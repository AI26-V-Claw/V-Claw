# Demo: N2 Approval & E2E Harness — Sprint 2

Kịch bản này kiểm chứng Milestone N2 của V-Claw: approval boundary, scripted E2E harness, read/write/read-back/cleanup với Google Workspace, và các nhánh lifecycle không được tạo side-effect ngoài ý muốn.

---

## Prerequisites

- Google OAuth đã cấu hình cho account test.
- File fixture tồn tại: `testing-e2e/e2e.env.ps1`.
- Policy hiện tại yêu cầu approval cho `external_write`.
- Chạy command từ repo root.
- Nếu chạy N2.4 full lifecycle với Postgres, cần set `DATABASE_URL`.

Kiểm tra nhanh env fixture:

```powershell
. testing-e2e/e2e.env.ps1
$env:VCLAW_E2E_TARGET_EMAIL
$env:VCLAW_E2E_CALENDAR_ID
$env:VCLAW_E2E_DRIVE_FOLDER_ID
$env:VCLAW_E2E_CHAT_SPACE
```

---

## S1 — Golden Workspace Flow

**Mục tiêu**: Xác nhận agent thật sự đọc Gmail/Calendar/Drive, request approval đúng policy `external_write`, execute write sau approve, read-back object, cleanup object, và summary không false-positive.

### Bước

1. Chạy:
   ```powershell
   powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\run_n2_e2e.ps1" -Scenario n2.1-golden-workspace-flow -RunChat
   ```
2. Ghi lại `run_id` và path `summary.json` được in ra.
3. Validate artifact:
   ```powershell
   powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\validate_artifact.ps1" -SummaryPath "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\artifacts\<run_id>\summary.json"
   ```

### Kết quả kỳ vọng

- `status = pass`.
- `readiness_counted = true`.
- `objects_written` không rỗng.
- `read_back_assertions` có pass.
- `cleanup.attempted = true`.
- `cleanup.status = pass`.
- `hard_assertions` không có số rác `0..n`.
- `hard_assertions` có pass cho:
  - `expected_tool_observed:gmail.listEmails`
  - `expected_tool_observed:calendar.listEvents`
  - `expected_tool_observed:drive.listFiles`
  - `approval_required_observed`
  - `write_tool_executed`
  - `write_object_contains_run_id`
  - `object_read_back_matches_final_content`
  - `cleanup_attempted`

### Evidence đã có

Run pass gần nhất:

```text
run_id: vclaw-e2e-20260619-090957
summary: testing-e2e/artifacts/vclaw-e2e-20260619-090957/summary.json
validator: valid=true
object: 1dl1Vfx-tn0uKXVpt7qsZn5yFWzi9ehve
cleanup: pass
```

---

## S2 — Google Workspace Manual Verification

**Mục tiêu**: Xác nhận object Google Drive tạo bởi E2E đã được cleanup thật trên Workspace.

### Bước

1. Check object theo ID từ `summary.json`:
   ```powershell
   . testing-e2e/e2e.env.ps1
   go run ./cmd/vclaw google drive get -id 1dl1Vfx-tn0uKXVpt7qsZn5yFWzi9ehve
   ```
2. Mở Google Drive bằng account fixture và search object ID hoặc tên folder.

### Kết quả kỳ vọng

- Object name chứa `[VCLAW-E2E]` và `run_id`.
- Field `Trashed` là `true`, hoặc object không còn hiện trong folder fixture active.
- Folder fixture không còn rác active từ run cũ.

---

## S3 — Approval Lifecycle E2E Coverage

**Mục tiêu**: Xác nhận các nhánh approval ngoài N2.1 chạy E2E thật với Google Drive: approve+duplicate tạo đúng một side-effect, reject/cancel không tạo side-effect.

### Bước

Chạy từng scenario E2E:

```powershell
powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\run_n2_e2e.ps1" -Scenario n2.2-approval-duplicate-drive-write -RunChat
powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\run_n2_e2e.ps1" -Scenario n2.3-approval-reject-no-write -RunChat
powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\run_n2_e2e.ps1" -Scenario n2.5-approval-cancel-no-write -RunChat
```

Validate từng artifact:

```powershell
powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\validate_artifact.ps1" -SummaryPath "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\artifacts\<run_id>\summary.json"
```

### Kết quả kỳ vọng

- N2.2: có approval `external_write`, write đúng một Drive folder, duplicate approve không tạo side-effect lần hai, read-back pass, cleanup pass.
- N2.3: reject approval, không có write tool success, `objects_written` rỗng, cleanup not needed pass.
- N2.5: cancel approval, không có write tool success, `objects_written` rỗng, cleanup not needed pass.

### Evidence đã có

```text
n2.2 run_id: vclaw-e2e-20260619-100837 | status=pass | validator=true | objects=1 | read_back=pass | cleanup=pass
n2.3 run_id: vclaw-e2e-20260619-101156 | status=pass | validator=true | objects=0 | no_write=pass | cleanup_not_needed=pass
n2.5 run_id: vclaw-e2e-20260619-101237 | status=pass | validator=true | objects=0 | no_write=pass | cleanup_not_needed=pass
```

---

## S3b — Approval Lifecycle Unit Coverage

**Mục tiêu**: Bổ sung unit coverage cho nhánh khó E2E hóa ổn định nếu chưa có Postgres/time control: expire và revise.

### Bước

Chạy targeted tests:

```powershell
go test ./internal/agent -run "TestApproval(RejectDoesNotExecuteWrite|ApproveDuplicateExecutesOnce|ExpiredDoesNotExecuteWrite|ReviseCreatesReplacementApprovalWithoutExecutingOriginal)$"
```

Hoặc chạy toàn bộ agent package:

```powershell
go test ./internal/agent
```

### Kết quả kỳ vọng

- Reject/cancel semantics không execute write.
- Duplicate approve trả `APPROVAL_NOT_FOUND` và không execute lần hai.
- Expired approval trả `APPROVAL_EXPIRED` và không execute write.
- Revise tạo approval thay thế, giữ `ParentApprovalID`, dùng args revised, và chưa execute original write.

### Evidence đã có

```text
go test ./internal/agent -run "TestApproval(...)$" => ok
go test ./internal/agent => ok
```

---

## S4 — N2.4 Full Lifecycle Gate

**Mục tiêu**: Xác nhận full lifecycle scenario không được tính readiness nếu thiếu Postgres state store.

### Bước

Chạy dry-run:

```powershell
powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\run_n2_e2e.ps1" -Scenario n2.4-approval-full-lifecycle -DryRun
```

### Kết quả kỳ vọng khi chưa có DB

- `status = blocked_env`.
- `missing_env` có `DATABASE_URL`.
- `readiness_counted = false`.
- Không được claim pass.

### Evidence đã có

```text
run_id: vclaw-e2e-20260619-092036
status: blocked_env
missing_env: DATABASE_URL
readiness_counted: false
```

---

## S5 — Cleanup Orphan Fixtures

**Mục tiêu**: Không để lại folder Drive test sau các run fail/pending.

### Bước

Trash object theo ID nếu summary cũ chưa cleanup:

```powershell
. testing-e2e/e2e.env.ps1
go run ./cmd/vclaw google drive trash -id <object_id>
```

### Orphan đã cleanup

```text
1H5pxul26u0JbcYEKt1kay5gGd_PuWgXl
1oYfEOS_TtGvr8gs3lCIdHwckZszw-MZb
1MOY3vtdxb5yNsdi4oS6sMKSINM31btOG
1Dy81AEAs9RRr_b8L4HhRiwg4cx5IM_0e
1-0DEUqD9mC-gZf3dxKPeXLRlCY6MSvO-
```

---

## Demo Checklist

| Scenario | Command / Evidence | Expected |
|---|---|---|
| N2.1 golden E2E | `run_n2_e2e.ps1 -Scenario n2.1-golden-workspace-flow -RunChat` | `pass`, readiness counted |
| Artifact validator | `validate_artifact.ps1 -SummaryPath ...` | `valid=true` |
| Workspace cleanup | `go run ./cmd/vclaw google drive get -id <object>` | object trashed |
| N2.3 reject E2E | `n2.3-approval-reject-no-write -RunChat` | `pass`, no object written |
| N2.5 cancel E2E | `n2.5-approval-cancel-no-write -RunChat` | `pass`, no object written |
| N2.2 duplicate E2E | `n2.2-approval-duplicate-drive-write -RunChat` | exactly one object, cleanup pass |
| Expire | `TestApprovalExpiredDoesNotExecuteWrite` | no write, expired error |
| Revise | `TestApprovalReviseCreatesReplacementApprovalWithoutExecutingOriginal` | replacement approval |
| N2.4 DB gate | `n2.4-approval-full-lifecycle -DryRun` without DB | `blocked_env`, not readiness |

> Không claim N2 pass nếu read-back hoặc cleanup còn `pending_verification`, hoặc nếu `blocked_env` đang bị tính readiness.


