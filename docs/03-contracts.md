# Contracts

> Các contract tối thiểu để Integration Team và Agent Core Team phát triển độc lập.

---

## 1. Boundary

```text
Channel -> Agent Core -> Safety/HITL -> Tool Layer -> Tool Execution -> Agent Core -> Channel
```

- Channel chuẩn hóa input thành `UserMessage`.
- Agent Core chỉ gọi tool qua `ToolCall`.
- Tool trả kết quả qua `ToolResult`.
- Action có side effect phải có `RiskDecision`.
- Nếu `requiresApproval = true`, tool không được execute trước khi có `ApprovalDecision = approved`.

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
  "userId": "user_001",
  "channel": "telegram",
  "text": "Kiểm tra mail xem có ai hẹn họp không, nếu có thì xếp lịch giúp tôi.",
  "locale": "vi-VN",
  "timestamp": "2026-05-29T09:00:00+07:00",
  "metadata": {}
}
```

Required:

```text
requestId, sessionId, userId, channel, text, timestamp
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
```

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
  "userId": "user_001",
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
toolCallId, requestId, sessionId, userId, toolName, input
```

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
  "decidedBy": "user_001",
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
```

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

## 4. Tool Registry

### Gmail

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `gmail.listEmails` | Integration | `safe_read` | No |
| `gmail.getEmail` | Integration | `safe_read` | No |
| `gmail.sendEmail` | Integration | `external_write` | Yes |

> `gmail.getEmail` trả dữ liệu raw từ connector (headers/body/attachments).  
> Render text để hiển thị (ví dụ fallback từ HTML sang text) thuộc tool layer, không thuộc connector raw API boundary.

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
| `chat.listMessages` | Integration | `safe_read` | No |
| `chat.sendMessage` | Integration | `external_write` | Yes |

> `chat.sendMessage` bao gồm cả gửi tin nhắn mới và trả lời trong một thread/message cụ thể.  
> Nếu là reply, input có thể kèm `threadId` hoặc `replyToMessageId`. Không tách `chat.replyMessage` nếu chưa có nhu cầu riêng.

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
