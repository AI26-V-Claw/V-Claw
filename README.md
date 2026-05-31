# V-Claw

V-Claw Sprint 1 is a single-owner Telegram bot prototype focused on one vertical slice:

User sends a Telegram message → long polling receives it → owner guard validates it → agent classifies intent → agent replies from memory → bot sends the response back.

## What is in scope

- Telegram long polling only
- single owner via `ALLOWED_TELEGRAM_USER_ID`
- in-memory session history, capped at 20 messages
- append-only JSONL audit log in `logs/audit.jsonl`
- rule-based intent classifier for Sprint 1

## Out of scope

- webhook delivery
- Google Workspace
- sandbox execution
- real HITL approval buttons
- Redis, database, dashboard, multi-user support
- gateway/local API, Slack/Zalo/other channels
- dangerous/system actions

## Layout

- `cmd/vclaw`: application entrypoint
- `internal/app`: wiring and lifecycle
- `internal/channels/telegram`: Telegram long polling transport
- `internal/agent`: message orchestration
- `internal/intent`: intent classification stub
- `internal/memory`: in-memory session history
- `internal/audit`: audit logging

## Configure

Copy `.env.example` to `.env` and set:

- `TELEGRAM_BOT_TOKEN`
- `ALLOWED_TELEGRAM_USER_ID`
- optional `DATA_DIR` and `LOG_DIR`

## Intent evaluation

Run the offline classifier harness:

```bash
rtk go run ./cmd/intent-eval
```

The report prints:

- total cases
- intent accuracy
- system op accuracy
- exact-match accuracy
- a list of failed cases, if any

## Run

```bash
rtk go run ./cmd/vclaw
```

The bot uses Telegram long polling, writes offset state to `data/telegram_offset.txt`, and appends audit entries to `logs/audit.jsonl`.

## Manual checks

- `xin chào` → greeting reply
- `hôm nay tôi có lịch gì` → read-info placeholder
- `gửi email cho Nam` → system-op guard response
- `làm đi` → clarification question
- `nãy mình nói gì` → recent history summary
