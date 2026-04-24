# Capability Authoring Guide

How to write capability declarations that agents can discover.

## Why This Matters

capx exposes each capability's `description` and `keywords` in two places:

1. **Placeholder tools** — deferred tool entries that agents see before a capability is activated
2. **`search` results** — when an agent searches for capabilities by keyword

If description says "iOS build tool" but the capability also does UI automation, an agent looking for "UI testing" will never find it.

## Description

Write like a table-of-contents entry: one-sentence positioning, then key sub-capabilities.

```
<core function in 2-4 words>. Key capabilities: <2-4 sub-capabilities, comma-separated>.
```

Total length: under 80 characters.

### Good

```yaml
description: "iOS simulator dev. Key capabilities: build/test/run, UI automation, log capture."
```

### Bad

```yaml
description: "Xcode iOS"           # Too terse — agent can't judge relevance
description: "A comprehensive tool  # Too verbose — wastes deferred-list space
  for building, testing, debugging,
  and automating iOS applications..."
```

## Keywords

3–8 lowercase terms. Cover both **naming** (what it's called) and **describing** (what it does):

```yaml
keywords: [ios, simulator, build, test, ui-automation, screenshot, log]
#          ^^^^^^^^^^^^^^^^^^^^^^^^^^^  ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
#          naming (identity)            describing (capability)
```

Use hyphens for multi-word terms: `ui-test` not `ui_test`.

## Aliases

Short names users might say when asking for this capability. Unlike keywords, aliases participate in `enable` resolution — `mcp__capx__enable { name: "browser" }` resolves to `playwright-mcp` if `aliases: [browser]`.

Keep aliases unique across all capabilities. Duplicates cause a hard startup failure.

## Tags

Free-form grouping for `search { tag: "docs" }`. Tags don't affect enable resolution.

## Large MCP Servers

If an MCP server exposes 30+ tools with distinct workflows, split it into multiple capabilities — one per usage scenario. Use the server's own configuration mechanism (env vars, config files) to control which tools are exposed.

Example — XcodeBuildMCP split by platform:

```yaml
XcodeBuildMCP/ios:
  env:
    XCODEBUILDMCP_ENABLED_WORKFLOWS: "simulator,ui-automation,logging"

XcodeBuildMCP/macos:
  env:
    XCODEBUILDMCP_ENABLED_WORKFLOWS: "macos,debugging,logging"
```

Each split capability gets its own description and keywords targeting that specific subset.
