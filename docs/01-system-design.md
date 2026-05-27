## I. Context Diagram

```mermaid
flowchart LR
    user(["Người dùng"])
    vclaw(["V-Claw Assistant\n(GoClaw Gateway + Agent Loop)"])

    gws(["Google Workspace\n(Gmail / Calendar / Chat)"])
    msg(["Message Channels\n(Telegram / Slack)"])
    sandbox(["Sandbox OS / File System"])

    user --> |"Gửi tin"| msg
    msg --> |"Phản hồi"| user

    msg --> |"Tin nhắn"| vclaw
    vclaw --> |"Tin nhắn"| msg

    vclaw --> |"Email / Lịch / Chat"| gws
    gws --> |"Dữ liệu"| vclaw

    vclaw --> |"Python / Shell"| sandbox
    sandbox --> |"Kết quả"| vclaw
```

- Hệ thống trung tâm là V‑Claw Assistant, chịu trách nhiệm điều phối agent loop và tool call.
- Người dùng tương tác qua Message Channels; không giao tiếp trực tiếp với V‑Claw.
- V‑Claw tích hợp Google Workspace và Sandbox để xử lý tác vụ.
- Các mũi tên thể hiện luồng yêu cầu/kết quả giữa các thành phần.

## II. Usecase Diagram


## III. System architecture

