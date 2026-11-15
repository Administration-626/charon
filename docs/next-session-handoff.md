# 新 Session 接棒：把隐藏合并到 h（按 endpoint）

更新：2026-11-11。工作仓库 **`/home/tan/charon`**，不是学习笔记仓库。分支 `main`，HEAD `305beab`。

## 1. 用户最新决定：已经确认，下一步直接实施

用户通常使用 **TUI**。他试用后拒绝了“h 隐藏某条方案 + p 独立 Disable/Enable key + 恢复后还要 apply”的双层设计，宁可不实现也不接受它。随后用户提出合并到 h，并回复 **“OK，继续”**，确认以下方案。

- **只保留 h 的一个可逆隐藏状态**，取消本轮试验新增的 p key 菜单、独立 key 禁用政策及相关新增 CLI 命令。不能只是藏菜单而后台继续挡着恢复。
- **按 endpoint 地址归组，作用范围仅 Pi**。同一地址下不同名字、不同 key 的 Pi 保存方案一起隐藏／显示。
- **不同地址，即使用同一把 key，也不连带隐藏**。这是最新决定，覆盖早先“按 key 字面值跨 endpoint 禁用”的探索，不再要求用户重答这道题。
- h 隐藏：马上从 Charon 管理的 Pi 配置中移除该 endpoint 的托管 provider；保留保存方案；相关行统一隐藏；其他地址、用户非托管配置及当前默认 provider/model 保持。
- 再按 h 显示：马上重新注册该 endpoint，不切换当前方案、不重置模型，也不要求用户另按 r / 执行 apply。
- **正在使用的 endpoint 不能隐藏**：既检查 Charon 最后确认的 active endpoint，也检查 Pi 磁盘默认会不会被删。先拒绝，提示在 TUI 切到其他 endpoint；若另一个方案已经 active，需 r 明确重新应用，不能误导用户重复 no-op Enter。不自动选替代网关。
- **d 仍只删除选中的保存方案**，不扩大成删除整组，避免无授权删其他记录。同地址其他记录还存在时，不承诺删一条就删掉整个 endpoint。
- endpoint 的 key/model 代表选择沿用已有聚合规则，不另造 UI 优先级系统。恢复其他 endpoint 不能借全量 Reapply 重设当前默认或重建无关用户修改。
- 地址等价复用现有 Provider 身份规则（去首尾空白、末尾 `/`）；**不要拿显示比较用的 `sameEndpoint`（会忽略 `/v1`）来合并不同请求路径**。

尚未实现这版！上一 session 最后只确认方案、更新交接文档，没有撤回旧代码或完成新的 h。

## 2. 用户怎么希望我们推进

- 不要先抛“回滚/只读/catalog/参数共享”术语选择题。先说 **原来状态 → 用户按什么 → 哪些内容改变 → 用户看到什么**。
- 用户已授权助手决定这个方案；别重新问已确认的 h/p 设计。确有新边界，用一条具体案例说明。
- 一步一步做：先把旧试验精准撤回并保留无关工作，再补 h 按 endpoint 的失败案例，最小实现、回归、用户 TUI 试用。
- 可使用 **herdr 的 `cbtask`**。CodeBuddy 不是 Pi 子代理，不能用 spawn 的 kind 启动。长报告让它写临时文件，核对报告确属当前任务。
- **同型号 Pi 最多 2 个并发，保守按含主代理最多两个 Pi 控制。** 不用为了简单任务凑代理数量。
- 不新增依赖、事务框架、产品菜单或额外 CLI。优先现有 Catalog、Pi provider builder、格式保留 editor 与原子写入。
- 不操作真实 HOME/XDG/Keychain，不读取真实 secret，不安装／替换现有二进制，不自动提交。

## 3. 工作树与必须保留的内容

工作树混有大量先前的 header、JSON/TOML 格式保留 editor、差分测试等改动，**都未提交**。

禁止 `git reset --hard`、整文件 restore 到 HEAD、`git clean -fd` 或 `git add .`。

**必须保留：**

- 原有 header 存储／投影／继承、Pi API 保存、格式保留编辑器、差分/fuzz/off 测试及其他用户原有改动。
- 独立安全修复：`cmdEdit --help`/解析错误不打印保存的 key。当前 `--key` 可打印默认为空，解析成功后通过 `fs.Visit` 判断省略才继承旧 key；显式空值仍拒绝。`TestRunEditHelpDoesNotLeakSecrets`、`TestRunEditKeyFlagSemantics` 保留。
- 复制隐藏状态的修复与验收：辅助 CLI 与 TUI 的副本继承隐藏，不激活／不重置模型。现有 `TestRunCopyPreservesHiddenOnlyForMultiEndpoint`、`TestDuplicateHiddenBindingViaKeys` 需在新组语义下继续成立或精准更新，不简单删掉。
- “TUI 主入口”、清楚讲场景、自动测试不等于产品验收等工程约定。

**需精准撤掉的试验：**

- `Credential.PiDisabled`、`AuthSpec.PiDisabledKeys`、同值 key 政策继承、投影／active／stash／headers 中的这层禁用屏障。
- 本轮新增 `disable-key` / `enable-key` CLI 命令、dispatch、completion 与专用测试；不是删除所有 CLI。
- 新 p 菜单、确认／失败结果页、keyDisabled/keyUnknown 标记、专用 UI 状态和 guard。
- 仅服务于该试验的 `SetPiKeyDisabled`、可选 expectedCredentialID guard、相关独立 helper／测试。
- README/AGENTS/spec/codebase 文档中把这版称为有效产品的内容。测试记录可保留为失败产品设计的历史证据，但不能再指导交付。

别按文件名机械删除：被拒方案跨多个原有文件，部分 pi/header helper 可能还可复用。需要读所有调用者，逐块移除／复用。

## 4. 关键源码入口

- `internal/tui/update.go`：h / p / r / Enter 分发与帮助／输入上下文。
- `internal/tui/actions.go`：`onHideKey` 现在调用 SetBindingHidden 后 **Reapply(active)**，这会重置 Pi 手工选择的默认模型；新的 h 不能照搬。`duplicateBinding` / `hideDuplicate`。
- `internal/tui/model.go`、`load.go`、`views.go`：状态、隐藏行、帮助与渲染。
- `internal/catalog/catalog.go`：`Binding.Hidden`、`SetBindingHidden` 目前按单个 binding，需改成一致的 endpoint 组语义；Add/Update/Activate/复制等也要避免组内标志重新不一致。
- `internal/catalog/project.go`：endpoint 聚合、去重和 active；同 binding switch 是 no-op，Reapply 会重设默认。
- `internal/tools/pi.go`：provider/value/model builders、ApplyAuth；目前全量 ApplyAuth 会写 models 与 settings，新 h 要用只变目标 endpoint 的路径，保留无关 provider 和默认选择，不能全文件 marshal/unmarshal。
- `internal/catalog/headers.go` 与各 headers 测试：保留。
- 旧实验：`cmd/charon/key_commands.go`、`internal/catalog/{pi_keys.go,key_policy_test.go,pi_keys_test.go}`、`internal/tools/{pi_keys.go,pi_keys_test.go}`、`internal/tui/{pi_keys.go,pi_keys_test.go}`，及它们在原有文件中的调用。

新组状态建议先复用现有数据结构与锁，避免为一个可逆动作新造“策略管理器”。特别检查：新建／复制同地址方案、激活隐藏地址、编辑改 endpoint，以及旧数据里同地址一部分隐藏的处理；旧数据不要悄悄隐藏正在使用的 endpoint。仅有真实歧义才用具体案例讨论，不自行迁移真实配置。

## 5. 第一组验收案例（新 session 先写测试）

设 work=A，地址 `https://a.example/v1`，保存默认 small、注册列表 small/big；Pi 磁盘已选 big。
backup1、backup2 都是 `https://b.example/v1`，可以使用不同 key。
other=C 是另一地址，可与 backup1 共用同一把 key。

1. 在 TUI backup1 按 h：backup1/2 都隐藏，B 的托管 provider 立即消失；C 仍在；A 的默认 big、其配置与非托管内容不变。
2. 再按 h：backup1/2 都显示，B 立即出现；不切到 B、不改 big，无需额外 apply。
3. 选 A 或它的同地址别名按 h：先拒绝，政策／文件都不变。
4. Pi 磁盘默认实际在 B，但 Charon active 身份仍为 A：不能删 B 后留下悬空默认，也不能暗中换回 A；先拒绝并给正确的 TUI 恢复步骤。
5. 不同 endpoint 共享 key：隐藏 B 不隐藏 C；同 endpoint 不同 key：一起隐藏 B。这个对照很重要，防止又按旧 key 政策实现。
6. 复制隐藏组的方案／再打开数据：不能意外让 B 显示；保存字段/认证标头保留。
7. 格式/权限/I/O 失败明确报错。用户接受修好原因后重试，不要求自动全量回滚；但不能报虚假成功或丢失原配置。失败的具体中间态要实测并写清。

测试用 m.Update(KeyMsg) 走 TUI 输入，View 检查文本／标记，真实临时配置比较字节。故意破坏按 endpoint 归组、保留模型和恢复即时生效等规则，确认测试报警。

## 6. 回退与验证证据在哪里

**当前交接前的完整仓库源文件保护快照：**
`/tmp/charon-session-handoff-_gbokj4d/`
- `hashes.json`：所有仓库文件摘要（排除 .git）。
- `files/`：Go/Markdown/配置等文本源文件拷贝，供精准比较。
- `tracked.patch`：相对 HEAD 的 tracked diff；不含 untracked 内容，不能单独当完整备份。
- `status.txt`、`untracked.txt`。
这份快照在本轮交接文档写入前制作，交接文件/最新文档会比它新。

**试验开始前的既有工作：**
`/tmp/charon-before-pi-disable.patch`、`/tmp/charon-before-pi-disable-status`。
可在临时源码副本重建并比对，不能直接套回原工作树覆盖后来的独立修复。

**阶段证据（历史实现，不代表新设计通过）：**
- `/tmp/charon-help-fix-di397ba1/`：帮助泄露安全修复；应保留。
- `/tmp/charon-tui-copy-6ut6dpsl/`：隐藏复制修复及 c/x 事件测试；应保留语义。
- `/tmp/charon-tui-key-_zn2u3tt/`：已被拒的 p/key 方案，包含 before-hashes、源码拷贝、checks、7个overlay变异、PTY脚本与原始记录。其旧 try-tui/binary **不能当成新 h 方案试用版本**。
- `docs/spec.md` 后半 KEY/p 章节、README、AGENTS 的部分说明仍在描述待撤代码，最新产品结论和本文件优先；实现后要清理这些旧条款。

## 7. 已有门禁结果和限制

上一实现版本：fmt、vet、`go test -race -count=1 ./...`、Linux 构建、darwin/arm64 交叉编译、7项变异、80×24 PTY 通过。**回退/修改后必须重新跑，不沿用旧绿灯。**

lint 两处原有错误未改：
- `cmd/charon/headers_test.go:110`：局部变量 copy 遮蔽 builtin（revive）。
- `internal/tools/validate.go:74`：De Morgan 提示（staticcheck）。

gopls 默认不可用，LSP未运行；要启用需安装或改 pi-lsp.json 的可用命令，不自动安装。govulncheck未安装/未跑，macOS仅交叉编译，没有 macOS 执行证据。

安全测试环境保留缓存：
`GOCACHE=/home/tan/.cache/go-build`、`GOMODCACHE=/home/tan/go/pkg/mod`、`GOPATH=/home/tan/go`、`GOPROXY=off`、`GOTOOLCHAIN=local`；HOME/XDG_CONFIG_HOME 与 PATH 临时隔离防真实 Keychain。

最小路线：**精准撤旧试验 → 新 h 失败测试 → 最小实现 → 全仓门禁与变异 → 假配置 TUI 试用 → 再决定提交/安装。** 当前 session 到此交接，不再继续编码。
