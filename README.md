# capx

Agent Capability Runtime. One MCP connection, all your tools.

capx is a single MCP server that manages all your agent's capabilities — MCP servers, CLI tools, and more — with runtime enable/disable, scene-based presets, and zero token waste.

## Why

- Your AI agent loads 90+ MCP tools at startup, even when you only need 5
- MCP configs are scattered across ~/.claude.json, .mcp.json, config.toml
- Adding a new agent means writing another config adapter

capx fixes this: one config file, one MCP connection, capabilities on demand.

## Quick Start

```bash
# Build
go build -o capx ./cmd/capx/

# Create config
mkdir -p ~/.config/capx
cp config.example.yaml ~/.config/capx/config.yaml
# Edit to match your setup

# Test
./capx list
./capx scene list

# Use with Claude Code
./capx setup claude-code
```

Or configure manually in `~/.claude.json`:

```json
{
  "mcpServers": {
    "capx": {
      "command": "capx",
      "args": ["serve"]
    }
  }
}
```

## Features

- **MCP servers**: proxy and manage any MCP server (stdio + HTTP)
- **CLI tools**: wrap any CLI as MCP tools (webx, gh, jq, ...)
- **Scenes**: preset capability combinations (ios-dev, web-dev, ...)
- **Runtime control**: enable/disable capabilities without restarting
- **Agent-native**: management via MCP tools, not Web UI
- **Zero dependencies**: single Go binary

## How It Works

capx sits between your AI agent and your actual tools:

```
Agent ←stdio→ capx ←stdio/http→ Backend MCP servers
                   ←exec→       CLI tools
```

At startup, capx loads a scene (a preset list of capabilities). The agent sees capx's 4 management tools (`list`, `enable`, `disable`, `set_scene`) plus any tools from enabled capabilities.

When the agent calls `enable("XcodeBuildMCP/ios")`, capx spawns the backend MCP server, connects to it, discovers its tools, and registers them as its own — then sends a `tools/list_changed` notification so the agent refreshes its tool list. The agent can now use those tools directly.

Tool names are prefixed to avoid conflicts: `XcodeBuildMCP_ios__build_sim`, `webx_read`, etc.

## Config

See [config.example.yaml](config.example.yaml) for the full schema.

### Capability types

- `mcp` with `transport: stdio` — spawns a subprocess, communicates via MCP over stdin/stdout
- `mcp` with `transport: http` — connects to a remote MCP server via HTTP
- `cli` — wraps a CLI tool's subcommands as MCP tools with template arguments

### Scenes

Named presets that auto-enable a set of capabilities. The special value `all` enables everything.

```yaml
scenes:
  ios-dev:
    auto_enable: [context7, XcodeBuildMCP/ios, webx]
```

### Environment

| Variable | Description |
|---|---|
| `CAPX_CONFIG` | Override config file path |
| `CAPX_SCENE` | Override initial scene |

## CLI

```
capx serve                  # Start MCP server (stdio)
capx list                   # List configured capabilities
capx scene list             # List scenes
capx add <name> [options]   # Add capability to config
capx setup claude-code      # Migrate Claude Code config
capx setup codex            # Migrate Codex CLI config
capx version                # Print version
```

## License

MIT
