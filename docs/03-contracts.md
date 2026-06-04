# Contracts

> Các contract tối thiểu để Integration Team và Agent Core Team phát triển độc lập.

---

## 1. Boundary

```text
Channel -> Agent Core -> Intent/Safety -> Task Planning -> HITL -> Tool Layer -> Tool Execution -> Agent Core -> Channel
```

- Intent/Safety may return `need_clarification` before planning or tool calls.
- Task Planning creates `Plan` only; planner must not execute tools.
- Channel approval UI, such as Telegram/Slack buttons or modal comments, must resolve to `ApprovalDecision`.

- Channel chuẩn hóa input thành `UserMessage`.
- Agent Core chỉ gọi tool qua `ToolCall`.
- Tool trả kết quả qua `ToolResult`.
- Action có side effect phải có `RiskDecision`.
- Nếu `requiresApproval = true`, tool không được execute trước khi có `ApprovalDecision = approved`.
- MVP là single-owner deployment; không dùng `userId` trong runtime contract.

---

## 2. Naming

### ID

```text
requestId
sessionId
toolCallId
approvalId
```

### Time

```text
ISO-8601, ví dụ: 2026-05-29T09:00:00+07:00
```

### Tool Name

```text
<domain>.<action>
```

Ví dụ:

```text
gmail.listEmails
gmail.sendEmail
calendar.listEvents
calendar.createEvent
chat.sendMessage
sandbox.runPython
sandbox.runShell
```

---

## 3. Data Contracts

## 3.1. UserMessage

```text
Channel / Integration -> Agent Core
```

```json
{
  "requestId": "req_001",
  "sessionId": "sess_001",
  "channel": "telegram",
  "text": "Kiểm tra mail xem có ai hẹn họp không, nếu có thì xếp lịch giúp tôi.",
  "locale": "vi-VN",
  "timestamp": "2026-05-29T09:00:00+07:00",
  "metadata": {}
}
```

Required:

```text
requestId, sessionId, channel, text, timestamp
```

---

## 3.2. AgentResponse

```text
Agent Core -> Channel / Integration
```

```json
{
  "requestId": "req_001",
  "sessionId": "sess_001",
  "status": "approval_required",
  "message": "Tôi cần bạn xác nhận trước khi tạo sự kiện lịch.",
  "approvalId": "appr_001",
  "plan": {
    "steps": [
      {
        "id": "step_1",
        "description": "calendar.createEvent: Tao su kien lich sau khi user approve.",
        "status": "pending"
      }
    ]
  },
  "data": {}
}
```

Status:

```text
completed
approval_required
need_clarification
failed
blocked
max_iterations_reached
```

`max_iterations_reached` is reserved for an agent runtime that exhausts its loop budget before producing a final answer. It must not be used as a `RiskLevel`.

---

## 3.3. ToolCall

```text
Agent Core -> Tool Layer
```

```json
{
  "toolCallId": "toolcall_001",
  "requestId": "req_001",
  "sessionId": "sess_001",
  "toolName": "calendar.createEvent",
  "input": {
    "title": "Họp với anh Minh",
    "start": "2026-05-30T10:00:00+07:00",
    "end": "2026-05-30T11:00:00+07:00",
    "attendees": ["minh@example.com"]
  },
  "reason": "Create a calendar event based on the meeting email that was found."
}
```

Required:

```text
toolCallId, requestId, sessionId, toolName, input
```

Runtime boundary rule:

- `contracts.ToolCall` with `toolCallId`, `requestId`, `sessionId`, `toolName`, and `input` is the canonical contract shape at Agent Core, approval, audit, and channel boundaries.
- The Go tool execution layer may use an internal adapter struct such as `tools.ToolCall{ID, Name, Arguments}` after the runtime has already attached request/session context.
- Internal adapter fields map as follows:

```text
ID        -> toolCallId
Name      -> toolName
Arguments -> input
```

- Internal `tools.ToolCall` must not cross channel, approval, audit, or external API boundaries without being converted back to the canonical contract shape.

---

## 3.4. ToolResult

```text
Tool Layer -> Agent Core
```

```json
{
  "toolCallId": "toolcall_001",
  "toolName": "calendar.createEvent",
  "success": true,
  "data": {
    "eventId": "event_001"
  },
  "error": null
}
```

Error:

```json
{
  "toolCallId": "toolcall_001",
  "toolName": "calendar.createEvent",
  "success": false,
  "data": null,
  "error": {
    "code": "AUTH_EXPIRED",
    "message": "Google access token expired.",
    "retryable": true
  }
}
```

---

## 3.5. RiskDecision

```text
Safety Layer -> Agent Core / Approvals
```

```json
{
  "toolCallId": "toolcall_001",
  "toolName": "calendar.createEvent",
  "riskLevel": "external_write",
  "decision": "requires_approval",
  "requiresApproval": true,
  "reason": "Creating a new Google Calendar event changes external data.",
  "checkedAt": "2026-05-29T09:00:00+07:00"
}
```

Decision:

```text
allow
requires_approval
block
```

RiskLevel:

```text
safe_read
safe_compute
sensitive_read
external_write
local_write
code_execution
destructive
```

`blocked` is a `decision` / `AgentResponse.status`, not a `RiskLevel`.

---

## 3.6. ApprovalRequest

```text
Safety / Agent Core -> Channel / User
```

```json
{
  "approvalId": "appr_001",
  "requestId": "req_001",
  "sessionId": "sess_001",
  "toolCallId": "toolcall_001",
  "status": "pending",
  "riskLevel": "external_write",
  "summary": "The agent wants to create a meeting with Minh at 10:00 tomorrow.",
  "details": "This action will create a new event in Google Calendar.",
  "toolCall": {
    "toolName": "calendar.createEvent",
    "input": {
      "title": "Họp với anh Minh",
      "start": "2026-05-30T10:00:00+07:00",
      "end": "2026-05-30T11:00:00+07:00"
    }
  },
  "createdAt": "2026-05-29T09:00:00+07:00",
  "expiresAt": "2026-05-29T09:10:00+07:00"
}
```

Status:

```text
pending
approved
rejected
expired
cancelled
```

If `expiresAt` passes before an approval decision is received, the approval becomes `expired`; the tool must not execute, and a later `approved` decision for the same `approvalId` must be rejected.

---

## 3.7. ApprovalDecision

```text
Channel / User -> Approval Service / Agent Core
```

```json
{
  "approvalId": "appr_001",
  "requestId": "req_001",
  "decision": "approved",
  "decidedBy": "owner",
  "decidedAt": "2026-05-29T09:02:00+07:00",
  "comment": "Đồng ý tạo lịch."
}
```

Decision:

```text
approved
rejected
```

---

## 3.8. ErrorShape

```json
{
  "code": "AUTH_EXPIRED",
  "message": "Google access token expired.",
  "details": {},
  "retryable": true,
  "retryAfterMs": 0
}
```

ErrorCode:

```text
INVALID_INPUT
MISSING_REQUIRED_FIELD
UNSUPPORTED_CHANNEL
TOOL_NOT_FOUND
RESOURCE_NOT_FOUND
TOOL_INPUT_INVALID
AUTH_EXPIRED
AUTH_MISSING_SCOPE
RATE_LIMITED
PROVIDER_ERROR
PROVIDER_TIMEOUT
PROVIDER_UNAVAILABLE
ACTION_REQUIRES_APPROVAL
ACTION_BLOCKED_BY_POLICY
APPROVAL_NOT_FOUND
APPROVAL_EXPIRED
SANDBOX_TIMEOUT
COMMAND_NOT_ALLOWED
FILE_ACCESS_DENIED
INTERNAL_ERROR
MAX_ITERATIONS_EXCEEDED
```

`PROVIDER_ERROR` is used for non-retryable LLM/provider failures. `PROVIDER_UNAVAILABLE` is used for retryable provider outages.

`MAX_ITERATIONS_EXCEEDED` is used when the agent runtime reaches its configured iteration limit before completing the request.

---

## 3.9. ToolRegistryEntry

```text
Tool owners -> Agent Core / Safety Layer
```

```json
{
  "name": "calendar.createEvent",
  "owner": "integration",
  "description": "Create a new event in Google Calendar.",
  "defaultRiskLevel": "external_write",
  "requiresApproval": true,
  "timeoutMs": 30000,
  "inputExample": {},
  "outputExample": {}
}
```

Required:

```text
name, owner, description, defaultRiskLevel, requiresApproval
```

`timeoutMs` is optional. If omitted, the Tool Layer uses its default execution timeout.

---

## 3.10. Plan

```text
Task Planning -> Agent Core -> Channel / Integration
```

```json
{
  "steps": [
    {
      "id": "step_1",
      "description": "gmail.listEmails: Doc email gan day de tim thong tin hop.",
      "status": "pending"
    },
    {
      "id": "step_2",
      "description": "calendar.createEvent: Tao su kien lich neu user approve.",
      "status": "pending"
    }
  ]
}
```

Rules:

- `Plan` is optional on `AgentResponse`.
- `Plan` is advisory only. It does not authorize tool execution.
- Planner must not execute tools, call connectors, or bypass safety.
- Side-effect tools in a plan still require `RiskDecision` and `ApprovalRequest` before execution.
- If the planner cannot build a safe plan because required information is missing, Agent Core must return `need_clarification`.

---

## 4. Tool Registry

### Gmail

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `gmail.listEmails` | Integration | `safe_read` | No |
| `gmail.getEmail` | Integration | `safe_read` | No |
| `gmail.listLabels` | Integration | `safe_read` | No |
| `gmail.getProfile` | Integration | `safe_read` | No |
| `gmail.listThreads` | Integration | `safe_read` | No |
| `gmail.getThread` | Integration | `safe_read` | No |
| `gmail.listDrafts` | Integration | `safe_read` | No |
| `gmail.getDraft` | Integration | `safe_read` | No |
| `gmail.createDraft` | Integration | `external_write` | Yes |
| `gmail.updateDraft` | Integration | `external_write` | Yes |
| `gmail.sendDraft` | Integration | `external_write` | Yes |
| `gmail.deleteDraft` | Integration | `destructive` | Yes |
| `gmail.replyDraft` | Integration | `external_write` | Yes |
| `gmail.forwardDraft` | Integration | `external_write` | Yes |
| `gmail.downloadAttachments` | Integration | `local_write` | Yes |
| `gmail.modifyMessage` | Integration | `external_write` | Yes |
| `gmail.batchModifyMessages` | Integration | `external_write` | Yes |
| `gmail.trashMessage` | Integration | `destructive` | Yes |
| `gmail.untrashMessage` | Integration | `external_write` | Yes |

> `gmail.getEmail` trả dữ liệu raw từ connector (headers/body/attachments).  
> Render text để hiển thị (ví dụ fallback từ HTML sang text) thuộc tool layer, không thuộc connector raw API boundary.

> Draft/reply/forward tools create or send Gmail drafts and must pass the approval boundary before agent-triggered execution.
> `gmail.downloadAttachments` writes local files and is treated as `local_write`; `gmail.modifyMessage` supports read/unread, star/unstar, archive, moveToInbox, addLabels, and removeLabels.
> Draft creation/update/reply/forward may include local file attachments via `attachments`; local file reading happens in the Gmail tool layer before creating the external draft.
> `gmail.batchModifyMessages` applies the same modify actions to 1-50 messages. `gmail.deleteDraft` and `gmail.trashMessage` are destructive and require approval; `gmail.untrashMessage` is an external write and also requires approval.
> These additions use the existing G1 Gmail scopes: `gmail.readonly`, `gmail.compose`, `gmail.send`, and `gmail.modify`; no new OAuth scope is required.

### Calendar

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `calendar.listEvents` | Integration | `safe_read` | No |
| `calendar.createEvent` | Integration | `external_write` | Yes |
| `calendar.updateEvent` | Integration | `external_write` | Yes |
| `calendar.deleteEvent` | Integration | `destructive` | Yes |

### Chat

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `chat.listSpaces` | Integration | `safe_read` | No |
| `chat.listMembers` | Integration | `safe_read` | No |
| `chat.findSpacesByMembers` | Integration | `safe_read` | No |
| `chat.listMessages` | Integration | `safe_read` | No |
| `chat.sendMessage` | Integration | `external_write` | Yes |

> `chat.sendMessage` bao gồm cả gửi tin nhắn mới và trả lời trong một thread/message cụ thể.  
> Nếu là reply, input có thể kèm `threadId` hoặc `replyToMessageId`. Không tách `chat.replyMessage` nếu chưa có nhu cầu riêng.
> `chat.findSpacesByMembers` chỉ đọc danh sách spaces/members để tìm space chứa các `users/...` đã resolve từ People API trước khi gọi `chat.listMessages`.

### People

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `people.searchDirectory` | Integration | `safe_read` | No |

> `people.searchDirectory` only reads Google Workspace directory profiles to resolve names/emails before matching Google Chat members.

### Web

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `web.search` | Integration | `safe_read` | No |
| `web.fetch` | Integration | `safe_read` | No |

> `web.search` searches the public web through the configured web provider and returns concise result snippets with URLs.  
> `web.fetch` extracts readable content from one public `http` or `https` URL and truncates long page content before returning it to the agent.
> Tavily is the current implementation provider, but the public tool contract remains provider-neutral.
> These tools are read-only, but user prompts and tool descriptions should avoid sending private or sensitive data to the external web provider unless the user explicitly asks for it.

### Sandbox

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `sandbox.runPython` | Agent Core | `code_execution` | Yes |
| `sandbox.runShell` | Agent Core | `code_execution` | Yes |

---

## 5. Events

```text
agent.run.started
agent.run.completed
agent.run.failed
agent.run.cancelled

intent.classified
intent.clarification_required

task.plan.started
task.plan.completed
task.plan.failed
task.plan.clarification_required

tool.call.requested
tool.call.started
tool.call.completed
tool.call.failed

approval.requested
approval.approved
approval.rejected
approval.expired
approval.resolved

safety.risk.checked
safety.action.blocked

sandbox.run.started
sandbox.run.completed
sandbox.run.failed
sandbox.run.timeout
```

---

## 6. E2E Contract Scenarios

## 6.1. Email to Calendar with Approval

Input:

```text
Kiểm tra mail xem có ai hẹn họp không, nếu có thì xếp lịch giúp tôi.
```

Expected:

```text
UserMessage
-> Intent/Safety classification
-> Task Planning
-> gmail.listEmails
-> calendar.listEvents
-> calendar.createEvent proposed
-> RiskDecision: external_write, requires_approval
-> ApprovalRequest
-> ApprovalDecision: approved
-> calendar.createEvent executed
-> AgentResponse: completed
```

Must not happen:

```text
calendar.createEvent executed before approval
```

---

## 6.2. Shell Command Requires Approval

Input:

```text
Xóa các file tạm trong thư mục Downloads giúp tôi.
```

Expected:

```text
UserMessage
-> Intent/Safety classification
-> Task Planning
-> sandbox.runShell or sandbox.runPython proposed
-> RiskDecision: destructive/code_execution, requires_approval
-> ApprovalRequest
-> ApprovalDecision: approved/rejected
-> execute only if approved
-> AgentResponse: completed/failed/blocked
```

Must not happen:

```text
file deletion or command execution before approval
```

---

## 7. Ownership

| Contract | Producer | Consumer |
|---|---|---|
| `UserMessage` | Integration/Channels | Agent Core |
| `AgentResponse` | Agent Core | Integration/Channels |
| `Plan` | Task Planning / Agent Core | Agent Core / Channels |
| `ToolCall` | Agent Core | Tools |
| `ToolResult` | Tools | Agent Core |
| `RiskDecision` | Safety | Agent Core/Approvals |
| `ApprovalRequest` | Safety/Approvals | Channel/User |
| `ApprovalDecision` | Channel/User | Approvals/Agent Core |
| `ErrorShape` | All modules | All modules |
| `ToolRegistryEntry` | Tool owners | Agent Core/Safety |

---

## 8. Change Policy

Cần giải thích rõ trong PR nếu thay đổi:

- field trong contract;
- enum/status;
- tool name;
- risk level;
- approval behavior;
- event name;
- E2E expected flow.

```text
Nếu thay đổi làm team khác phải sửa code, đó là contract change.
```
