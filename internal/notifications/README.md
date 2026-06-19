# notifications

Outbound user notifications live here.

This package is responsible for V-Claw proactively sending approval requests,
reminders, job completion notices, and user-actionable errors to configured
channels such as CLI, Telegram, Google Chat, or Discord.

Dependency direction:

- `notifications` may call `channels/<adapter>` to deliver outbound messages.
- `channels/<adapter>` must not import `notifications`.

Inbound user messages and commands belong in `internal/channels`.
