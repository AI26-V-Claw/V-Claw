# migrations

PostgreSQL database migrations for V-Claw.

PostgreSQL backs run/tool/approval/audit persistence. Session transcript and
short-term memory stay on local files under `DATA_DIR`. The DB is required for
the audit commands (`vclaw logs`, `vclaw approvals`, `GET /metrics/history`).

## Migration files

Apply these in order. `002` alters tables created by `001`, so the order matters.

| Order | File | Purpose |
|---|---|---|
| 1 | `001_init_vclaw_schema.sql` | Base schema: `agent_runs`, `tool_registry_entries`, approval/audit tables. |
| 2 | `002_persistence_runtime_state.sql` | Runtime state columns, `tool_calls` / `approval_actions` tables, drops legacy columns. |

> The repo does not ship a CLI migration command. Apply the files manually with
> the steps below.

## Setup when cloning the repo

### 1. Create the `.env` file

```bash
cp .env.example .env
```

The default database settings already work for local development:

```env
VCLAW_POSTGRES_DB=vclaw
VCLAW_POSTGRES_USER=vclaw
VCLAW_POSTGRES_PASSWORD=vclaw
DATABASE_URL=postgres://vclaw:vclaw@localhost:5432/vclaw?sslmode=disable
```

### 2. Start PostgreSQL

```bash
docker compose up -d postgres
```

This runs `postgres:16-alpine`, creates the `vclaw` database, and publishes it
on `localhost:5432`. Wait a few seconds for the container healthcheck to pass.

### 3. Apply the migrations in order

```bash
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/001_init_vclaw_schema.sql
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/002_persistence_runtime_state.sql
```

### 4. Verify

List the tables:

```bash
docker exec -it vclaw-postgres psql -U vclaw -d vclaw -c "\dt"
```

Or use the app health check (the `postgres` line should read `ok`):

```bash
go run ./cmd/vclaw status
```

## Adding a new migration

- Name the file with the next sequential prefix, e.g. `003_<description>.sql`.
- Use idempotent statements (`IF NOT EXISTS`, `ADD COLUMN IF NOT EXISTS`) so
  re-applying is safe.
- Add the filename to the ordered list applied in
  `internal/store/pg/store_test.go` so the store tests run against the same
  schema.
