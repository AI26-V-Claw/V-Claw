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
- Channel approval UI, such as Telegram buttons or modal comments, must resolve to `ApprovalDecision`.

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
calendar.getEvent
calendar.createEvent
calendar.respondEvent
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
iteration_budget_exhausted
```

`iteration_budget_exhausted` is reserved for an agent runtime that exhausts its loop budget before producing a final answer. It must not be used as a `RiskLevel`.

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

Known artifact kinds include `gmail.message`, `chat.message`, `calendar.event`, `google.meet.space`, `drive.file`, `docs.document`, and `sheets.spreadsheet`.

`failureReason` is a machine-readable string populated on failed, blocked, expired, cancelled, or max-iteration responses. It is omitted when empty, including completed and approval-required responses. Values are typed constants from `orchestration.FailureReason`:

| Value | Trigger |
|---|---|
| `` (empty) | Status is `completed` - happy path |
| `timeout` | Context deadline exceeded |
| `canceled` | Request was cancelled |
| `iteration_budget` | Agent exhausted iteration budget |
| `provider_error` | LLM provider returned an error |
| `provider_unavailable` | LLM provider returned retryable error |
| `tool_error` | Tool execution failed |
| `approval_expired` | Approval TTL (10 min) elapsed without decision |
| `approval_rejected` | User explicitly rejected the approval |
| `policy_blocked` | Action blocked by safety policy |
| `aborted` | Internal setup failure or unclassified error |

`failureReason` contains only typed constant values - never raw error messages or internal paths. Safe to expose to channel adapters (Telegram).

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
  "reason": "Create a calendar event based on the meeting email that was found.",
  "governance": {
    "model": "claude-opus-4-8",
    "promptVersion": "abc12345",
    "toolSchemaVersion": "deadbeef",
    "policyDecisionRef": "policy:run_001:toolcall_001:1781870400"
  }
}
```

Required:

```text
toolCallId, requestId, sessionId, toolName, input
```

`governance` is optional but is populated by the agent runtime before the call crosses any boundary; see §3.11 GovernanceMetadata.

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
    "contentForUser": "Created calendar event: Sprint Review",
    "contentForLLM": "{\"Event\":{\"ID\":\"event_001\",\"Title\":\"Sprint Review\"}}"
  },
  "artifactRef": {
    "kind": "calendar.event",
    "label": "Google Calendar event",
    "id": "event_001",
    "uri": "https://calendar.google.com/calendar/r/eventedit/event_001"
  },
  "metadata": {},
  "truncated": false,
  "redacted": false,
  "source": "tool:google_workspace",
  "governance": {
    "model": "claude-opus-4-8",
    "promptVersion": "abc12345",
    "toolSchemaVersion": "deadbeef",
    "policyDecisionRef": "policy:run_001:toolcall_001:1781870400"
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

`data` carries the result **content** payload (`contentForUser` and `contentForLLM`). The sibling fields — `artifactRef`, `metadata`, `truncated`, `redacted` — are typed **metadata about that payload**. The two axes are independent: `data` is the content channel (and the only field the agent transcript/memory reads, via `contentForLLM`), while the typed fields describe it. Do not duplicate content into the typed fields, and do not store flags inside `data`.

`artifactRef` is required when the tool reads or writes a concrete primary resource that can be referenced safely, such as a file, URL, Gmail message, Chat message, Calendar event, Drive file, Docs document, or Sheets spreadsheet. **The tool that produces the resource sets `artifactRef` directly from its typed output** (e.g. each office tool's `*ArtifactRef` helper). Downstream consumers (messenger, channels) read it as-is; they MUST NOT reverse-engineer a reference by re-parsing `contentForLLM`/`contentForUser`. `artifactRef.meta` carries optional secondary references tied to the resource, e.g. a Calendar event's Google Meet link.

`metadata` is for structured non-sensitive execution details such as byte counts, line counts, query parameters, and pagination state. It MUST NOT carry control flags such as redaction state.

`truncated=true` means one or more result payloads were shortened before crossing the contract boundary.

`redacted=true` means sensitive content was removed or masked before the result was added to LLM context. `redacted` is a typed boolean and is the single source of truth for redaction state (it is never stored as a `metadata` key). User-facing text can remain more detailed when appropriate, but logs, session observations, and LLM-visible content must use the sanitized result.

`source` is an optional attribution string that identifies the origin layer producing the result, e.g. `tool:google_workspace`, `tool:sandbox.python`, `connector:tavily`. Audit/N4 use it to group records by origin without parsing tool names. Use the prefixes from `internal/governance` (`SourceToolPrefix`, `SourceConnectorPrefix`, `SourceUserChannel`).

`governance` is optional and mirrors the bundle from the originating `ToolCall`. The agent runtime copies it through after execution so consumers reading only the result still know which model, prompt, schema, and policy decision produced it. See §3.11 GovernanceMetadata.

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
  "checkedAt": "2026-05-29T09:00:00+07:00",
  "policyDecisionRef": "policy:run_001:toolcall_001:1781870400"
}
```

`policyDecisionRef` is a composite reference shared with every record (tool call, action, audit) that descends from this risk decision. Format: `policy:<runID>:<toolCallId>:<unixSec>`. Computed by `governance.PolicyRef()`. See §3.11.

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
  "expiresAt": "2026-05-29T09:10:00+07:00",
  "governance": {
    "model": "claude-opus-4-8",
    "promptVersion": "abc12345",
    "toolSchemaVersion": "deadbeef",
    "policyDecisionRef": "policy:run_001:toolcall_001:1781870400"
  }
}
```

`governance` makes the approval record self-contained for audit/trace — see §3.11. Channel UIs that render approvals (Telegram) MUST NOT display these fields to the end user; they are for backend consumers (logs, dashboards, N4 trace) only.

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
ITERATION_BUDGET_EXHAUSTED
```

`PROVIDER_ERROR` is used for non-retryable LLM/provider failures. `PROVIDER_UNAVAILABLE` is used for retryable provider outages.

`ITERATION_BUDGET_EXHAUSTED` is used when the agent runtime reaches its configured iteration limit before completing the request.

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

## 3.11. GovernanceMetadata

```text
Agent Core -> ToolCall, ToolResult, ApprovalRequest, RiskDecision, audit_entries
```

```json
{
  "model": "claude-opus-4-8",
  "promptVersion": "abc12345",
  "toolSchemaVersion": "deadbeef",
  "policyDecisionRef": "policy:run_001:toolcall_001:1781870400"
}
```

Provenance bundle attached to every significant runtime record so the N4 monitoring/trace UI can answer "which model + prompt + tool schema + policy decision produced this result?" without joining across tables.

Fields:

- `model` — LLM model ID used for the surrounding request, e.g. `claude-opus-4-8`, `gemini-1.5-pro`. Empty if the record was produced outside an LLM-driven step.
- `promptVersion` — short content-hash fingerprint of the effective system prompt (`runtimeSystemPrompt`). Computed by `governance.PromptVersion()` once when the runtime is constructed; same prompt → same version. Eight hex characters. (`configs/SOUL.md` is reference documentation only — it is not injected at runtime and does not contribute to this hash.)
- `toolSchemaVersion` — short content-hash fingerprint of the tool's parameter schema at call time. Computed by `governance.ToolSchemaVersion()` from the canonicalised schema; cosmetic key reordering does not shift the version. Eight hex characters.
- `policyDecisionRef` — composite reference back to the `RiskDecision` that authorised the tool call. Format: `policy:<runID>:<toolCallId>:<unixSec>`. Computed by `governance.PolicyRef()` from the decision's `checkedAt` in UTC seconds. Readable in JSONL audit dumps via `grep`.

Where it appears:

| Record | Field | Notes |
|---|---|---|
| `ToolCall` | `governance` | Stamped by Runtime before the call leaves Agent Core. |
| `ToolResult` | `governance` | Copied through after execution; same bundle as the originating `ToolCall`. |
| `ToolResult` | `source` | Origin attribution prefix, e.g. `tool:google_workspace`, `connector:tavily`. Not in `governance` because it describes the result layer, not the request context. |
| `RiskDecision` | `policyDecisionRef` | Self-reference — the decision IS the policy reference. |
| `ApprovalRequest` | `governance` | Carries the bundle so approval records are self-contained. |
| `audit_entries` | `model`, `prompt_version`, `tool_schema_version`, `policy_decision_ref`, `source` | Five separate columns (not nested) for index efficiency — see [docs/05-erd.md](./05-erd.md#governance-columns). |

Rules:

- The runtime computes governance values once per construction (`promptVersion`) or once per call (`toolSchemaVersion`, `policyDecisionRef`) — they are not provided by the channel adapter or LLM.
- All fields are optional strings. Empty values mean "unknown" and round-trip cleanly; existing producers that do not yet stamp governance keep working.
- Channel UIs (Telegram, web) MUST NOT display governance to end users — these are backend trace identifiers, not human-facing context.
- Governance values are stable identifiers, not secrets. They may be logged, indexed, and shipped to monitoring without redaction.
- A change in any input that contributes to a hash (system prompt body, tool parameter schema) automatically shifts the corresponding version. There is no manual version-bump step.

Implementation: see `internal/governance/governance.go`. Migration: `migrations/003_governance_metadata.sql`. Persistence: see [docs/05-erd.md §Governance columns](./05-erd.md#governance-columns).

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

### Google Drive

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `drive.listFiles` | Integration | `safe_read` | No |
| `drive.getFile` | Integration | `safe_read` | No |
| `drive.exportFile` | Integration | `safe_read` | No |
| `drive.downloadFile` | Integration | `safe_read` | No |
| `drive.saveFile` | Integration | `local_write` | Yes |
| `drive.createFolder` | Integration | `external_write` | Yes |
| `drive.createFile` | Integration | `external_write` | Yes |
| `drive.uploadFile` | Integration | `external_write` | Yes |
| `drive.updateFileMetadata` | Integration | `external_write` | Yes |
| `drive.shareFile` | Integration | `external_write` | Yes |
| `drive.listPermissions` | Integration | `safe_read` | No |
| `drive.revokePermission` | Integration | `external_write` | Yes |
| `drive.moveFile` | Integration | `external_write` | Yes |
| `drive.moveFiles` | Integration | `external_write` | Yes |
| `drive.trashFile` | Integration | `destructive` | Yes |
| `drive.untrashFile` | Integration | `external_write` | Yes |

### Google Docs

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `docs.getDocument` | Integration | `sensitive_read` | No |
| `docs.createDocument` | Integration | `external_write` | Yes |
| `docs.appendText` | Integration | `external_write` | Yes |
| `docs.replaceText` | Integration | `external_write` | Yes |
| `docs.insertText` | Integration | `external_write` | Yes |
| `docs.deleteContent` | Integration | `external_write` | Yes |

### Google Sheets

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `sheets.getSpreadsheet` | Integration | `safe_read` | No |
| `sheets.readValues` | Integration | `sensitive_read` | No |
| `sheets.batchGetValues` | Integration | `safe_read` | No |
| `sheets.createSpreadsheet` | Integration | `external_write` | Yes |
| `sheets.updateValues` | Integration | `external_write` | Yes |
| `sheets.batchUpdateValues` | Integration | `external_write` | Yes |
| `sheets.appendValues` | Integration | `external_write` | Yes |
| `sheets.clearValues` | Integration | `external_write` | Yes |
| `sheets.addSheet` | Integration | `external_write` | Yes |
| `sheets.renameSheet` | Integration | `external_write` | Yes |
| `sheets.deleteSheet` | Integration | `destructive` | Yes |
| `sheets.duplicateSheet` | Integration | `external_write` | Yes |

> Drive/Docs/Sheets remain read-first: list/get/read/export/download tools are safe reads with bounded output. Create/update/append/share/move/upload/revoke/clear/tab-management tools must pass the same HITL approval boundary before execution. `drive.trashFile` and `sheets.deleteSheet` are destructive because they remove content from normal user views.
>
> Two additional safety constraints apply beyond approval:
> - **`drive.uploadFile` is sandboxed.** Its `localPath` is resolved through the same workspace `PathGuard` as the filesystem tools; a path outside the sandbox workspace is rejected with `INVALID_INPUT`. The tool must be constructed with a guard — without one, upload is refused. This prevents the agent from uploading host files such as `configs/google/token.json` or `.env`.
> - **`drive.shareFile` cannot grant public write access.** When `type=anyone`, `role` must be `reader`; `writer` or `commenter` for `anyone` is rejected with `INVALID_INPUT`. Public links are read-only.
>
> Local file downloads are sandboxed too:
> - **`drive.saveFile`** writes a Drive file to disk inside the sandbox workspace (`local_write`, approval required). Google Docs Editors files are auto-exported; binary files are downloaded directly. `outputDir` is optional and is resolved through the workspace `PathGuard`, defaulting to the workspace root; paths outside the workspace are rejected with `INVALID_INPUT`. Without a guard the tool refuses to save. `drive.downloadFile` stays read-only (content into the response, no local write).
> - **`gmail.downloadAttachments`** also resolves `outputDir` through the workspace `PathGuard` when one is configured, making `outputDir` optional (defaults to the workspace root) and rejecting paths outside the workspace.

### Calendar

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `calendar.listEvents` | Integration | `safe_read` | No |
| `calendar.getEvent` | Integration | `safe_read` | No |
| `calendar.createEvent` | Integration | `external_write` | Yes |
| `calendar.updateEvent` | Integration | `external_write` | Yes |
| `calendar.respondEvent` | Integration | `external_write` | Yes |
| `calendar.deleteEvent` | Integration | `destructive` | Yes |

> `calendar.createEvent` requires an explicit start date+time and an explicit end date+time or duration before approval. Date-only phrases such as `tomorrow` / `ngay mai` are not enough to infer a start time. If the user also asks to send an email about the event, Calendar attendees/invitations do not satisfy that separate Gmail action.
> `calendar.updateEvent` preserves existing Calendar attendee RSVP state when adding attendees. Tool input `attendees` is treated as attendees to add, not a blind replacement list; RSVP changes must use `calendar.respondEvent`.
> `calendar.createEvent` and `calendar.updateEvent` accept `createConference=true` to ask Google Calendar to generate a Google Meet link for the event. Tool input must not accept a caller-supplied `meetLink`; the link must come from the current Calendar API result. Adding Meet to an existing event is idempotent when the event already has a Meet link.

### Google Meet

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `meet.createMeeting` | Integration | `external_write` | Yes |

> `meet.createMeeting` creates a standalone Google Meet space for later use or immediate sharing. Scheduled Calendar meetings must use `calendar.createEvent` with `createConference=true`, and adding Meet to an existing Calendar event must use `calendar.updateEvent` with `createConference=true`. Agent responses must not invent or reuse Meet links from older transcript/memory; share only links returned by the current Meet or Calendar tool result.

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

### Memory

| Tool | Owner | Risk | Approval |
|---|---|---|---|
| `memory.getUserMemory` | Agent Core | `safe_read` | No |
| `memory.editUserMemory` | Agent Core | `local_write` | Yes |
| `memory.resetMemory` | Agent Core | `destructive` | Yes |

> `memory.getUserMemory` đọc bộ nhớ dài hạn hiện tại (`USER.md` + `NOTES.md`), không cần approval.
> `memory.editUserMemory` thêm/xóa một fact trong bộ nhớ dài hạn (`local_write`, cần approval). Ngoài approval, tool còn enforce data contract §9.1: nội dung có dấu hiệu chứa credential/token/password/secret bị từ chối với `INVALID_INPUT` trước khi ghi — approval không thay thế ràng buộc này.
> `memory.resetMemory` xóa toàn bộ bộ nhớ dài hạn và tạo lại skeleton mặc định (`destructive`, cần approval).

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
-> Agent Runtime: tools available
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
-> Agent Runtime: tools available
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

### 9.3. Provenance sidecar

Current implementation stores provenance in `cache/memory/memory_sources.json`.
Markdown bullets in `USER.md` and `NOTES.md` may include an internal marker comment:

```markdown
- <fact text> <!-- mem:<memoryFactId> -->
```

Rules:

- `memory_sources.json` is machine-readable provenance for each long-term fact.
- Each source entry records `id`, `kind`, `file`, `section`, `text`, timestamps, and observations.
- Observations record `sourceType` such as `session_compaction`, `session_compaction_fallback`, `repeated_habit`, or `manual_migration`.
- Session compaction observations should include `sessionId`, optional `runId`/`requestId`, classifier model, and summary hash when available.
- Repeated habit counting is global across sessions in `cache/memory/habit_patterns.json`.
- Habit detection uses an LLM classifier to normalize natural-language variants into canonical patterns (for example `list/read/check/view` -> `inspect`) and falls back to deterministic heuristics if the classifier fails.
- A repeated habit is promoted only when the same normalized pattern has `eligibleCount >= 3` and either appears in at least 2 distinct sessions or spans at least 72 hours from `firstSeen` to `lastSeen`.
- Safe-read habits may become eligible from repeated user messages. Side-effect habits only become eligible after the user approves the action and the tool succeeds.
- Habit dedup happens in 3 layers: normalized pattern (`promoted=true` prevents re-promotion), normalized fact text in `USER.md`, and exact observation dedup in `memory_sources.json`.
- Repeated-habit observations in `memory_sources.json` must include the promoted aggregate `count`.
- Loader must strip `<!-- mem:... -->` markers before injecting memory into the runtime prompt.
- Long-term memory is context only. It must never override Tool Policy, approval decisions, HITL state, system prompt, or tool contracts.
- Long-term memory loader must wrap memory as `authority="context_only"` and filter existing memory lines that attempt to bypass/ignore/override system instructions, tool policy, approvals, HITL, or tool contracts.
- Long-term memory writers must reject new facts that attempt to bypass/ignore/override system instructions, tool policy, approvals, HITL, or tool contracts.
- `USER.md` work rules are user preferences only. They are not a security, approval, or tool-policy authority.

---

### 9.4. Loading rules (Sprint 3)

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

### 9.5. Pending Approval — In-memory Limitation

**Đây là giới hạn thiết kế được chấp nhận trong MVP:**

- `Runtime.pendingApprovals` là in-memory map trong `agent.Runtime` struct.
- Pending approvals **không persist** vào session store hoặc file.
- Nếu process restart xảy ra trong khi có approval đang chờ, approval đó bị mất.
- Approval có TTL `10 phút` (`approvalTTL = 10 * time.Minute`); sau TTL tự expire và không thể execute.
- Compaction **bắt buộc skip** khi session đang có pending approval (xem Section 9.6).

Implication cho user:

```text
Nếu bot restart khi đang chờ user bấm Approve/Reject,
user sẽ thấy nút Approve nhưng bot không tìm thấy approval → trả về APPROVAL_NOT_FOUND.
User cần gửi lại yêu cầu từ đầu.
```

Đây là trade-off được chấp nhận trong MVP single-owner deployment. Persistence sẽ được xem xét ở Sprint 3+ khi có PostgreSQL store.

---

### 9.6. Compaction Guard — Pending Approval Protection

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
