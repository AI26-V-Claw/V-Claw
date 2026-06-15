# V-Claw Runbook

Practical notes for starting, checking, and debugging the current V-Claw codebase.

## 1. Prerequisites

### Go and local services

- Go `1.26` (`go.mod`)
- PostgreSQL 16 is the repo default in `docker-compose.yml`
- A writable data directory. Default is `./data`
- Google OAuth credentials and token if you want Google tools enabled
- Tavily API key if you want web search/fetch enabled

### Current runtime state

- The runtime itself stores transcript and runtime state on local files under `DATA_DIR`
- `vclaw logs`, `vclaw approvals`, and `GET /metrics/history` read from Postgres audit tables
- The repo does not currently contain a CLI migration command for those audit tables. Apply `migrations/001_init_vclaw_schema.sql` yourself before expecting audit data

### Required env vars

Required for all real runtimes:

```env
OPENAI_API_KEY=...
```

Required for Telegram runtime:

```env
TELEGRAM_BOT_TOKEN=...
ALLOWED_TELEGRAM_USER_ID=...
```

Accepted aliases:

```env
VCLAW_TELEGRAM_BOT_TOKEN=...
VCLAW_TELEGRAM_ALLOWED_USER_IDS=...
```

Required for Slack runtime:

```env
VCLAW_SLACK_BOT_TOKEN=xoxb-...
VCLAW_SLACK_APP_TOKEN=xapp-...
VCLAW_SLACK_OWNER_USER_ID=U...
```

### Optional env vars

Common optional env vars:

```env
OPENAI_MODEL=gpt-4o
OPENAI_BASE_URL=https://api.openai.com/v1
DATA_DIR=./data
METRICS_PORT=8080
DATABASE_URL=postgres://vclaw:vclaw@localhost:5432/vclaw?sslmode=disable
VCLAW_USER_POLICY_PATH=./data/user-policy.json
VCLAW_SANDBOX_WORKSPACE_DIR=.sandbox-workspace
VCLAW_SANDBOX_IMAGE=...
```

Google OAuth and Google tool wiring:

```env
VCLAW_GOOGLE_CREDENTIALS_PATH=configs/google/credentials.json
VCLAW_GOOGLE_TOKEN_PATH=configs/google/token.json
VCLAW_GOOGLE_TOOLS_MODE=auto
```

Web tool wiring:

```env
VCLAW_WEB_TOOLS_MODE=auto
TAVILY_API_KEY=...
TAVILY_BASE_URL=...
```

Slack channel allow-list:

```env
VCLAW_SLACK_ALLOWED_CHANNEL_IDS=C123...,C456...
```

## 2. Starting the runtime

V-Claw loads `.env` automatically on startup.

### Telegram

```bash
go run ./cmd/vclaw telegram run --google-tools auto --web-tools auto
```

Useful flags:

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

### Slack

```bash
go run ./cmd/vclaw slack run --google-tools auto --web-tools auto
```

Useful flags:

```bash
--bot-token xoxb-...
--app-token xapp-...
--owner-user U...
--allowed-channels C123...,C456...
--data-dir ./data
--max-iterations 8
--credentials configs/google/credentials.json
--google-token configs/google/token.json
--google-tools auto|required|off
--web-tools auto|required|off
```

### `--google-tools` values

- `auto`: register Google tools only when both credentials and token files exist
- `required`: fail startup if Google OAuth is not ready
- `off`: disable Google tool registration

### `--web-tools` values

- `auto`: register Tavily-backed web tools only when `TAVILY_API_KEY` is set
- `required`: fail startup if Tavily is not configured
- `off`: disable web tool registration

### Postgres for audit commands

Start the repo’s default Postgres container:

```bash
docker compose up -d postgres
```

## 3. Health check

### CLI

Use:

```bash
go run ./cmd/vclaw status
```

Behavior:

- If `METRICS_PORT` is set and the local metrics server is reachable, `vclaw status` calls `GET /health`
- Otherwise it runs the same health checks directly in-process

Example output:

```text
Status:    ok
Uptime:    2h34m
Checked:   2026-06-11T12:30:46+07:00

postgres       ok       8ms
llm_provider   ok
google_oauth   ok
tavily         ok
channel        ok
tool_registry  ok       40 tools
```

### HTTP endpoint

The metrics/health server starts in a goroutine beside the Slack or Telegram runtime. Port comes from `METRICS_PORT`; default is `8080`.

```bash
curl http://127.0.0.1:8080/health
```

If you set another port:

```bash
curl http://127.0.0.1:${METRICS_PORT}/health
```

## 4. Viewing logs and approvals

### Logs

Default behavior:

```bash
go run ./cmd/vclaw logs
```

Common examples:

```bash
go run ./cmd/vclaw logs --limit 100
go run ./cmd/vclaw logs --since 15m
go run ./cmd/vclaw logs --since 2h --level error
go run ./cmd/vclaw logs --tool gmail.createDraft
```

Notes:

- Default `--limit` is `50`
- Default `--since` is `1h`
- Valid levels are `error` and `info`
- If Postgres is configured but the audit tables are missing or empty, the command prints `No audit log events found.`

### Approvals

Default behavior:

```bash
go run ./cmd/vclaw approvals
```

Common examples:

```bash
go run ./cmd/vclaw approvals --limit 50
go run ./cmd/vclaw approvals --status pending
go run ./cmd/vclaw approvals --status approved
go run ./cmd/vclaw approvals --status expired
go run ./cmd/vclaw approvals --status revised
```

Notes:

- Default `--limit` is `20`
- Valid statuses are `pending`, `approved`, `rejected`, `expired`, and `revised`
- If Postgres is configured but the approval audit tables are missing or empty, the command prints `No approval requests found.`

## 5. User policy config

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

Slack and Telegram runtimes install a `SIGHUP` watcher. Reload policy without restarting:

```bash
kill -HUP <pid>
```

On success the runtime logs `reloaded user policy config`.

### If the file is missing

If the file does not exist:

- startup does not fail
- V-Claw uses an empty in-memory policy config
- the runtime logs a warning that the user policy config is missing and empty defaults are being used

## 6. Common errors and how to fix them

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

## 7. Running tests

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
