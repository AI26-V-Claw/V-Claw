# Scenario 06: Auto-Allow Policy

## Purpose

Luồng chuẩn khi owner cấu hình policy để tự động cho qua các risk level thấp, không cần tạo approval request.

Scenario này đại diện cho:

- `UserPolicyConfig.auto_allow`
- `RiskDecision=allow` từ policy layer
- Tool execution đi thẳng qua router khi không cần HITL

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
    participant GmailTool as Gmail Tool
    participant GmailConnector as Gmail Connector
    participant Google as Gmail API

    User->>Channel: "Đọc danh sách email gần đây và tóm tắt giúp tôi"
    Channel->>Adapter: Deliver message
    Adapter->>Agent: UserMessage

    Agent->>Policy: Check gmail.listEmails
    Policy-->>Agent: RiskDecision(safe_read, allow)
    Agent->>Router: ToolCall gmail.listEmails
    Router->>GmailTool: Execute gmail.listEmails
    GmailTool->>GmailConnector: ListMessages
    GmailConnector->>Google: users.messages.list
    Google-->>GmailConnector: Messages
    GmailConnector-->>GmailTool: Normalized summaries
    GmailTool-->>Router: ToolResult(success=true)
    Router-->>Agent: ToolResult

    Agent->>Router: ToolCall gmail.getEmail if needed
    Router->>GmailTool: Execute gmail.getEmail
    GmailTool->>GmailConnector: GetMessage
    GmailConnector->>Google: users.messages.get
    Google-->>GmailConnector: Raw message
    GmailConnector-->>GmailTool: Message detail
    GmailTool-->>Router: ToolResult(success=true)
    Router-->>Agent: ToolResult

    Agent-->>Adapter: AgentResponse(status=completed)
    Adapter-->>Channel: Send summary
```

## Implementation Checklist

- `auto_allow` phải được lưu trong `UserPolicyConfig`.
- Nếu risk level nằm trong `auto_allow`, policy layer phải trả `allow`.
- Không tạo `ApprovalRequest` cho risk level đã auto-allowed.
- Luồng này vẫn phải ghi nhận `RiskDecision` để audit.

