# Scenario 01: Channel Message To Agent Response

## Purpose

Luồng chuẩn cho việc nhận một tin nhắn từ Telegram/Slack, chuẩn hóa thành `UserMessage`, đưa vào Agent Core và trả `AgentResponse`.

Scenario này đại diện cho:

- Sprint 1 G3: nhận diện intent cơ bản.
- Sprint 1 G4: channel Telegram/Slack.
- Sprint 1 G5: agent loop tối giản.

Không mô tả login/logout. `allowed_user_id` hoặc `allowed_chat_id` là allowlist của channel trong single-owner deployment.

## Sequence

```mermaid
sequenceDiagram
    autonumber

    actor User as Người dùng
    participant Channel as Message Channel<br/>(Telegram / Slack)
    participant Adapter as Channel Adapter
    participant Agent as Agent Core
    participant Safety as Intent & Safety Layer
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

        Agent->>Safety: Classify intent + missing info + risk hint
        Safety-->>Agent: safe_read / safe_compute / needs_tool / need_clarification

        alt Thiếu thông tin
            Agent-->>Adapter: AgentResponse(status=need_clarification)
            Adapter-->>Channel: Send reply
            Channel-->>User: Hỏi lại thông tin còn thiếu
        else Có thể trả lời trực tiếp
            Agent->>LLM: Generate answer
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
- Missing information trả `AgentResponse.status=need_clarification`.
- Không gọi tool nếu intent chỉ là chat hoặc hỏi đáp thường.
- Session memory là ngắn hạn; không phụ thuộc long-term memory trong Sprint 1.
