# deploy

Local deployment and runtime support assets live here.

V-Claw is local-first. Deployment assets exist to make local development reproducible, not to imply a hosted production service.

## Current Assets

- `docker-compose.yml` at repo root starts local PostgreSQL.
- `deploy/docker/` documents Docker-oriented local runtime plans.
- `internal/sandbox/docker/` contains the sandbox image profile for Python execution.

## Typical Local Services

```powershell
docker compose up -d postgres
vclaw doctor
vclaw start
```

Keep user data, secrets, Google tokens, and runtime logs local and ignored by git.
