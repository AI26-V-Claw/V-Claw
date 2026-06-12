# Documentation Map

This directory contains the shared V-Claw documentation used by the team and by coding agents.

## Shared Source Of Truth

- `00-project-brief.md`: product problem, safety model, roadmap, and team split.
- `01-system-design.md`: high-level system diagrams and component relationships.
- `02-usecase-diagram.md`: user-facing capabilities and risk categories.
- `03-contracts.md`: intended runtime contracts between channel, agent, safety, and tools.
- `04-sequences.md`: canonical sequence scenarios for review and E2E planning.
- `TEST_MATRIX.md`: current V-Claw behavior-to-proof matrix.
- `runbook.md`: local operation and troubleshooting notes.
- `../ACTIVE_MODULES.md`: current implementation scope, ownership, and frozen areas.
- `../PROJECT_STRUCTURE.md`: repository layout and module boundaries.

## Personal Harnesses

Personal workflow harnesses should stay outside shared docs. Khang's local harness, when present, lives under ignored path `docs/khang-harness/`. Other contributors and coding agents should not treat that folder as team policy.

## Current State

The shared docs above remain the team source of truth. If a personal harness discovers a useful rule, promote it intentionally into the shared docs through review instead of relying on ignored local files.
