# Testing E2E

Thư mục `testing-e2e/` chứa bộ usecase chạy agent thật qua `cmd/vclaw`, kèm fixture, seed session, artifact và script đánh giá.

## Mục tiêu

- Kiểm tra end-to-end behavior của agent ở mức user-facing.
- Bám theo flow thật: prompt, tool call, approval, attachment, memory, seeded follow-up.
- Lưu artifact JSON để có thể rerun, review, hoặc chấm lại bằng LLM evaluator.
- Hỗ trợ cả happy path lẫn negative/error path có chủ đích.

## Cấu trúc thư mục

```text
testing-e2e/
├── README.md
├── usecases/                  # file JSON nguồn để chạy
├── fixtures/                  # file local dùng cho attachment/file tests
├── sessions/                  # seed session cho memory/follow-up cases
├── artifacts/usecases/        # kết quả sau mỗi lần chạy
└── scripts/
    ├── run_agent_usecase.py
    └── evaluate_agent_expectations.py
```

### Các thư mục con đang dùng

- `fixtures/read-uploaded-file/`: fixture đọc file local.
- `fixtures/uploaded-file-prompt-injection-safe/`: fixture prompt injection.
- `fixtures/uploaded-file-safety/`: fixture fake PDF / file safety.
- `fixtures/uploaded-to-sheets/`: fixture CSV import.
- `sessions/memory-seeded-recall/`: seed memory cơ bản.
- `sessions/calendar-summary-email-seeded-followup/`: seed follow-up từ email/lịch.
- `sessions/drive-folder-summary-chat-seeded-followup/`: seed follow-up từ Drive/Chat.
- `artifacts/usecases/`: artifact per usecase và báo cáo evaluator.

## Script chính

### 1. Chạy usecase thật

Script: [testing-e2e/scripts/run_agent_usecase.py](/home/nxhai/V_Claw/testing-e2e/scripts/run_agent_usecase.py:1)

Lệnh cơ bản:

```bash
rtk .venv/bin/python testing-e2e/scripts/run_agent_usecase.py
```

Chạy một file:

```bash
rtk .venv/bin/python testing-e2e/scripts/run_agent_usecase.py --usecase testing-e2e/usecases/chat-to-calendar-and-docs.json
```

Chạy cả thư mục:

```bash
rtk .venv/bin/python testing-e2e/scripts/run_agent_usecase.py --usecase testing-e2e/usecases
```

Option đang hỗ trợ:

- `--usecase`: file JSON hoặc thư mục usecase.
- `--artifact-dir`: nơi ghi artifact. Mặc định là `testing-e2e/artifacts/usecases`.
- `--skip-passed`: bỏ qua case đã có artifact pass.
- `--skip-existing`: bỏ qua case đã có artifact, bất kể pass/fail.
- `--session`: ép dùng session ID cố định.
- `--channel`: ép channel. Mặc định là `eval-cmd` nếu usecase không override.
- `--timeout-seconds`: timeout mỗi step. Mặc định `300`.

### 2. Chấm lại expectation bằng LLM

Script: [testing-e2e/scripts/evaluate_agent_expectations.py](/home/nxhai/V_Claw/testing-e2e/scripts/evaluate_agent_expectations.py:1)

Lệnh:

```bash
rtk .venv/bin/python testing-e2e/scripts/evaluate_agent_expectations.py
```

Script này:

- đọc toàn bộ artifact trong `testing-e2e/artifacts/usecases`
- chỉ chấm các run mà source artifact đã pass
- dùng `agent.expectation` trong usecase làm ground truth mềm
- ghi summary vào `testing-e2e/artifacts/usecases/agent-evaluation-report.json`

## Environment

Runner tự load env từ:

- repo `.env`
- `testing-e2e/.env`

Mặc định usecase yêu cầu `OPENAI_API_KEY`, trừ khi top-level `requiredEnv` override.

## Schema usecase thực tế

Runner chấp nhận 2 dạng:

### Dạng list

```json
[
  {
    "step": 0,
    "user": {
      "message": "..."
    },
    "agent": {
      "expectation": "...",
      "requires_approval": false,
      "expected_tools": [],
      "expected_approval_tool": null,
      "response_contains": []
    }
  }
]
```

### Dạng object có metadata

```json
{
  "feature": "periodic_workflow",
  "workflow": {
    "id": "weekly-calendar-event",
    "trigger": "schedule",
    "cron": "0 8 * * MON",
    "timezone": "Asia/Ho_Chi_Minh",
    "frequency": "weekly"
  },
  "steps": [
    {
      "step": 0,
      "user": {
        "message": "..."
      },
      "agent": {
        "expectation": "...",
        "requires_approval": true,
        "expected_tools": ["calendar.createEvent"],
        "expected_approval_tool": "calendar.createEvent",
        "response_contains": []
      }
    }
  ]
}
```

## Field guide

### Top-level object

Các field top-level runner hiện dùng hoặc cho phép mang metadata:

- `steps`: danh sách step thực thi.
- `feature`: metadata phân loại feature, ví dụ `periodic_workflow`.
- `workflow`: metadata workflow định kỳ hoặc orchestration.
- `sessionPrefix`: prefix session ID khi auto-generate.
- `channel`: override channel mặc định.
- `requiredEnv`: danh sách env bắt buộc cho usecase đó.
- `variables`: biến nội suy `${VAR}` vào message hoặc attachment path.
- `agentFlags`: cấu hình thêm cho `cmd/vclaw agent`, ví dụ `dataDir`, `googleTools`, `webTools`, `credentials`, `googleToken`.

### Step-level

- `step`: số thứ tự step.
- `allowed_exit_codes`: optional. Dùng cho negative case khi runtime cố ý trả lỗi, ví dụ `[1]`.
- `user.message`: prompt gửi vào agent.
- `user.attachments`: danh sách file đính kèm local cho step đó.
- `agent.expectation`: mô tả ngắn kỳ vọng nghiệp vụ ở step.
- `agent.requires_approval`: step này có phải dừng ở approval không.
- `agent.expected_tools`: các tool phải xuất hiện trong trace.
- `agent.expected_approval_tool`: tool đang chờ approve/reject ở step.
- `agent.expected_status`: ép status cụ thể như `need_clarification` hoặc `failed`.
- `agent.response_contains`: chuỗi phải có trong câu trả lời user-facing hoặc fallback error text.

## Rule quan trọng của runner

- Mỗi step chỉ được có một trong hai:
  - `user.message`
  - `user.attachments`
- Không được trộn message và attachment trong cùng step.
- Attachment path có thể là:
  - path tương đối từ file usecase
  - path từ repo root
- Với file ngoài workspace sandbox, runner sẽ copy vào `.sandbox-workspace/agent/workspace/data/e2e_attachments/...`
- Biến attachment được inject tự động:
  - `${ATTACHMENT_COUNT}`
  - `${ATTACHMENT_1_PATH}`, `${ATTACHMENT_1_FILENAME}`, `${ATTACHMENT_1_SOURCE}`
  - `${LAST_ATTACHMENT_PATH}`, `${LAST_ATTACHMENT_FILENAME}`, `${LAST_ATTACHMENT_SOURCE}`
- Biến approval được inject giữa các step:
  - `${LAST_APPROVAL_ID}`
  - `${LAST_APPROVAL_TOOL_CALL_ID}`
  - `${LAST_APPROVAL_TOOL_NAME}`

## Seeded sessions

Nếu có thư mục seed trùng tên usecase trong `testing-e2e/sessions/<usecase-name>/`, runner sẽ copy sang `data/sessions/<sessionId>/` trước step đầu.

Use để test:

- memory recall
- follow-up question trên cùng session
- reuse context từ tool result trước đó
- transcript continuity

## Negative cases

Runner hiện support negative/error case theo 2 lớp:

- `allowed_exit_codes`: cho phép step được xem là hợp lệ dù command trả exit code khác `0`
- fallback raw output: nếu command fail và không trả JSON, runner sẽ lấy `stderr/stdout` làm `agent` text để tiếp tục so expectation

Phù hợp cho các case như:

- file không tồn tại
- provider/tool trả `not found`
- error path có chủ đích mà agent không được bịa kết quả

Ví dụ: [testing-e2e/usecases/read-file-not-found.json](/home/nxhai/V_Claw/testing-e2e/usecases/read-file-not-found.json:1)

## Artifact format

Sau mỗi run, artifact được ghi vào `testing-e2e/artifacts/usecases/<usecase>.json`.

Artifact thường chứa:

- `runId`
- `usecase`
- `sessionId`
- `startedAt`
- `finishedAt`
- `conversation`
- `passed`
- `failureReason`
- `failedStep`
- `seedSession`

Mỗi turn trong `conversation` có thể chứa:

- `step`
- `passed`
- `failureReason`
- `user.message`
- `user.attachments`
- `agent.message`
- `agent.status`
- `agent.approvalId`
- `agent.approvalTool`
- `agent.artifact`
- `agent.tools`
- `agent.toolTrace`
- `agent.trace`

## Catalog usecase hiện có

### Memory / seeded follow-up

- `memory-seeded-recall.json`
- `calendar-summary-email-seeded-followup.json`
- `drive-folder-summary-chat-seeded-followup.json`

### HITL / approval / reject / revise

- `approval-revise-then-approve.json`
- `calendar-create-event-reject.json`
- `gmail-send-clarify-then-approve.json`

### Periodic workflow

- `calendar-summary-email.json`

### Multi-step orchestration

- `chat-to-calendar-and-docs.json`
- `docs-update-from-email.json`
- `gmail-read-then-draft.json`
- `gmail-reply-with-drive-context.json`
- `drive-folder-summary-to-chat.json`
- `drive-pdf-to-docs-report.json`

### Google Workspace read / write happy paths

- `chat-read-messages-happy.json`
- `docs-get-document-happy.json`
- `drive-list-files-happy.json`
- `sheets-read-values-happy.json`

### File upload / local file handling

- `read-uploaded-file.json`
- `uploaded-file-to-docs.json`
- `uploaded-file-to-drive.json`
- `uploaded-file-to-gmail-draft.json`
- `uploaded-file-to-sheets.json`

### File safety / negative

- `uploaded-file-prompt-injection-safe.json`
- `uploaded-file-safety-check.json`
- `read-file-not-found.json`

## Khi nào nên tạo file mới thay vì sửa file cũ

- Muốn thêm `reject/cancel flow` cho một feature đang chỉ có happy path.
- Muốn giữ một happy case ổn định và tách error/edge sang file riêng.
- Muốn thêm negative case có `allowed_exit_codes`.
- Muốn tạo follow-up case có seed session riêng.

## Gợi ý naming

- `*-happy.json`: happy path.
- `*-reject.json`: reject/cancel flow.
- `*-edge.json`: edge case nhưng không nhất thiết command fail.
- `*-not-found.json`: negative case kiểu missing resource.
- `*-seeded-followup.json`: follow-up dựa trên seed session.

## Ví dụ workflow đang ổn định

- Reject flow pass: [testing-e2e/usecases/calendar-create-event-reject.json](/home/nxhai/V_Claw/testing-e2e/usecases/calendar-create-event-reject.json:1)
- Negative file-not-found pass nhờ harness mới: [testing-e2e/usecases/read-file-not-found.json](/home/nxhai/V_Claw/testing-e2e/usecases/read-file-not-found.json:1)
- Periodic workflow cơ bản: [testing-e2e/usecases/calendar-summary-email.json](/home/nxhai/V_Claw/testing-e2e/usecases/calendar-summary-email.json:1)

## Lưu ý thực tế

- Một số case phụ thuộc provider thật nên có thể flaky nếu token, quyền Drive/Gmail/Calendar, hoặc backend local đang lỗi.
- Với negative case, ưu tiên test các tình huống deterministic như `not found`, policy block, hoặc attachment/path không hợp lệ.
- Nếu muốn đánh giá chất lượng ngôn ngữ/cách diễn đạt sau khi run pass, chạy thêm evaluator để có `agent-evaluation-report.json`.
