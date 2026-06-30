# Documentation Map

This directory contains the shared V-Claw documentation used by the team and by coding agents.

## Shared Source Of Truth

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

## Personal Harnesses

Personal workflow harnesses should stay outside shared docs. Khang's local harness, when present, lives under ignored path `docs/khang-harness/`. Other contributors and coding agents should not treat that folder as team policy.

Repository-harness files inside `docs/khang-harness/` may mention paths such as `docs/HARNESS.md`, `docs/product-Khang/*`, `docs/stories-Khang/*`, `docs/decisions-Khang/*`, and `docs/templates-Khang/*`. In this V-Claw checkout, those are Khang-harness aliases that resolve under `docs/khang-harness/`, not shared documentation paths, unless a task explicitly promotes a rule into the shared docs.

## Current State

The shared docs above remain the team source of truth. If a personal harness discovers a useful rule, promote it intentionally into the shared docs through review instead of relying on ignored local files.
