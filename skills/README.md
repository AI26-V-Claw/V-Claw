# skills

V-Claw-specific skill content lives here.

## Current Role

The runtime skill loader lives in `internal/skills`. Root-level `skills/` contains skill content that can be loaded or reviewed by the assistant runtime.

Current skill content may be minimal while the MVP hardens safety, tools, and release setup.

## Guidelines

- Keep skills small and auditable.
- Do not store secrets or user-private data in skill files.
- Treat generated or auto-learned skills as review-required until the release gates explicitly allow them.
- Keep `VCLAW_SKILL_NUDGE_INTERVAL=0` for release config unless reviewed skill-learning gates are implemented.

Possible future skills:

- workspace triage
- meeting prep
- local document/data handling
- cautious desktop operation
