# 技术栈

## 1) 运行时概览

| 项目 | 当前实现 | 证据 |
|---|---|---|
| 主要语言 | Go | `go.mod` |
| 运行时版本 | Go 1.24.2 | `go.mod` 的 `go` 字段；CI 使用 Go 1.24 |
| 包管理 | Go Modules | `go.mod`、`go.sum` |
| 构建系统 | Makefile + `go build`；发布由 GoReleaser 交叉编译 | `Makefile`、`.goreleaser.yaml` |

## 2) 生产依赖

| 依赖 | 作用 | 证据 |
|---|---|---|
| `bubbletea`、`bubbles`、`lipgloss` | 终端交互界面、列表、输入框和样式 | `go.mod`、`internal/tui/` |
| `pelletier/go-toml/v2` | 读写 Codex、Grok 的 TOML 配置 | `go.mod`、`internal/tools/edit.go` |
| `gopkg.in/yaml.v3` | 保留节点结构读写 Oh My Pi YAML | `go.mod`、`internal/tools/edit.go` |
| `sahilm/fuzzy` | 模型列表模糊搜索 | `go.mod`、`internal/tui/picker.go` |
| `golang.org/x/sys/unix` | Linux/macOS 的 advisory flock | `go.mod`、`internal/catalog/lock_unix.go` |

## 3) 开发工具链

| 工具 | 用途 | 证据 |
|---|---|---|
| `gofmt`、`goimports` | 格式化和导入整理 | `.golangci.yml`、`Makefile` |
| `go vet`、`go test -race` | 静态检查和竞态测试 | `Makefile`、`.github/workflows/ci.yml` |
| `golangci-lint` | 质量规则检查 | `.golangci.yml`、CI |
| GoReleaser | Linux/macOS、amd64/arm64 发布 | `.goreleaser.yaml` |

## 4) 常用命令

```sh
make build
make test
make lint
make fmt
make run
```

## 5) 环境与配置

- Charon 自身的数据目录是 `$XDG_CONFIG_HOME/charon`；未设置时回退到 `~/.config/charon`。证据：`internal/catalog/catalog.go`。
- 工具配置读取用户 `HOME` 下的固定目录，如 `~/.codex`、`~/.claude`、`~/.config/opencode`、`~/.pi/agent`、`~/.omp/agent`、`~/.grok`。证据：`internal/tools/*.go`。
- `HOME` 没有业务配置含义之外的显式环境变量要求；`XDG_CONFIG_HOME` 是 Charon 存储位置覆盖项。密钥链只在 macOS 实现。证据：`internal/catalog/catalog.go`、`internal/secret/keychain_*.go`。
- 不提供容器或服务进程；运行约束是本地文件权限、用户目录可写以及对应 CLI 的配置格式可解析。

## 6) 证据

- `go.mod`
- `Makefile`
- `.github/workflows/ci.yml`
- `.goreleaser.yaml`
- `internal/catalog/catalog.go`
