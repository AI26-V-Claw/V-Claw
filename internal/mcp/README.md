# mcp

Model Context Protocol support lives here.

V-Claw should support MCP in two directions:

- Client mode: connect V-Claw to external MCP servers and import their tools/resources/prompts.
- Server mode: expose V-Claw capabilities as an MCP server for local clients such as Claude Desktop, Codex, or other agents.

Planned submodules:

- `server`: MCP server implementation exposed by V-Claw.
- `client`: MCP client connections to external MCP servers.
- `registry`: registered MCP tools, resources, prompts, and grants.
- `bridge`: mapping between MCP calls and V-Claw internal tools.
- `transports`: stdio, SSE, and streamable HTTP transports.
- `auth`: local grants, permission checks, and audit hooks for MCP access.

All MCP tool execution should still pass through policies, approvals, safety, and audit.

