# desktop tools

Agent-facing desktop tools live here.

These wrappers call the low-level drivers in `internal/desktop` and must enforce
tool registry metadata, policy checks, approvals, safety rules, and audit
receipts before performing desktop actions.

Typical tools:

- Screenshot.
- Window focus and inspection.
- Click and mouse movement.
- Typing and keyboard shortcuts.
