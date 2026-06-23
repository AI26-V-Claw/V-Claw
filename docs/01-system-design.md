## I. Context Diagram

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
flowchart LR
    user(["Người dùng"])
    vclaw(["V-Claw Assistant\n(GoClaw Gateway + Agent Loop)"])

    gws(["Google Workspace\n(Gmail / Calendar / Chat)"])
    msg(["Message Channels\n(Telegram)"])
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

## II. System Architecture

Phần này mô tả các khối chính của hệ thống và quan hệ phụ thuộc tĩnh giữa
chúng. Luồng xử lý runtime chi tiết được tách sang `04-sequences.md` để tránh
trộn component diagram với sequence/processing flow.

```mermaid
flowchart LR
  subgraph CH["Channels"]
    direction TB
    TG["Telegram"]
  end

  subgraph BE["Backend"]
    API["API / Bot Server"]
  end

  subgraph CORE["Agent Core"]
    direction TB
    LOOP["Agent Loop"]
    MEMORY["Session / Memory"]
  end

  subgraph TOOLS["Tool Layer"]
    direction TB
    REGISTRY["Tool Registry"]
    GTOOLS["Workspace Tools"]
    SANDBOX["Sandbox Tools"]
    INTERNAL["Internal Tools"]
  end

  subgraph SAFETY["Safety Layer"]
    direction TB
    POLICY["Tool Policy"]
    HITL["HITL Manager"]

    POLICY -. approval .-> HITL
  end

  subgraph LLM["LLM Providers"]
    direction TB
    MROUTER["Model Router"]
    MODELS["OpenAI / Anthropic<br/>Gemini / Local"]

    MROUTER --> MODELS
  end

  subgraph EXT["External Services"]
    direction TB
    GAPI["Google APIs"]
    DOCKER["Docker Sandbox"]
  end

  subgraph STORE["Storage"]
    direction TB
    PG[("PostgreSQL")]
    VDB[("Vector DB")]
  end

  CH --> API
  API --> CORE

  CORE --> LLM
  CORE --> TOOLS
  CORE --> SAFETY
  CORE --> STORE

  TOOLS --> SAFETY
  TOOLS --> EXT
  TOOLS --> STORE

  SAFETY --> STORE
  SAFETY -. approval UI .-> API

  GTOOLS --> GAPI
  SANDBOX --> DOCKER
```

### 2.1 Component Responsibilities

| Khối | Trách nhiệm |
|---|---|
| Channels | Nhận/gửi tin qua Telegram hoặc chat app tương đương. |
| Backend | Chuẩn hóa request từ channel, gọi Agent Core, trả response về channel. |
| Agent Core | Điều phối agent loop, session/memory, model calls và tool calls. |
| Tool Layer | Đăng ký tool, validate schema, gọi Workspace/Sandbox/Internal tools. |
| Safety Layer | Phân loại risk, quyết định allow/block/approval, quản lý HITL. |
| LLM Providers | Định tuyến model và gọi OpenAI/Anthropic/Gemini/Local provider. |
| External Services | Google APIs và Docker sandbox mà tool layer gọi ra ngoài. |
| Storage | PostgreSQL cho runtime/audit/session; Vector DB cho retrieval/memory khi cần. |

### 2.2 Runtime Flow Reference

Các bước xử lý request, tool call, approval và trả kết quả được mô tả ở
`04-sequences.md`. Contract chi tiết nằm ở `03-contracts.md`.

## III. Usecase Diagram

[Xem Usecase Diagram](02-usecase-diagram.md)
