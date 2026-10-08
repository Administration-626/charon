# 编码约定

## 1) 命名规则

| 项目 | 规则 | 示例 | 证据 |
|---|---|---|---|
| 文件 | 小写领域名；测试追加 `_test.go` | `catalog.go`、`project_test.go` | `internal/` |
| 函数 | Go 驼峰；导出函数首字母大写 | `ProjectIfActive`、`storeBinding` | `internal/catalog/` |
| 类型 | 导出类型 PascalCase，内部结构小写 | `Catalog`、`Binding`、`piProviderConfig` | `internal/catalog/`、`internal/tools/pi.go` |
| 常量和环境键 | 常量用 PascalCase 或全大写语义名，外部键保留工具原名 | `FallbackContextWindow`、`ANTHROPIC_API_KEY` | `internal/models/`、`internal/tools/claude.go` |

## 2) 格式化和检查

- 格式化：`gofmt`；导入整理由 `goimports` formatter 规则覆盖。证据：`.golangci.yml`。
- Linter：`golangci-lint`，启用 `errcheck`、`govet`、`ineffassign`、`staticcheck`、`unused`、`misspell`、`revive`、`unconvert`、`bodyclose`。
- 命令：`make fmt`、`make test`、`make lint`；CI 还单独执行 `gofmt`、`go vet`、`go test -race`、`go build`。

## 3) 导入和模块约定

- 使用标准 Go import block，项目内部通过 `charon/internal/...` 导入。
- 没有路径别名或 barrel export；包边界由 `internal` 目录和 Go package 定义。
- `cmd` 保持薄；业务逻辑放在 `internal`，以便测试。证据：`AGENTS.md`。

## 4) 错误和日志约定

- 业务函数返回 `error`，跨层边界用 `%w` 包装；CLI 顶层把错误写到 stderr 并以非零状态退出。证据：`cmd/charon/main.go`、`internal/catalog/*.go`。
- 没有日志框架；状态和错误主要通过 CLI/TUI 输出返回。
- 原始密钥不得输出；status/JSON 使用 `secret.Mask`，工具适配器只把原始 key 传入写配置流程。证据：`cmd/charon/commands.go`、`internal/secret/mask.go`。
- 写配置必须经过原子替换，并设置目录 `0700`、凭据文件通常 `0600`。证据：`internal/artifact/artifact.go`、各 adapter。

## 5) 测试约定

- 测试与实现同目录，使用 Go 标准 `testing`，表格测试较多。
- 文件系统测试通过 `t.TempDir`、`HOME` 和 `XDG_CONFIG_HOME` 隔离；网络测试用 `httptest`。
- 竞态测试使用 `go test -race`；没有配置强制覆盖率阈值。证据：`Makefile`、`internal/*/*_test.go`、CI。

## 6) 证据

- `.golangci.yml`
- `Makefile`
- `cmd/charon/main.go`
- `internal/artifact/artifact.go`
- `internal/secret/mask.go`
- `AGENTS.md`
