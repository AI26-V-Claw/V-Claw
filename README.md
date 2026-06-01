# V-Claw

V-Claw is planned as a local-first personal AI agent assistant built on top of GoClaw's architecture patterns: agent loop, provider routing, tool registry, workspace isolation, scheduling, skills, audit logs, and safe execution.

The first product target is a safe automation assistant for office work and local computer control:

- Manage email, calendar, files, and chat through Google Workspace connectors.
- Execute local file/data tasks through Python, shell, and desktop automation.
- Route requests across multiple LLM providers such as Anthropic, OpenAI, OpenAI-compatible endpoints, Gemini, OpenRouter, and local models.
- Require explicit policy, approval, sandboxing, and audit trails for risky actions.
- Keep the user in control through a local CLI/chat loop, approvals, and run history.

This folder currently contains only the project skeleton and architecture notes. No runtime implementation has been added yet.

The intended setup path is local: a user should be able to clone the repo, configure providers/accounts, and run V-Claw on their own machine. Docker assets are kept for local reproducibility and sandboxing, not for a hosted deployment.

## Structure

See [PROJECT_STRUCTURE.md](PROJECT_STRUCTURE.md) for the intended module layout.

Start with the docs in this order:

1. [Project Brief](docs/00-project-brief.md)
2. [System Design](docs/01-system-design.md)
3. [Usecase Diagram](docs/02-usecase-diagram.md)
