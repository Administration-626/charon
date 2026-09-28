# 代码库结构

## 1) 顶层目录

| 路径 | 用途 | 证据 |
|---|---|---|
| `cmd/charon/` | CLI 入口、子命令分发、shell 补全 | `cmd/charon/main.go`、`commands.go`、`completions.go` |
| `internal/catalog/` | provider、credential、model、binding、active 指针和投影 | `internal/catalog/*.go` |
| `internal/tools/` | Codex、Claude、OpenCode、Pi、omp、Grok 的配置适配器 | `internal/tools/tool.go`、各工具文件 |
| `internal/models/` | 远端模型列表获取和上下文窗口规则 | `internal/models/fetch.go`、`builtin.go` |
| `internal/artifact/` | 临时文件、同步、权限、rename 原子写入 | `internal/artifact/artifact.go` |
| `internal/secret/` | 密钥遮罩和平台密钥链读取 | `internal/secret/` |
| `internal/tui/` | Bubble Tea 交互菜单、向导、模型选择器 | `internal/tui/` |
| `docs/` | ADR、WorkBuddy 上下文窗口同步说明、代码库文档 | `docs/` |
| `scripts/` | 安装脚本和模型规则提取脚本 | `scripts/` |
| `.github/workflows/` | CI 与发布流水线 | `.github/workflows/` |

## 2) 入口

- 主入口：`cmd/charon/main.go` 的 `main` 和 `run`。
- 交互入口：无参数时调用 `tui.Run`。
- 脚本入口：`status`、`ls`、`models`、`add`、`edit`、`switch`、`rename`、`cp`、`rm`、`completion` 等由 `cmd/charon/commands.go` 分发。
- 发布入口：GoReleaser 以 `./cmd/charon` 构建二进制。证据：`.goreleaser.yaml`。

## 3) 模块边界

| 边界 | 属于这里的职责 | 不应放入这里的职责 |
|---|---|---|
| `cmd/charon` | 参数解析、输出、调用应用服务 | 工具配置格式细节、目录表完整性规则 |
| `internal/tui` | 键盘事件、视图状态、表单和选择器 | 直接解析六种工具配置 |
| `internal/catalog` | 本地目录模型、绑定校验、锁、激活和投影编排 | 具体 JSON/TOML/YAML 字段渲染 |
| `internal/tools` | 每个工具的检测、描述、ApplyAuth、配置合并 | 绑定持久化和 CLI 参数解析 |
| `internal/models` | `/v1/models` 请求、方言重试、上下文窗口推断 | 修改工具配置 |
| `internal/artifact` | 通用原子文件替换 | 业务字段和认证策略 |
| `internal/secret` | 遮罩、平台密钥链适配 | 保存绑定目录或写工具配置 |

## 4) 命名和组织规则

- Go 源文件使用小写、按领域命名，例如 `catalog.go`、`project.go`、`claude.go`。
- 工具适配器按一个工具一个文件；注册集中在 `internal/tools/tool.go` 的 `All`。
- 测试与实现同目录，文件名使用 `_test.go`。
- Go 导入使用模块路径 `charon/internal/...`，没有 TypeScript 或 Go 的路径别名。

## 5) 证据

- `cmd/charon/main.go`
- `internal/catalog/catalog.go`
- `internal/tools/tool.go`
- `internal/tui/tui.go`
- `docs/codebase/` 中的七份架构文档
