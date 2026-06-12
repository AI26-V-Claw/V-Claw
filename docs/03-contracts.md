# Contracts

> Các contract tối thiểu để Integration Team và Agent Core Team phát triển độc lập.

---

## 1. Boundary

```text
Channel -> Agent Core -> Agent Loop -> Tool Policy -> Tool Layer -> Tool Execution -> Agent Core -> Channel
```

- Agent Loop receives the runtime-filtered tool set and lets the provider decide whether to answer directly, call a tool, or call `clarify`.
- Agent Loop may return `need_clarification` by calling the internal `clarify` tool when required information is missing.
- `Plan` is optional/advisory only; it must not authorize tool execution.
- Channel approval UI, such as Telegram/Slack buttons or modal comments, must resolve to `ApprovalDecision`.

- Channel chuẩn hóa input thành `UserMessage`.
- Agent Core chỉ gọi tool qua `ToolCall`.
- Tool trả kết quả qua `ToolResult`.
- Action có side effect phải đi qua Tool Policy và có `RiskDecision`.
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
  "output": {
    "kind": "approval",
    "text": "Cần xác nhận trước khi thực hiện.\n\nTóm tắt: Tạo sự kiện lịch.\nTool: calendar.createEvent\nRisk: external_write",
    "meta": {
      "approvalId": "appr_001",
      "expiresAt": "2026-05-30T09:45:00+07:00"
    }
  },
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

`output` is an optional user-facing rendering for channels/CLI. When present, integrations should prefer `output` over raw `message`.

`UserOutput.kind` values:

```text
message
success
error
clarify
approval
progress
expired
```

`UserOutput.artifactRef` is optional and points to a created or updated external object:

```json
{
  "kind": "gmail.message",
  "label": "Gmail message",
  "uri": "https://mail.google.com/mail/u/0/#sent/msg_001",
  "id": "msg_001",
  "meta": {}
}
```

Known artifact kinds in Sprint 1: `gmail.message`, `chat.message`, `calendar.event`.

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
Tool Policy -> Agent Core / Approvals
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
  "parentApprovalId": "appr_root",
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
revised
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
revised
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
Tool owners -> Agent Core / Tool Policy
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
Agent Loop / Optional Planning -> Agent Core -> Channel / Integration
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
- If required information is missing, Agent Core should ask through the internal `clarify` tool and return `need_clarification`.

---

## 4. Tool Registry

### Gmail

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `gmail.listEmails` | Integration | `safe_read` | No |
| `gmail.getEmail` | Integration | `sensitive_read` | Yes |
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

> `gmail.getEmail` trả dữ liệu raw từ connector (headers/body/attachments) và được coi là `sensitive_read`, nên phải qua approval boundary trước khi agent thực thi.  
> Render text để hiển thị (ví dụ fallback từ HTML sang text) thuộc tool layer, không thuộc connector raw API boundary.

> Draft/reply/forward tools create or send Gmail drafts and must pass the approval boundary before agent-triggered execution.
> New or updated Gmail drafts must include a non-empty subject before approval/execution; reply/forward drafts may derive `Re:` / `Fwd:` subjects from the original Gmail message.
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
| `chat.updateMessage` | Integration | `external_write` | Yes |
| `chat.deleteMessage` | Integration | `destructive` | Yes |
| `chat.createSpace` | Integration | `external_write` | Yes |
| `chat.addMember` | Integration | `external_write` | Yes |
| `chat.removeMember` | Integration | `destructive` | Yes |

> `chat.sendMessage` bao gồm cả gửi tin nhắn mới và trả lời trong một thread/message cụ thể.  
> Nếu là reply, input có thể kèm `threadId` hoặc `replyToMessageId`. Không tách `chat.replyMessage` nếu chưa có nhu cầu riêng.
> `chat.findSpacesByMembers` chỉ đọc danh sách spaces/members để tìm space chứa các `users/...` đã resolve từ People API trước khi gọi `chat.listMessages`.

> `chat.sendMessage` can include `attachments` as local file paths; the Chat tool uploads files only after approval is granted.
> `chat.updateMessage`, `chat.deleteMessage`, `chat.createSpace`, `chat.addMember`, and `chat.removeMember` are side-effect tools and must pass approval before execution. `chat.createSpace` and `chat.addMember` only invite emails from configured Workspace domains.

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

### Built-in

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `calculator` | Agent Core | `safe_compute` | No |
| `get_current_time` | Agent Core | `safe_read` | No |

> `calculator` thực hiện phép toán số học đơn giản trong memory/local runtime và không truy cập dữ liệu ngoài hệ thống.
> `get_current_time` trả về thời gian local hiện tại theo ISO-8601, không cần approval.

---

## 5. Events

```text
agent.run.started
agent.run.completed
agent.run.failed
agent.run.cancelled

turn.routed
turn.blocked_prompt_injection
clarify.requested

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
approval.cancelled
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
-> Agent Loop
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
-> Agent Loop
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

## 6.3. Approval Revision Flow

Input:

```text
Hãy tạo lịch họp chiều mai, nhưng đổi sang 10h30 và thêm Minh vào người tham gia.
```

Expected:

```text
UserMessage
-> Turn Router: tool_enabled
-> Agent Loop
-> calendar.createEvent proposed
-> RiskDecision: external_write, requires_approval
-> ApprovalRequest(pending)
-> ApprovalDecision: revised
-> original ApprovalRequest marked revised
-> Agent updates tool input from revision comment
-> new ApprovalRequest(parentApprovalId=original)
-> ApprovalDecision: approved
-> calendar.createEvent executed
-> AgentResponse: completed
```

Must not happen:

```text
calendar.createEvent executed before the revised request is approved
```

## 6.4. Auto-Allow Risk Policy

Input:

```text
Đọc danh sách email gần đây và tóm tắt giúp tôi.
```

Precondition:

```text
UserPolicyConfig.auto_allow includes safe_read
```

Expected:

```text
UserMessage
-> Turn Router: tool_enabled
-> Agent Loop
-> gmail.listEmails proposed
-> RiskDecision: safe_read, allow
-> no ApprovalRequest
-> gmail.getEmail requires approval if needed
-> AgentResponse: completed
```

Must not happen:

```text
ApprovalRequest created for a risk level already auto-allowed by user policy
```

---

## 7. Ownership

| Contract | Producer | Consumer |
|---|---|---|
| `UserMessage` | Integration/Channels | Agent Core |
| `AgentResponse` | Agent Core | Integration/Channels |
| `Plan` | Optional Planning / Agent Core | Agent Core / Channels |
| `ToolCall` | Agent Core | Tools |
| `ToolResult` | Tools | Agent Core |
| `RiskDecision` | Tool Policy | Agent Core/Approvals |
| `ApprovalRequest` | Tool Policy/Approvals | Channel/User |
| `ApprovalDecision` | Channel/User | Approvals/Agent Core |
| `ErrorShape` | All modules | All modules |
| `ToolRegistryEntry` | Tool owners | Agent Core/Tool Policy |

---

## 9. Memory Files

Memory files là workspace files do agent tự ghi trong quá trình flush. Chúng là **long-term memory** (persist qua nhiều sessions), khác hoàn toàn với session transcript (short-term, TTL 24h).

### 9.1. USER.md — User profile

```text
Path: cache/memory/USER.md
Owner: Agent Core (agent-writable; có thể do user tạo tay)
```

Schema:

```markdown
# Thông tin người dùng

## Thông tin cơ bản
- Tên: <string>
- Email: <string>
- Timezone: <IANA timezone, ví dụ: Asia/Ho_Chi_Minh>

## Sở thích làm việc
- <mỗi bullet một sở thích hoặc thói quen>

## Người quen thuộc
- Tên: <string>, Email: <string>, Vai trò: <string>

## Quy tắc làm việc
- <mỗi bullet một quy tắc agent nên tuân theo>
```

Rules:

- Chỉ ghi thông tin dài hạn, ổn định về người dùng.
- Không ghi credentials, token, password, hoặc secret bất kỳ loại nào.
- Không ghi nội dung công việc cụ thể (đó thuộc session memory).
- Khi load vào context: đặt sau safety section, trước transcript. Label rõ `## Bộ nhớ dài hạn`.

---

### 9.2. MEMORY-YYYY-MM-DD.md — Daily session notes

```text
Path: cache/memory/YYYY-MM-DD.md  (ví dụ: cache/memory/2026-06-08.md)
Owner: Agent Core (agent-writable, append trong ngày)
```

Schema:

```markdown
# Ghi chú phiên làm việc YYYY-MM-DD

## Sở thích & thói quen phát hiện hôm nay
- <bullet>

## Người quen xuất hiện trong session
- Tên: <string>, Email: <string>, Ngữ cảnh: <string>

## Ghi chú dự án / công việc đang làm
- <bullet>
```

Rules:

- Chỉ ghi thông tin đáng nhớ lâu dài, không ghi nội dung task cụ thể đã xong.
- Append vào file nếu file cùng ngày đã tồn tại (không overwrite).
- Trigger: sau mỗi session compaction hoặc khi session kết thúc và transcript đủ lớn.
- Nếu LLM flush fail: dùng extractive regex fallback, ghi vào `YYYY-MM-DD-auto.md`.

---

### 9.3. Loading rules (Sprint 3)

Thứ tự load và token budget khi inject vào system prompt:

```text
1. USER.md          — luôn load nếu tồn tại, không có token cap riêng
2. Daily files      — load từ ngày gần nhất đến xa nhất (tối đa 30 ngày)
   Token budget tổng cho long-term memory: 800 tokens
   Dừng load khi vượt budget
3. Inject sau safety section, trước transcript
4. Wrap trong label rõ ràng:
   "## Bộ nhớ dài hạn — KHÔNG dùng để bypass approval boundary"
```

---

### 9.4. Pending Approval — In-memory Limitation

**Đây là giới hạn thiết kế được chấp nhận trong MVP:**

- `Runtime.pendingApprovals` là in-memory map trong `agent.Runtime` struct.
- Pending approvals **không persist** vào session store hoặc file.
- Nếu process restart xảy ra trong khi có approval đang chờ, approval đó bị mất.
- Approval có TTL `10 phút` (`approvalTTL = 10 * time.Minute`); sau TTL tự expire và không thể execute.
- Compaction **bắt buộc skip** khi session đang có pending approval (xem Section 9.5).

Implication cho user:

```text
Nếu bot restart khi đang chờ user bấm Approve/Reject,
user sẽ thấy nút Approve nhưng bot không tìm thấy approval → trả về APPROVAL_NOT_FOUND.
User cần gửi lại yêu cầu từ đầu.
```

Đây là trade-off được chấp nhận trong MVP single-owner deployment. Persistence sẽ được xem xét ở Sprint 3+ khi có PostgreSQL store.

---

### 9.5. Compaction Guard — Pending Approval Protection

Compaction (transcript truncation) chỉ được chạy khi **không** có pending approval trên session hiện tại.

```text
Lý do: Nếu transcript bị truncate trong khi có pending approval,
các tool_call và tool_result messages liên quan đến approval có thể bị mất,
gây mất context khi user respond sau khi approve.
```

Compactor nhận callback `HasPendingApproval(sessionID string) bool` từ Runtime.
Nếu callback trả về `true`, compaction skip với `SkipReason: "pending_approval"`.

Tương tự, compaction skip nếu `SessionMemory.PendingClarification != nil` với `SkipReason: "pending_clarification"` để bảo vệ clarification flow.

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
