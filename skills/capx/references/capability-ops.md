# Capability 操作

## 3. 搜索可用能力

调 `mcp__capx__search`。参数组合：

- 关键词 → `{ query: "<keyword>" }`（子串匹配 name / description / aliases / keywords）
- 按类型 → `{ type: "mcp" }` 或 `{ type: "cli" }`
- 按标签 → `{ tag: "docs" }`
- 当前 scene → 先从 `scene_info` 拿 `active_scene`，再 `{ scene: "<name>" }`
- 全部 → `{}`

返回 `[{name, type, summary}, ...]`，按 name 显示，附 `(mcp)` / `(cli)` 标记。

## 4. 查能力详情

调 `mcp__capx__describe { name: "<name>" }`。加 `scene: "<X>"` 可以看 scene 内联 override 后的版本。

返回字段：

- `type` / `transport` / `command` / `url` — 怎么起的
- `source` — 定义来自哪个层（global / project / scene inline）
- `aliases` / `keywords` / `tags` — 触发语义
- `tools` — CLI 的工具名列表（MCP 类型用 ToolSearch 查）
- `example_invocation` — 手动 enable 的写法

## 5. 启停单个能力

```
mcp__capx__enable  { name: "<name>" }
mcp__capx__disable { name: "<name>" }
```

提醒用户：session 级临时操作，不改配置。下次 `set_scene` 按 scene 声明重新洗牌。

## 6. 添加新 Capability

需要编辑用户的 `.capx/capabilities.yaml`。

### Step 1：定位文件

1. 项目级：从 `$PWD` 向上找最近的 `.capx/`
2. 用户说全局：`~/.config/capx/capabilities.yaml`
3. 两个都没有：提醒先 `capx init`

问："加到项目级还是全局？"

### Step 2：收集信息

必问：`name` 和 `type`（mcp / cli）。

**mcp 类型**：
- http 还是 stdio？http → 问 `url`；stdio → 问 `command` + `args`
- 需要 env？→ `env: {KEY: "value"}` + `required_env: [KEY]`

**cli 类型**：
- `command`（可执行文件）
- 逐个 tool：tool_name / description / args / params

可选字段：`description`、`aliases`、`keywords`、`tags`、`disabled: true`

### 大型 MCP 检测

添加 MCP 类型时，如果用户提到的 MCP 已知工具多（30+）或有 workflow 机制，主动提醒：

"这个 MCP 工具比较多，建议按 workflow/场景拆成多个 capability，每个独立启停。比如 XcodeBuildMCP 可以按 ios/macos/debugging 拆分。要我帮你拆吗？"

### Step 3：校验

- `mcp` + 同时有 `command` 和 `url` → 拒绝（互斥）
- `mcp` + 两者都没 → 拒绝
- `cli` + 没 `command` → 拒绝
- `aliases` 和另一个 capability 的 name 撞了 → 警告（启动时 hard fail）

### Step 4：写文件

读现有 `capabilities.yaml`，追加新条目。保持缩进风格。不重写整个文件。

### Step 5：生效

```
已加到 <path>。生效方式：
  - 新开 session
  - 或当前 session：mcp__capx__enable { name: "<name>" }
想加入 scene？告诉我 scene 名字，我帮你改 scenes/<name>.yaml。
```
