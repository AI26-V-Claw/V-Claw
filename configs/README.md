# configs

Runtime configuration examples and provider setup notes live here.

## Current Files

- `google/README.md`: Google Cloud OAuth and API smoke-test guide.
- `google/credentials.json`: local OAuth client file, ignored by git.
- `google/token.json`: local OAuth token cache, ignored by git.

The root `.env.example` is the main environment template. `vclaw setup` creates `.env` from that file when running inside a source checkout, or from a built-in release template when running as a standalone binary.

## Core Environment

```env
OPENAI_API_KEY=
TELEGRAM_BOT_TOKEN=
ALLOWED_TELEGRAM_USER_ID=
VCLAW_SKILL_NUDGE_INTERVAL=0
VCLAW_GOOGLE_TOOLS_MODE=auto
VCLAW_WEB_TOOLS_MODE=auto
```

## Google Tools

```env
VCLAW_GOOGLE_CREDENTIALS_PATH=configs/google/credentials.json
VCLAW_GOOGLE_TOKEN_PATH=configs/google/token.json
VCLAW_GOOGLE_WORKSPACE_DOMAINS=vclaw.site
VCLAW_GOOGLE_TOOLS_MODE=auto
```

Modes:

- `auto`: register Google tools only when credentials and token files exist.
- `required`: fail `doctor`/startup if Google OAuth is not ready.
- `off`: disable Google Workspace tools intentionally.

## Tavily Web Tools

```env
VCLAW_WEB_TOOLS_MODE=auto
TAVILY_API_KEY=
TAVILY_BASE_URL=
```

Modes:

- `auto`: register `web.search` and `web.fetch` only when `TAVILY_API_KEY` is set.
- `required`: fail `doctor`/startup if Tavily is not configured.
- `off`: do not expose web search/fetch tools.

`TALIVY_API_KEY` is accepted as a temporary typo-compatible fallback by runtime code, but new config should use `TAVILY_API_KEY`.
