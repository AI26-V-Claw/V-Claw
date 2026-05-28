# internal

Private V-Claw application modules.

This folder intentionally mirrors GoClaw's internal architecture while separating V-Claw-specific additions:

- Google Workspace connectors.
- Approval-first OS automation.
- Multiple LLM provider adapters.
- Optional local HTTP/WebSocket control through `localapi`.
- Personal assistant memory and workflow orchestration.

Boundary notes:

- `desktop` is low-level driver code only; tool wrappers live under `tools/os`.
- `notifications` sends outbound messages through channel adapters. Channel
  adapters must not import `notifications`.
- `backup`, `vault`, and `knowledgegraph` are Sprint 3 placeholders.
