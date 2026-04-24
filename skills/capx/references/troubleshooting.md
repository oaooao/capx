# 排错

按顺序检查：

## Step 1：scene_info 看宏观状态

调 `mcp__capx__scene_info`，看 `degraded` / `failed[]` / `degradation_reason`。从 `last_committed_switch.status` 判断是切换引入的还是启动时就不对。

## Step 2：list 看每个能力状态

调 `mcp__capx__list`，关注 status 为 `failed` 的项，它们有 `error` 字段。

## Step 3：看原始 backend 错误

capx subprocess 的 stderr 在 agent 里不直接可见。让用户直接跑 backend 进程：

- stdio MCP：执行 `command + args`，看输出。如 `npx -y @playwright/mcp@latest`
- http MCP：`curl <url>` 看可达性
- CLI：`which <command>` 看是否在 PATH

## Step 4：检查 required_env

如果 capability 声明了 `required_env: [FOO, BAR]`，跑 `echo $FOO $BAR` 确认有值。capx 在 set_scene Phase 1 检查这个，缺了会拒绝。

## Step 5：alias 冲突

capx 启动日志（`--mcp-debug`）会显示 `alias conflicts: alias "X" is declared by multiple capabilities`。两个 cap 共用 alias 会 hard fail。

## Step 6：hash 没变但体验像没切

两个 scene 给同名 cap 配了只有 description 不同的配置（process_hash 相同），切换被识别为 keep，进程不重启。

跑 `capx dump --scene X` 和 `capx dump --scene Y` 对比同名 cap 的 `process_hash`。一样就是 keep。
