# Scenario 05: Approval Revision Flow

## Purpose

Luồng chuẩn khi người dùng không muốn reject hẳn một hành động đang chờ, mà muốn chỉnh lại nội dung rồi xin duyệt lại.

Scenario này đại diện cho:

- `ApprovalDecision=revised`
- `parentApprovalId` trên approval mới
- Agent cập nhật input theo comment của user trước khi tạo approval request mới

## Sequence

```mermaid
sequenceDiagram
    autonumber

    actor User as Người dùng
    participant Channel as Message Channel
    participant Adapter as Channel Adapter
    participant Agent as Agent Core
    participant Policy as Tool Policy
    participant Approval as Approvals
    participant Router as Tool Router
    participant CalTool as Calendar Tool
    participant CalConnector as Calendar Connector
    participant Google as Google Calendar API

    User->>Channel: "Hãy tạo lịch họp chiều mai"
    Channel->>Adapter: Deliver message
    Adapter->>Agent: UserMessage

    Agent->>Policy: Check proposed calendar.createEvent
    Policy-->>Agent: RiskDecision(external_write, requires_approval)
    Agent->>Approval: Create ApprovalRequest(status=pending)
    Approval-->>Adapter: AgentResponse(status=approval_required)
    Channel-->>User: Preview + Approve / Reject / Revise

    User->>Channel: "revise đổi giờ sang 10:30 và thêm Minh"
    Channel->>Adapter: Deliver message
    Adapter->>Approval: ApprovalDecision(revised, comment)
    Approval-->>Agent: Resolve revision

    Agent->>Agent: Mark original approval revised
    Agent->>Agent: Update tool input from revision comment
    Agent->>Approval: Create new ApprovalRequest(parentApprovalId=original)
    Approval-->>Adapter: AgentResponse(status=approval_required)
    Channel-->>User: Preview revised request

    User->>Channel: Approve
    Channel->>Adapter: ApprovalDecision(approved)
    Adapter->>Approval: Resolve approval
    Approval->>Router: ToolCall calendar.createEvent
    Router->>CalTool: Execute calendar.createEvent
    CalTool->>CalConnector: CreateEvent
    CalConnector->>Google: events.insert
    Google-->>CalConnector: Created event
    CalConnector-->>CalTool: Event result
    CalTool-->>Router: ToolResult(success=true)
    Router-->>Agent: ToolResult
    Agent-->>Adapter: AgentResponse(status=completed)
    Adapter-->>Channel: Send confirmation
```

## Implementation Checklist

- `revised` must be a valid approval decision and a valid approval request status.
- The revised approval request must carry `parentApprovalId`.
- The original pending approval must stop being treated as pending once revised.
- The revised tool input should come from the user comment plus the agent's update pass.
- No external write may execute before the revised request is approved.

