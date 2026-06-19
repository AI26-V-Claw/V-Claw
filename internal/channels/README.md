# Channels

Message channel adapters live here.

Channels are the user-facing conversation surfaces for chatting with V-Claw. They are different from connectors:

- `channels` receive user messages and send agent responses.
- `connectors` call external business APIs such as Gmail, Calendar, and Google Chat.

All channel-triggered actions must still pass through Agent Runtime, policies, approvals, safety, and audit. In the current MVP, Telegram is a **single-owner channel**: requests are handled for one local owner and use the Google OAuth token stored at `configs/google/token.json`.

## Shared Setup

Before testing Telegram with Google Workspace tools:

1. Configure OpenAI in `.env`.
2. Configure Google credentials and OAuth by following `configs/google/README.md`.
3. Run Google OAuth once:

```powershell
go run ./cmd/vclaw google auth
```

4. Test the same agent flow from CLI before testing a bot:

```powershell
go run ./cmd/vclaw agent -google-tools required -session gmail-cli-test -prompt "liệt kê 10 email gần đây"
go run ./cmd/vclaw agent -google-tools required -session calendar-cli-test -prompt "hôm nay tôi có lịch gì"
```

Use `--google-tools required` when startup should fail if Google OAuth is not ready. Use `--google-tools auto` during local development when Google tools should be registered only if credentials and token files exist.


Session memory is kept in-process (no external server required). The transcript and pending clarification state are keyed by `sessionId`, so follow-ups like `11am`, `17h00`, or a natural answer to a previous clarification are interpreted in the same chat session. Sessions are lost on process restart, which is acceptable for a personal local assistant. File-based persistence is planned for Sprint 3.

## Telegram Setup

### Create A Bot

1. Open Telegram and chat with `@BotFather`.
2. Run:

```text
/newbot
```

3. Choose a display name and username.
4. Copy the bot token.

### Get Owner User ID

Get your numeric Telegram user ID with a helper bot such as `@userinfobot`, or inspect local logs while testing. This value is a numeric ID, not a Telegram username.

### Environment

Add these values to `.env`:

```env
TELEGRAM_BOT_TOKEN=
ALLOWED_TELEGRAM_USER_ID=
DATA_DIR=./data
VCLAW_GOOGLE_TOOLS_MODE=auto
```

The config loader also accepts V-Claw aliases:

```env
VCLAW_TELEGRAM_BOT_TOKEN=
VCLAW_TELEGRAM_ALLOWED_USER_IDS=
```

### Start Telegram Bot

```powershell
go run ./cmd/vclaw telegram run --google-tools auto
```

Use strict Google startup checks:

```powershell
go run ./cmd/vclaw telegram run --google-tools required
```

### Test In Telegram

Send one of these messages to the bot:

```text
liệt kê 10 email gần đây
hôm nay tôi có lịch gì
lịch trình tuần này như thế nào
hãy liệt kê 10 tin nhắn tôi nhắn với Bao
```

Detailed tool/provider errors are logged in the local terminal. Telegram should receive a short user-friendly failure message instead of raw tokens, IDs, stack traces, or provider errors.

### Policy Settings In Telegram

Send `/policy` in the same chat to open the policy menu.

- Tap a risk-level button to cycle it between `Tự động cho phép`, `Cần phê duyệt`, and `Luôn chặn`.
- Tap `Lưu` to save the policy and reload it locally.
- If destructive actions are placed in `Tự động cho phép`, Telegram shows a short validation warning and keeps the menu open.

## Runtime Commands

Telegram:

```powershell
go run ./cmd/vclaw telegram run --google-tools auto
```

## HITL Approval Commands

When the agent proposes a tool that changes external data or local files, the bot returns an approval request before executing it.

Telegram shows inline buttons:

```text
Yes / No / Revise
```

On Telegram, `Revise` asks you to send the comment as a follow-up message.

Reply in the same Telegram conversation:

```text
approve
reject
revise <nội dung muốn chỉnh>
```

Examples:

```text
approve
reject
revise đổi giờ họp sang 10:00
```

`approve` executes the pending tool. `reject` cancels it. `revise` cancels the pending tool and sends your comment back as clarification so the next request can be adjusted.

## Security Notes

- Never commit real bot tokens, OpenAI keys, Google credentials, or Google tokens.
- Keep `.env`, `configs/google/credentials.json`, and `configs/google/token.json` local.
- Rotate a token immediately if it is pasted into chat, logs, a PR, or git history.
- Channel bots do not replace approval rules. Any side-effect tool must still pass the approval boundary before execution.
