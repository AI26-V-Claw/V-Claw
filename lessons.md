# Lessons

[2026-06-02] | PR #17 dùng classifier duplicate, tool name ngoài contract và `blocked` như risk level | Consolidate classifier về `internal/agent/intent`, normalize emitted tool names theo `docs/03-contracts.md`, và chuyển blocked thành `Decision: Block` với risk level hợp lệ | Khi sửa intent/safety phải kiểm tra module ownership trong `ACTIVE_MODULES.md`, boundary tool names trong contracts, và không dùng status/decision làm risk level.
