# localapi

Optional local control plane for V-Claw lives here.

This package owns local-only HTTP handlers and WebSocket RPC when needed. It is
not a public server boundary and should not grow into a UI layer.

Planned submodules:

- `handlers`: local HTTP handlers.
- `middleware`: auth, rate limit, redaction, and request validation.
- `rpc`: optional WebSocket RPC/event streaming for local automation clients.
