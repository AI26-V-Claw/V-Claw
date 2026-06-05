# Scenario 03: Calendar Create With HITL

## Purpose

Luồng chuẩn cho external write: Agent đề xuất tạo Calendar event, nhưng chỉ execute sau khi người dùng approve.

Scenario này đại diện cho:

- Missing information handling.
- Read-before-write conflict check.
- `RiskDecision` và `ApprovalRequest`.
- Rule: không execute side effect trước approval.

## Sequence

```mermaid
sequenceDiagram
    autonumber

    actor User as Người dùng
    participant Channel as Message Channel
    participant Adapter as Channel Adapter
    participant Agent as Agent Core
    participant Policy as Tool Policy
    participant Router as Tool Router
    participant CalTool as Calendar Tool
    participant CalConnector as Calendar Connector
    participant Google as Google Calendar API
    participant Approval as Approvals
    participant Store as Session / Audit Store

    User->>Channel: "Tạo lịch họp review chiều mai mời anh Minh"
    Channel->>Adapter: Deliver message
    Adapter->>Agent: UserMessage

    Agent->>Agent: Let agent loop derive required tool input

    alt Thiếu thông tin bắt buộc
        Agent->>Agent: Call internal clarify tool
        Agent-->>Adapter: AgentResponse(status=need_clarification)
        Adapter-->>Channel: Ask missing params
        Channel-->>User: "Mấy giờ, họp bao lâu?"
        User->>Channel: "3h chiều, 1 tiếng"
        Channel->>Adapter: Deliver message
        Adapter->>Agent: UserMessage with additional params
        Agent->>Agent: Merge params into current run
    end

    Agent->>Router: ToolCall calendar.listEvents(time range)
    Router->>CalTool: Execute calendar.listEvents
    CalTool->>CalConnector: ListEvents(timeMin, timeMax)
    CalConnector->>Google: events.list

    alt Calendar unavailable / auth error
        Google-->>CalConnector: Error
        CalConnector-->>CalTool: ErrorShape
        CalTool-->>Router: ToolResult(success=false)
        Router-->>Agent: ToolResult error
        Agent-->>Adapter: AgentResponse(status=failed)
        Adapter-->>Channel: Send error
        Channel-->>User: Không thể kiểm tra lịch
    else Slot conflict
        Google-->>CalConnector: Conflicting events
        CalConnector-->>CalTool: Normalized events
        CalTool-->>Router: ToolResult(success=true)
        Router-->>Agent: Conflict result
        Agent-->>Adapter: AgentResponse(status=need_clarification)
        Adapter-->>Channel: Suggest another slot
        Channel-->>User: "15h bị trùng, đổi 16h được không?"
        User->>Channel: "OK 16h"
        Channel->>Adapter: Deliver message
        Adapter->>Agent: UserMessage with confirmed time
        Agent->>Router: ToolCall calendar.listEvents(new time range)
        Router->>CalTool: Execute calendar.listEvents
        CalTool->>CalConnector: ListEvents(new range)
        CalConnector->>Google: events.list
        Google-->>CalConnector: Empty list
        CalConnector-->>CalTool: No conflicts
        CalTool-->>Router: ToolResult(success=true)
        Router-->>Agent: Slot available
    else Slot available
        Google-->>CalConnector: Empty list
        CalConnector-->>CalTool: No conflicts
        CalTool-->>Router: ToolResult(success=true)
        Router-->>Agent: Slot available
    end

    Agent->>Policy: Check proposed calendar.createEvent
    Policy-->>Agent: RiskDecision(external_write, requires_approval)

    Agent->>Approval: Create ApprovalRequest(calendar.createEvent preview)
    Approval->>Store: Save pending approval
    Approval-->>Adapter: AgentResponse(status=approval_required)
    Adapter-->>Channel: Send approval preview
    Channel-->>User: Preview + Approve / Reject

    alt User rejects or approval expires
        User->>Channel: Reject / no response
        Channel->>Adapter: ApprovalDecision(rejected/expired)
        Adapter->>Approval: Resolve approval
        Approval->>Store: Save decision
        Approval-->>Agent: Not approved
        Agent-->>Adapter: AgentResponse(status=blocked)
        Adapter-->>Channel: Send cancellation
        Channel-->>User: Không tạo lịch
    else User approves
        User->>Channel: Approve
        Channel->>Adapter: ApprovalDecision(approved)
        Adapter->>Approval: Resolve approval
        Approval->>Store: Save decision
        Approval-->>Agent: Approved

        Agent->>Router: ToolCall calendar.createEvent
        Router->>CalTool: Execute calendar.createEvent
        CalTool->>CalConnector: CreateEvent
        CalConnector->>Google: events.insert
        Google-->>CalConnector: Created event
        CalConnector-->>CalTool: Event result
        CalTool-->>Router: ToolResult(success=true)
        Router-->>Agent: ToolResult

        Agent->>Store: Save audit metadata
        Agent-->>Adapter: AgentResponse(status=completed)
        Adapter-->>Channel: Send confirmation
        Channel-->>User: Đã tạo lịch
    end
```

## Implementation Checklist

- `calendar.listEvents` là read-only và có thể chạy trước approval.
- `calendar.createEvent` không được execute trước `ApprovalDecision=approved`.
- `ApprovalRequest` phải chứa preview đủ rõ: title, time, attendees, side effect.
- Reject, expire hoặc cancel đều không được gọi `calendar.createEvent`.
- Conflict handling là hỏi lại/đề xuất slot, không tự chọn giờ mới nếu người dùng chưa xác nhận.
- Tool/result/error code phải khớp `03-contracts.md`.
