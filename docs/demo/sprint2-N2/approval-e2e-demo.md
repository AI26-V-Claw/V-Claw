# Demo: Sprint 2 N2 Production Stress E2E

Bộ E2E N2 đã được viết lại theo hướng ít scenario hơn nhưng sâu hơn. Thay vì nhiều case nhỏ như approve/reject/cancel rời rạc, Sprint 2 hiện tập trung vào 2 stress test lớn mô phỏng agent production: nhiều bước, nhiều tool, nhiều approval, nhiều artifact, nhiều fallback.

---

## Mục Tiêu Chung

- Kiểm thử orchestration dài nhiều bước trong một session agent.
- Ép agent đọc nhiều nguồn Workspace trước khi write.
- Ép agent tạo nhiều artifact thật: Drive, Docs, Sheets, Gmail draft.
- Kiểm thử approval boundary cho nhiều `external_write` liên tiếp.
- Kiểm thử context retention: mọi artifact phải giữ `[VCLAW-E2E]` và `run_id`.
- Kiểm thử fallback khi tool thiếu env, thiếu quyền, hoặc không cleanup-safe.
- Không tính pass nếu object write không read-back được hoặc cleanup không pass.

---

## Scenario 1 — Production Briefing Mega Flow

**File**: `testing-e2e/scenarios/n2.1-production-briefing-mega-flow.json`

### Use Case

Agent nhận một yêu cầu kiểu production:

- Tìm/tổng hợp thông tin hôm nay nếu web search khả dụng.
- Audit Gmail, Calendar, Drive, và Chat nếu có quyền.
- Viết briefing tiếng Việt khoảng 300 từ.
- Tạo kế hoạch tuần tới dạng `plan.md` trong Drive.
- Copy/nâng cấp nội dung sang Google Docs.
- Tạo Google Sheets checklist/task table.
- Tạo Gmail draft thông báo demo chiều nay.
- Ghi rõ limitation/fallback cho Telegram và Calendar send nếu không cleanup-safe hoặc không có tool.

### Năng Lực Được Test

- `multi_step_orchestration`: agent phải tự chia workflow dài thành các bước hợp lý.
- `parallel_or_subtask_delegation_when_available`: scenario khuyến khích dùng `spawn_subtask` nếu tool có sẵn.
- `workspace_read_audit`: bắt buộc đọc Gmail/Calendar/Drive; Chat là nhánh optional/fallback.
- `web_research_or_fallback`: nếu thiếu `TAVILY_API_KEY`, agent phải nói rõ fallback thay vì fail toàn bộ.
- `multi_artifact_generation`: phải tạo Drive file, Docs document, Sheets spreadsheet, Gmail draft.
- `approval_boundary`: mỗi write external phải đi qua approval.
- `context_retention`: artifact read-back phải chứa `run_id`.
- `cleanup_hygiene`: artifact tạo ra phải cleanup được.
- `user_handoff`: response cuối phải nêu successful tools, fallback, limitation, next steps.

### Command

```powershell
powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\run_n2_e2e.ps1" -Scenario n2.1-production-briefing-mega-flow -RunChat
```

### Pass Criteria

- `status = pass`.
- Có ít nhất 4 successful write results.
- Có ít nhất 4 objects written.
- Các object có `[VCLAW-E2E]` và `run_id`.
- Read-back pass cho artifact cleanup-safe.
- Cleanup pass.
- Có approval request cho write.
- Trace có Gmail, Calendar, Drive reads.
- Trace có Drive, Docs, Sheets, Gmail draft writes.
- Response cuối có `Telegram`, `fallback`, `15:00`, `next steps`.

---

## Scenario 2 — Resilience Continuation Mega Flow

**File**: `testing-e2e/scenarios/n2.2-resilience-continuation-mega-flow.json`

### Use Case

Agent xử lý một session dài có nhiều điểm dễ fail:

- Audit Gmail/Drive/Calendar.
- Tạo Drive `plan.md`.
- Tạo Docs narrative.
- Tạo Sheets checklist.
- Tạo Gmail draft.
- Nếu Chat, Web Search hoặc Telegram không khả dụng, không được đứng im hoặc abort sớm.
- Agent phải chọn fallback an toàn: lưu thông tin vào Docs/Gmail draft và giải thích rõ.

### Năng Lực Được Test

- `long_context_retention`: giữ đúng `run_id` xuyên suốt nhiều tool call.
- `multi_turn_like_scripted_continuation`: nhiều approval liên tiếp trong một session.
- `tool_failure_recovery`: nếu một tool fail, agent phải tiếp tục bằng tool khác.
- `safe_fallback_instead_of_abort`: fallback phải rõ và an toàn.
- `artifact_traceability`: response cuối phải có artifact refs/object ids.
- `clear_user_handoff`: trả lời cuối phải có successful tools, failed/skipped tools, fallback decisions, next step.

### Command

```powershell
powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\run_n2_e2e.ps1" -Scenario n2.2-resilience-continuation-mega-flow -RunChat
```

### Pass Criteria

- `status = pass`.
- Read tools chạy trước write tools.
- Có approval request.
- Có ít nhất 4 successful writes và 4 objects written.
- Tất cả artifact cleanup-safe đều read-back chứa `run_id`.
- Cleanup pass.
- Response cuối có `successful tools`, `failed`, `fallback`, `artifact`, `next`.

---

## Dry-Run Verification

Đã kiểm tra dry-run cho cả 2 scenario:

```text
n2.1-production-briefing-mega-flow => DryRun OK
n2.2-resilience-continuation-mega-flow => DryRun OK
```

Dry-run chỉ xác nhận scenario/env/harness load được. Real pass chỉ được claim sau khi chạy `-RunChat`, read-back pass và cleanup pass.

---

## Lưu Ý Thiết Kế

- Telegram hiện không phải direct agent chat tool trong registry, nên scenario ép agent phải nhận diện limitation và dùng fallback thay vì giả vờ đã gửi Telegram.
- Calendar write thật chưa được bật làm hard requirement vì harness chưa có cleanup Calendar event an toàn qua CLI. Scenario yêu cầu tạo event proposal 15:00 trong artifacts thay vì tạo event thật không cleanup được.
- Chat write có thể thiếu quyền trong fixture hiện tại; scenario xem Chat là nhánh optional/fallback, nhưng vẫn bắt agent audit hoặc nêu limitation.
- Web search phụ thuộc `TAVILY_API_KEY`; nếu thiếu env, agent phải fallback và giải thích.

---

## Artifact Validation

Sau khi chạy thật:

```powershell
powershell -ExecutionPolicy Bypass -File "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\scripts\validate_artifact.ps1" -SummaryPath "D:\01_learning\ai_ml\AI20K_VINUNI\V-Claw\testing-e2e\artifacts\<run_id>\summary.json"
```

Không claim pass nếu:

- `readiness_counted = false`.
- Có hard assertion `fail`, `pending_verification`, hoặc `blocked_env`.
- `objects_written` ít hơn minimum của scenario.
- Object không có `run_id` khi read-back.
- Cleanup không pass.
