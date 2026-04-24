# Changelog

All notable changes to capx are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/).

## [1.0.1] — 2026-04-25

### Added

- Initialize instructions now list all available scene names
  (alphabetical) so an agent can discover which scenes exist from the
  session-start context, without needing to pull `set_scene`'s tool
  schema via ToolSearch first.

  Before: the agent knew "there are scenes" but had to query for names.
  After: the instructions end with an `Available scenes:` line so
  casual requests like "switch to the macos scene" map to the concrete
  scene name immediately.

  `BuildInstructions` now takes a `*config.Config` (pass `nil` in unit
  tests that don't need the scenes section). `newCapxMCPServer`
  signature updated accordingly.

## [1.0.0] — 2026-04-25

First public release. Agent Capability Runtime — one MCP server, dynamic
control of MCP servers and CLI tools.

### Core

- v0.2 config layout: `capabilities.yaml` + `scenes/*.yaml` + `settings.yaml`
- Three-scope discovery:
  - Global (`~/.config/capx/`) + Project (`.capx/` walking up from pwd),
    merged with project-overrides-global semantics
  - `CAPX_HOME` relocates the global scope directory (project discovery still
    applies)
  - `CAPX_ISOLATE=1` forces single-scope mode (useful for tests, CI, and
    diagnostics)
- Scene semantics: diff-based switching with three-state response
  (`ok` / `rejected` / `partial_failure`), rollback on required restart failure
- MCP server with full lifecycle management: `enable` / `disable` / `restart` /
  `refresh_tools` / `keep`

### Agent discoverability (three-layer model)

- **Data layer**: TOC-style capability descriptions (see `docs/AUTHORING.md`)
- **Behavior layer**: MCP initialize instructions embed a capability-resolution
  trigger so agents search capx proactively when a named or described
  capability isn't visible
- **Structure layer**: placeholder tools `mcp__capx__<name>` for every
  declared-but-inactive capability — agents see every capability in the
  deferred tool list from session start, not just the active-scene ones

### CLI

- `capx serve` — MCP server (stdio)
- `capx list` / `capx scene list` / `capx scenes` — inspect merged config
- `capx dump [--scene <n>] [--format json|yaml] [--config <dir>]` —
  authoritative merged view, v1 schema (`schemas/dump-v1.json`)
- `capx init [--global] [--add-scenes] [--agent claude-code|codex]` — scaffold
  and agent registration
- `capx migrate [--dry-run]` — v0.1 single-file → v0.2 directory layout with
  rollback-safe FS atomicity

### Companion skill

- `skills/capx/SKILL.md` — thin entry + `references/*.md` progressive disclosure
  for interactive `/capx` operation in Claude Code

### Notes

- Requires Go 1.26+
- v0.1 single-file config (`~/.config/capx/config.yaml`) still accepted for
  backwards compatibility; migrate with `capx migrate`
- MIT licensed

[1.0.0]: https://github.com/oaooao/capx/releases/tag/v1.0.0
[1.0.1]: https://github.com/oaooao/capx/releases/tag/v1.0.1
