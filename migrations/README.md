# Migrations

## Setup database

### Docker (recommended)
```bash
docker compose up -d postgres
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/001_init_vclaw_schema.sql
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/002_persistence_runtime_state.sql
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/003_governance_metadata.sql
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/004_knowledge_graph.sql
```

### Native PostgreSQL
```bash
psql -h localhost -U vclaw -d vclaw -f migrations/001_init_vclaw_schema.sql
psql -h localhost -U vclaw -d vclaw -f migrations/002_persistence_runtime_state.sql
psql -h localhost -U vclaw -d vclaw -f migrations/003_governance_metadata.sql
psql -h localhost -U vclaw -d vclaw -f migrations/004_knowledge_graph.sql
```

## Order
1. `001_init_vclaw_schema.sql`
2. `002_persistence_runtime_state.sql`
3. `003_governance_metadata.sql`
4. `004_knowledge_graph.sql`

## Pulling new code

After pulling code that includes a new migration:

1. Apply any migration files that are not yet present in your local database.
2. Restart the bot or CLI process so the running application uses the new code.

The PostgreSQL store also applies embedded migrations on startup from the repo root
(`internal/store/pg/store.go`). If your bot starts successfully with a configured
database, the migration should already be applied. Manual commands are still useful
for local setup, pgAdmin workflows, or when you want to verify the database before
starting the bot.

For the knowledge graph feature, make sure migration `004_knowledge_graph.sql` has
run before testing linked knowledge. It creates these tables:

- `knowledge_nodes`
- `knowledge_edges`
- `knowledge_observations`

You can verify the tables in pgAdmin Query Tool or `psql`:

```sql
SELECT table_name
FROM information_schema.tables
WHERE table_schema = 'public'
  AND table_name IN (
    'knowledge_nodes',
    'knowledge_edges',
    'knowledge_observations'
  )
ORDER BY table_name;
```

Expected result: all 3 table names are returned. If any table is missing, run
`004_knowledge_graph.sql` and restart the bot.

## Notes
- `001` tạo schema gốc.
- `002` thêm persistence cho run / tool call / approval / audit.
- `003` thêm governance metadata.
- `004` thêm linked knowledge graph cho user / project / document / meeting / note context.
- Nếu muốn làm sạch DB, drop database rồi chạy lại các file theo đúng thứ tự.
