# 外部集成

## 1) 集成清单

| 系统 | 类型 | 用途 | 认证方式 | 关键性 | 证据 |
|---|---|---|---|---|---|
| Codex | 本地 CLI 配置 | 写入 `~/.codex/config.toml` 的 charon provider | 配置内 bearer token；OAuth 文件保持不动 | 高 | `internal/tools/codex.go` |
| Claude Code | 本地 CLI 配置和 macOS Keychain | 写入 `settings.json`；读取 OAuth 账户状态 | 官方 endpoint 使用 API key；自定义 endpoint 使用 `ANTHROPIC_AUTH_TOKEN` | 高 | `internal/tools/claude.go`、`internal/secret/keychain_darwin.go` |
| OpenCode | 本地 JSONC/JSON 配置 | 写入 `provider.charon` | provider `apiKey` | 高 | `internal/tools/opencode.go` |
| Pi | 本地 JSON 配置 | 写入 `models.json` 的 `providers.charon` 和默认模型；安全迁移旧版生成的扩展 | provider `apiKey` | 高 | `internal/tools/pi.go` |
| Grok | 本地 TOML 配置 | 管理 `[model.charon-*]` 和默认模型 | model `api_key` | 高 | `internal/tools/grok.go` |
| OpenAI/Anthropic 兼容 endpoint | HTTP API | 获取 `/v1/models` | Bearer、`x-api-key` 和 Anthropic 版本头；按方言重试 | 中 | `internal/models/fetch.go`、`cmd/charon/commands.go` |
| GitHub Releases | HTTP 下载 | `charon update` 下载 `charon_<os>_<arch>.tar.gz` 和 `checksums.txt`，本地校验后原地替换二进制；不执行远程脚本 | HTTPS；SHA-256 校验 release 自带的 `checksums.txt` | 中 | `cmd/charon/commands.go`、`.goreleaser.yaml` |

## 2) 数据存储

| 存储 | 作用 | 访问层 | 主要风险 | 证据 |
|---|---|---|---|---|
| `~/.config/charon/providers.json` | endpoint 去重 | `internal/catalog` | 多文件更新没有跨文件事务 | `internal/catalog/catalog.go` |
| `credentials.json` | 保存 API key | `internal/catalog` | 本地明文，依赖文件权限保护 | `internal/catalog/catalog.go` |
| `models.json` | 模型 slug 和上下文窗口 | `internal/catalog` | 模型元数据可能过期 | `internal/catalog/windows.go`、`internal/models/builtin.go` |
| `bindings.json` | 工具到 credential/model 的引用 | `internal/catalog` | 引用完整性依赖写入时校验 | `internal/catalog/catalog.go` |
| `active.json` | 每个工具当前 binding | `internal/catalog` | 需要与 live config 保持一致 | `internal/catalog/project.go` |
| 目标 CLI 配置文件 | 实际运行配置 | `internal/tools` | 不同格式和多文件投影 | `internal/tools/*.go` |

## 3) 凭据处理

- Charon 将 key 保存在 `credentials.json`，并把 key 写入对应工具自己的配置或扩展；文件写入通常为 `0600`，目录为 `0700`。
- 官方 OAuth 登录属于各 CLI 自己的业务；适配器只读取必要的状态，不迁移或覆盖 OAuth 凭据。证据：`internal/tools/*.go`、`AGENTS.md`。
- `status` 和 JSON 输出遮罩密钥；没有密钥轮换、加密存储或导入导出机制。后两项当前为 `[TODO]`，不是已实现能力。

## 4) 可靠性和失败行为

- 模型 HTTP 请求设置 20 秒 context timeout；跨主机重定向会删除认证头；没有重试或熔断。证据：`internal/models/fetch.go`。
- 模型发现失败时，TUI 切换到手动输入模型 ID；CLI 直接返回错误。证据：`internal/tui/picker.go`、`cmd/charon/commands.go`。
- 目标配置写入通过临时文件、`Sync`、`Chmod`、`Rename`；Pi 的两份 JSON 配置仍然分两次写入。证据：`internal/artifact/artifact.go`、`internal/tools/pi.go`。

## 5) 可观测性

- 没有日志、指标或 tracing 集成。
- 用户可见信息来自 CLI 表格/JSON、TUI 状态栏和返回错误；HTTP 错误只包含状态和通用检查提示。证据：`cmd/charon/commands.go`、`internal/models/fetch.go`。

## 6) 证据

- `internal/tools/*.go`
- `internal/catalog/catalog.go`
- `internal/models/fetch.go`
- `internal/artifact/artifact.go`
- `cmd/charon/commands.go`
- `.goreleaser.yaml`（发布 `tar.gz` 与 `checksums.txt`）
- `scripts/install.sh`（仅首次安装；`charon update` 不再使用它）
