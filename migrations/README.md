# Migrations

## Setup database

### Docker (recommended)
```bash
docker compose up -d postgres
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/001_init_vclaw_schema.sql
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/002_persistence_runtime_state.sql
docker exec -i vclaw-postgres psql -U vclaw -d vclaw < migrations/003_governance_metadata.sql
```

### Native PostgreSQL
```bash
psql -h localhost -U vclaw -d vclaw -f migrations/001_init_vclaw_schema.sql
psql -h localhost -U vclaw -d vclaw -f migrations/002_persistence_runtime_state.sql
psql -h localhost -U vclaw -d vclaw -f migrations/003_governance_metadata.sql
```

## Order
1. `001_init_vclaw_schema.sql`
2. `002_persistence_runtime_state.sql`
3. `003_governance_metadata.sql`

## Notes
- `001` tạo schema gốc.
- `002` thêm persistence cho run / tool call / approval / audit.
- `003` thêm governance metadata.
- Nếu muốn làm sạch DB, drop database rồi chạy lại 3 file theo đúng thứ tự.
