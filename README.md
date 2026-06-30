# V-Claw

<p align="center">
  <strong>Local-first AI assistant for safe office automation and controlled computer tasks.</strong>
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Platform" src="https://img.shields.io/badge/platform-Windows%20%7C%20local--first-blueviolet">
  <img alt="Interface" src="https://img.shields.io/badge/interface-CLI%20%7C%20Telegram-2CA5E0?logo=telegram&logoColor=white">
  <img alt="Safety" src="https://img.shields.io/badge/safety-HITL%20approvals-16A34A">
  <img alt="Status" src="https://img.shields.io/badge/status-MVP%20hardening-orange">
</p>

---

## What V-Claw Does

V-Claw is a personal assistant runtime that connects a Telegram chat or local CLI to an agent loop, Google Workspace tools, safe local tools, policy checks, approvals, and audit-friendly state.

It is designed for office workflows where the assistant can read context, propose actions, and only perform risky writes after the owner approves them.

**Current MVP capabilities**

- **CLI + Telegram runtime**: `vclaw start` / `vclaw telegram run` starts the Telegram bot runtime.
- **Terminal setup and diagnostics**: `vclaw install`, `vclaw setup`, and `vclaw doctor` support the Windows release flow.
- **Google Workspace tools**: Gmail, Calendar, Chat, Drive, Docs, Sheets, Meet, and People connectors/tools are wired behind OAuth and tool modes.
- **Local tools**: filesystem, sandbox shell/Python, memory, calculator, time, and Tavily-backed web search/fetch when configured.
- **Safety and HITL**: risky tool calls carry risk metadata and are routed through policy/approval behavior before mutation.
- **Runtime visibility**: status, logs, approvals, monitoring health, and PostgreSQL-backed audit paths are available when configured.

**Still intentionally local/MVP**

- V-Claw is not a hosted service.
- Desktop UI, MCP lifecycle, upgrades, backup, and advanced platform layers are skeleton/future boundaries unless code and tests prove otherwise.
- Google and web tools can be disabled or required through release config modes.

---

## Quick Start For Windows Users

### Option A: Download / unzip release package

1. Extract `vclaw-windows-amd64.zip`.
2. Double-click `start-vclaw.cmd`.
3. Follow the terminal prompts for API keys and Telegram owner ID.

The ZIP also includes `install-vclaw.cmd` if you want the global `vclaw` command added to your user PATH.

### Option B: Build and install from this checkout

```powershell
go build -o .\vclaw.exe .\cmd\vclaw
.\vclaw.exe install
# Open a new terminal after PATH updates.
vclaw setup
vclaw doctor
vclaw google auth  # optional; enables Google Workspace tools
vclaw start
```

`dist/` is a generated release-artifact folder and is not present in a fresh clone. Build a local `vclaw.exe` first, or use the source commands below without installing.

### Option C: Run from source

```powershell
go run ./cmd/vclaw setup
go run ./cmd/vclaw doctor
go run ./cmd/vclaw start
```

The canonical runtime command remains `vclaw telegram run`; `vclaw start` and `vclaw run` are convenience aliases.

---

## Command Cheat Sheet

| Command | Purpose |
|---|---|
| `vclaw install` | Copy `vclaw.exe` to `%LOCALAPPDATA%\Programs\V-Claw\bin` and add it to user PATH. |
| `vclaw setup` | Create/update `.env`, prompt for required secrets, and write safe release defaults. |
| `vclaw doctor` | Validate release blockers and warnings before startup. |
| `vclaw start` / `vclaw run` | Start the Telegram runtime with existing defaults. |
| `vclaw telegram run` | Canonical Telegram runtime command. |
| `vclaw agent -prompt "..."` | Run one local CLI agent turn for development/debugging. |
| `vclaw google auth` | Run Google OAuth and write `configs/google/token.json`. |
| `vclaw tools list` | Show registered built-in/local tools available without runtime OAuth. |
| `vclaw status` | Query the monitoring server started by the runtime. |
| `vclaw logs` / `vclaw approvals` | Inspect audit-backed logs and approval records when Postgres is configured. |

---

## Configuration Model

`vclaw setup` creates `.env` from `.env.example` when available, or from a built-in release template in standalone installs.

Important settings:

```env
OPENAI_API_KEY=
TELEGRAM_BOT_TOKEN=
ALLOWED_TELEGRAM_USER_ID=
VCLAW_SKILL_NUDGE_INTERVAL=0
VCLAW_GOOGLE_TOOLS_MODE=auto
VCLAW_WEB_TOOLS_MODE=auto
```

Tool modes:

| Mode | Google / Web behavior |
|---|---|
| `auto` | Register tools only when credentials/API keys exist. |
| `required` | Fail startup/doctor if dependencies are missing. |
| `off` | Keep that tool family disabled intentionally. |

Secrets and tokens that must stay local:

- `.env`
- `configs/google/credentials.json`
- `configs/google/token.json`
- generated runtime data under `data/`, `logs/`, `.sandbox-workspace/`, and `cache/`

---

## Safety Model

V-Claw treats mutation as the dangerous boundary.

- Read-only or compute-only tools may run automatically when policy allows them.
- Sensitive reads, external writes, local writes, code execution, and destructive actions are risk-classified.
- Side-effecting tools must pass through one policy/approval boundary before execution.
- Destructive actions are blocked by default policy unless explicitly changed by the owner.

Useful references:

- [Contracts](docs/03-contracts.md)
- [Canonical Sequences](docs/04-sequences.md)
- [Production Harness Review](docs/production-harness-review.md)
- [Office Tool Risk Matrix](internal/tools/office/README.md)

---

## Documentation Map

| Need | Start here |
|---|---|
| Product goal and roadmap | [docs/00-project-brief.md](docs/00-project-brief.md) |
| Architecture overview | [docs/01-system-design.md](docs/01-system-design.md) |
| Runtime contracts | [docs/03-contracts.md](docs/03-contracts.md) |
| Canonical flows | [docs/04-sequences.md](docs/04-sequences.md) and [docs/scenarios/](docs/scenarios/) |
| Local operations | [docs/runbook.md](docs/runbook.md) |
| Test/readiness matrix | [docs/TEST_MATRIX.md](docs/TEST_MATRIX.md) |
| Google setup | [configs/google/README.md](configs/google/README.md) |
| Telegram setup | [internal/channels/README.md](internal/channels/README.md) |
| Repository layout | [PROJECT_STRUCTURE.md](PROJECT_STRUCTURE.md) |
| Active/frozen areas | [ACTIVE_MODULES.md](ACTIVE_MODULES.md) |

The full documentation index is [docs/README.md](docs/README.md).

---

## Repository Layout

```text
cmd/                  CLI commands and runtime entrypoints
configs/              Environment and provider setup notes
deploy/               Local Docker/Postgres/sandbox support
internal/             Private Go packages for agent, app, tools, channels, policies, providers, storage
docs/                 Product, architecture, contracts, runbooks, scenarios, reviews
migrations/           PostgreSQL schema migrations
scripts/              Developer/operator helper scripts
skills/               V-Claw skill content
tests/                Cross-package contract tests and future E2E assets
```

See [PROJECT_STRUCTURE.md](PROJECT_STRUCTURE.md) for boundary rules.

---

## Development
- [Google Workspace Setup](configs/google/README.md) - Google Cloud OAuth, credentials, auth, and Google API smoke tests.
- [Telegram Channel Setup](internal/channels/README.md) - Telegram bot setup, HITL approval, and channel runtime commands.
- [Pre-release Guide](docs/pre-release-guide.md) - simple release/demo prep for non-technical teammates.
- [Release Readiness](docs/release-readiness.md) - which features are ready, partial, or not ready yet.
- [Release Checklist](docs/release-checklist.md) - practical go/no-go checklist before release.
- [Demo Checklist](docs/demo-checklist.md) - easy demo script and fallback plan.
- [Safety Guide](docs/safety-guide.md) - plain-language safety notes for users and presenters.

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

Copy the example environment file before running local commands:

```powershell
go test ./...
go run ./cmd/vclaw --help
go run ./cmd/vclaw tools list
.\scripts\ops\release-check.ps1 -SkipGitNexus
```

Before changing core symbols, follow the repo instructions in `AGENTS.md` and use GitNexus impact/detect checks. Keep changes small, vertical, and aligned with the active module scope.
