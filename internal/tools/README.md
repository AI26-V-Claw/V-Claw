# tools

Agent-callable tools live here. Tools are registered through the shared registry and carry metadata used by policy, approval, and audit.

## Implemented Groups

| Group | Examples | Notes |
|---|---|---|
| `builtin` | calculator, current time | Safe compute/read helpers. |
| `filesystem` | list/read/info/write file | Writes are local-write risk. |
| `sandbox` | run Python, run shell, extract PDF | Code execution/local write risk and approval-gated. |
| `memory` | get/edit/reset user memory | Mutating memory requires approval. |
| `web` | Tavily search/fetch | Registered when `VCLAW_WEB_TOOLS_MODE` allows and API key exists. |
| `google_workspace` | Gmail, Calendar, Chat, Drive, Docs, Sheets, Meet, People | Registered when Google OAuth is available or required. |
| `delegation` | subtask tool | Registered by runtime for agent decomposition. |

## Boundaries

- Tools should expose narrow, schema-friendly operations.
- Tools may call connectors, stores, sandbox runners, or memory services.
- Tools should not own channel UX or long-running runtime orchestration.
- Risk metadata must stay accurate; see `office/README.md` for Google Workspace risk matrix.

## Planned / Future

Scheduler and desktop/clipboard tools are not active MVP defaults unless code and tests explicitly wire them into the runtime.
