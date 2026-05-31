# docker

Local Docker setup for V-Claw.

This folder is for assets that make a cloned repo easy to run on a user's machine. It is not intended as a public server or cloud deployment target.

Planned assets:

- `docker-compose.local.yml`: local V-Claw runtime, PostgreSQL, and optional helper services.
- `Dockerfile.local`: reproducible local runtime image.
- `sandbox/`: isolated container profile for shell/Python tool execution.
- `volumes/`: documented local data mounts for PostgreSQL data, logs, workspace, and cache.

## Local Runtime Responsibilities

- Run the V-Claw CLI/runtime in a predictable environment when the user chooses Docker.
- Run PostgreSQL locally as the primary database.
- Provide isolated execution for risky shell/Python tasks.
- Keep user data mounted locally.
- Avoid exposing remote ports unless explicitly configured.
