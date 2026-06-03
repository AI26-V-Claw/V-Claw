# configs

Runtime configuration templates will live here.

Planned files:

- `config.example.json5`: local API, PostgreSQL store, providers, scheduler, and policy defaults.
- `.env.example`: local environment variable template.
- `providers.example.yaml`: Anthropic, OpenAI, OpenAI-compatible, local model, fallback, and default model settings.
- `tool-policy.example.yaml`: default risk and approval policy.
- `google-scopes.example.yaml`: Google Workspace OAuth scope groups.

## Tavily web tools

V-Claw can expose provider-neutral web tools backed by Tavily:

```env
VCLAW_WEB_TOOLS_MODE=auto
TAVILY_API_KEY=
```

Modes:

- `auto`: register `web.search` and `web.fetch` only when `TAVILY_API_KEY` is set.
- `required`: fail startup if Tavily is not configured.
- `off`: do not expose web tools.

`TALIVY_API_KEY` is accepted as a temporary typo-compatible fallback, but new config should use `TAVILY_API_KEY`.
