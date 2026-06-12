# Test Matrix

This file maps current V-Claw product behavior to proof. It is a shared product
reference, not a Khang-only harness backlog. Keep rows aligned with code, tests,
`ACTIVE_MODULES.md`, and the canonical contracts/sequences.

## Status Values

| Status | Meaning |
| --- | --- |
| planned | Accepted as intended behavior, not implemented |
| in_progress | Actively being built or partially implemented |
| implemented | Implemented and proof exists |
| changed | Contract changed after earlier implementation |
| retired | No longer part of the product contract |

## Matrix

| Area | Contract / Behavior | Unit | Integration | E2E | Platform | Status | Evidence |
| --- | --- | --- | --- | --- | --- | --- | --- |
| Agent runtime | Route a user turn through runtime state, tool calls, clarification, approvals, and formatted output | yes | yes | no | no | implemented | `internal/agent/*_test.go`, `cmd/vclaw/agent_test.go` |
| Safety policy | Detect and gate risky shell/python/local/external actions before execution | yes | yes | no | no | implemented | `internal/safety/*_test.go`, `internal/policies/*_test.go`, `internal/sandbox/gate/*_test.go` |
| HITL approvals | Create, revise, expire, and route approval decisions for risky actions | yes | yes | partial | no | in_progress | `internal/agent/runtime_*approval*_test.go`, `cmd/vclaw/approvals.go`, `internal/channels/*_test.go` |
| Google Gmail tools | List/get/search/render Gmail content and support draft, send, modify, and attachment workflows with risk metadata | yes | yes | no | no | implemented | `internal/tools/office/gmail/*_test.go`, `internal/connectors/google/gmail/*_test.go`, `cmd/vclaw/google_gmail_test.go` |
| Google Calendar tools | Read and mutate calendar events through connector/tool boundaries | yes | yes | no | no | implemented | `internal/tools/office/calendar/*_test.go`, `internal/connectors/google/calendar/*_test.go` |
| Google Chat tools | Read and send/update/delete Chat messages/spaces through connector/tool boundaries | yes | yes | no | no | implemented | `internal/tools/office/chat/*_test.go`, `internal/connectors/google/chat/*_test.go`, `cmd/vclaw/google_chat_test.go` |
| People tools | Query Google People/contact data through connector/tool layers | yes | yes | no | no | implemented | `internal/tools/office/people/*_test.go`, `internal/connectors/google/people` |
| Channel adapters | Telegram and Slack receive messages, format responses, and carry approval interactions | yes | yes | no | no | in_progress | `internal/channels/telegram/*_test.go`, `internal/channels/slack/*_test.go`, `internal/channels/README.md` |
| Sandbox execution | Run shell/python jobs through guarded workspace, policy gate, Docker runtime, and timeout handling | yes | yes | no | yes | implemented | `internal/sandbox/runtime/*_test.go`, `internal/sandbox/gate/*_test.go`, `internal/tools/system/sandbox/*_test.go` |
| Filesystem tools | Restrict local file operations through path guard and tool risk metadata | yes | yes | no | no | implemented | `internal/tools/os/filesystem/*`, `internal/policies/*_test.go` |
| Web tools | Register and execute web search/tool behavior with provider boundaries | yes | yes | no | no | implemented | `internal/tools/web/tool_test.go`, `internal/connectors/tavily/*` |
| Audit and logs | Store/read audit records for CLI logs, approvals, and monitoring surfaces | yes | yes | no | partial | in_progress | `internal/audit/*_test.go`, `cmd/vclaw/logs.go`, `cmd/vclaw/approvals.go`, `migrations/001_init_vclaw_schema.sql` |
| Monitoring | Expose runtime metrics and HTTP monitoring endpoints | yes | yes | no | partial | implemented | `internal/monitoring/*_test.go`, `cmd/vclaw/metrics_server.go`, `cmd/vclaw/monitoring_cli.go` |
| Persistence migrations | Define Postgres audit/runtime persistence tables | no | partial | no | yes | in_progress | `migrations/001_init_vclaw_schema.sql`, `docs/runbook.md` |

## Evidence Rules

- Unit proof covers pure domain, routing, policy, formatting, and validation rules.
- Integration proof covers connectors, tool contracts, channel adapter behavior, database schema assumptions, and provider boundaries.
- E2E proof covers user-visible multi-system flows through a real channel/provider.
- Platform proof covers Docker sandboxing, Postgres migrations, local CLI behavior, deployment assets, and runtime processes.
- Mark a row `implemented` only when code and proof both exist; use `in_progress` when implementation exists but real E2E/platform proof is still incomplete.
