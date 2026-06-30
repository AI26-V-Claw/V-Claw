# sandbox

Controlled execution support for risky shell and Python work.

## Current Role

- Provides runtime types and runners for shell/Python jobs.
- Supports Docker-backed isolated execution via `internal/sandbox/runtime` and `internal/sandbox/docker` assets.
- Feeds policy/audit metadata for code-execution and local-write operations.
- Agent-facing wrappers live under `internal/tools/system/sandbox` and `internal/tools/os/*`.

## Safety Expectations

Sandbox tools are high-risk by default:

- Shell and Python execution are `code_execution` risk.
- File extraction/writes are `local_write` risk.
- Network and filesystem access should stay constrained by the runner profile.
- User approval must happen before execution when policy requires it.

See `internal/sandbox/docker/README.md` for the Docker image profile.
