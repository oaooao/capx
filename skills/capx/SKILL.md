---
name: capx
description: "capx 操作向导。当用户说 '/capx'、'capx 状态'、'切 scene'、'加 MCP'、'capx 出错了' 时触发。"
---

# capx — 操作向导

capx 是 Agent Capability Runtime——一个 MCP server 管理所有其他 MCP/CLI 工具。

## 认知同步

- **capx 是 MCP 的 MCP**：用户 agent 只连 capx，其他能力动态代理
- **Scene 是塑形器**：预加载的能力是默认倾向，未启用的通过占位 tool 按需激活
- **set_scene 三态响应**：ok / rejected / partial_failure，永远读完整 JSON
- **占位 tool**：deferred 列表中 `mcp__capx__<name>`（单层 namespace）是未激活 capability 的入口，支持 describe/enable 两个 action。详见 `references/placeholder-tools.md`
- **配置路径**：全局 `~/.config/capx/`，项目 `.capx/`（从 pwd 向上查找），项目覆盖全局
- **合并规则**：同名 capability 整对象 replace，不做字段 overlay

## 启动流程

### Step 1：诊断连接

检查 `mcp__capx__*` 工具是否存在。

**不存在**：

```
当前 session 没连上 capx MCP。
解决：capx init --agent claude-code（或 codex），然后重启 session。
```

到此停止，不进入菜单。

**存在**：调 `mcp__capx__scene_info`，展示简报：

```
✓ capx 已连接
当前 scene: <name>
就绪 (N): <ready 列表>
失败 (M): <failed 列表>
Degraded: <yes/no>
```

degraded 时主动提醒："有 required 能力不在位，要不要帮你看看？"

### Step 2：菜单

```
1. 看当前状态          → Read references/scene-ops.md
2. 切 scene            → Read references/scene-ops.md
3. 搜索能力            → Read references/capability-ops.md
4. 查能力详情          → Read references/capability-ops.md
5. 启停能力            → Read references/capability-ops.md
6. 添加新 capability   → Read references/capability-ops.md
7. 初始化项目          → Read references/setup-migration.md
8. v0.1→v0.2 迁移      → Read references/setup-migration.md
9. 导出 dump           → Read references/setup-migration.md
10. 排错               → Read references/troubleshooting.md
```

用户选编号或自然语言描述意图，Read 对应 reference 后执行。每次处理完问"还想做其他的吗？"

## 不要做

- 不编造 capx YAML 字段——不确定就让用户 `capx dump` 看实例
- 不直接改 `~/.claude.json`——让用户跑 `capx init --agent <name>`
- 不跑 `capx migrate` 前不做 `--dry-run`
- 不跳过 `capx dump` 直接解析多层 YAML——合并规则精细，凭印象会出错
