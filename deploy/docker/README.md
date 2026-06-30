# docker

Local Docker setup notes for V-Claw.

This folder is for assets that make a cloned repo easy to run on a user's machine. It is not a public server or cloud deployment target.

## Implemented Today

The repo root `docker-compose.yml` provides local PostgreSQL used by audit, monitoring, approvals, and runtime persistence paths when `DATABASE_URL` is configured.

```powershell
docker compose up -d postgres
```

## Related Docker Assets

- `internal/sandbox/docker/`: Python sandbox image and manual runner examples.
- `migrations/`: SQL migrations for PostgreSQL.

## Planned / Optional

Future local Docker assets may include a fully containerized V-Claw runtime, helper volumes, and sandbox profiles. Keep these local-first and avoid exposing remote ports unless explicitly configured.
