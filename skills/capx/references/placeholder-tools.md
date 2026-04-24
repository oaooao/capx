# 占位 Tool 使用指南

## 什么是占位 Tool

deferred 列表中 `mcp__capx__<name>` 格式（单层 namespace，无 `__<tool>` 后缀）的条目是未激活 capability 的占位工具。

区别于激活后的真实工具 `mcp__capx__<name>__<tool>`（双层 namespace）。

占位 tool 的 description 来自 capability 配置里的 description 字段——所以 description 写得好，Agent 扫到占位 tool 就能判断相关性。

## 两个 Action

- `{action: "describe"}` — 只读查看元数据（type, command, env, description, keywords），不启动子进程。用于判断这个 capability 是否有你需要的能力。
- `{action: "enable"}` — 激活 capability，启动子进程，注册真实工具。占位 tool 消失，真实工具出现在 deferred 列表。等价于 `mcp__capx__enable {name}`。

## 典型使用场景

Agent 在 deferred 列表看到一个占位 tool，description 匹配用户意图时：

1. 先 `{action: "describe"}` 确认这是需要的能力
2. 确认后 `{action: "enable"}` 激活
3. 激活后 ToolSearch 对应的 `mcp__capx__<name>__*` 工具拉 schema 使用

如果 description 已经足够明确（比如"iOS 模拟器开发"且用户说"帮我 build iOS app"），可以跳过 describe 直接 enable。

## 生命周期

- **启动时注册**：声明但未在 scene auto_enable 中的、非 disabled 的 capability
- **enable 后消失**：真实工具出现（通过 tools/list_changed 通知客户端刷新）
- **disable 后恢复**：真实工具消失，占位回来
- **set_scene 时批量刷新**：新 scene 的 capability enable，旧 scene 独有的 disable 恢复占位

## 注意

- `disabled: true` 的 capability 不会有占位 tool
- 占位 tool 的 description 质量取决于 capabilities.yaml 的编写——参见 `docs/AUTHORING.md`
