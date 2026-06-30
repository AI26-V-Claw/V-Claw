# scripts

Developer and operator helper scripts live here.

## Current Contents

This directory is reserved for shared project scripts. Personal harness binaries, databases, and schemas should stay in ignored local paths such as `docs/khang-harness/` or `scripts/bin/`.

If Khang's repository-harness is installed locally, the durable-layer CLI is expected at `scripts/bin/harness-cli.exe` on Windows. Treat commands documented as `scripts/bin/harness-cli ...` in `docs/khang-harness/` as equivalent to `./scripts/bin/harness-cli.exe ...` from the repo root. The binary and `harness.db` are personal/local resources, not shared project dependencies.

The main V-Claw application remains a Go project; shared commands are exposed through `cmd/` and the Makefile.

## Release checks

Run the lightweight production readiness check before cutting an MVP release:

```powershell
.\scripts\ops\release-check.ps1
```

Use `-RunTests` to include `go test ./...` in the same pass.

