## I. Context Diagram

```mermaid
%%{init: {"theme": "base", "themeVariables": {"background": "#ffffff", "mainBkg": "#ffffff", "darkMode": false, "lineColor": "#374151"}}}%%
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

## II. System architecture
```mermaid
flowchart LR
  subgraph CH["Channels"]
    direction TB
    TG["Telegram"]
    CHAT["Chat Apps"]
  end

  subgraph BE["Backend"]
    API["API / Bot Server"]
  end

  subgraph CORE["Agent Core"]
    direction TB
    LOOP["Agent Loop"]
    TR["Turn Router"]
    LOOP --> TR
  end

  subgraph TOOLS["Tool Layer"]
    direction TB
    ROUTER["Tool Router & Executor"]
    CLARIFY["Clarify Tool"]
    GTOOLS["Google Workspace"]
    SANDBOX["Sandbox Tools"]

    ROUTER --> CLARIFY
    ROUTER --> GTOOLS
    ROUTER --> SANDBOX
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
    GAPI["Google API"]
    DOCKER["Docker Sandbox"]
  end

  subgraph STORE["Storage"]
    direction TB
    PG[("PostgreSQL")]
    VDB[("Vector DB")]
  end

  CH --> API
  API --> LOOP

  LOOP --> MROUTER
  LOOP --> PG
  LOOP --> VDB

  TR --> ROUTER

  ROUTER --> POLICY
  POLICY --> GTOOLS
  POLICY --> SANDBOX

  ROUTER --> LOOP

  HITL --> LOOP
  HITL --> PG

  CLARIFY -. need_clarification .-> API
  HITL -. approval UI .-> API
  API -. decision .-> HITL

  GTOOLS --> GAPI
  SANDBOX --> DOCKER

  ROUTER --> PG
```

## III. Usecase Diagram

[Xem Usecase Diagram](02-usecase-diagram.md)
