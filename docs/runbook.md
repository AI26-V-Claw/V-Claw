# V-Claw Runbook

Practical steps for starting, checking, and debugging V-Claw with monitoring enabled.

## 1. Starting the system

### Prerequisites

- Go `1.26` (`go.mod`)
- PostgreSQL 16 is the repo default in `docker-compose.yml`
- A writable data directory. Default is `./data`
- Google OAuth credentials and token if you want Google tools enabled
- Tavily API key if you want web search/fetch enabled

### Current runtime state

- Runtime stores transcript and runtime state on local files under `DATA_DIR`
- `vclaw logs`, `vclaw approvals`, and `GET /metrics/history` read from Postgres audit tables
- Repo does not contain CLI migration command for audit tables. Apply `migrations/001_init_vclaw_schema.sql` before expecting audit data
- V-Claw loads `.env` automatically on startup
- Runtime provider vars from repo `.env` override inherited shell values for `OPENAI_*`, `LLM_*`, and Telegram bot token settings so local project config wins over stale global exports

### Start Postgres for audit and monitoring data

```bash
docker compose up -d postgres
```

`DATABASE_URL` is required if you want:
- `vclaw logs`
- `vclaw approvals`
- latest run trace lookup in `vclaw status`
- monitoring history backed by Postgres

Apply audit schema before expecting those commands to return data:

```bash
psql "$DATABASE_URL" -f migrations/001_init_vclaw_schema.sql
```

### Start Telegram runtime

```bash
go run ./cmd/vclaw telegram run --google-tools auto --web-tools auto
```

Exact startup behavior:
- starts agent runtime
- starts Telegram bot polling loop
- starts monitoring HTTP server on `METRICS_PORT` or `8080`

Useful flag overrides:

```bash
--token <telegram-bot-token>
--allowed-user <telegram-user-id>
--data-dir ./data
--max-iterations 8
--credentials configs/google/credentials.json
--google-token configs/google/token.json
--google-tools auto|required|off
--web-tools auto|required|off
```

### Start CLI runtime for local debugging

```bash
go run ./cmd/vclaw agent --prompt "ping" --session dev --channel dev-cli
```

This runs one agent turn. It does not start Telegram or the monitoring HTTP server.

### `--google-tools` values

- `auto`: register Google tools only when both credentials and token files exist
- `required`: fail startup if Google OAuth is not ready
- `off`: disable Google tool registration

### `--web-tools` values

- `auto`: register Tavily-backed web tools only when `TAVILY_API_KEY` is set
- `required`: fail startup if Tavily is not configured
- `off`: disable web tool registration

### Environment variables by purpose

#### Database

```env
DATABASE_URL=postgres://vclaw:vclaw@localhost:5432/vclaw?sslmode=disable
```

Behavior:
- required for Postgres-backed audit and monitoring history features
- if missing, runtime can still start
- if missing, `postgres` health becomes `unhealthy`
- if missing, `vclaw logs` and `vclaw approvals` cannot query audit data successfully
- if missing, latest run trace lookup in `vclaw status` has no database source

#### LLM provider

```env
OPENAI_API_KEY=...
OPENAI_MODEL=gpt-4o
OPENAI_BASE_URL=https://api.openai.com/v1
```

Behavior:
- `OPENAI_API_KEY` required for real agent runtime
- if missing, runtime build fails for real provider usage and `llm_provider` health is `unhealthy`
- `OPENAI_MODEL` optional, default chosen by runtime if omitted
- `OPENAI_BASE_URL` optional, used for OpenAI-compatible endpoint override

#### Google OAuth

```env
VCLAW_GOOGLE_CREDENTIALS_PATH=configs/google/credentials.json
VCLAW_GOOGLE_TOKEN_PATH=configs/google/token.json
VCLAW_GOOGLE_TOOLS_MODE=auto
```

Behavior:
- credentials path and token path required only when Google tools must be usable
- with `VCLAW_GOOGLE_TOOLS_MODE=auto`, missing files do not block startup; Google tools stay unregistered and `google_oauth` health becomes `unhealthy`
- with `VCLAW_GOOGLE_TOOLS_MODE=required`, missing OAuth setup should fail runtime startup
- with `VCLAW_GOOGLE_TOOLS_MODE=off`, Google tools are disabled intentionally

Prepare OAuth once with:

```bash
go run ./cmd/vclaw google auth --credentials configs/google/credentials.json --token configs/google/token.json
```

#### Telegram

```env
TELEGRAM_BOT_TOKEN=...
ALLOWED_TELEGRAM_USER_ID=123456789
```

Accepted aliases:

```env
VCLAW_TELEGRAM_BOT_TOKEN=...
VCLAW_TELEGRAM_ALLOWED_USER_IDS=123456789
```

Behavior:
- required for `go run ./cmd/vclaw telegram run`
- if missing, Telegram runtime exits immediately with validation error
- optional for CLI-only usage

#### Langfuse

```env
LANGFUSE_PUBLIC_KEY=...
LANGFUSE_SECRET_KEY=...
LANGFUSE_HOST=https://cloud.langfuse.com
LANGFUSE_PROJECT_ID=...
```

Behavior:
- tracing integration depends on Langfuse config in runtime build
- `LANGFUSE_HOST` optional; defaults to `https://cloud.langfuse.com` when building trace URLs
- `LANGFUSE_PROJECT_ID` is required to build clickable trace URLs in logs and status output
- if Langfuse keys are missing, runtime can still start, but trace export may be absent
- if `LANGFUSE_PROJECT_ID` is missing, runs may still have trace IDs internally, but `traceUrl=` in logs and `Latest run trace:` in status stay empty

#### Tavily

```env
VCLAW_WEB_TOOLS_MODE=auto
TAVILY_API_KEY=...
TAVILY_BASE_URL=...
```

Behavior:
- `TAVILY_API_KEY` optional when `VCLAW_WEB_TOOLS_MODE=auto`
- with `auto`, missing key does not block startup; Tavily tools are skipped
- with `required`, missing key should fail runtime startup
- `TAVILY_BASE_URL` optional endpoint override
- typo-compatible fallback `TALIVY_API_KEY` is still accepted by code, but new config should use `TAVILY_API_KEY`

## 2. Health check

### Status command

Run:

```bash
go run ./cmd/vclaw status
```

Important: this command always tries `GET http://127.0.0.1:${METRICS_PORT:-8080}/health`.
If monitoring server is not running, it returns unreachable error telling you to start runtime first.

When status says monitoring server is unreachable:
1. Start Telegram runtime first.
2. Confirm `METRICS_PORT` matches runtime environment.
3. Run `go run ./cmd/vclaw status` again.

### Health fields

`vclaw status` prints these components:
- `postgres`: checks whether monitoring server can ping `DATABASE_URL`
- `llm_provider`: reports whether provider configuration exists
- `google_oauth`: reports whether Google OAuth is configured enough for current mode
- `tavily`: reports whether Tavily-backed web tools are configured
- `channel`: reports whether runtime knows active channel identity such as `telegram` or `cli`
- `tool_registry`: reports whether runtime built any tools; also prints tool count

### Status values

- `ok`: component configured and healthy
- `degraded`: overall app status only; core dependencies are up, but at least one non-core component is not `ok` or `skipped`
- `unhealthy`: component missing/broken, or overall status when `postgres` or `llm_provider` is not `ok`
- `skipped`: component intentionally not active for this configuration, currently used for Tavily when no API key is configured in auto mode

Overall status rules:
- `unhealthy` if `postgres` is not `ok`
- `unhealthy` if `llm_provider` is not `ok`
- `degraded` if core components are healthy but another component is not `ok` and not `skipped`
- `ok` otherwise

Example output:

```text
Status:    degraded
Uptime:    12m3s
Checked:   2026-06-17T10:15:00+07:00

postgres       ok       7ms
llm_provider   ok
google_oauth   unhealthy
tavily         skipped
channel        ok
tool_registry  ok       40 tools
```

If latest run has Langfuse trace URL in audit DB, status also prints:

```text
Latest run trace: https://cloud.langfuse.com/project/<project-id>/traces/<trace-id>
```

## 3. Debugging a failed run

### Find recent failures

Start with recent error logs:

```bash
go run ./cmd/vclaw logs --level error
```

Useful filters:

```bash
go run ./cmd/vclaw logs --level error --limit 100
go run ./cmd/vclaw logs --level error --since 15m
go run ./cmd/vclaw logs --level error --since 2h
go run ./cmd/vclaw logs --level error --since 7d
go run ./cmd/vclaw logs --level error --since 2026-06-01
go run ./cmd/vclaw logs --level error --since 2026-06-01T15:04:05
go run ./cmd/vclaw logs --level error --since 2026-06-01T15:04:05Z
go run ./cmd/vclaw logs --level error --tool gmail.createDraft
```

`--since` supports:
- duration: `15m`, `2h`, `24h`, `7d`
- local date: `2026-06-01`
- local datetime: `2026-06-01T15:04:05`
- RFC3339 timestamp: `2026-06-01T15:04:05Z`

Logs may include fields like:
- `tool=...`
- `requestId=...`
- `sessionId=...`
- `approvalId=...`
- `traceUrl=...`
- `error=...`

### Filter by session

`vclaw logs` has no direct `--session` flag.
Use full output and match `sessionId=` manually:

```bash
go run ./cmd/vclaw logs --level error --since 24h
go run ./cmd/vclaw logs --since 24h --tool gmail.createDraft
```

Then inspect lines containing target `sessionId=<value>`.

### Inspect full trace in Langfuse Cloud

When logs include `traceUrl=...`, open that URL directly in browser. Format comes from:

```text
$LANGFUSE_HOST/project/$LANGFUSE_PROJECT_ID/traces/<trace-id>
```

If `LANGFUSE_HOST` is unset, default host is:

```text
https://cloud.langfuse.com
```

Use Langfuse trace to inspect:
- full run timeline
- prompts and model responses
- tool calls and failures
- linked trace and run metadata

If Telegram or status output surfaces latest trace URL, same workflow applies: open URL and inspect trace there.

### Check approvals when run seems stuck

Pending approvals often explain runs that appear stalled.
Check approval queue with:

```bash
go run ./cmd/vclaw approvals --status pending
go run ./cmd/vclaw approvals --status rejected
go run ./cmd/vclaw approvals --status revised
go run ./cmd/vclaw approvals --since 24h
go run ./cmd/vclaw approvals --tool gmail.createDraft
go run ./cmd/vclaw approvals --limit 50
```

`vclaw approvals` supports:
- `--status pending|approved|rejected|expired|revised`
- `--tool <tool-name>`
- `--since` with same date/duration formats as logs
- `--limit <n>`

Record fields printed:
- `approvalId`
- `tool`
- `risk`
- `status`
- `created`
- `decided`

## 4. Common failure scenarios

### Monitoring server unreachable

Symptom:
- `go run ./cmd/vclaw status` returns unreachable error
- `/health` cannot be reached on `127.0.0.1:${METRICS_PORT:-8080}`

Likely cause:
- Telegram runtime is not running
- runtime started with different `METRICS_PORT`
- port already occupied and monitoring server did not bind correctly

Fix:
- start runtime with `go run ./cmd/vclaw telegram run ...`
- verify same `METRICS_PORT` in shell used for `status`
- retry `go run ./cmd/vclaw status`

### Langfuse trace URL missing from logs or Telegram

Symptom:
- log lines have no `traceUrl=` field
- `Latest run trace:` does not appear in `vclaw status`
- Telegram messages do not surface clickable Langfuse trace URL

Likely cause:
- `LANGFUSE_PROJECT_ID` is not set

Fix:
- set `LANGFUSE_PROJECT_ID`
- optionally set `LANGFUSE_HOST` if not using Langfuse Cloud default
- ensure Langfuse keys are present if trace export itself is also missing
- restart runtime, then trigger new run and check logs again

### Tavily reported as `skipped`

Symptom:
- `vclaw status` shows `tavily skipped`

Likely cause:
- no `TAVILY_API_KEY` configured while `VCLAW_WEB_TOOLS_MODE=auto`

Fix:
- no action needed if web tools are optional
- set `TAVILY_API_KEY` if you want `web.search` and `web.fetch`
- treat as real problem only when `VCLAW_WEB_TOOLS_MODE=required`

## 5. Quick reference

```bash
# Start Postgres
docker compose up -d postgres

# Start Telegram runtime + monitoring
go run ./cmd/vclaw telegram run --google-tools auto --web-tools auto

# Health check
go run ./cmd/vclaw status
curl http://127.0.0.1:${METRICS_PORT:-8080}/health

# Logs
go run ./cmd/vclaw logs
go run ./cmd/vclaw logs --level error
go run ./cmd/vclaw logs --since 15m --level error
go run ./cmd/vclaw logs --since 2026-06-01 --tool gmail.createDraft

# Approvals
go run ./cmd/vclaw approvals
go run ./cmd/vclaw approvals --status pending
go run ./cmd/vclaw approvals --status rejected
go run ./cmd/vclaw approvals --since 24h --tool gmail.createDraft
```

## 6. User policy config

### File location

Default path:

```text
./data/user-policy.json
```

Override with:

```env
VCLAW_USER_POLICY_PATH=/path/to/user-policy.json
```

### Current file format

Important: the current code writes and reads `snake_case` JSON keys on disk, not camelCase.

Example:

```json
{
  "auto_allow": ["safe_read", "safe_compute"],
  "require_approval": ["external_write", "code_execution"],
  "always_block": ["destructive"]
}
```

Known risk levels:

- `safe_read`
- `safe_compute`
- `sensitive_read`
- `external_write`
- `local_write`
- `code_execution`
- `destructive`

### Reloading policy

The Telegram runtime installs a `SIGHUP` watcher. Reload policy without restarting:

```bash
kill -HUP <pid>
```

On success the runtime logs `reloaded user policy config`.

### If the file is missing

If the file does not exist:

- startup does not fail
- V-Claw uses an empty in-memory policy config
- the runtime logs a warning that the user policy config is missing and empty defaults are being used

## 7. Common errors and how to fix them

### `AUTH_EXPIRED`

Meaning:

- Google OAuth token is expired/revoked, or a provider such as Tavily returned unauthorized/forbidden

Fix:

```bash
go run ./cmd/vclaw google auth
```

If scopes changed, remove the old token first:

```bash
rm -f configs/google/token.json
go run ./cmd/vclaw google auth
```

Also verify:

- `VCLAW_GOOGLE_CREDENTIALS_PATH`
- `VCLAW_GOOGLE_TOKEN_PATH`
- `TAVILY_API_KEY` if the failing tool is a web tool

### `AUTH_MISSING_SCOPE`

Meaning:

- The stored Google token does not include the scope required by the current Gmail, Calendar, Chat, or People tool call

Fix:

- re-run `go run ./cmd/vclaw google auth`
- if needed, delete `configs/google/token.json` first so consent is re-issued
- verify the required Google APIs are enabled in the Cloud project

### `PROVIDER_ERROR`

Meaning:

- The LLM provider failed during a model call

Fix:

- verify `OPENAI_API_KEY`
- verify `OPENAI_BASE_URL` if using a compatible endpoint
- retry the request
- if the problem persists, try a known-good model with `OPENAI_MODEL=gpt-4o`

### `ACTION_BLOCKED_BY_POLICY`

Meaning:

- The request was blocked by policy or by a guardrail before tool execution

Fix:

- inspect the current user policy file at `./data/user-policy.json` or `VCLAW_USER_POLICY_PATH`
- relax the matching risk level if appropriate
- send `SIGHUP` to the runtime to reload the policy
- for Google Chat member/domain checks, verify the target user belongs to an allowed Workspace domain

### `APPROVAL_EXPIRED`

Meaning:

- The approval request timed out before the user approved or rejected it

Fix:

- resend the original request so the runtime creates a new approval request
- do not try to approve the old approval ID after expiry

### `SANDBOX_TIMEOUT`

Current code note:

- There is no literal `SANDBOX_TIMEOUT` error code in this repo today
- sandbox timeout failures currently surface as `PROVIDER_TIMEOUT`

Meaning:

- `sandbox.runPython` or `sandbox.runShell` exceeded its timeout

Fix:

- reduce the work done by the command/script
- set a larger tool input timeout when appropriate:

```json
{
  "timeout_seconds": 60
}
```

- inspect sandbox stderr in terminal logs for the timeout reason

## 8. Running tests

Build the CLI:

```bash
go build ./cmd/vclaw
```

Run all tests:

```bash
go test ./...
```

Useful targeted test commands:

```bash
go test ./cmd/vclaw ./internal/monitoring
go test ./internal/agent ./internal/app
```
