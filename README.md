# capx

Agent Capability Runtime — one MCP server, dynamic control of everything else.

capx is a single MCP server you point your AI agent (Claude Code, Codex CLI,
etc.) at. All your other MCP servers and CLI tools are declared to capx; you
group them into "scenes", switch between scenes at runtime, and add/remove
capabilities without restarting the agent.

## Why

- Your agent loads 90+ MCP tools at startup even when you need 5
- MCP configs scatter across `~/.claude.json`, `.mcp.json`, `config.toml`
- Swapping toolsets for different tasks (iOS dev / web / writing) means editing files and restarting

capx replaces that with: declare everything once in `.capx/`, compose scenes, switch via an MCP tool.

## Requirements

- Go 1.26+ (see [`go.mod`](go.mod))
- An MCP-capable agent: Claude Code, Codex CLI, or any client that speaks MCP over stdio

## Quick Start

```bash
# Install
go install github.com/oaooao/capx/cmd/capx@latest

# Scaffold project-local scope
cd ~/your-project
capx init
capx init --add-scenes

# Register with your agent (writes ~/.claude.json / ~/.codex/config.toml)
capx init --agent claude-code    # or: --agent codex

# Start the agent; capx spawns automatically
CAPX_SCENE=default claude
```

## Agent-Assisted Setup

Paste one of these prompts to your AI agent to get started:

**First-time install:**

> Install capx (Agent Capability Runtime) for me:
> 1. Run: `go install github.com/oaooao/capx/cmd/capx@latest`
> 2. Run: `capx init --agent claude-code`
> 3. Restart this session so capx connects.
> After restart, verify by calling `mcp__capx__scene_info`.

**Add a new MCP capability:**

> I want to add [NAME] to capx. Help me:
> 1. Search if it exists: `mcp__capx__search { query: "[NAME]" }`
> 2. If not found, add it to capabilities.yaml with description and keywords
> 3. If this MCP has 30+ tools, suggest splitting by workflow into separate capabilities

**Switch work context:**

> Switch my capx scene to [SCENE_NAME].
> If you don't know available scenes, run: `capx scene list`

> **Tip**: If the MCP you're adding exposes many tools with distinct workflows
> (like XcodeBuildMCP's simulator / ui-automation / debugging), split it into
> multiple capabilities — one per workflow. This improves discoverability and
> lets you enable only what you need.

Inside the agent session you now have:

```
mcp__capx__scene_info     Current scene state + ready/failed capabilities
mcp__capx__set_scene      Switch scenes atomically
mcp__capx__search         Discover capabilities
mcp__capx__describe       Full metadata for one capability
mcp__capx__list           All capabilities + status
mcp__capx__enable         Runtime enable
mcp__capx__disable        Runtime disable
```

During MCP initialization, capx also sends concise server instructions that
explain the mental model to the agent: capx is a capability runtime, call
`scene_info` at session start, use `search → describe → enable` when a task
needs a missing capability, and inspect the three-state `set_scene` result
instead of treating it as a boolean.

The instructions also define a capability-resolution trigger: if the user asks
to use/call/invoke/run a named MCP, CLI, command, tool, or capability that is
not already visible in the current tool set, the agent should proactively
`search` capx before giving up or substituting another tool. These instructions
intentionally do not embed dynamic capability lists; use `scene_info`, `search`,
and `describe` for current state.

### Placeholder tools — inactive capabilities stay discoverable

Every declared-but-inactive capability registers a single-action placeholder
tool named `mcp__capx__<name>`. Its description is the capability's
description, so the agent sees a one-line summary of every available capability
from session start — not just the ones in the active scene.

```
mcp__capx__<name>   action: describe | enable
```

- `describe` — returns metadata (type, command, env, keywords) without starting
  the backend. Use it to decide whether this capability fits the task.
- `enable` — activates the capability and registers its real tools. The
  placeholder disappears and real tools appear as `mcp__capx__<name>__<tool>`
  (two-segment namespace). `disable` restores the placeholder.

This closes the discoverability gap most MCP aggregators leave open: without
placeholders, agents only find out about inactive capabilities if they
proactively call `search` — which they rarely do. With placeholders, the
capability's description advertises itself directly in the tool list.

Write capability descriptions that tell the agent *what it can do*, not just
*what it is* — see [`docs/AUTHORING.md`](docs/AUTHORING.md) for the
TOC-style convention.

Prefer interactive operation? Install the companion skill and use `/capx`:

```bash
git clone https://github.com/oaooao/capx.git
cp -r capx/skills/capx ~/.claude/skills/
```

## Configuration Layout

capx discovers config in this order (lowest → highest priority):

1. **Global** — `~/.config/capx/` (or `$XDG_CONFIG_HOME/capx/`)
2. **Project** — nearest `.capx/` walking up from `$PWD`
3. **Override** — `$CAPX_HOME` bypasses both layers if set

Each `.capx/` directory looks like:

```
.capx/
├── capabilities.yaml          # main declarations
├── capabilities.d/            # optional, scanned lexicographically
│   └── 01-extras.yaml
├── scenes/                    # one file per scene
│   ├── default.yaml
│   └── web.yaml
└── settings.yaml              # default_scene, etc.
```

Project-scope same-name capabilities replace global ones (whole-object replace,
no field overlay).

## Capabilities

### MCP (http or stdio)

```yaml
capabilities:
  context7:
    type: mcp
    url: https://mcp.context7.com/mcp      # transport inferred: http
    tags: [docs]

  playwright:
    type: mcp
    command: npx                           # transport inferred: stdio
    args: ["-y", "@playwright/mcp@latest"]
    description: "Browser automation"
    aliases: [browser]
    keywords: [browser, automation, e2e]
```

### CLI (wraps a command as MCP tools)

```yaml
capabilities:
  webx:
    type: cli
    command: webx
    description: "Web fetch & search"
    tools:
      read:
        description: "Read any URL"
        args: ["read", "{{url}}", "--format", "markdown"]
        params:
          url: { type: string, required: true }
```

### Optional fields

- `aliases` — alternate names; `mcp__capx__enable browser` resolves to `playwright`
- `keywords` — soft-match for `search`, not for `enable`
- `required_env` — env vars checked at startup; missing → capability refuses to start
- `disabled: true` — hides the capability from list/search/enable entirely
- `tags` — free-form grouping

## Scenes

A scene is a named preset of capabilities. Declare one per file under `scenes/`:

```yaml
# .capx/scenes/web.yaml
description: "Web development workbench"
auto_enable:
  required: [playwright]          # failure → scene is degraded
  optional: [webx, context7]      # failure → silently skipped
```

Shorthand — flat list means all-optional:

```yaml
auto_enable: [playwright, webx, context7]
```

### Scene inheritance

```yaml
# .capx/scenes/web-headless.yaml
extends: [web]
description: "Web dev with headless browser"
capabilities:
  playwright:                     # inline: replaces global playwright in this scene
    type: mcp
    command: npx
    args: ["-y", "@playwright/mcp@latest", "--headless"]
    aliases: [browser]
```

Extends is DFS + left-to-right + first-seen-wins. `required` merge is strict:
child can upgrade parent `optional`→`required` but cannot demote `required`→`optional`.

## Scene switching

`set_scene` is diff-based and best-effort atomic:

- Capabilities unchanged (same `process_hash` + `tools_hash`) → kept, no restart
- Only tool schema changed → schema re-registered, process untouched
- Process config changed → stop + restart with rollback on failure
- New → started in shadow and committed
- Removed → stopped

Response is three-state JSON:

```json
{
  "status": "ok" | "rejected" | "partial_failure",
  "active_scene": "web",
  "failed":  [{"name": "x", "reason": "...", "required": true, "rollback": "succeeded"}],
  "applied": [{"name": "...", "action": "enable|restart|refresh_tools|disable|keep"}],
  "reason":  null
}
```

- `rejected` — Phase 1 refused, old scene untouched
- `ok` with non-empty `failed` — switch succeeded but some `optional` caps didn't start
- `partial_failure` — a `required` restart failed AND rollback also failed (scene degraded)

`scene_info` always gives you the ground truth: `ready` / `failed` / `degraded` /
`degradation_reason` / `last_switch` / `last_committed_switch`. Recommended
for agents to call once at session start.

## CLI reference

```bash
capx serve                                  # Run as MCP server (stdio)
capx list                                   # Merged capability list
capx scene list | capx scenes               # Scene names

capx init [--global] [--add-scenes]         # Scaffold a scope
capx init --agent claude-code | codex       # Register with an agent

capx dump [--scene <n>] [--format json|yaml] [--config <dir>]
                                            # Authoritative merged view (schema v1)
capx migrate [--dry-run]                    # v0.1 single-file → v0.2 directory

capx version
```

`capx dump` is the authoritative contract for file-reading consumers
(prompt-easy, typefree, CI validators). Its JSON shape is published at
[`schemas/dump-v1.json`](schemas/dump-v1.json).

## Migrating from v0.1

If you have an existing `~/.config/capx/config.yaml`:

```bash
capx migrate --dry-run     # preview splits + warnings
capx migrate               # execute
```

Migration is FS-atomic (two `rename(2)` calls with rollback), resolves symlinks
(chezmoi / dotfiles pass through transparently), and leaves your old file as
`config.yaml.v01.bak`. Any failure and you're back to a clean v0.1 state.

## Documentation

- Capability authoring guide: [`docs/AUTHORING.md`](docs/AUTHORING.md)
- Dump schema (v1): [`schemas/dump-v1.json`](schemas/dump-v1.json)
- Companion skill for AI agents: [`skills/capx/SKILL.md`](skills/capx/SKILL.md)

## Status

v0.2 is stable for personal use. Expect rough edges around multi-platform
packaging, CI, and release binaries — contributions welcome.

## License

MIT
