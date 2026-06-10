# Scenario 02: Read-Only Gmail Summary

## Purpose

Luồng chuẩn cho thao tác Google Workspace read-only: đọc email theo điều kiện, lấy nội dung cần thiết và tóm tắt cho người dùng.

Scenario này đại diện cho:

- Gmail read-only tools.
- OAuth Google Workspace như external account link/refresh, không phải login của V-Claw.
- Tool execution không cần HITL vì risk level là `safe_read`.

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
    participant LLM as LLM Provider
    participant Store as Session / Audit Store

    User->>Channel: "Đọc Gmail hôm nay và tóm tắt giúp tôi"
    Channel->>Adapter: Deliver message
    Adapter->>Agent: UserMessage

    Agent->>LLM: Request with runtime-filtered tools
    LLM-->>Agent: Decide whether a Gmail tool is needed
    LLM-->>Agent: ToolCall gmail.listEmails(query=today)
    Agent->>Policy: Check gmail.listEmails
    Policy-->>Agent: RiskDecision(safe_read, allow)

    Agent->>Router: ToolCall gmail.listEmails(query=today)
    Router->>GmailTool: Execute gmail.listEmails
    GmailTool->>GmailConnector: ListMessages(query=today)
    GmailConnector->>Google: users.messages.list

    alt OAuth missing / expired / insufficient scope
        Google-->>GmailConnector: 401 / 403
        GmailConnector-->>GmailTool: ErrorShape AUTH_EXPIRED / AUTH_MISSING_SCOPE
        GmailTool-->>Router: ToolResult(success=false)
        Router-->>Agent: ToolResult error
        Agent-->>Adapter: AgentResponse(status=failed, reconnect Google required)
        Adapter-->>Channel: Send reconnect instruction
        Channel-->>User: Yêu cầu cấp lại quyền Gmail
    else Gmail API unavailable
        Google-->>GmailConnector: 429 / 5xx / timeout
        GmailConnector-->>GmailTool: ErrorShape RATE_LIMITED / PROVIDER_UNAVAILABLE
        GmailTool-->>Router: ToolResult(success=false)
        Router-->>Agent: ToolResult error
        Agent-->>Adapter: AgentResponse(status=failed)
        Adapter-->>Channel: Send retry-later message
        Channel-->>User: Thông báo lỗi tạm thời
    else Found messages
        Google-->>GmailConnector: Message summaries
        GmailConnector-->>GmailTool: Normalized summaries
        GmailTool-->>Router: ToolResult(success=true)
        Router-->>Agent: Message summaries

        loop Với email cần đọc chi tiết
            Agent->>Router: ToolCall gmail.getEmail(messageId)
            Router->>GmailTool: Execute gmail.getEmail
            GmailTool->>GmailConnector: GetMessage(messageId)
            GmailConnector->>Google: users.messages.get
            Google-->>GmailConnector: Raw message
            GmailConnector-->>GmailTool: Message detail
            GmailTool-->>Router: ToolResult(success=true)
            Router-->>Agent: Message detail
        end

        Agent->>LLM: Summarize selected email content
        LLM-->>Agent: Summary
        Agent->>Store: Save run metadata + tool results summary
        Agent-->>Adapter: AgentResponse(status=completed)
        Adapter-->>Channel: Send summary
        Channel-->>User: Bản tóm tắt Gmail
    end
```

## Implementation Checklist

- Tool names phải là `gmail.listEmails` và `gmail.getEmail`.
- Read-only Gmail flow không tạo `ApprovalRequest`.
- OAuth failure trả lỗi theo `ErrorShape`, ví dụ `AUTH_EXPIRED` hoặc `AUTH_MISSING_SCOPE`.
- Gmail connector chỉ gọi API và normalize response; không chứa agent reasoning.
- Gmail tool chịu trách nhiệm render nội dung hiển thị an toàn cho Agent/User.
- Audit/session store chỉ ghi metadata hoặc dữ liệu đã redacted khi cần.
