# 测试体系

## 行为规格与测试的关联

D5 的独立 key 停供设计已被用户否决并撤回，原有 p 菜单与 key 政策测试已删除；下列测试只保留仍在使用的行为证据。见[规格最新产品结论](../spec.md)。

[行为规格](../spec.md)按 switch、dry-run、off、绑定操作、Pi 聚合／隐藏和模型设置列出前置条件、结果、反例及现有测试。测试名称是证据入口，不代表该规格已被穷尽验证。

- G-SECRET 的 edit help 泄露已修复：`TestRunEditHelpDoesNotLeakSecrets` 覆盖 stdout/stderr、返回错误及参数解析拒绝；`TestRunEditKeyFlagSemantics` 覆盖省略／更换／显式空 key。旧代码四个 help/usage 案例失败，修复后通过；两项独立变异可被新测试发现。此结论只覆盖上述场景，不代表所有安全规则已穷尽。
- COPY-02 已统一：TUI 主验收 `TestDuplicateHiddenBindingViaKeys` 驱动 c/x、目的选择、逐字符命名与隐藏行渲染，检查副本、原方案、当前合法模型和配置字节；辅助入口 `TestRunCopyPreservesHiddenOnlyForMultiEndpoint` 验证继承与非多 endpoint 目标例外。CLI 旧实现红、新实现绿；TUI 原行为已绿。三项独立变异可报警。
- G-TUI 由绑定列表的按键／渲染测试覆盖，不把 CLI 测试当作 TUI 验收；按 endpoint 分组的新隐藏行为尚未实现，其验收案例见接棒文档。
- 一次实际门禁、覆盖率与验证边界见[工程走查记录](../engineering-walkthrough.md)；记录是具体工作树结果，不是对当前所有状态的保证。

## TUI 怎么测试：三层证据

1. **按键事件与状态转换**：构造真实 `model`，调用生产 `Update(tea.KeyMsg)`，模拟 Enter／Esc、h、c／x、箭头与逐字符输入，再检查实际 view、光标与文件。关键操作不是直接调用 handler 冒充按键；纯文案转换函数另做单元测试。同步写入可以在按键流程中检查，不需要启动终端。
2. **生产渲染与副作用**：调用同一个 `View()`，检查列表、隐藏标记、详情与假凭据不泄露；用 Lip Gloss 量宽高验证常见终端尺寸。配置文件在临时 HOME 真实写入／读取并比较字节；这不是模拟文件对象，也不证明所有格式／权限／崩溃条件。
3. **实际程序的 PTY smoke**：可选地在隔离 HOME 启动编译后的 Charon，用真实终端字节覆盖输入解码与 Bubble Tea 主循环；它不是启动真实 Pi、真人视觉验收或网关验证。

错误不能只检查 `m.status` 的长字符串：Bubble Tea 会截断超过终端宽度的行，完整原因应放在可换行区域。永久回归位于 `internal/tui/hidden_test.go` 与 `internal/tui/tui_test.go`；按 endpoint 分组的新隐藏语义另有待实现测试。颜色、字体、所有终端尺寸、macOS 终端和运行中 Pi 不在这些测试的证明范围内。

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
- `internal/tools/tools_test.go` 覆盖各 CLI 的描述和写入；`internal/catalog/*_test.go` 覆盖目录约束、迁移、锁、stash 和投影；`internal/edit/*_test.go` 覆盖 JSONC/TOML 保格式编辑；`internal/models/*_test.go` 覆盖 URL、HTTP 方言、内置窗口和思考预设；`internal/tui/*_test.go` 覆盖纯逻辑和渲染。
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
