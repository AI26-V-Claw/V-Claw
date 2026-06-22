# Scenario 01: Channel Message To Agent Response

## Purpose

Luồng chuẩn cho việc nhận một tin nhắn từ Telegram, chuẩn hóa thành `UserMessage`, đưa vào Agent Core và trả `AgentResponse`.

Scenario này đại diện cho:

- Sprint 1 G3: định tuyến lượt chat và clarify tối thiểu.
- Sprint 1 G4: channel Telegram.
- Sprint 1 G5: agent loop tối giản.

Không mô tả login/logout. `allowed_user_id` hoặc `allowed_chat_id` là allowlist của channel trong single-owner deployment.

## Sequence

```mermaid
sequenceDiagram
    autonumber

    actor User as Người dùng
    participant Channel as Message Channel<br/>(Telegram)
    participant Adapter as Channel Adapter
    participant Agent as Agent Core
    participant LLM as LLM Provider
    participant Memory as Session Memory

    User->>Channel: Gửi tin nhắn
    Channel->>Adapter: Deliver update / event

    Adapter->>Adapter: Kiểm tra channel allowlist

    alt Không được phép
        Adapter-->>Channel: Ignore hoặc unauthorized response
        Channel-->>User: Không xử lý yêu cầu
    else Được phép
        Adapter->>Adapter: Chuẩn hóa thành UserMessage
        Adapter->>Agent: UserMessage

        Agent->>Memory: Load session context
        Memory-->>Agent: Recent messages / context

        alt Thiếu thông tin bắt buộc
            Agent->>LLM: Request with runtime-filtered tools and clarify tool
            LLM-->>Agent: clarify(question)
            Agent-->>Adapter: AgentResponse(status=need_clarification)
            Adapter-->>Channel: Send reply
            Channel-->>User: Hỏi lại thông tin còn thiếu
        else Có thể trả lời trực tiếp
            Agent->>LLM: Decide answer vs tool call
            LLM-->>Agent: Response text
            Agent->>Memory: Save user message + response
            Agent-->>Adapter: AgentResponse(status=completed)
            Adapter-->>Channel: Send reply
            Channel-->>User: Hiển thị phản hồi
        end
    end
```

## Implementation Checklist

- Channel adapter chỉ chuẩn hóa input thành `UserMessage`; không chứa agent reasoning.
- Runtime contract không thêm `userId`; owner được kiểm soát ở channel/config boundary.
- Missing information trả `AgentResponse.status=need_clarification` qua internal `clarify` tool.
- Không expose tool nếu lượt chat chỉ là chào hỏi hoặc hỏi đáp an toàn.
- Session memory là ngắn hạn; không phụ thuộc long-term memory trong Sprint 1.
