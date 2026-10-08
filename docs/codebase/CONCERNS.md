# 代码库风险

## 1) 主要风险

| 严重度 | 问题 | 证据 | 影响 | 建议 |
|---|---|---|---|---|
| 高 | API key 在 `credentials.json` 和各目标 CLI 配置中以可读形式存在 | `internal/catalog/catalog.go`、`internal/tools/*.go` | 本地用户目录泄露会同时暴露多份凭据 | 明确威胁模型；可考虑平台密钥链或只保留引用，但不能削弱当前 `0600` |
| 中 | `StoreBinding` 的跨表写入不是一个完整锁定事务 | `internal/catalog/bind.go` 调用多个分别加锁的 Catalog 方法 | 并发进程可能在 provider、credential、model、binding 写入之间插入操作，留下半成品或重复行 | 增加 catalog 内部的组合 mutation API，让整段读改写只获取一次锁 |
| 中 | 多文件 mutation 没有跨文件事务 | `internal/catalog/catalog.go`、`internal/tools/pi.go` | 中途崩溃可能出现 catalog 与 live config 不一致，或 Pi 两份配置不同步 | 写入临时状态/恢复日志，或把一次投影封装成可检测、可重试的事务步骤 |
| 中 | OpenCode 路径允许 `opencode.jsonc`，但读取器使用标准 JSON 解析器 | `internal/tools/opencode.go`、`internal/tools/edit.go` | 带注释的合法 JSONC 文件无法被 Charon 读取或合并 | 使用支持 JSONC 注释的解析器，或明确拒绝带注释配置并在检测时给出可定位错误 |
| 中 | Windows/其他平台的跨进程锁是 no-op | `internal/catalog/lock_other.go` | 同时运行两个 Charon 可能交错写入 | 若支持并发，增加平台锁实现；否则在支持矩阵中明确限制 |
| 中 | TUI 状态机集中在大文件 | `internal/tui/tui.go`、`wizard.go`、`views.go`；`wc -l` 显示它们是源码中的大文件，`git log` 显示近期持续修改 | 新增流程可能引入视图状态回归 | 抽取纯状态转换和流程服务，保留 Bubble Tea 外壳 |

## 2) 技术债

| 债务 | 成因 | 位置 | 忽略风险 | 建议 |
|---|---|---|---|---|
| 认证方言不是目录字段 | 同一 endpoint 可被多个 CLI 以不同协议消费 | `internal/catalog/catalog.go`、`internal/tools/*.go` | 用户无法在绑定层表达某个网关要求 `x-api-key` 还是 Bearer | 只有出现真实兼容性需求时再增加显式 auth scheme；不要按 URL 猜测 |
| 手写配置格式适配分散 | 五种 CLI 的配置结构确实不同 | `internal/tools/codex.go` 等 | 格式变更需要逐个 adapter 更新 | 为每个 adapter 保持独立测试和 owned-key guard，避免过早抽象 |
| 内置上下文窗口是静态规则 | API 不一定返回窗口，需 WorkBuddy/官方规则兜底 | `internal/models/builtin.go`、`docs/workbuddy-context-sync.md` | 新模型可能被错误估计，影响压缩行为 | 定期同步规则；继续保留手动覆盖优先级 |

## 3) 安全风险

| 风险 | OWASP 类别 | 证据 | 当前缓解 | 缺口 |
|---|---|---|---|---|
| 远程更新下载 | A08 软件和数据完整性故障 | `cmd/charon/commands.go` 的 `cmdUpdate` | HTTPS、下载后校验 release 自带的 `checksums.txt`、tar 条目大小上限、`artifact.AtomicWrite` 原地替换二进制；不执行任何远程脚本 | checksums.txt 与二进制同源同渠道，未做发布方签名；升级到签名校验需要密钥管理，当前不做 |
| API key 落盘 | A02 加密失败 | `internal/catalog/catalog.go` | `0600` 文件、`0700` 目录、输出遮罩 | 没有静态加密或密钥链存储 |
| 模型请求携带认证头 | A07 身份认证失败 | `internal/models/fetch.go` | 跨主机重定向时删除认证头、20 秒超时 | 同一请求同时设置 Bearer 与 `x-api-key`，某些非标准网关可能记录或误解其中一个头 |

## 4) 性能和扩展性

| 问题 | 证据 | 当前表现 | 扩展风险 | 建议 |
|---|---|---|---|---|
| Catalog 每次操作读取并重写整张 JSON 表 | `internal/catalog/catalog.go` | 数据量小时简单可靠 | 绑定和模型数量很大时 I/O 与锁持有时间线性增长 | 当前规模无需数据库；若规模增长再引入单文件事务或嵌入式存储 |
| 模型发现最多串行请求两种方言 | `cmd/charon/commands.go`、`internal/tui/picker.go` | 第一个方言失败后才尝试第二个 | 慢 endpoint 会让交互等待两个 timeout | 保持当前顺序以减少请求；必要时再做并发请求并取消败者 |

## 5) 脆弱或高变更区域

| 区域 | 脆弱原因 | 变更信号 | 安全修改方式 |
|---|---|---|---|
| `internal/tui/tui.go`、`wizard.go`、`views.go` | 视图、焦点、异步消息共享一个状态模型 | `git log` 和 `wc -l` 显示近期修改集中且文件较大 | 先抽取纯函数并增加状态表测试，再改事件分支 |
| `internal/tools/codex.go`、`claude.go`、`opencode.go` | 外部 CLI 配置格式和认证语义经常变化 | 近期提交和测试集中 | 只改 owned key，先补 sandbox adapter 测试 |
| `cmd/charon/commands.go` | 子命令、输出、更新逻辑集中 | 770 行 | 保持命令薄；把新业务放回 `catalog`/`tools` |

## 6) `[ASK USER]` 问题

1. `[ASK USER]` Windows 和其他非 Linux/macOS 平台是否属于需要保证跨进程并发安全的正式支持范围？当前实现明确使用 no-op 锁。（已答复：保留 Unix advisory flock，其他平台标为“无跨进程并发保证”，见 `internal/catalog/lock_other.go`。）
2. ~~`charon update` 是否必须保留“下载并执行远程安装脚本”的升级体验？~~ 已决策（2026-09-28）：不保留。`cmdUpdate` 已改为下载 release 的 `charon_<os>_<arch>.tar.gz` 与 `checksums.txt`，本地校验 SHA-256 后解包并用 `artifact.AtomicWrite` 原地替换二进制，全程不执行远程脚本。初装的 `scripts/install.sh` 仍保留（见 `docs/codebase/INTEGRATIONS.md`）。

## 7) 证据

- `wc -l cmd/charon/*.go internal/catalog/*.go internal/tools/*.go internal/tui/*.go`
- `git log --oneline -20 -- internal/tui internal/tools internal/catalog`
- `cmd/charon/commands.go`
- `internal/catalog/catalog.go`
- `internal/catalog/lock_other.go`
- `internal/tools/*.go`
- `internal/models/fetch.go`
- `internal/tui/tui.go`
