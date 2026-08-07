# Pi MCP bridge

Global pi extension that connects to MCP servers over Streamable HTTP or stdio.
It discovers remote tools lazily so large MCP catalogs do not inflate every model
request.

## Configuration

Servers are defined in `~/.pi/agent/mcp.json`:

```json
{
  "servers": {
    "docs": {
      "transport": "http",
      "url": "https://example.com/mcp",
      "headers": {
        "Authorization": "$DOCS_MCP_AUTHORIZATION"
      }
    },
    "local": {
      "transport": "stdio",
      "command": "bunx",
      "args": ["some-mcp-server"]
    }
  },
  "defaults": {
    "startupTimeoutMs": 15000,
    "toolTimeoutMs": 120000
  }
}
```

String values support `$NAME` and `${NAME}` environment interpolation. `$$`
produces a literal dollar sign. Relative stdio working directories resolve from
the current pi project.

## Agent tool

The `mcp` loader tool supports:

- `servers`: show configured server status
- `search`: connect to one or all servers, find matching tools, and enable them
- `load`: enable every tool from one server

Loaded tools use stable names such as `mcp_docs_search` and become available on
the model's next turn.

## Commands

- `/mcp status`
- `/mcp connect <server|all>`
- `/mcp enable <server|all>`
- `/mcp tools <server>`
- `/mcp reload`

Run `/reload` after editing the extension itself. Editing `mcp.json` only needs
`/mcp reload`.

## Computer Use

The `computer-use` server runs the signed Codex MCP relay rather than the legacy
`SkyComputerUseClient` executable. OpenAI's native Computer Use service requires
an OpenAI-signed parent process, so the legacy executable can advertise tools
when launched by pi but its calls fail authentication. The signed relay keeps
that security boundary intact and delegates narrowly scoped UI tasks to the
installed Codex Computer Use plugin. See `~/.agents/skills/computer-use/SKILL.md`.

## Security

Keep credentials outside `mcp.json`; reference environment variables instead.
The bridge redacts resolved HTTP header values from connection errors and writes
truncated full tool results to mode-`0600` temporary files.
