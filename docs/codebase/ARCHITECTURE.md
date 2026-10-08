# 软件架构

## 1) 架构风格

- 主要风格：分层的本地 CLI，加上工具适配器和目录投影。
- 依据：入口层只解析命令并调用 `Catalog` 或 `tui`；`Catalog` 保存规范化数据并把 binding 投影到工具；每种工具把自己的配置格式封装在 `internal/tools/<tool>.go`；文件写入和模型发现又分别下沉到 `artifact`、`models`。
- 主要约束：
  1. Charon 修改真实用户凭据，必须保留用户未托管字段，并采用原子写入和文件权限控制。
  2. 绑定不是配置快照，而是 provider、credential、model 的引用；激活时重新渲染工具配置。
  3. 同一目录可能被多个 Charon 进程修改，Linux/macOS 上的完整 mutation 必须持有 advisory lock。

## 2) 系统流转

```text
CLI/TUI 入口
  -> 参数或表单校验
  -> Catalog 写入 provider/credential/model/binding
  -> Activate 读取 binding 并组装 AuthSpec
  -> Tool.ApplyAuth 合并到目标 CLI 配置
  -> active.json 记录当前 binding，目标 CLI 读取新 endpoint/key/model
```

一次 `charon switch <tool> <binding>` 的具体流转：

1. `cmd/charon/commands.go` 解析工具名和绑定名。
2. `Catalog.BindingByName` 找到 binding；`Catalog.Activate` 获取进程锁和文件锁。
3. `Catalog.project` 通过 binding 的 `CredentialID` 找到 key，通过 provider 找到 endpoint，通过模型 ID 找到默认模型和模型列表。
4. `Tool.ApplyAuth` 只更新自己拥有的配置键；Codex/OpenCode/Pi/Grok 还用 guard 检查用户 provider/model 没被改动。
5. 投影成功后才写 `active.json`，避免活动指针先于目标配置切换。

模型发现走另一条流：`cmd` 或 TUI 调用 `models.Fetch`，先用工具历史方言、失败后用另一方言，请求 `/v1/models`，成功的方言不存储。

## 3) 层和模块职责

| 模块 | 拥有的职责 | 不应拥有的职责 | 证据 |
|---|---|---|---|
| `cmd/charon` | 命令分发、参数解析、表格/JSON 输出 | 工具配置结构解析 | `cmd/charon/main.go`、`commands.go` |
| `internal/tui` | Bubble Tea 状态机、向导、异步模型请求展示 | 绑定完整性和工具字段合并 | `internal/tui/tui.go`、`wizard.go`、`picker.go` |
| `internal/catalog` | 四张本地表、active 指针、约束、锁、投影编排 | 具体工具文件格式 | `internal/catalog/catalog.go`、`project.go` |
| `internal/tools` | Tool 注册、Describe、ApplyAuth、配置格式适配 | 目录数据生命周期 | `internal/tools/tool.go`、`claude.go` 等 |
| `internal/models` | HTTP 模型发现和内置上下文窗口表 | 写用户配置 | `internal/models/fetch.go`、`builtin.go` |
| `internal/artifact` | 原子替换、sync、chmod、rename | 业务事务 | `internal/artifact/artifact.go` |
| `internal/secret` | 密钥遮罩和平台密钥链读取 | API 请求和目录建模 | `internal/secret/*.go` |

## 4) 重复使用的模式

| 模式 | 位置 | 作用 |
|---|---|---|
| 适配器注册表 | `internal/tools/tool.go` 的 `Tool`、`All`、`Find` | 用统一 `Describe`/`ApplyAuth` 接口支持五种异构 CLI |
| 规范化目录 | `internal/catalog/catalog.go` | provider、credential、model 去重，binding 只保存引用 |
| 投影/渲染 | `internal/catalog/project.go` | 把绑定转换成工具特有的 `AuthSpec` 并写入 live config |
| 受保护的 owned key | `internal/tools/providers.go`、各 adapter | 只允许修改 `charon` provider 或 `charon-*` 模型表 |
| 原子写入 | `internal/artifact/artifact.go` | 防止崩溃留下半个凭据文件 |
| 双层锁 | `internal/catalog/catalog.go`、`lock_unix.go` | 进程内 mutex 加跨进程 flock，覆盖完整读改写流程 |

## 5) 已知架构风险

- `Catalog` 以多个 JSON 文件保存一次逻辑 mutation；没有跨文件事务或恢复日志，进程在中间失败时可能留下部分更新。证据：`internal/catalog/catalog.go`、`internal/artifact/artifact.go`。
- `catalog.StoreBinding` 把 provider、credential、model、binding 写入拆成多次带锁调用，整个组合操作没有占用一把覆盖全流程的锁；并发进程可能在中间观察到半成品。证据：`internal/catalog/bind.go`。
- 一次工具投影可能写多个文件，例如 Pi 先写 `models.json` 再写 `settings.json`；两个文件之间没有事务。证据：`internal/tools/pi.go`。
- `internal/tui/tui.go`、`wizard.go` 和 `picker.go` 承担大量状态和视图逻辑；`wc -l` 和近期 `git log` 显示它们是较大且持续修改的文件，新增流程容易触及共享状态机。
- 非 Linux/macOS 平台的锁实现是 no-op，跨进程并发写入不受序列化保证。证据：`internal/catalog/lock_other.go`。
- `Tool.ApplyAuth` 的认证策略是工具和 endpoint 适配规则，不是目录中的数据字段；因此同一 endpoint 在不同工具上可能使用不同认证头。证据：`internal/tools/claude.go`、`codex.go`、`opencode.go`。

## 6) 证据

- `cmd/charon/main.go`
- `cmd/charon/commands.go`
- `internal/catalog/catalog.go`
- `internal/catalog/project.go`
- `internal/tools/tool.go`
- `internal/artifact/artifact.go`
- `internal/models/fetch.go`
- `AGENTS.md` 的架构和锁约束
