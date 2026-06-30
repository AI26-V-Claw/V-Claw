# cmd

Executable entrypoints for V-Claw.

## Current Entrypoints

- `vclaw`: primary CLI and local runtime command.

## Main Commands

| Command | Purpose |
|---|---|
| `vclaw install` | Install the Windows binary into user PATH. |
| `vclaw setup` | Create/update `.env` for release use. |
| `vclaw doctor` | Validate configuration blockers/warnings. |
| `vclaw start` / `vclaw run` | Convenience aliases for Telegram runtime startup. |
| `vclaw telegram run` | Canonical Telegram runtime command. |
| `vclaw agent` | One-shot or interactive CLI agent session for development/debugging. |
| `vclaw google ...` | OAuth, smoke tests, and manual Google Workspace operations. |
| `vclaw tools list` | List locally registered non-OAuth tools. |
| `vclaw status`, `logs`, `approvals` | Runtime health and audit/approval inspection. |

Command packages should stay thin: parse args, load environment, and call app/runtime packages. Do not add agent, connector, or business logic here.
