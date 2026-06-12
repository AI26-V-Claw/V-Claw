# Project Structure

This document explains the intended repository layout for V-Claw. It describes module boundaries at a stable level, not the exact implementation status of every package.

For current implementation scope, ownership, and frozen areas, use [ACTIVE_MODULES.md](ACTIVE_MODULES.md) as the source of truth.

## Top-level layout

```text
V-Claw/
├── cmd/                  # CLI entrypoints and thin command wiring
├── configs/              # Local configuration examples and provider setup notes
├── deploy/               # Local runtime, Docker, and sandbox setup assets
├── docs/                 # Product, architecture, contract, and scenario documents
├── internal/             # Private Go application packages
├── migrations/           # Database migrations when persistence is enabled
├── scripts/              # Developer and operator helper scripts
├── skills/               # V-Claw-specific skill content
└── tests/                # Contract, integration, safety, and E2E tests
```

## Main directories

### `cmd/`

Contains executable entrypoints such as the local `vclaw` CLI.

Command packages should stay thin:

- parse arguments and configuration;
- call application/bootstrap code;
- avoid embedding agent, connector, or business logic directly.

### `configs/`

Contains local configuration examples and setup notes for providers or external services.

This directory may include templates or documentation for OAuth credentials, local tokens, sandbox policy, and provider configuration. Real secrets and local tokens must not be committed.

### `deploy/`

Contains local runtime assets such as Docker Compose files, sandbox containers, and reproducible development setup.

V-Claw is currently oriented toward local development and local-first use. Deployment assets should support reproducibility and sandboxing rather than imply a hosted production platform unless that scope is explicitly approved.

### `docs/`

Contains project documentation:

- product brief and roadmap;
- system design diagrams;
- usecase diagrams;
- runtime contracts;
- canonical sequence scenarios;
- future ADRs or design decisions.

The most important contract and workflow docs are:

- `docs/00-project-brief.md`
- `docs/01-system-design.md`
- `docs/02-usecase-diagram.md`
- `docs/03-contracts.md`
- `docs/04-sequences.md`

### `internal/`

Contains private Go packages for the V-Claw application. Package names should follow clear responsibility boundaries and should not expose product APIs directly.

Key package groups:

| Area | Responsibility |
|---|---|
| `internal/agent/` | Agent loop, planning, request handling, and tool-call orchestration. |
| `internal/approvals/` | Human-in-the-loop approval state and approve/reject flow. |
| `internal/audit/` | Action logs and execution evidence. |
| `internal/channels/` | User-facing message adapters such as Telegram or Slack. |
| `internal/connectors/` | Raw external API clients and adapters. |
| `internal/connectors/google/` | Gmail, Calendar, Chat, OAuth, and related Google Workspace clients. |
| `internal/contracts/` | Shared runtime objects when implemented: messages, tool calls, results, risk, approvals, and errors. |
| `internal/notifications/` | Outbound notifications for approvals, reminders, or alerts. |
| `internal/policies/` | Tool policy and risk classification. |
| `internal/providers/` | LLM provider abstractions and adapters. |
| `internal/safety/` | Safety checks for external writes, local writes, destructive actions, and code execution. |
| `internal/sandbox/` | Controlled Python/shell execution and sandbox policy. |
| `internal/sessions/` | Conversation or run lifecycle state when persistence is introduced. |
| `internal/skills/` | Runtime skill loading and lookup. |
| `internal/store/` | Persistence interfaces and database implementation when needed. |
| `internal/tasks/` | Task state, queues, workflows, and results when needed. |
| `internal/tools/` | Agent-callable tool interfaces, registry, and wrappers. |
| `internal/workspace/` | Local workspace and file isolation. |

Some GoClaw-inspired areas may exist as reserved boundaries but are not necessarily active MVP scope. Examples include MCP, event bus, advanced pipeline/orchestration, tracing, backup, vault, desktop UI, and upgrade management. Check `ACTIVE_MODULES.md` before implementing these areas.

## Boundary rules

### Connectors vs tools

```text
connectors = raw external API clients
tools      = agent-callable operations
```

Connectors should not know about agent reasoning, HITL, or tool-call contracts. Tools can call connectors and should expose risk metadata and input/output shapes to the agent/safety layer.

Example:

```text
internal/connectors/google/gmail/
  - handles Gmail API calls, OAuth client usage, and raw API responses

internal/tools/office/gmail/
  - exposes agent-callable Gmail operations
  - validates tool input/output
  - declares risk level and approval requirement
```

### Safety boundary

Actions with side effects must go through a single safety/approval boundary before execution. This includes external writes, local file changes, destructive operations, and Python/shell execution.

### Local-first scope

Prefer a small, working vertical slice before adding platform-level abstractions. Do not add broad infrastructure simply because it exists in GoClaw unless it directly supports the current V-Claw roadmap.

## Tests and support assets

- `tests/contracts/` should cover shared contract compatibility.
- `tests/safety/` should cover risk classification, approval gates, and sandbox policy.
- `tests/e2e/` should cover a small number of canonical flows from `docs/04-sequences.md`.
- `tests/fixtures/` should hold shared mock data for integration and agent-core tests.

## Non-goals for the current structure

The current repository structure is not meant to guarantee that every listed package is already implemented. It is also not a commitment to build every GoClaw subsystem.

Current MVP work should stay aligned with the project brief, contracts, canonical scenarios, and active-module guidance.
