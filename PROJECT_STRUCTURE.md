# Project Structure

This skeleton follows GoClaw's separation of concerns while narrowing V-Claw to a local-first assistant with PostgreSQL, local Docker setup, multiple LLM providers, MCP, Google Workspace connectors, and safe OS control.

```text
V-Claw/
├── cmd/                         # Local CLI entrypoints
├── configs/                     # Runtime configuration and policy templates
├── deploy/                      # Local Docker/runtime setup assets
├── docs/                        # Product, architecture, security, data, and API plans
├── internal/                    # Private application modules
│   ├── agent/                   # Personal assistant loop, planning, and intent handling
│   ├── approvals/               # Human-in-the-loop approval workflows
│   ├── audit/                   # Action logs and evidence capture
│   ├── backup/                  # (Sprint 3) Local backup and restore workflows
│   ├── cache/                   # Short-lived caches for providers, MCP, Google metadata, state
│   ├── channels/                # Message channels for chatting with and commanding agents
│   ├── config/                  # Runtime config loading and validation
│   ├── connectors/google/       # Gmail, Calendar, Drive, Chat, OAuth API clients
│   ├── crypto/                  # Encryption, hashing, and key primitives
│   ├── desktop/                 # Low-level desktop drivers only
│   ├── eventbus/                # Typed local domain events
│   ├── knowledgegraph/          # (Sprint 3) Entities and relationships across work context
│   ├── localapi/                # Optional local HTTP/WebSocket control plane
│   ├── mcp/                     # MCP server/client, transports, registry, and tool bridge
│   ├── memory/                  # User memory and work context
│   ├── notifications/           # Outbound approval, reminder, and alert delivery
│   ├── orchestration/           # Multi-step task routing and coordination
│   ├── permissions/             # Access-control checks and grants
│   ├── pipeline/                # Pluggable agent execution stages
│   ├── policies/                # Tool policy and risk classification
│   ├── providers/               # Anthropic, OpenAI, OpenAI-compatible, local model adapters
│   ├── safety/                  # Guardrails for OS and external actions
│   ├── sandbox/                 # Docker/local sandbox runtime abstractions
│   ├── scheduler/               # Cron and event-triggered jobs
│   ├── secrets/                 # Token storage and secret lifecycle
│   ├── sessions/                # Chat sessions, run lifecycle, cancellation, resume
│   ├── skills/                  # Runtime SKILL.md loading, search, and injection
│   ├── store/                   # Persistence interfaces, PostgreSQL implementation, optional SQLite
│   ├── tasks/                   # Task state, queues, workflows, and results
│   ├── tokencount/              # Token counting and context-window estimation
│   ├── tools/                   # Agent tools: office, OS, memory, scheduler
│   ├── tracing/                 # Spans, run history, metrics, observability
│   ├── upgrade/                 # Schema/app upgrade coordination
│   ├── vault/                   # (Sprint 3) Local/Drive document vault and search
│   ├── version/                 # App version and build metadata
│   └── workspace/               # Per-user workspace and file isolation
├── migrations/                  # PostgreSQL database migrations
├── scripts/                     # Developer and operator helper scripts
├── skills/                      # V-Claw-specific SKILL.md content
└── tests/                       # Contract, integration, safety, and e2e test plans
```

## GoClaw Alignment

- `internal/providers` mirrors GoClaw's provider abstraction so V-Claw can switch between Anthropic, OpenAI, OpenAI-compatible providers, and local models.
- `internal/channels` mirrors GoClaw's channel-adapter pattern so users can chat with V-Claw through CLI and messaging platforms.
- `internal/mcp` mirrors GoClaw's MCP bridge/server layer for connecting external MCP servers and exposing V-Claw tools through MCP.
- `internal/pipeline`, `internal/sessions`, `internal/eventbus`, `internal/tasks`, and `internal/tokencount` preserve GoClaw's execution and orchestration shape.
- `internal/tools`, `internal/policies`, `internal/permissions`, `internal/approvals`, `internal/safety`, `internal/sandbox`, and `internal/audit` keep all external actions controlled.
- `internal/store/pg` is the primary persistence implementation, matching the PostgreSQL-first direction.
- `internal/skills` loads/searches runtime skills, while root `skills/` stores skill content.
- `internal/localapi` combines the optional local HTTP handlers and WebSocket RPC control plane.
- `internal/desktop` is low-level driver code only; agent-facing desktop and clipboard wrappers stay under `internal/tools/os`.
- `internal/notifications` is outbound-only and may call channel adapters, while channels must not import notifications.
- `internal/backup`, `internal/vault`, and `internal/knowledgegraph` are included as Sprint 3 boundaries so they are not built too early.
- `deploy/docker` is kept for local reproducible setup, PostgreSQL, sandbox containers, and optional supporting services. It is not a cloud/server deployment target.

## Deliberately Excluded For Now

- `ui/`: no web or desktop UI in the current direction.
- `audio/`, `tts/`, `media/`: useful later, but not needed for the first local automation core.
- `edition/`, `webui/`, `updater/`: GoClaw product/runtime concerns that are not required for the initial V-Claw skeleton.
