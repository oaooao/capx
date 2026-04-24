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

Prefer interactive operation? Install the companion skill and use `/capx`:

```bash
cp -r skills/capx ~/.claude/skills/
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

- [User guide + how-it-works](https://github.com/oaooao/capx) (see `docs/` when published)
- Full design spec: `projects/capx/reference/[design]-20260424-capx-v0.2-platform.md` (in Axiom; to be published alongside capx)
- Companion skill: [`skills/capx/SKILL.md`](skills/capx/SKILL.md)

## Status

v0.2 is stable for personal use. Open-source preparation (docs site, release
binaries, `go install` publishing) is next.

## License

MIT
