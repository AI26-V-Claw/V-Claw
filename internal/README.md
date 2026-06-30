# internal

Private Go application modules for V-Claw.

## Active Runtime Areas

| Area | Current role |
|---|---|
| `app` | Composition root for runtime, providers, tools, policies, monitoring, and storage wiring. |
| `agent` | Agent loop, tool orchestration, runtime state, approvals, and response formatting. |
| `channels/telegram` | Telegram owner channel, sessions, policy UI, approval buttons, and bot polling. |
| `connectors/google` | Raw Google Workspace API clients and OAuth helpers. |
| `tools` | Agent-callable tool interface, registry, built-ins, office tools, OS tools, memory, web. |
| `policies`, `safety`, `filesafety` | Risk classification, policy decisions, file safety, and approval gating inputs. |
| `sandbox` | Controlled shell/Python execution boundary and Docker runtime helpers. |
| `providers` | LLM provider interfaces and OpenAI/Gemini adapters. |
| `store/pg` | PostgreSQL persistence and embedded migration application. |
| `monitoring`, `audit`, `localapi/control` | Health/status, audit events, and local runtime control helpers. |

## Skeleton / Future Boundaries

Some directories intentionally exist as architectural placeholders. Do not treat their existence as implementation approval. Check `../ACTIVE_MODULES.md` before adding work in `backup`, `desktop`, `eventbus`, `mcp`, `notifications`, `permissions`, `tasks`, `upgrade`, `vault`, or similar platform layers.

## Boundary Rules

- `cmd` parses CLI and delegates; core behavior belongs under `internal`.
- `connectors` call external APIs and should not know about agent reasoning or HITL.
- `tools` wrap connectors/local operations for agent use and carry risk metadata.
- Side effects must pass through the policy/approval boundary before execution.
- Channel adapters receive/send messages; they must not bypass the agent runtime.
