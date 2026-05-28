# clipboard tools

Agent-facing clipboard tools live here.

These wrappers call raw clipboard primitives in `internal/desktop`. Clipboard
reads and writes are sensitive because the clipboard may contain credentials or
private content, so policy, approval, redaction, and audit must be applied here.
