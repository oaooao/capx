---
name: capx
description: "capx (Agent Capability Runtime) 的操作向导——帮你记不住的命令、切 scene、加能力、看状态、排错。当用户说 '/capx'、'capx'、'切 scene'、'加 MCP'、'capx 状态'、'capx 出错了' 时触发。本 skill 加载时会先诊断 capx MCP 连接状态并同步关键认知给 Agent，之后再进入操作菜单。"
---

# capx — 操作向导

capx 是一个中间层 MCP server，动态管理你的其他 MCP 和 CLI 工具。这份 skill 是它的日常操作菜单——你记不住确切命令或参数时，呼出 `/capx`，照单执行。

## 启动流程（skill 加载时立即执行）

### Step 0：认知同步（给 Agent 自己的一段 briefing）

读完这段再继续。你（Agent）需要知道以下几点：

- **capx 是 MCP server 的 MCP server**。用户的 agent 客户端（cc/codex/amp）只连一个 capx，其他能力由 capx 代理，运行时动态启停。
- **Scene 是塑形器，不是过滤器**。Scene 预加载的能力是 Agent 首选项，但通过 cc 的 deferred tool 机制，未启用的能力也能按需激活（`mcp__capx__enable` / `set_scene`），token 成本接近零。不要跟用户说"Scene 省 token"，说"Scene 决定工作台默认倾向"。
- **set_scene 是三态响应**，不是布尔。`ok` / `rejected` / `partial_failure`，每种都带 `failed[]` 和 `reason`。永远读完整 JSON，不要只看 `status`。
- **scene_info 是查状态的入口**。用户问"现在什么状态"、"哪些能出错了"、"能力够不够"，都调 `mcp__capx__scene_info`。
- **配置路径**：全局 `~/.config/capx/`，项目 `.capx/`（从 pwd 向上查找），项目覆盖全局。`CAPX_HOME` 环境变量显式覆盖两者。
- **合并规则**：同名 capability 按整对象 replace，不做字段 overlay。scene 内联 > 项目 > 全局。
- **两个 fingerprint**：`process_hash`（要不要 restart 进程）、`tools_hash`（要不要重注册 tool schema）。切 scene 时 capx 按两个 hash 独立 diff，决定 keep / restart / refresh_tools。

### Step 1：诊断 capx MCP 连接

检查当前 session 可用工具里是否有 `mcp__capx__set_scene` 和 `mcp__capx__scene_info` 等 `mcp__capx__*` 前缀工具。

**情况 A — 工具不存在**：

capx 没接到当前 agent。告诉用户：

```
当前 session 没连上 capx MCP。可能的原因：
  1. 还没跑 `capx init --agent <claude-code|codex>` 注册
  2. 注册过但还没重开 agent session

解决：
  capx init --agent claude-code   # 或 codex
然后退出并重新打开你的 agent（cc / codex），skill 会自动生效。
```

到此停止，不继续菜单。

**情况 B — 工具存在**：

立刻调用 `mcp__capx__scene_info`。用返回值给用户一段简报：

```
✓ capx 已连接

当前 scene: <active_scene 或 "(未设置)">
描述: <description>
就绪 (N): <ready[]> 的 name 列表 + tool_count
失败 (M): <failed[]> 的 name + required + error 一行摘要
Degraded: <yes/no>  <degradation_reason 如果有>
最近切换: <last_switch.status> (<from_scene> → <to_scene>, <at 相对时间>)
```

如果 `degraded: true`，在简报之后立刻加一句提醒："有 required 能力不在位，任务规划时注意规避。要不要我帮你看看怎么修？"

简报之后进入 Step 2 菜单。

### Step 2：展示菜单

```
我能帮你做什么？

  1. 看当前 scene 和能力状态（重新跑诊断）
  2. 切 scene
  3. 搜索可用能力
  4. 查某个能力的详情
  5. 启停单个能力
  6. 加一个新 capability 到配置
  7. 初始化一个新项目 scope
  8. 从 v0.1 单文件迁移到 v0.2 目录结构
  9. 导出合并后的视图（dump）
  10. 能力起不来，帮我排错

告诉我编号，或者直接用自然语言描述你想做的事。
```

用户给编号就跳对应章节；用户给自然语言，你判断意图对应哪个编号再执行。

---

## 1. 看当前 scene 和能力状态

重新调 `mcp__capx__scene_info`，按 Step 1 的格式展示。如果用户已经在会话中操作过 capx（比如刚 set_scene），这次能看到更新后的状态。

关键字段解读：

- `active_scene` + `description` — 当前工作台身份
- `ready[]` — 已就绪的能力，每个带 `tool_count`
- `failed[]` — 失败的能力，每个带 `error` 和 `required: bool`
- `degraded: bool` 和 `degradation_reason` — workbench 是否完整
- `last_switch` / `last_committed_switch` — 最近一次切换的结果

`degradation_reason` 的处置建议：

- `startup_failure` → 初次启动就有 required 没起来。让用户查 `failed[].error`，修配置
- `failed_switch` → 上次 set_scene 的残留 degraded。建议 `set_scene <last_committed_switch.from_scene>` 回退到已知稳定的 scene
- `runtime_crash`（v0.3+ 才可能出现） → `mcp__capx__enable <name>` 重启该能力

## 2. 切 scene

让用户告诉你目标 scene 名。如果用户不知道有哪些 scene，让他在 shell 跑 `capx scene list`（scene 列表没有 MCP 接口）。

调用：

```
mcp__capx__set_scene { scene: "<name>" }
```

解读三态返回：

- `status: ok` + `failed: []` → 全好，告诉用户切成功 + 哪些 cap 在位
- `status: ok` + `failed: [...]` → 切成功但有 **optional** 没起来，列一下哪些失败以及原因
- `status: rejected` → **旧 scene 没动**，读 `reason` + `failed[]` 定位问题。典型原因：required_env 缺失、进程起不来、required cap 在别的 scene 有独占资源还没释放
- `status: partial_failure` → scene 处于 degraded，`failed[]` 里带 `rollback: failed` 的是关键失败点。建议用户：
  - 回退：`set_scene <from_scene>`
  - 或修问题后 `set_scene <same_name>` 重试

不要只说 "set_scene failed"——返回 JSON 里信息全在，逐条展示。

## 3. 搜索可用能力

调 `mcp__capx__search`。参数组合：

- 用户给了关键词 → `{ query: "<keyword>" }`（子串匹配 name / description / aliases / keywords）
- 用户说"所有 MCP" → `{ type: "mcp" }`
- 用户说"带 docs 标签的" → `{ tag: "docs" }`
- 用户说"当前 scene 里有哪些" → 先从 `scene_info` 拿 `active_scene`，再 `{ scene: "<name>" }`
- 用户说"全部列出来" → `{}`

返回是 `[{name, type, summary}, ...]`。按 name 显示，附带 `(mcp)` / `(cli)` 类型标记。

## 4. 查某个能力的详情

调 `mcp__capx__describe { name: "<name>" }`。若用户说"在 X scene 视角下看"，加 `scene: "<X>"` 参数——这会应用 scene 内联 override，返回的字段反映实际在 scene X 中生效的版本。

返回字段关注：

- `type` / `transport` / `command` / `url` — 怎么起的
- `source` — 这个定义来自哪个层（global / project / scene inline / global.d:xxx.yaml）
- `aliases` / `keywords` / `tags` — 触发语义
- `tools` — CLI 的工具名列表（MCP 类型不在这里，用 cc 的 ToolSearch 查）
- `example_invocation` — 手动 enable 的写法

## 5. 启停单个能力

这是"逃生舱"路径——正常应该用 scene 组合。但如果用户就想临时加/减一个：

```
mcp__capx__enable  { name: "<name>" }
mcp__capx__disable { name: "<name>" }
```

提醒用户：这是 session 级临时操作，不改配置文件。下次 `set_scene` 会按 scene 声明重新洗牌。

## 6. 加一个新 capability 到配置

这个流程需要你作为 Agent **编辑用户的 `.capx/capabilities.yaml`**。

### Step 1：定位要改的文件

优先级：

1. 项目级：从 `$PWD` 向上找最近的 `.capx/`，改 `<scope_root>/.capx/capabilities.yaml`
2. 用户明确说要加全局：改 `~/.config/capx/capabilities.yaml`
3. 两个都没有：提醒用户先跑 `capx init`

问一句："加到项目级还是全局？项目级只在这个 repo 用，全局所有项目都用。"

### Step 2：收集信息

必问：

- `name`：你想叫它什么？
- `type`：`mcp` 还是 `cli`？

根据 type 分支：

**mcp 类型**：

- 它是走 http 还是 stdio 的？
  - http → 问 `url`（https://...）
  - stdio → 问 `command` 和 `args`（例如 `npx` + `["-y", "@foo/mcp"]`）
- 需要环境变量吗？→ `env: {KEY: "value"}` + `required_env: [KEY]`（后者让 capx 启动前检查）

**cli 类型**：

- 问 `command`（可执行文件路径或名字）
- 逐个问 tool：tool_name / description / args 前缀 / params（name, type, required, enum）

其他可选字段：

- `description`（一句话说明）
- `aliases`（别名，用户说"用 browser" 会被解析到这个 cap）
- `keywords`（搜索用，不参与 enable 匹配）
- `tags`（分类）
- `disabled: true`（占位，不参与 list / search / enable）

### Step 3：校验

写文件前自己过一遍：

- `mcp` + 同时有 `command` 和 `url` → 拒绝（互斥）
- `mcp` + 两者都没 → 拒绝
- `mcp` + 显式 `transport: http` 但只给了 `command` → 拒绝
- `cli` + 没 `command` → 拒绝
- `aliases` 里的某个值和另一个 capability 的 name 撞了 → 警告用户（会在 capx 启动时 hard fail）

### Step 4：写文件

读取现有 `capabilities.yaml`，往 `capabilities:` 下追加新条目。保持 YAML 的缩进风格和注释。不要重写整个文件；只插入新块。

### Step 5：生效提示

```
已加到 <path>。想生效要新开 session，或者在当前 session 里：
  mcp__capx__enable { name: "<name>" }

想把它加入某个 scene？告诉我 scene 名字，我帮你改 scenes/<name>.yaml。
```

## 7. 初始化一个新项目 scope

确认 `$PWD` 是项目根（或问用户具体在哪初始化）。然后告诉用户在 shell 跑：

```bash
cd <project_root>
capx init               # 创建 .capx/capabilities.yaml
capx init --add-scenes  # 补 scenes/default.yaml 样例
```

说明：

- `capx init` 会拒绝在已有 `.capx/` 的目录内再建（除非 `--force`）
- `capx init --global` 改为创建全局 `~/.config/capx/capabilities.yaml`
- `--agent claude-code` / `--agent codex` 注册到 agent 配置，让 agent 自动启动 capx

## 8. 从 v0.1 单文件迁移

检查用户是不是还在用老配置：问 `~/.config/capx/config.yaml` 是否存在。

迁移前先 dry-run：

```bash
capx migrate --dry-run
```

结果 JSON 关注：

- `status: ok` → 可以安心跑
- `warnings[]` → 看每一条，尤其 `env_value_null_to_empty`（这个是强 warning，让用户核对意图）
- `errors[]` → transport 冲突这类，需要手改老 config 后重试

确认 dry-run OK：

```bash
capx migrate
```

告诉用户：

- 老 `config.yaml` 保留为 `~/.config/capx/config.yaml.v01.bak`
- 新结构是 `capabilities.yaml` + `scenes/*.yaml` + `settings.yaml`
- 任何失败都会自动回滚，手里永远是完整的 v0.1 或完整的 v0.2，没有中间态

## 9. 导出合并后的视图

当用户想：

- 看多层合并后的最终 capability
- 调试"为什么我改了项目配置没生效"
- 给 prompt-easy / 自制 UI 提供数据源

让用户在 shell 跑：

```bash
capx dump                              # 全部合并
capx dump --scene <name>               # 指定 scene 视角（应用 inline override）
capx dump --format yaml                # yaml 格式
capx dump --config <dir>               # 从指定目录读
```

输出是 JSON，schema v1。schema 文件在 capx repo 的 `schemas/dump-v1.json`。

## 10. 能力起不来，帮我排错

按这个顺序检查：

### Step 1：scene_info 看宏观状态

调 `mcp__capx__scene_info`，看 `degraded` / `failed[]` / `degradation_reason`。如果 scene 是 degraded，从 `last_committed_switch.status` 判断是切换引入的还是启动时就不对。

### Step 2：`mcp__capx__list` 看每个能力状态

关注 status 为 `failed` 的项，它们有 `error` 字段给具体错误。

### Step 3：看原始 backend 错误

capx 作为 subprocess 的 stderr 在 cc 里不容易直接看。最可靠的做法是让用户**直接跑 backend 进程**：

- stdio MCP：执行它的 `command + args`，看第一屏输出。比如 `npx -y @playwright/mcp@latest` 看有没有 Playwright 安装、依赖、权限问题
- http MCP：`curl <url>` 看可达性
- CLI：直接跑 `command` 看它是不是在 PATH 里（`which <command>`）

### Step 4：检查 required_env

如果 capability 声明了 `required_env: [FOO, BAR]`，跑 `echo $FOO $BAR` 确认都有值。capx 在 set_scene Phase 1 会检查这个，缺了会拒绝但 error message 很明确。

### Step 5：alias 冲突

capx 启动时日志（cc 的 MCP debug 模式 `--mcp-debug`）会显示 `alias conflicts: alias "X" is declared by multiple capabilities`。两个 cap 共用 alias 会 hard fail。

### Step 6：hash 没变但体验像没切

常见误解：两个 scene 用同名 capability 但 inline 配置不同，用户以为切了 scene 就改变了。**capx 是按 process_hash + tools_hash diff** 的——如果两个 scene 给同名 cap 配置了完全相同的 process_hash（比如只改了 description、没改 args/env），切换被识别为 keep，进程不重启。

让用户跑 `capx dump --scene X` 和 `capx dump --scene Y` 对比两个 scene 下同名 cap 的 `process_hash`，如果一样，diff 就是 keep。

---

## 结束

每次处理完一个选项，问用户"还想做其他的吗？"继续菜单循环，或 natural exit。

## 不要做

- **不要编造 capx 的 YAML 字段**。所有字段见 capx repo 的 `reference/` 设计文档 §A.5 和 §A.6。不确定就让用户先 `capx dump` 看实例
- **不要直接修改 `~/.claude.json` 或 `~/.codex/config.toml`**。让用户跑 `capx init --agent <name>`——那条路径有 dry-run 确认
- **不要跑 `capx migrate` 的实际命令而不先做 `--dry-run`**。迁移是个大操作，哪怕原子回滚存在，dry-run 确认对用户心理负担小得多
- **不要跳过 capx dump 直接解析多层 YAML**。合并规则精细，凭印象解析会出错；capx dump 是权威契约
