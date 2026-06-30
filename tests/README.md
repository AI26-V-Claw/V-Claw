# tests

Cross-package verification assets for V-Claw.

## Current State

Most executable tests live beside Go packages under `cmd/` and `internal/`. This top-level directory currently hosts shared contract tests and is reserved for broader integration/E2E suites.

Implemented today:

- `tests/contracts/`: cross-package contract checks.
- Package-local unit/integration tests under `internal/**` and `cmd/vclaw`.

Planned as the product matures:

- Safety/HITL scenario tests.
- Telegram E2E smoke tests.
- Google Workspace fake-backed integration tests.
- Release smoke fixtures tied to `docs/TEST_MATRIX.md`.

Use `go test ./...` as the default regression command.
