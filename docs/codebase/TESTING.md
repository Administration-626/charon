# 测试体系

## 1) 测试工具和命令

- 测试框架：Go 标准库 `testing`。
- 断言和模拟：标准 `testing`，网络边界使用 `net/http/httptest`；没有额外断言框架。
- 命令：

```sh
go vet ./...
go test -race ./...
make test
make cover
```

## 2) 测试布局

- 测试与源码同目录，使用 `*_test.go`。
- `internal/tools/tools_test.go` 覆盖各 CLI 的描述和写入；`internal/catalog/*_test.go` 覆盖目录约束、迁移、锁和投影；`internal/models/*_test.go` 覆盖 URL、HTTP 方言和内置窗口；`internal/tui/*_test.go` 覆盖纯逻辑和渲染。
- 没有全局测试 setup；测试辅助函数在各包中创建临时 HOME/XDG 目录。证据：`internal/tools/tools_test.go`、`internal/catalog/catalog_test.go`。

## 3) 测试范围矩阵

| 范围 | 是否覆盖 | 典型目标 | 说明 |
|---|---|---|---|
| 单元测试 | 是 | 名称校验、模型规则、密钥遮罩、配置编辑 | 大多数包使用纯函数或临时文件 |
| 集成测试 | 是 | Catalog 与文件表、Tool adapter 与真实格式、模型 HTTP | 使用临时目录和 `httptest`，不访问真实用户配置 |
| 端到端/渲染 | 部分 | TUI render、CLI `run` 分发 | `internal/tui/e2e_render_test.go` 和 `cmd/charon/main_test.go`；没有真实终端全流程测试 |

## 4) 模拟和隔离

- 文件隔离：`t.Setenv("HOME", t.TempDir())` 和 `t.Setenv("XDG_CONFIG_HOME", t.TempDir())`。
- 网络隔离：`httptest.NewServer` 返回模型列表和错误状态。
- 并发隔离：`lock_unix_test.go` 使用两个 Catalog 实例验证跨进程锁等待；CI 强制 `-race`。
- 真实平台 Keychain 不在测试中读写，macOS 行为使用替代实现或跳过。证据：`AGENTS.md`、`internal/catalog/lock_unix_test.go`。

## 5) 覆盖率和质量信号

- 覆盖率工具：`go test -coverprofile=coverage.txt` 后 `go tool cover -func`；仓库没有阈值配置。
- CI 信号：gofmt 检查、vet、race 测试、build、golangci-lint。证据：`.github/workflows/ci.yml`。
- 主要缺口：没有真实第三方 endpoint 集成测试；`charon update` 的网络下载路径没有端到端测试（`checksumFor`、`extractReleaseBinary` 有单元测试，见 `cmd/charon/main_test.go`）；Windows 的 no-op 锁行为没有跨进程测试。证据：`internal/catalog/lock_other.go`。

## 6) 证据

- `Makefile`
- `.github/workflows/ci.yml`
- `internal/catalog/*_test.go`
- `internal/tools/*_test.go`
- `internal/models/*_test.go`
- `internal/tui/*_test.go`
