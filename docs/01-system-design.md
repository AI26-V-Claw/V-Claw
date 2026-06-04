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
---
config:
  theme: base
  themeVariables:
    background: '#ffffff'
    mainBkg: '#ffffff'
    darkMode: false
    fontSize: 22px
    fontFamily: Arial
    primaryTextColor: '#111827'
    lineColor: '#374151'
    edgeLabelBackground: '#ffffff'
  flowchart:
    nodeSpacing: 110
    rankSpacing: 110
    curve: linear
    htmlLabels: true
---
flowchart TB
 subgraph LLMs["LLM Providers"]
    direction TB
        MODEL_ROUTER["Model Router"]
        LLM_ALL["OpenAI / Anthropic<br>Gemini / Local LLM"]
        MODEL_ROUTER --> LLM_ALL
  end
 subgraph Channels["Message Channels"]
        TG["Telegram"]
        CHATAPP["Other Chat Channels"]
  end
 subgraph Backend["Backend / Gateway"]
        API["Backend API / Bot Server"]
  end
 subgraph Core["Agent Core"]
        LOOP["Agent Loop / Orchestrator"]
        TURN_ROUTER["Turn Router<br>Tool Exposure Only<br>no_tool / tool_enabled / blocked_prompt_injection"]
        POLICY["Tool Policy Boundary<br>Risk / Approval Decision"]
        HITL["HITL Approval Manager<br>Approve / Reject / Revise"]
        CLARIFY["Clarify Tool<br>Pending User Input"]
  end
 subgraph Tools["Tool Layer"]
        ROUTER["Tool Router & Executor"]
        GTOOLS["Google Workspace Tools<br>Gmail / Calendar / Chat"]
        SANDBOX["Sandbox Tools<br>Python / Shell / File Processing"]
  end
 subgraph External["External Services"]
        GAPI["Google Workspace API"]
        DOCKER["Docker Sandbox"]
        FILES["Workspace Files"]
  end
 subgraph Store["Storage & Memory"]
        PG[("PostgreSQL<br>Tasks / Messages / Logs / Approvals")]
        REDIS[("Redis<br>Short-term Context")]
        VDB[("Semantic Memory<br>Vector DB / pgvector")]
  end

    Channels --> API
    API --> LOOP
    LOOP --> TURN_ROUTER & PG & REDIS & VDB
    LOOP --> MODEL_ROUTER
    LOOP --> ROUTER
    LOOP --> CLARIFY
    ROUTER --> POLICY
    POLICY --> GTOOLS & SANDBOX
    ROUTER --> LOOP & PG
    GTOOLS --> GAPI
    SANDBOX --> DOCKER
    DOCKER --> FILES
    POLICY -. requires approval .-> HITL
    CLARIFY -. need_clarification .-> API
    HITL -. approval UI .-> API
    API -. approval decision / revision comment .-> HITL
    HITL --> LOOP & PG

    TG:::channel
    CHATAPP:::channel
    API:::backend
    LOOP:::core
    TURN_ROUTER:::safety
    POLICY:::safety
    HITL:::hitl
    CLARIFY:::core
    MODEL_ROUTER:::llm
    LLM_ALL:::llm
    ROUTER:::tool
    GTOOLS:::tool
    SANDBOX:::tool
    GAPI:::external
    DOCKER:::external
    FILES:::external
    PG:::store
    REDIS:::store
    VDB:::store

    classDef channel fill:#E3F2FD,stroke:#1565C0,stroke-width:3px,color:#0D47A1,font-size:22px
    classDef backend fill:#E8F5E9,stroke:#2E7D32,stroke-width:3px,color:#1B5E20,font-size:22px
    classDef core fill:#FFF3E0,stroke:#EF6C00,stroke-width:3px,color:#E65100,font-size:22px
    classDef safety fill:#FFEBEE,stroke:#C62828,stroke-width:4px,color:#B71C1C,font-size:22px
    classDef hitl fill:#FFFDE7,stroke:#F9A825,stroke-width:4px,color:#7A4F00,font-size:22px
    classDef llm fill:#EDE7F6,stroke:#512DA8,stroke-width:3px,color:#311B92,font-size:22px
    classDef tool fill:#F3E5F5,stroke:#7B1FA2,stroke-width:3px,color:#4A148C,font-size:22px
    classDef external fill:#E0F7FA,stroke:#00838F,stroke-width:3px,color:#006064,font-size:22px
    classDef store fill:#FCE4EC,stroke:#AD1457,stroke-width:3px,color:#880E4F,font-size:22px

    style LLMs fill:#F5F0FF,stroke:#B39DDB,stroke-width:2px
    style Channels fill:#F5FAFF,stroke:#90CAF9,stroke-width:2px
    style Backend fill:#F6FFF7,stroke:#A5D6A7,stroke-width:2px
    style Core fill:#FFF8EF,stroke:#FFCC80,stroke-width:2px
    style Tools fill:#FBF2FF,stroke:#CE93D8,stroke-width:2px
    style External fill:#F0FDFF,stroke:#80DEEA,stroke-width:2px
    style Store fill:#FFF3F7,stroke:#F48FB1,stroke-width:2px
```

## III. Usecase Diagram

[Xem Usecase Diagram](02-usecase-diagram.md)
