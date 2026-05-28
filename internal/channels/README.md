# channels

Message channel adapters live here.

Channels are the user-facing conversation surfaces for chatting with and commanding V-Claw agents. They are different from connectors:

- `channels` receive/send user messages and commands.
- `connectors` call external business APIs such as Gmail, Calendar, Drive, or Google Chat.

Planned submodules:

- `router`: normalize inbound messages, select agent/session, and dispatch runs.
- `commands`: slash commands and channel-specific command parsing.
- `identity`: map channel users/conversations to V-Claw users and sessions.
- `cli`: local terminal chat channel.
- `googlechat`: Google Chat as a message channel.
- `telegram`: Telegram bot channel if enabled.
- `slack`: Slack channel if enabled.
- `discord`: Discord channel if enabled.
- `webhook`: generic local/webhook message ingress.

All channel-triggered actions should still pass through sessions, policies, permissions, approvals, safety, and audit.

