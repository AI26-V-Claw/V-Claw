# Documentation Index

This directory contains the shared V-Claw documentation. It mixes product intent, architecture contracts, operations, test planning, and historical demos, so this page is the routing table.

## Read First

<<<<<<< HEAD
- `00-project-brief.md`: product problem, safety model, roadmap, and team split.
- `01-system-design.md`: high-level system diagrams and component relationships.
- `02-usecase-diagram.md`: user-facing capabilities and risk categories.
- `03-contracts.md`: intended runtime contracts between channel, agent, safety, and tools.
- `04-sequences.md`: canonical sequence scenarios for review and E2E planning.
- `scenarios/05-drive-docs-sheets-hitl.md`: canonical Drive/Docs/Sheets read-before-write + HITL sequence.
- `testing-e2e/`: Telegram-first manual E2E testing plan, feature discovery, demo stories, and readiness checklist.
- `TEST_MATRIX.md`: current V-Claw behavior-to-proof matrix.
- `runbook.md`: local operation and troubleshooting notes.
- `pre-release-guide.md`: simple start-here guide for release/demo prep.
- `release-readiness.md`: plain-language readiness view by feature.
- `release-checklist.md`: go/no-go checklist before release or demo.
- `demo-checklist.md`: simple demo flow and fallback checklist.
- `safety-guide.md`: end-user-friendly safety notes and warnings.
- `../ACTIVE_MODULES.md`: current implementation scope, ownership, and frozen areas.
- `../PROJECT_STRUCTURE.md`: repository layout and module boundaries.
=======
| Order | Document | Use for | Status |
|---:|---|---|---|
| 1 | [Project Brief](00-project-brief.md) | Product goal, HITL model, sprint roadmap, team split | Living product baseline |
| 2 | [System Design](01-system-design.md) | Component map and responsibility boundaries | Architecture baseline |
| 3 | [Contracts](03-contracts.md) | Runtime objects, risk, approvals, tool/result contracts | Design + implementation guide |
| 4 | [Canonical Sequences](04-sequences.md) | Request, tool, approval, and sandbox flows | Review/E2E baseline |
| 5 | [Runbook](runbook.md) | Startup, health, logs, debugging, operations | Operator guide |
| 6 | [Test Matrix](TEST_MATRIX.md) | Behavior proof, implemented/planned matrix | Release readiness input |
>>>>>>> d20ae96ecfd80b21043d7780d2d31b3c76885da7

## Setup Guides

| Guide | Scope |
|---|---|
| [Root README](../README.md) | Release install, quick start, command cheat sheet |
| [Google Workspace OAuth](../configs/google/README.md) | Google Cloud setup, OAuth, Google CLI smoke tests |
| [Telegram Channel](../internal/channels/README.md) | BotFather setup, owner ID, sessions, HITL commands |
| [Migrations](../migrations/README.md) | PostgreSQL schema order and manual migration commands |
| [Scripts](../scripts/README.md) | Release-check helper scripts |

## Design References

| Document | Use for |
|---|---|
| [Usecase Diagram](02-usecase-diagram.md) | Capability/risk overview from the product perspective |
| [ERD](05-erd.md) | Persistence model reference |
| [Production Harness Review](production-harness-review.md) | Release blockers, harness principles, runtime-state checklist |
| [Multi-tools Flow Review](reviews/multi-tools-flow-review.md) | Focused review notes for multi-tool execution |
| [Scenario Library](scenarios/) | Canonical user stories and HITL examples |

## Historical / Demo Material

The `demo/` directory contains sprint demo scripts and manual notes. Treat these as historical evidence, not current source of truth. If a demo conflicts with code, prefer tests and the current setup/runbook docs.

## Source Of Truth Rules

- Current command behavior comes from `cmd/vclaw` and should be reflected in the root README and runbook.
- Current runtime/tool wiring comes from `internal/app` and `internal/tools`.
- Current active/frozen module scope comes from [ACTIVE_MODULES.md](../ACTIVE_MODULES.md).
- Personal/local harness docs under ignored paths are not team policy unless promoted here.
- When docs and code differ, update the stale side in the same change.

## Maintenance Checklist

When changing runtime behavior, update the relevant docs in the same PR:

- CLI command or setup behavior: root README, runbook, smoke guide.
- Google tool behavior: Google setup guide, office tool risk matrix.
- Telegram UX: channel README and scenario docs.
- Risk/approval semantics: contracts, canonical sequences, test matrix.
- Release process: production harness review and `scripts/ops/release-check.ps1`.
