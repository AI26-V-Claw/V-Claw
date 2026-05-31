# desktop

Low-level local desktop automation drivers live here.

This package is driver-only. Agents should never call it directly. Agent-facing
tool wrappers live in `internal/tools/os/desktop` and
`internal/tools/os/clipboard`, where policy, approval, safety, and audit are
enforced.

Responsibilities:

- Screenshot capture.
- Active window and app inspection.
- Keyboard and mouse primitives.
- Raw clipboard access.
- OS-specific backends.
