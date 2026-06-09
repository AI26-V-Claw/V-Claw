# Channels

Message channel adapters live here.

Channels are the user-facing conversation surfaces for chatting with V-Claw. They are different from connectors:

- `channels` receive user messages and send agent responses.
- `connectors` call external business APIs such as Gmail, Calendar, Google Chat, or Slack APIs.

All channel-triggered actions must still pass through Agent Runtime, policies, approvals, safety, and audit. In the current MVP, Telegram and Slack are **single-owner channels**: requests are handled for one local owner and use the Google OAuth token stored at `configs/google/token.json`.

## Shared Setup

Before testing Telegram or Slack with Google Workspace tools:

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

Intent classifier mode:

```env
VCLAW_INTENT_CLASSIFIER_MODE=fallback
```

Supported values:

```text
fallback  use LLM classifier first, then heuristic if classifier LLM fails
llm       use only LLM classifier
heuristic use heuristic classifier for quick local testing
```

Even in `heuristic` mode, the main agent response model still needs `OPENAI_API_KEY`.

Short-term session memory:

```env
VCLAW_SESSION_STORE=redis
VCLAW_REDIS_URL=redis://localhost:6379/0
VCLAW_SESSION_MAX_MESSAGES=40
VCLAW_SESSION_TTL_SECONDS=86400
```

Use Redis when Telegram/Slack should remember recent turns across bot restarts. The transcript and pending clarification state are keyed by `sessionId`, so follow-ups like `11am`, `17h00`, or a natural answer to the previous clarification can be interpreted in the same chat/session. If Redis is not configured, V-Claw falls back to in-process memory.

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

## Slack Setup

Slack uses Socket Mode, so V-Claw can run locally without exposing a public HTTP endpoint.

### Create A Slack App

1. Go to:

```text
https://api.slack.com/apps
```

2. Create an app from scratch.
3. Select the target workspace.

### Enable Socket Mode

1. Open the Slack app settings.
2. Go to `Socket Mode`.
3. Enable Socket Mode.
4. Create an app-level token with this scope:

```text
connections:write
```

Copy the app-level token. It starts with:

```text
xapp-
```

### Configure Bot Permissions

Go to `OAuth & Permissions`, then add these Bot Token Scopes:

```text
chat:write
im:history
im:read
app_mentions:read
```

Install or reinstall the app to the workspace, then copy the Bot User OAuth Token. It starts with:

```text
xoxb-
```

### Enable Events And Interactivity

Go to `Event Subscriptions`, enable events, and subscribe to these bot events:

```text
message.im
app_mention
```

Go to `Interactivity & Shortcuts` and enable interactivity. Socket Mode will deliver button clicks and Revise modal submissions locally.

### Enable Direct Messages

Go to `App Home` and enable the Messages tab so users can send direct messages to the app.

### Get Owner User ID

In Slack, open your profile, choose `More`, then copy your member ID. It starts with:

```text
U...
```

### Environment

Add these values to `.env`:

```env
VCLAW_SLACK_BOT_TOKEN=xoxb-...
VCLAW_SLACK_APP_TOKEN=xapp-...
VCLAW_SLACK_OWNER_USER_ID=U...
VCLAW_SLACK_ALLOWED_CHANNEL_IDS=
VCLAW_GOOGLE_TOOLS_MODE=auto
```

`VCLAW_SLACK_ALLOWED_CHANNEL_IDS` is optional. If set, V-Claw only handles messages from the owner in those channel IDs.

### Start Slack Bot

```powershell
go run ./cmd/vclaw slack run --google-tools auto
```

Use strict Google startup checks:

```powershell
go run ./cmd/vclaw slack run --google-tools required
```

### Test In Slack

Direct message the app:

```text
liệt kê 10 email gần đây
hôm nay tôi có lịch gì
lịch trình tuần này như thế nào
```

Or invite the app to a channel and mention it:

```text
/invite @V-Claw Assistant
@V-Claw Assistant hôm nay tôi có lịch gì
```

Workspace users may see the Slack app, but only `VCLAW_SLACK_OWNER_USER_ID` is allowed to trigger agent runs in single-owner mode.

### Policy Settings In Slack

Send `policy`, `settings`, or `cài đặt` to the app to open the policy modal.

- Each risk level shows the description text instead of the technical risk key.
- Choose a group from the dropdown for each risk level, then submit the modal to save and reload the policy locally.
- If destructive actions are placed in `Tự động cho phép`, Slack shows a validation error on the destructive field and keeps the modal open.

## Runtime Commands

Telegram:

```powershell
go run ./cmd/vclaw telegram run --google-tools auto
```

Slack:

```powershell
go run ./cmd/vclaw slack run --google-tools auto
```

## HITL Approval Commands

When the agent proposes a tool that changes external data or local files, the bot returns an approval request before executing it.

Telegram shows inline buttons:

```text
Yes / No / Revise
```

Slack shows Block Kit buttons:

```text
Yes / No / Revise
```

On Slack, `Revise` opens a modal where you can enter a comment. On Telegram, `Revise` asks you to send the comment as a follow-up message.

Reply in the same Telegram/Slack conversation:

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
