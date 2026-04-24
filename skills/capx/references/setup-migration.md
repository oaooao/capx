# 初始化、迁移与导出

## 7. 初始化项目 Scope

确认 `$PWD` 是项目根。让用户在 shell 跑：

```bash
cd <project_root>
capx init               # 创建 .capx/capabilities.yaml
capx init --add-scenes  # 补 scenes/default.yaml 样例
```

说明：
- 已有 `.capx/` 时拒绝（除非 `--force`）
- `capx init --global` 创建全局 `~/.config/capx/capabilities.yaml`
- `--agent claude-code` / `--agent codex` 注册到 agent 配置

## 8. 从 v0.1 单文件迁移

检查 `~/.config/capx/config.yaml` 是否存在。

先 dry-run：

```bash
capx migrate --dry-run
```

结果 JSON：
- `status: ok` → 可以跑
- `warnings[]` → 看每条，尤其 `env_value_null_to_empty`（让用户核对意图）
- `errors[]` → transport 冲突等，需手改后重试

确认 OK 后：

```bash
capx migrate
```

告诉用户：
- 老文件保留为 `config.yaml.v01.bak`
- 新结构：`capabilities.yaml` + `scenes/*.yaml` + `settings.yaml`
- 失败自动回滚，不会有中间态

## 9. 导出合并视图

```bash
capx dump                              # 全部合并
capx dump --scene <name>               # 指定 scene 视角
capx dump --format yaml                # yaml 格式
capx dump --config <dir>               # 从指定目录读
```

输出 JSON，schema v1。schema 文件：`schemas/dump-v1.json`。

用途：调试合并结果、给 prompt-easy / CI 消费。
