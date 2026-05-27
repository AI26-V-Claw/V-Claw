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

## II. Usecase Diagram


## III. System architecture
```mermaid
%%{init: {
  "theme": "base",
  "themeVariables": {
    "background": "#ffffff",
    "mainBkg": "#ffffff",
    "darkMode": false,
    "fontSize": "22px",
    "fontFamily": "Arial",
    "primaryTextColor": "#111827",
    "lineColor": "#374151",
    "edgeLabelBackground": "#ffffff"
  },
  "flowchart": {
    "nodeSpacing": 110,
    "rankSpacing": 110,
    "curve": "linear",
    "htmlLabels": true
  }
}}%%
flowchart TB
    %% ===== Message Channels =====
    subgraph Channels["Message Channels"]
        TG["Telegram"]
        CHATAPP["Other Chat Channels"]
    end

    %% ===== Backend =====
    subgraph Backend["Backend / Gateway"]
        API["Backend API / Bot Server"]
    end

    %% ===== Agent Core =====
    subgraph Core["Agent Core"]
        LOOP["Agent Loop / Orchestrator"]
        SAFETY["Intent, Context & Safety Layer<br>Intent Classification / Missing Info / Risk Gate / HITL"]
        PLANNER["Task Planning & Execution Control"]
    end

    %% ===== LLM Providers =====
    subgraph LLMs["LLM Providers"]
        direction TB

        MODEL_ROUTER["Model Router"]

        subgraph LLMProviderRow[" "]
            direction LR
            OPENAI["OpenAI"]
            SP1[" "]
            ANTHROPIC["Anthropic"]
            SP2[" "]
            GEMINI["Google Gemini"]
            SP3[" "]
            LOCAL["Local LLM<br>Ollama / vLLM"]
        end
    end

    %% ===== Tools =====
    subgraph Tools["Tool Layer"]
        ROUTER["Tool Router & Executor"]
        GTOOLS["Google Workspace Tools<br>Gmail / Calendar / Chat / Drive"]
        SANDBOX["Sandbox Tools<br>Python / Shell / File Processing"]
    end

    %% ===== External =====
    subgraph External["External Services"]
        GAPI["Google Workspace API"]
        DOCKER["Docker Sandbox"]
        FILES["Workspace Files"]
    end

    %% ===== Store =====
    subgraph Store["Storage & Memory"]
        PG[("PostgreSQL<br>Tasks / Messages / Logs")]
        REDIS[("Redis<br>Short-term Context")]
        VDB[("Semantic Memory<br>Vector DB / pgvector")]
    end

    %% ===== Main Flow =====
    Channels --> API
    API --> LOOP

    LOOP --> SAFETY
    SAFETY --> PLANNER
    PLANNER --> ROUTER

    ROUTER --> GTOOLS
    ROUTER --> SANDBOX

    GTOOLS --> GAPI
    SANDBOX --> DOCKER
    DOCKER --> FILES

    %% ===== LLM Flow =====
    LOOP --> MODEL_ROUTER

    MODEL_ROUTER --> OPENAI
    MODEL_ROUTER --> ANTHROPIC
    MODEL_ROUTER --> GEMINI
    MODEL_ROUTER --> LOCAL

    %% ===== Feedback / Result Flow =====
    ROUTER --> LOOP
    SAFETY -. "cần duyệt / hỏi lại" .-> API

    %% ===== Storage Flow =====
    LOOP --> PG
    LOOP --> REDIS
    LOOP --> VDB
    ROUTER --> PG

    %% ===== Classes =====
    classDef channel fill:#E3F2FD,stroke:#1565C0,stroke-width:3px,color:#0D47A1,font-size:22px
    classDef backend fill:#E8F5E9,stroke:#2E7D32,stroke-width:3px,color:#1B5E20,font-size:22px
    classDef core fill:#FFF3E0,stroke:#EF6C00,stroke-width:3px,color:#E65100,font-size:22px
    classDef safety fill:#FFEBEE,stroke:#C62828,stroke-width:4px,color:#B71C1C,font-size:22px
    classDef llm fill:#EDE7F6,stroke:#512DA8,stroke-width:3px,color:#311B92,font-size:22px
    classDef tool fill:#F3E5F5,stroke:#7B1FA2,stroke-width:3px,color:#4A148C,font-size:22px
    classDef external fill:#E0F7FA,stroke:#00838F,stroke-width:3px,color:#006064,font-size:22px
    classDef store fill:#FCE4EC,stroke:#AD1457,stroke-width:3px,color:#880E4F,font-size:22px
    classDef hidden fill:transparent,stroke:transparent,color:transparent

    class TG,CHATAPP channel
    class API backend
    class LOOP,PLANNER core
    class SAFETY safety
    class MODEL_ROUTER,OPENAI,ANTHROPIC,GEMINI,LOCAL llm
    class ROUTER,GTOOLS,SANDBOX tool
    class GAPI,DOCKER,FILES external
    class PG,REDIS,VDB store
    class SP1,SP2,SP3 hidden

    %% ===== Subgraph Styles =====
    style Channels fill:#F5FAFF,stroke:#90CAF9,stroke-width:2px
    style Backend fill:#F6FFF7,stroke:#A5D6A7,stroke-width:2px
    style Core fill:#FFF8EF,stroke:#FFCC80,stroke-width:2px
    style LLMs fill:#F5F0FF,stroke:#B39DDB,stroke-width:2px
    style LLMProviderRow fill:transparent,stroke:transparent
    style Tools fill:#FBF2FF,stroke:#CE93D8,stroke-width:2px
    style External fill:#F0FDFF,stroke:#80DEEA,stroke-width:2px
    style Store fill:#FFF3F7,stroke:#F48FB1,stroke-width:2px

    %% ===== Thicker Arrows =====
    linkStyle default stroke:#374151,stroke-width:2.5px,fill:none
    linkStyle 16 stroke:#C62828,stroke-width:2.5px,stroke-dasharray:6 4,fill:none
```