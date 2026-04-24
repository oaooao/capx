# Scene 操作

## 1. 查看当前状态

调 `mcp__capx__scene_info`，按以下格式展示：

```
当前 scene: <active_scene 或 "(未设置)">
描述: <description>
就绪 (N): <ready[]> 的 name 列表 + tool_count
失败 (M): <failed[]> 的 name + required + error 一行摘要
Degraded: <yes/no>  <degradation_reason 如果有>
最近切换: <last_switch.status> (<from_scene> → <to_scene>)
```

### 关键字段解读

- `ready[]` — 已就绪的能力，每个带 `tool_count`
- `failed[]` — 失败的能力，每个带 `error` 和 `required: bool`
- `degraded: bool` 和 `degradation_reason` — workbench 是否完整
- `last_switch` / `last_committed_switch` — 最近一次切换结果

### degradation_reason 处置

- `startup_failure` → 初次启动就有 required 没起来。查 `failed[].error`，修配置
- `failed_switch` → 上次 set_scene 残留。建议 `set_scene <last_committed_switch.from_scene>` 回退
- `runtime_crash` → `mcp__capx__enable <name>` 重启该能力

## 2. 切 Scene

让用户告诉目标 scene 名。不知道有哪些 scene 就跑 `capx scene list`。

调用：`mcp__capx__set_scene { scene: "<name>" }`

### 三态返回解读

- `status: ok` + `failed: []` → 全好，展示在位 cap 列表
- `status: ok` + `failed: [...]` → 切成功但有 **optional** 没起来，列一下原因
- `status: rejected` → 旧 scene 没动，读 `reason` + `failed[]`。典型：required_env 缺失、进程起不来
- `status: partial_failure` → scene degraded，`failed[]` 里 `rollback: failed` 是关键失败点。建议：
  - 回退：`set_scene <from_scene>`
  - 或修问题后 `set_scene <same_name>` 重试

不要只说 "set_scene failed"——返回 JSON 里信息全在，逐条展示。
