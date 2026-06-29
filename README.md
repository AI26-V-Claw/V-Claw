# V-Claw

V-Claw is a local-first personal AI assistant for safe office automation and controlled computer tasks. The project is inspired by GoClaw architecture patterns, but narrows the scope to a personal assistant that can connect to Google Workspace, route work through an agent loop, call tools, and require human approval before risky actions.

The current focus is an early development/MVP foundation, not a production-ready assistant.

## Goals

V-Claw is intended to help a user:

- Read and summarize work information from Google Workspace services such as Gmail, Calendar, and Chat.
- Coordinate multi-step office workflows, for example reading email, checking calendar availability, and preparing a response or calendar action.
- Run local file/data tasks through controlled Python, shell, or sandboxed system tools.
- Route requests to different LLM providers through a common provider boundary.
- Keep risky actions under user control through policy checks, human-in-the-loop approval, and audit-friendly execution records.

## Current project state

The repository contains both architecture documentation and an initial Go codebase. Some runtime pieces already exist, including a CLI entrypoint, Google connector experiments, an early agent loop, tool registry code, and Gmail read tooling.

Not all documented workflows are implemented yet. In particular, write actions, approval flows, sandbox execution, persistence, and channel adapters should be treated as planned or in-progress unless their implementation is present in code and tests.

## Documentation map

Start with these documents:

1. [Project Brief](docs/00-project-brief.md) — product problem, safety model, roadmap, and team split.
2. [System Design](docs/01-system-design.md) — high-level system diagrams and component relationships.
3. [Usecase Diagram](docs/02-usecase-diagram.md) — expected user-facing capabilities and risk categories.
4. [Contracts](docs/03-contracts.md) — intended runtime contracts between channel, agent, safety, and tools.
5. [Canonical Sequence Scenarios](docs/04-sequences.md) — reference flows for implementation review and E2E tests.
6. [Active Modules & Ownership](ACTIVE_MODULES.md) — current implementation scope and frozen areas.
7. [Project Structure](PROJECT_STRUCTURE.md) — repository layout and module responsibilities.
8. [Production Harness Review](docs/production-harness-review.md) — release blockers, harness principles, runtime state machine, and context engineering checklist.

Additional setup guides:

- [Google Workspace Setup](configs/google/README.md) - Google Cloud OAuth, credentials, auth, and Google API smoke tests.
- [Telegram Channel Setup](internal/channels/README.md) - Telegram bot setup, HITL approval, and channel runtime commands.

When documents and code differ, treat `docs/03-contracts.md` and `ACTIVE_MODULES.md` as the intended design baseline for new work, then update code or docs explicitly as part of the task.

## Repository layout

See [PROJECT_STRUCTURE.md](PROJECT_STRUCTURE.md) for the repository layout and module boundaries.

At a high level:

- `cmd/` contains CLI entrypoints.
- `configs/` contains local configuration examples and provider setup notes.
- `docs/` contains product, architecture, contract, and scenario documentation.
- `internal/` contains private Go application packages.
- `migrations/`, `scripts/`, `skills/`, and `tests/` contain supporting project assets.

## Safety principle

Any action with side effects should pass through one safety/approval boundary before execution. This includes sending email or chat messages, creating or changing calendar events, modifying local files, or running Python/shell commands.

Read-only operations may be allowed directly when policy permits them. Destructive, external-write, local-write, or code-execution actions must be reviewed through the approved HITL flow once that flow is implemented.

## Local setup

Install the release binary once, then use the global `vclaw` command:

```powershell
.\dist\vclaw.exe install
# Open a new terminal after PATH updates.
vclaw setup
vclaw doctor
vclaw google auth  # optional, enables Google tools
vclaw start
```

For an extracted ZIP, run `./vclaw.exe install` from the extracted folder first. For development from source, use `go run ./cmd/vclaw setup`, `go run ./cmd/vclaw doctor`, and `go run ./cmd/vclaw start`. The canonical runtime command remains `vclaw telegram run`; `vclaw start` and `vclaw run` are convenience aliases.

Repo `.env` values for runtime provider config such as `OPENAI_API_KEY`, `OPENAI_MODEL`, and `OPENAI_BASE_URL` take precedence over inherited shell exports.

Google Workspace setup lives in [configs/google/README.md](configs/google/README.md). Telegram setup lives in [internal/channels/README.md](internal/channels/README.md).

## Development note

Keep implementation small and vertical-slice oriented. Do not add GoClaw-inspired platform layers unless they are required by the current roadmap or explicitly approved in `ACTIVE_MODULES.md`.
