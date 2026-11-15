# Charon 行为规格与验收基线

状态：**草案 v1，待逐项确认，不代表全部验收完成**。更新日期：2026-11-15；依据当前未提交工作树，Git HEAD `cf947c6`。

**最新产品结论：按 endpoint 合并到 h，已确认并实现。** 用户先否决“h 隐藏 + p 停供／恢复 key”的双层交互，随后提出合并到 h，并确认继续。当前采用：同地址的所有 Pi 方案一起隐藏／显示，立即更新托管 provider，保留当前默认模型；不同地址即使用同 key，也不连带隐藏。正在使用的地址先拒绝隐藏，d 仍只删选中保存方案。试验用的 p 菜单、独立 key 政策、`disable-key`／`enable-key` 命令及相关字段与屏障已经精准删除。历史方案与回退边界见[接棒记录](next-session-handoff.md)。

本文件回答“Charon 应该做什么、什么结果算正确”。[README](../README.zh-CN.md)说明操作方式，[AGENTS.md](../AGENTS.md)规定开发安全与工程约束，[代码库文档](codebase/ARCHITECTURE.md)解释结构，[走查记录](engineering-walkthrough.md)保存一轮验证结果。新增规格先放在这里，不为每个命令另造一套配置或测试框架。

## 1. 规格的来源与使用方式

- **最新用户确认**：以 endpoint 地址为单位合并到 h，同地址 Pi 方案一起隐藏／显示，不自动切换或重置模型；不同地址即使同 key 也独立。早先按 key 字面值跨 endpoint 禁用的探索已放弃，不把它继续当产品合同。
- **主要使用方式**：用户通常使用 TUI；常用动作和状态必须能在界面中完成与理解，不能要求用户改用 CLI 才算验收。CLI 保留为辅助／自动化入口，不默认新增或删除。
- **用户授权本轮决策**：复制隐藏方案时，副本仍隐藏；不激活、不写 Pi 配置、不重置当前默认模型。以 TUI 按键流程验收，备用入口遵守相同规则。
- **本轮跨工具复制审查**：用户要求评估易出问题的设计并记录到 spec；本轮只补事实、设计判断与验收建议，不视为已批准修改复制流程、默认协议或数据结构。详见 7.1、D6、Q5。
- **项目既有约定**：配置所有权、凭据保护、switch / reapply / off 等已公开行为，来自 README、AGENTS 和现有测试。
- **当前行为**：由源码或隔离实验观察到，但尚无明确产品决定时，不能自动升级为“必须这样”。
- **`[ASK USER]`**：需要用户选择；在确认之前保持现有实现，不让 AI 擅自改变产品行为。
- **`[TODO]`**：证据或验收测试不足，不代表必须增加一个新功能。

每次修改先选一个下文的规则 ID，写清前置条件、操作、成功结果、失败结果和必须保持的内容；再把正例与反例放进现有 Go 测试。测试通过只说明相应案例成立，不等于整个系统的数学证明。

向用户询问前，必须先交代**原来是什么状态 → 用户做了什么 → 具体哪些文件或选项会变 → 用户会看到什么区别**。不能只问“回滚还是重试”“是否保留模型”，让用户猜测操作对象；源码能确定的技术事实由开发者先查清，不能包装成产品选择抛给用户。

## 2. 项目目标、状态与保证范围

Charon 管理 Codex、Claude Code、OpenCode、Pi、Grok 的 endpoint、凭据和模型选择。绑定是保存的配置选择，**不是 Agent 进程，也不是工具配置文件的完整快照**。

| 对象 | 含义 |
|---|---|
| Provider | 一个 endpoint；可被多个工具使用 |
| Credential | provider 与字面 key 的记录；不同 provider 可以保存相同的 key |
| Model | provider 下的模型元数据；同一 provider / slug 的记录可以被多条绑定共用 |
| Binding | 工具、名字、CredentialID、默认模型和模型列表；Pi 还可保存协议、标头、隐藏状态 |
| Last confirmed binding | `active.json` 保存的绑定身份，表示上次确认的选择；不是实时工具状态或不可变配置版本 |
| On-disk configuration | 工具当前磁盘配置；可能被用户修改或在部分写入失败后与 catalog 不同 |
| Stash | 首次写入前的托管字段原值，供 off 恢复；不是整份用户配置备份 |

范围：只管理各 adapter 明确声明的字段、文件与命名空间。不管理运行中会话内存，不提供服务端 key 撤销，不迁移 OAuth 登录，不提供通用配置导入、导出或同步。

证据：`internal/catalog/catalog.go`、`internal/catalog/project.go`、`internal/catalog/stash.go`、`internal/tools/tool.go`、`AGENTS.md`。

## 3. 全局不变式

| ID | 必须满足的规则 | 验收与边界 |
|---|---|---|
| G-OWN | 只修改 adapter 声明的托管内容；用户的其他 provider、字段、注释和格式必须保留 | edit corpus、差分测试与 off 格式矩阵；不宣称已穷尽所有格式 |
| G-SECRET | 正常输出、错误、帮助信息、JSON 与预览都不能输出原始 key 或凭据标头值 | D1 已修复，帮助与参数错误回归通过；这不代表任意输出／任意输入里的所有凭据泄露都已排除 |
| G-FILE | 写入通过同目录临时文件和 rename 替换；凭据文件 0600，数据目录 0700 | 正常替换与权限测试；单文件原子性不等于多文件事务或断电恢复证明 |
| G-REF | binding 的模型必须属于其 credential 的 provider；默认模型必须在其注册模型列表中；绑定名在同工具内唯一，不能是含空白／控制字符的名称 | `TestBindingSameProviderConstraint`、`TestNameValidation`、CLI 参数测试 |
| G-LOCK | mutating Catalog 操作遵守既有 mutex 与 Unix flock；读改写与相关投影不能随意拆开 | Linux/macOS 约束；其他平台没有跨进程并发保证；`StoreBinding` 跨表复合事务仍是已知限制 |
| G-FAIL | 必须返回真实错误，不把部分成功显示为完全成功；错误结果不自动等于“所有文件未变” | 区分输入拒绝、单文件替换失败、跨文件部分成功；见 Q1 |
| G-TUI | 主要用户操作必须在 TUI 可用、状态可见；CLI 测试通过不能替代 TUI 操作验收 | 绑定列表、隐藏与复制以按键／渲染测试验收；独立 key 停供试验已被否决并撤回，见 D5；分组 h 同时由 catalog 配置断言与 TUI 按键测试覆盖 |
| G-SANDBOX | 开发测试隔离 HOME / XDG_CONFIG_HOME，用假 key 与 httptest；不能触碰真实配置或 Keychain | 现有测试辅助函数与 AGENTS；本次实验全部在临时目录中执行 |

证据：`internal/artifact/`、`internal/edit/`、`internal/catalog/catalog.go`、`internal/catalog/lock_unix.go`、`internal/catalog/bind.go`、`internal/secret/`、`internal/preview/headers_test.go`。

## 4. switch 与 reapply

**目的**：明确区分“切换选择”和“把已保存配置重新应用”，不要意外覆盖工具内的模型选择。

- **SW-01 前置条件**：工具和绑定存在、引用可解析；实际渲染写入时，目标格式必须可被相应 adapter 接受。同绑定 no-op 不承诺读取或修复目标配置。
- **SW-02 切换成功**：目标工具配置包含绑定指定的 endpoint、key、默认模型及注册列表；配置写入成功后才记录 active 身份。
- **SW-03 同绑定 switch**：保持 no-op，不因再次选择相同绑定而重置用户手工选择的模型；也不承诺修复配置漂移。
- **SW-04 reapply**：即使绑定已经是 active，也显式重写其默认模型和注册列表。
- **SW-05 失败**：未知绑定被拒绝；投影失败不提前把 active 改成目标。若工具配置已更新而 active 写入失败，错误应说明配置可能已变，可通过 status 和显式 reapply 恢复。不能承诺跨文件自动回滚。
- **SW-KEEP**：不修改未托管内容、不把工具内的临时模型选择写回 catalog。

验收场景：**Given** 已应用 A，用户在工具内选择另一模型；**When** `switch A`；**Then** 该模型保留。**When** `reapply A`；**Then** 恢复 A 保存的默认模型。

测试：`TestActivateSameBindingPreservesLiveModel`、`TestStatusAndReapplyDistinguishSavedBindingFromLiveModel`、`TestActivateProjectionFailureKeepsPreviousActive`、`TestActivateActiveWriteFailureReportsChangedToolConfig`。

证据：`internal/catalog/project.go`、`cmd/charon/commands.go`、`internal/catalog/project_test.go`、`cmd/charon/main_test.go`。

## 5. dry-run

**目的**：让用户看见将要发生的配置变化，而不实际应用。

- **DRY-01**：在临时 HOME 的工具配置副本上渲染，输出 unified diff；真实目标工具配置的字节和存在状态保持不变。
- **DRY-02**：diff 保留可理解的字段名、注释上下文与文件位置，但遮罩 key 和标头值。
- **DRY-03**：预览与真实 switch 使用同一投影规则；实际 apply 会拒绝的配置，预览不能生成一个可用的绕过规格。
- **DRY-04**：同绑定 switch 的预览应说明真正 switch 是 no-op，展示的是 reapply 会产生的效果。
- **DRY-BOUNDARY**：本规格约束目标工具配置。CLI 启动时 `Catalog.Open` 可能创建 catalog、执行迁移或补齐元数据；不能把它误述成整个进程绝不写任何文件。更强只读要求见 Q2。

验收场景：**Given** models.json 有 BOM、旧 key 和凭据标头；**When** dry-run；**Then** diff 中看见字段变化但不出现原始凭据，原文件逐字节不变，原来不存在的目标文件仍不存在。

测试：`TestDiffRunsOnCopiesAndLeavesRealFilesAlone`、`TestPiHeaderDryRunDoesNotWriteOrExposeValues`、`TestHeaderMasksKeepShortSecretChangesVisible`。

证据：`internal/preview/`、`cmd/charon/commands.go`、`internal/catalog/catalog.go`、`internal/catalog/project.go`。

## 6. off

**目的**：移除 Charon 对某工具的配置影响，而不是删除用户账户、全部绑定或历史使用期间的新内容。

- **OFF-01**：有 stash 时恢复首次切换前的托管字段原文；恢复支持的注释、键序、缩进、BOM、CRLF 与末尾换行状态。
- **OFF-02**：删除 Charon 生成的命名空间；原先不存在的文件只在没有用户新增有效内容时删除。用户后来添加的非托管内容保留。
- **OFF-03**：成功后清除该工具的 active 身份和 stash；catalog 中的绑定、凭据、模型保留。
- **OFF-04**：无 stash 时只移除托管内容，不猜测原始 key，不创造 default 绑定。
- **OFF-BOUNDARY**：纯空白文件恢复为空文件是现有明确例外。逐字段恢复不是整文件历史快照。I/O 失败可能部分改变文件，不保证整次 off 自动回滚。

验收场景：**Given** 用户原配置含注释，Charon 首次应用后用户新增非托管字段；**When** off；**Then** 托管旧值与格式恢复、新字段保留、active 清除、保存的绑定仍存在。

测试：`TestOffCodexRestoresByteExact`、`TestOffPiRestoresByteExact`、`TestOffRestoresGeneratedConfigs`、`TestOffKeepsWhatTheUserAddedWhileActive`、`TestOffWithoutStashRemovesCharonOnly`。

证据：`internal/catalog/stash.go`、`internal/tools/stash.go`、`internal/tools/off_diff_test.go`、`internal/catalog/stash_test.go`。

## 7. 新增、编辑、重命名、复制与删除绑定

| ID | 操作合同 | 成功后的保持项与失败边界 |
|---|---|---|
| ADD-01 | 新绑定须有合法名字、key、endpoint 与至少一个模型；CLI add 保存后应用并激活 | 不自动捕获 default 绑定或 OAuth 快照；应用失败时保存的 catalog 记录可能已经存在，见 Q1 |
| EDIT-01 | 未传的字段保留；编辑非 active 绑定只更新 catalog，不自动切换 | key、endpoint、默认模型、列表和 Pi 设置按明确参数修改；保存和投影不是一个全文件回滚事务 |
| EDIT-02 | 编辑 active 绑定后，经既有锁校验再投影 | 失败后应区分已保存记录与磁盘实况，不把部分写入说成全部成功 |
| EDIT-03 | 明确替换模型列表且未指定默认模型时，若旧默认不在新列表，则提升第一项；明确指定默认时把它纳入列表 | 不保留上个 endpoint 的模型列表作为新绑定的列表 |
| NAME-01 | 重命名只改变合法的名字，保持绑定身份及其凭据、模型和 Pi 设置 | 不把绑定名当文件路径 |
| COPY-01 | 创建另一条配置选择，复用同 endpoint / key 的凭据，不向网关申请新 key；复制本身不激活 | Pi→Pi 保留保存的协议和标头；Pi→其他工具不传播这两项 Pi 设置。复制不是协议转换，也不证明目标可用，见 7.1 |
| COPY-02 | 隐藏方案复制到支持多 endpoint 的工具，副本仍隐藏；不支持隐藏的目标不附加该标志 | 原方案、active 身份与 Pi 磁盘默认保持；TUI 主路径与辅助 CLI 必须一致，见 D2 |
| RM-01 | CLI/TUI 删除正在使用的绑定必须拒绝；非 active 绑定可以删除 | 删除绑定不删除可能仍被共享的凭据与模型 |

API 边界：`Catalog.RemoveBinding` 是内部删除原语，允许删除并清除 active 身份；“active 不能删除”的用户操作规则由 CLI/TUI 执行，不能把两层合同混写。

验收场景：**Given** A active、B inactive；**When** edit B；**Then** B 的保存值改变，但工具仍使用 A。**When** cp B→C；**Then** C 保存但不激活，凭据没有被换成新 key。**When** rm A；**Then** 拒绝。

测试：`TestRunBindingLifecycle`、`TestRunEditDoesNotSwitchInactiveProfile`、`TestRunEditModelsPromotesFirstItemWhenOldDefaultIsRemoved`、`TestRunCopyBindingAcrossTools`、`TestRunPreservesPiEndpointType`、`TestBindingRejectsInvalidPiProtocolBeforeWriting`、`cmd/charon/headers_test.go`。COPY-02 主验收为 `TestDuplicateHiddenBindingViaKeys`（`internal/tui/hidden_test.go`），辅助入口回归为 `TestRunCopyPreservesHiddenOnlyForMultiEndpoint`。

证据：`internal/catalog/bind.go`、`internal/catalog/catalog.go`、`cmd/charon/commands.go`、`internal/tui/actions.go`。

**COPY-02 的 TUI 主验收场景**：

1. Pi 当前应用方案 A；B 属于另一网关且已隐藏，B 的 key 不在 Pi 托管配置中。
2. Pi 磁盘默认已选 A 的另一个合法模型，而非 A 保存的默认模型。
3. 在绑定列表选 B，按 c／x，选择本工具、输入副本名、确认复制。
4. 副本出现在列表中并保留隐藏标记；原 B 和 A 的 active 身份不变；副本继承凭据、模型、Pi 协议与明确保存的标头。
5. 复制前后 Pi 的 models/settings 文件逐字节不变，当前模型不被重置；以后明确重新应用 A，B 及其隐藏副本也不能让备用 key 重新注册。

第 5 项的“复制不改默认”不等于“显式重新应用也不改默认”：reapply 原本就会恢复保存的默认。这两个用户动作必须分别断言。测试入口是 `internal/tui/hidden_test.go` 的按键事件／渲染流程，不是让用户手输 CLI。

### 7.1 跨工具复制：协议与可用性设计审查

**审查结论：配置复用的职责划分合理，但目前交互没有充分交代兼容性边界。** 不应把 Charon 扩成协议转换代理，也不应仅因源／目标协议不同就禁止复制：同一网关可能支持多个协议。真正容易误导的是“复制成功”后，用户不知道目标将换一种请求方式，或哪些必要设置没有带过去。下面的“当前行为”是源码事实；带 `[ASK USER]` 的改进尚未实现／批准，不能用现有测试通过宣称已交付。

#### 地址、请求协议与模型发现是三件事

- **地址**：catalog 的 `Provider.BaseURL` 表示保存的 API 基址；协议不是该地址的一项全局属性。同地址可以被不同工具／绑定以不同协议使用，但这不证明服务端支持这些协议。
- **请求协议**：Pi 的 `Binding.PiAPI` 决定 Pi 如何发推理请求，例如 `openai-completions`、`openai-responses`、`anthropic-messages`。其他工具由各自 adapter／客户端决定，不读取复制来源的 Pi 设置。
- **模型发现方言**：`Tool.Provider` 只选择列模型时先尝试的方言，不是目标工具推理协议。发现入口可以再尝试另一方言，成功结果不写回 `PiAPI`。`GET /models` 成功不证明对话、流式输出、工具调用或认证方式兼容。

**当前复制／投影矩阵**（仅描述本项目 adapter，不声称目标工具本身只能支持这些模式）：

| 复制方向／目标 | 当前保存与应用方式 | 不能由此推断 |
|---|---|---|
| Pi → Pi | 保留保存的 `PiAPI` 和明确保存的标头；缺省协议仍按 `openai-completions` 解析 | 仅存在于 Pi 磁盘文件里的标头不会自动反向复制；副本不等于完整配置快照 |
| Pi → Claude Code | 副本不保存 Pi 协议／标头；应用时写 Claude 的 Anthropic 配置；自定义网关使用 `ANTHROPIC_BASE_URL` 与 `ANTHROPIC_AUTH_TOKEN` | 不会让 Claude 改发 OpenAI 请求，不会把 OpenAI 服务转换成 Anthropic 服务 |
| Pi／其他工具 → Codex | 不带 Pi 设置；写入 `wire_api = "responses"` | “支持 OpenAI”不等于支持 Responses；Chat Completions 网关仍可能不可用 |
| Pi／其他工具 → OpenCode | 不带 Pi 设置；注册 `@ai-sdk/openai-compatible` provider | 不会沿用来源的 Anthropic／Gemini 协议 |
| Pi／其他工具 → Grok | 不带 Pi 设置；写入模型、`base_url` 和 `api_key`，没有请求协议选择字段 | 不能从 `Tool.Provider = "openai"` 推断其具体推理 wire；本轮未验证 Grok 客户端的实际请求 |
| 非 Pi → Pi | 当前一律保存 `openai-completions`，包括 Claude → Pi 和 Codex → Pi | 不会从 Claude 推出 Anthropic，也不会从 Codex 推出 Responses；此现状不是已确认的理想产品规则 |

各方向均沿用保存的 endpoint、key、默认模型与模型列表，不向服务端申请新凭据、不自动把模型替换成目标工具的品牌模型。模型元数据可能仍是同 provider／slug 的共享记录，见 Q4；“复制一条绑定”不意味着参数从此全部独立。

#### 易出问题的设计与判断

优先级表示影响用户理解／可用性的程度，不是本轮实施承诺。

| 优先级 | 场景与当前风险 | 判断与最小改进方向 |
|---|---|---|
| 高 | Pi 的 Chat Completions 方案复制到 Claude 或 Codex，命名后直接显示成功，没有说明目标分别使用 Anthropic／Responses | 按目标工具写配置是合理边界；静默改变请求方式不够清楚。建议保存前展示源／目标协议差异，而不是自动转换或一律禁止 |
| 高 | Claude／Codex → Pi 默认变成 Chat Completions，甚至 Pi → Claude → Pi 往返后也不恢复原协议 | 统一缺省适合“没有来源的新建”，不适合作为“已知来源的复制”的无提示决定。建议在 TUI 保存前选择 Pi 协议，来源协议只作可修改的预选，不能当成服务端能力检测 |
| 中 | `https://gateway.example/v1` 在 catalog 中保留；应用 Claude 时才去掉末尾 `/v1`。反向复制根地址到 Pi 不会自动补 `/v1`，模型发现却可能补 `/v1/models` 并成功 | 分工具整理地址合理；只展示保存地址或列模型结果会误导。建议同时说明保存基址与目标写入基址，不按协议名猜路径，不改源 provider |
| 中 | Pi 自定义标头跨工具被丢弃；即使 key 字面值相同，Claude 自定义网关也会按 Bearer 使用它，未必符合源方案的认证要求 | 不盲传标头合理，但当前 adapter 没有实现迁移不等于目标工具本身不支持。应提示哪些设置不迁移；只显示标头名，隐藏值。认证兼容性仍需网关说明 |
| 中 | 模型列表原样复制，目标协议可能不暴露这些 ID，或不支持所需工具调用；同网关参数还可能共享 | 保留用户选择合理；自动筛模型、改 ID 或宣称“已验证”不合理。允许编辑副本，明确列表只是已保存的候选，不是目标可用性认证 |
| 中 | 可见副本复制到 Pi 后当下不激活，但后续应用另一 Pi 绑定时，也可能聚合注册该副本；同地址多绑定则只选一个代表，协议／标头不同也不会同时注册 | “没切换”不等于“永不进入模型列表”。应说明后续聚合及同地址代表规则；沿用 8.1 的边界，不在本轮新增 pending 状态、独立 key 政策或改成按协议拆 provider |

地址细节的证据边界：`ValidateEndpoint` 只做 HTTP(S) 绝对 URL 等语法检查，不能证明输入是正确 API 基址。`normalizeClaudeBaseURL` 使用字符串后缀裁剪，地址提示则解析 URL path；带 query／fragment 的地址不能直接套用普通 `/v1` 示例。`[TODO]` 补齐这类地址与误填完整 `/chat/completions`／`/messages` 路径的行为测试，再确认支持范围；本轮不宣称已经兼容，也不引入自动试探／重写路径。

#### 建议规则及状态

| ID | 规则 | 状态 |
|---|---|---|
| COPY-03 | 复制的成功含义只限保存配置选择；沿用 COPY-01 的不激活边界。不把保存成功、磁盘写入成功、列模型成功描述为推理可用，也不提供隐式协议转换 | 现有职责边界的澄清；兼容性文案尚缺专项验收 |
| COPY-04 | 跨工具复制在保存前显示目标工具、源／目标请求方式、会保留与不迁移的字段，并说明未验证兼容性；利用现有复制／命名步骤，不另造管理菜单。同工具复制不增加无关阻碍 | `[ASK USER]` 建议提示后允许继续；是否强制显式确认见 Q5 |
| COPY-05 | 非 Pi → Pi 时，在写 catalog 前复用协议选择能力；可按来源的实际请求方式预选，允许修改。没有可靠来源信息时明确显示缺省值，不能把发现方言当推理协议 | `[ASK USER]` 尚未改变非 Pi → Pi 的默认行为；只影响新副本，不迁移既有绑定 |
| COPY-06 | 目标地址的适配由目标 adapter 负责，不改源地址或源绑定。保存前说明目标将写入的基址；网关需要另一条协议地址时，让用户明确编辑目标值，不猜测、拼接或替换厂商域名 | 现有写入边界 + `[ASK USER]` 复制步骤中的展示／编辑尚未实现；query／fragment 等边界仍待验证 |
| COPY-07 | 原 key／模型保留不等于认证／模型可用；跨工具不自动传播 Pi 凭据标头，不输出其值。对可能丢失的请求设置明确提示；同 provider／slug 的元数据共享仍遵循 Q4 | 现有数据边界 + `[ASK USER]` 丢失设置的提示尚未实现 |
| COPY-08 | 复制不自动发推理请求、不静默尝试另一协议、不借验证动作把 key 发到猜测的新地址；取消新交互时不得创建副本或改动源／目标工具配置。CLI 保留辅助入口，采用相同语义，不以新增 CLI 命令代替 TUI | 维持本地配置工具定位；新增提示／选择的 CLI 配套方式待 COPY-04/05 确认后再定 |

#### 验收基线与缺口

现有证据：`TestPiEndpointTypeSaveEditAndCopy` 覆盖 Pi 内复制保留协议／标头、Pi → Claude 丢弃两项及源 Pi 配置不变；`TestRunPreservesPiEndpointType` 明确断言 Claude → Pi 当前得到 `openai-completions`。这证明现状，不证明默认合理。`TestDuplicateFlowNamesAndCopiesBinding` 覆盖 TUI 目标选择与命名；`TestClaudeCustomEndpointUsesBearer`、`TestNormalizeClaudeBaseURL` 覆盖 Claude 普通地址裁剪与认证写入，不是实际 Claude 对话测试。

以下是新规则确认后的 `[TODO]` 验收，**不是已经存在／通过的测试**：

| 场景 | 应验收的结果 |
|---|---|
| Pi Chat Completions → Claude，地址为 `https://gateway.example/v1` | c／x 按键进入目标／命名流程，保存前可见目标 Anthropic、基址整理和“不验证可用”；确认只保存副本，取消不写；日后明确应用时 Claude 写根基址，源 Pi 协议／地址保持 |
| Pi Chat Completions → Codex；网关仅支持 Chat | UI 明示目标 Responses，不把两者统称“OpenAI，所以兼容”；不自动改为 Chat，不执行付费推理验证。可保存不等于已经能用 |
| Claude／Codex → Pi；以及 Pi → Claude → Pi 往返 | 按 COPY-05 展示待选协议及来源预选；用户可修改；保存值与选择一致，不从已丢弃的原 Pi 设置中假装恢复。源数据、两端 active 与工具文件保持不变 |
| 来源依赖 Pi 自定义认证标头 | 提示未迁移标头名，不泄露值；目标不偷偷写入这些标头；模型／key 未被换成猜测值 |
| 网关列模型成功，但未提供目标对话协议；根基址无 `/v1` | 用 httptest 验证发现结果不改变 `PiAPI`、不宣称推理通过、不推导／改写保存基址；列模型与推理能力分开断言 |
| Pi 中已有当前端点 A；复制另一地址 B 到 Pi | 复制当下不切换、不重置模型；日后显式应用 A 时按可见性聚合 B。界面提前说明这一影响；若 B 与已有 Pi 绑定同地址，按 8.1 选择代表，不声称两种协议同时生效 |
| 取消或目标重名 | TUI 停留／返回合理页面，源数据不变，目标不新增；提示不会先保存半个副本。保存中 I/O 失败仍遵循 Q1，不宣称全事务回滚 |
| 辅助 CLI 同方向复制 | 与已确认的 TUI 数据与不激活规则一致，警告不泄露凭据；不能只通过 CLI 测试就宣布 TUI 完成 |

测试使用假 key、临时 HOME／XDG_CONFIG_HOME 和 httptest；不得调用真实网关或把 mock／配置断言冒充外部 CLI 的实际会话兼容性验证。

源码证据：`internal/tui/actions.go`（`duplicateBinding`）、`internal/tui/load.go`（`loadDuplicateTools`）、`internal/tui/wizard.go`（`viewDupName`）、`cmd/charon/commands.go`（`cmdDuplicate`）、`internal/catalog/bind.go`（`bindingPiAPI`）、`internal/catalog/project.go`（`endpointGroupsFromBindings`）、`internal/tools/{tool,pi,claude,codex,opencode,grok,validate}.go`、`internal/tui/picker.go`、`internal/models/fetch.go`。

## 8. Pi 的聚合、隐藏与标头

### 8.1 endpoint 聚合

- **PI-GROUP-01**：只聚合为 Pi 保存的、可见的绑定；Codex / Claude 等工具独有的 endpoint 不自动进入 Pi。
- **PI-GROUP-02**：同 endpoint 只有一条 provider；主绑定优先，其余按第一个合格的可见绑定选取。过滤隐藏记录必须在去重前执行。
- **PI-GROUP-03**：每条 provider 使用自己的 key、协议、模型元数据和标头，不借用主 endpoint 的身份；provider 标识唯一，标签差异不能改变配置含义。

测试：`TestProjectSpecAggregatesToolBindingsForPi`、`TestPiGroupEntryMatchesAStandaloneApply`、`TestPiAggregationKeepsEachEndpointsProtocol`、`TestPiAggregationKeysStayUnique`。

### 8.2 隐藏绑定

- **HIDE-01**：Pi 按规范化 endpoint 分组，同地址的所有保存绑定一起隐藏／显示；隐藏保留 catalog 记录及设置。不同 endpoint 即使共用 key 也互不影响。
- **HIDE-02**：不能隐藏包含 active 绑定的 endpoint，也不能删除 Pi 磁盘默认正在使用的 provider；激活隐藏组内任一绑定会显示整组。
- **HIDE-03**：UI `h` 调用 `Catalog.SetBindingHidden`，立即只增删目标 endpoint 的托管 provider 并保存整组标志；不做全量 reapply，不改变 active、Pi settings 或当前默认模型。
- **HIDE-BOUNDARY**：配置写成功而 catalog 保存失败属于部分生效，必须明确报错并允许修复后重试；legacy extension 仍使用目标 endpoint 时先拒绝。新增、编辑与复制遵守同组状态，d 仍只删除选中的保存绑定。

测试：`internal/catalog/hidden_test.go`、`internal/catalog/pi_visibility_test.go`、`internal/tui/hidden_test.go`、`internal/tui/pi_visibility_test.go`；覆盖同组不同 key、跨组同 key、即时隐藏／恢复、默认模型与设置字节保留、active／磁盘默认拒绝及 TUI 按键流程。

### 8.3 请求标头与协议

- **HEADER-01**：Pi 标头保存为字符串映射；缺省保留已有设置，明确 `{}` 清空；普通编辑、重命名和 Pi 内复制保留保存值。
- **HEADER-02**：未保存标头时只继承同 endpoint 的托管 provider 标头，不串到其他网关；只存在于工具文件中的标头不自动反向存入 catalog。
- **HEADER-03**：明确输入按字面值处理，`$`、`!` 使用 Pi 既有转义；Charon 不求值这些表达式。
- **API-01**：Pi 请求协议保存在 Pi binding 上，缺省使用 OpenAI completions；模型发现成功的 HTTP 方言不自动改变它。跨工具复制不会传递这项设置；非 Pi → Pi 当前也用该缺省，设计风险与待确认规则见 7.1 / Q5。

证据：`internal/catalog/headers.go`、`internal/catalog/project.go`、`internal/tools/pi.go`、`internal/tools/pi_headers_test.go`、`internal/catalog/headers_test.go`。

## 9. 查询、模型发现与模型元数据

- **READ-01**：status 分开显示 last confirmed 与磁盘实况；缺失的上下文／输出上限显示未知，不能用内置推算假装读到了磁盘值。
- **READ-02**：ls 显示保存的绑定与 active / hidden；不把显示名称和 key 混为一谈。
- **READ-BOUNDARY**：查询不投影目标工具文件；初始化 catalog 不属于严格零写入保证，见 Q2。
- **FETCH-01**：模型发现按 HTTP 实况返回可用模型；失败时给出可操作错误。TUI 可转入手动输入，不能把失败当作已有模型通过验证。
- **FETCH-02**：每次 Fetch 有 20 秒超时，跨 host 重定向移除认证头；发现入口可尝试另一方言。当前同时提供 Bearer 和 x-api-key，这是已实现策略，不等于所有网关都要求两者。
- **MODEL-01**：手动上下文设置优先于 API、内置值和兜底；上下文预算与输出上限是不同参数，配置预算不证明真实网关接受相应长度。
- **MODEL-02**：set-context 只接受正整数或 unknown；set-effort 只对支持它的 Codex 模型设置执行。目标绑定 active 时投影，inactive 时不自动切换。
- **MODEL-BOUNDARY**：模型记录按 provider / slug 共享，改变它可能影响其他绑定的未来投影；是否需要 binding-local 覆盖，见 Q4。

测试：`TestStatusShowsOnDiskTokenLimits`、`TestRunSetContextUpdatesModelWindow`、`TestRunSetEffortWritesCodexConfig`、`internal/models/fetch_test.go`、`internal/catalog/windows_test.go`、`internal/models/builtin_test.go`、`internal/models/thinking_test.go`。

证据：`cmd/charon/commands.go`、`internal/models/fetch.go`、`internal/catalog/windows.go`、`internal/catalog/catalog.go`、`internal/models/builtin.go`、`internal/models/thinking.go`。

## 10. 更新、卸载和发布

- **UPDATE-01**：仅下载对应平台 release、核对 archive 的 SHA-256、读取合法 charon 条目后替换二进制；不执行远程安装脚本。
- **UPDATE-02**：下载、校验、解包或安装失败不能打印成功；校验值与二进制同渠道，不构成发布方签名认证。
- **UNINSTALL-01**：卸载运行中的 Charon 二进制，不自动删除 catalog、目标工具配置或用户 OAuth 登录；完整清理需用户明确执行相应操作。
- **RELEASE-01**：提交／发布前按项目门禁检查格式、vet、race、构建和 lint；macOS 交叉编译不能冒充 macOS 测试。
- **RELEASE-BOUNDARY**：`[TODO]` 更新与卸载尚缺完整二进制流程的隔离端到端验收；不能拿 checksum / tar helper 测试代替它。pre-push 在工具缺失时可跳过漏洞扫描，不能因此宣称已完成漏洞检查。

测试与证据：`TestUpdateHelpers`、`cmd/charon/commands.go`、`scripts/install.sh`、`.goreleaser.yaml`、`.github/workflows/ci.yml`、`.githooks/pre-push`。

## 11. 已确认的规格偏差与证据缺口

D1/D2 最初均用临时 HOME、假 key 复现。D1 帮助泄露已修复；随后用户授权决定复制规则，并明确以 TUI 为主。D2 的辅助 CLI 差异已补齐，同时增加 TUI 按键／渲染验收。隐藏 h 的模型重置、初始化、参数共享没有在这两轮改动。

| ID | 规则与现状的偏差 | 复现结果 | 下一步 |
|---|---|---|---|
| D1（已修复） | 原 edit 把保存的 key 当成可打印的参数默认值，帮助／参数错误会泄露 | 新增 `TestRunEditHelpDoesNotLeakSecrets`：旧代码四种输出路径失败，修复后通过；`TestRunEditKeyFlagSemantics` 保证省略、换 key、显式空 key 的原行为 | 两次独立变异分别恢复泄露、吞掉显式空值，测试均报警；不把这次窄修复宣称为完整安全审计 |
| D2（已修复） | 原辅助 CLI 没有继承 Hidden，TUI 已继承 | `TestRunCopyPreservesHiddenOnlyForMultiEndpoint` 旧代码失败、新代码通过；`TestDuplicateHiddenBindingViaKeys` 覆盖 c/x、逐键输入副本名、隐藏标记、模型保留与后续应用 | 三次独立变异：CLI 丢标志、TUI 丢标志、TUI 复制误触发重新应用，均被对应测试发现；不把隐藏等同整把 key 禁用 |
| D3 | README 的“dry-run 不写文件”若理解为全进程零写入，与启动初始化不一致 | `run` 在 dispatch 前执行 `Catalog.Open`，可建目录、迁移或补齐元数据 | `[ASK USER]` Q2；先明确只读范围再决定是否改 Open 入口 |
| D4（已修复） | 本地 lint 曾有两项原有错误 | `headers_test.go:110` 遮蔽 copy；`validate.go:74` De Morgan 提示 | 已重命名局部变量并等价简化布尔表达式；本轮新增格式与错误字符串问题也已清理，`golangci-lint run` 通过 |
| D5（已否决并撤回） | 用户拒绝 h 与停供／恢复 key 并存、恢复不自动生效的设计 | 自动按键／配置／PTY 测试曾通过，但真人反馈为“不采用，宁可不实现”；p 菜单、CLI 命令、政策字段与屏障已删除 | 不重新引入独立 key 政策；endpoint 分组 h 已实现，仍以用户试用为准 |
| D6（设计风险，未修复） | 跨工具复制只报告保存结果，没有解释请求协议变化、标头不迁移与兼容性未验证；反向复制到 Pi 统一落到 Chat Completions | 复制调用链及 `TestRunPreservesPiEndpointType` 可确认默认；Claude／Codex adapter 分别使用 Anthropic／Responses，列模型成功不验证推理 | 已补 7.1 的当前行为、设计判断与待补验收；`[ASK USER]` Q5。本轮仅文档，不把建议当成已实现行为 |

覆盖率阈值未配置；上一轮工作树总体 statement coverage 为 80.8%。这不是规格覆盖率，也不能证明没有回退。短时 fuzz、共享构造器的差分与有限变异检查均是有边界的证据。

## 12. 用户确认与具体场景：不是术语选择题

### Q1 — 编辑方案时，Pi 配置写到一半失败，接下来怎么办？

**已确认方向**：配置写入失败应明确报错，排除故障后允许用户重试。本轮不引入自动回滚框架，也不再要求用户回答抽象的“所有操作是否全部回滚”。

需要先讲清的事实：Charon 的一条命令可能分多步写文件。失败的那一次原子替换不会把目标写成半截；但**前面已经写成功的内容不会自动消失，甚至可能是同一个文件的前一次修改**。

例子：你把正在使用的 Pi 方案 `work` 的默认模型改为 `model-big`：

| 步骤 | 写入对象 | 假设的结果 |
|---|---|---|
| 1 | Charon 自己的 `~/.config/charon/bindings.json` 等记录 | 新模型已经保存 |
| 2 | Pi 的 `~/.pi/agent/models.json` | 模型列表写成功 |
| 3 | Pi 的 `~/.pi/agent/settings.json` | 写入失败，例如此时磁盘空间不足 |

这时不能说“什么都没做”：保存的方案和 Pi 模型列表可能已经更新，Pi 的默认选择还没更新。现有 Pi 错误会注明 `models.json already updated`。修好写入问题后，**在 TUI 绑定列表选中 work，按 r 重新应用**，让 Pi 配置与保存的方案一致。备用 CLI 的等价操作是 `charon reapply pi work`，不是用户必须走的路径。

另外，新增方案已经保存时，不能笼统要求“原命令再跑一次”：重复 `add --name work` 会碰到同名方案；应对已有方案执行 switch／reapply。已经 active 的方案执行 switch 是 no-op，修复漂移要用 reapply。

以上部分写入场景依据写入顺序与错误处理源码，不冒充本轮已注入所有磁盘故障。`[TODO]` 后续按具体文件失败点补验收，并检查错误是否把已经完成的步骤说清楚。

### Q2 — 只是查看／预览，为什么会出现 Charon 自己的文件？

先分清两套文件：

- **Charon 自己保存的方案**：`~/.config/charon/`。这里记着你的方案名字、网关、key、模型参数；代码中叫 catalog。
- **Pi 真正读取的配置**：`~/.pi/agent/models.json` 和 `settings.json`。真正应用方案才修改它们。

具体场景：第一次安装 Charon，你打开 TUI 只是看列表，还没增加任何方案。现在程序会先创建 Charon 自己的目录、锁文件和初始化标记，**但不会因此修改 Pi 的模型／默认选择**。旧版 Charon 的模型记录还可能在启动时补齐上下文大小；那也是修改 Charon 的记录，不是偷偷应用到 Pi。辅助 CLI 的 `charon status` 也有相同初始化行为。

`switch --dry-run` 同样先打开这些记录，然后在临时副本上生成预览；不会把预览写回你的 Pi 文件。预览的临时副本本身也会写盘，但不是用户的真实配置。

真正待确认的是：**你只想查看时，是否连 Charon 自己的首次建目录／整理旧数据也不能发生？** 不是在问“能否偷偷修改 Pi”。目前不改初始化流程；只有出现实际零写入需求时才讨论更强约束，不要求用户现在作决定。

### Q3 — 隐藏备用方案，为什么会把另一方案正在用的模型改回去？（已解决）

旧实现按 h 隐藏 `backup` 时会全量重新应用 active 的 `work`，因此可能把用户已在 Pi 中选择的 `model-big` 重置为 `work` 保存的 `model-small`。现在 h 只增删 `backup` 所在 endpoint 的托管 provider，不重写 `settings.json`，所以当前默认模型、active 身份和其他 provider 保持不变。

隐藏状态按 endpoint 而不是 key 归组：同地址的别名一起隐藏，不同地址即使共用 key 也互不影响。如果 active 身份属于目标组，或 Pi 磁盘默认已经指向目标 provider，操作会被拒绝并提示先在 TUI 切换到其他 endpoint；若目标已在 catalog 中 active 但磁盘默认漂移，则先按 r 明确重新应用。不会暗中切换网关，也不保证运行中 Pi 进程的内存选择随磁盘变化。

该方向已由用户确认并实现；`TestPiVisibilityEndpointImmediateAndDefaultsUntouched`、`TestPiVisibilityDiskDefaultRefused` 与 `TestPiVisibilityViaHKeys` 覆盖配置字节保留、拒绝路径和 TUI 按键流程。

### Q4 — 改 work 的 context size，为什么 backup 以后也会跟着变？

例子：work、backup 用**同一网关的 `model-x`**，可以用不同 key。你把 work 的 context size 从 128000 改为 256000；目前这两条方案引用同一份模型参数，所以下次切到／应用 backup，也会读到 256000。不同网关的同名 `model-x` 则是不同记录。

不是所有参数都这样：

| 设置 | 当前是否共用 | 具体含义 |
|---|---|---|
| context size | 同网关、同模型共用，跨 key／工具仍可能共享 | 改模型的一条记录，其他引用者后续应用会读到新值 |
| Codex reasoning effort | 同样存在共享模型记录中，主要由 Codex 使用 | 改一条 Codex 方案使用的模型，其他相同模型引用也可能受影响 |
| Pi 的手动思考等级映射 | 同样共用 | 即使两条 Pi 方案的请求格式不同，手动映射仍是同一份 |
| Pi 请求格式，例如 OpenAI／Anthropic | **每条方案单独保存** | 改 work 的协议，不自动改 backup 的协议 |
| 明确保存的请求标头 | **每条方案单独保存** | 没有明确保存时仍有同 endpoint 磁盘标头继承规则，不能假定永不继承 |
| 默认模型与可选模型列表 | **每条方案单独保存** | 换 work 的默认或列表，不自动改 backup 的保存值 |

“共用保存值”不等于所有工具立刻重写配置：更新哪些文件还取决于 CLI／TUI 入口。例如 CLI set-context 只应用点名的 active 方案；模型库的思考映射编辑还会联动 effort、刷新引用该模型的 active 方案。这些现状须先说明，不能笼统说所有参数都共享或都隔离。

`[ASK USER]` 真正需要考虑的是：**同一模型的 context size／思考设置，要不要允许 work、backup 分别保存不同值？** 请求格式本来就独立，不应混成同一个待决问题。本轮不改数据结构；先从用户实际需要的参数举例确认。


### Q5 — Pi 里用 OpenAI 的方案复制到 Claude，为什么保存成功却可能不能用？

**原状**：Pi 的 `work` 保存 `https://gateway.example/v1`、key K、模型 M、`openai-completions`；Claude Code 正在使用自己的另一方案。

**操作与当前结果**：在 Pi 绑定列表按 c／x，选 Claude Code，命名 `work-claude` 并确认。Charon 在自己的 catalog 里保存副本，沿用地址、K 与 M，不带 Pi 协议／标头；此时不切换 Claude，也不改变 Pi 的模型。当前界面只给复制成功信息，没有把协议差异解释出来。

**以后明确应用副本时**：Charon 才写 `~/.claude/settings.json`；自定义网关基址去掉末尾 `/v1`，K 按 Claude 的 Bearer 配置使用，M 保留为模型选择。Claude 按 Anthropic Messages 请求。若网关只提供 OpenAI Chat，失败来自协议不兼容；若也提供 Anthropic 且地址、认证与模型匹配，则可能可用。复制动作本身没有检查这些服务端条件。

**设计建议**：保留复制能力，保存前在现有 TUI 流程说明“将使用 Anthropic；Pi 的协议和标头不迁移；尚未验证网关支持”。反向复制到 Pi 时，让用户在保存前选择协议，按来源实际请求方式预选，而非无提示地一律选 Chat Completions。目标地址需要修改时，只修改副本，不动源绑定。

`[ASK USER]` 待确认的是以上交互：**采用“解释差异、确认后允许保存”，而不是“协议不同就禁止”；复制到 Pi 时增加可修改的协议选择，并以来源协议作预选，是否合适？** 本轮只记录建议，不新增转换代理、自动探测、兼容性状态字段或 CLI 命令；不迁移已有副本。确认后以 7.1 的 TUI 按键／渲染与配置保持场景验收。

## 13. 维护与验收顺序

1. 选择一个规则 ID，确认用户场景和成功／拒绝边界。
2. 阅读该操作所有入口，区分 CLI/TUI 与内部原语；不要只修被点名的一条路径。
3. 增加最小正例与反例，必要时做 CLI/TUI 对照、格式差分或故障注入。
4. 实现／修正后执行项目门禁；故意破坏关键规则，确认测试会报警。
5. 用户试用确认 Intent → Spec；工具检查支持 Spec → Impl；记录仍未覆盖的条件。
6. 审阅清晰的 diff 范围再提交，不把混合工作树一并 `git add .`，不自动发布。

前一轮已完成 **D1：edit 帮助泄露凭据** 的窄修复。将 key 放回可打印默认值、或将显式空值误当省略，两项变异都会被新测试发现。

本轮完成 **D2：隐藏方案复制规则收口**。TUI 原有复制行为正确，新增 c/x 按键、逐字符命名、隐藏行渲染、当前合法模型漂移、配置字节保留与后续应用回归；辅助 CLI 只补继承标志的差异。故意丢掉两条路径的隐藏标志，或在 TUI 复制时错误重新应用，三项变异均报警。全仓格式、vet、`go test -race -count=1 ./...`、Linux 构建、macOS arm64 交叉编译通过；lint 仍为 D4 两处原有错误。临时证据在 `/tmp/charon-tui-copy-6ut6dpsl/`。

gopls 不可用，LSP 检查未运行；需要时安装 gopls 或在 `pi-lsp.json` 指定可用命令。未运行漏洞扫描或 macOS 上的测试，未安装、未提交。TUI 按键测试不是终端启动／真人试用或运行中 Pi 验证，也不保证复合复制操作的并发事务性。

**D5 的独立 key 停供试验已撤回。** 曾实现的 p 双动作菜单、确认页、`disable-key`／`enable-key` 命令、`Credential.PiDisabled`、`AuthSpec.PiDisabledKeys` 以及投影／激活／stash／标头屏障均已精准删除。此前自动化与 PTY 结果仅作为“被否决设计”的历史证据，不能替代产品决定，也不能作为重新引入该功能的理由。

撤回时保留了原有 `h`、header 与格式保留编辑器、差分／fuzz 测试、`cmdEdit` 帮助不泄露 key 的修复、隐藏复制修复及无关工作树改动；未安装或替换用户现有二进制。按 endpoint 分组的新 `h` 现已实现，当前规则与验收见 8.2 和 Q3。

Q1 的“写配置失败，明确报错后重试”已收到用户答案；Q2 与 Q4 先解释具体场景，再按用户需要逐项确认，不能当成已批准的功能改动。Q3 已按 8.2 实现并验收。临时证据在 `/tmp/charon-help-fix-di397ba1/`：修复前后目标日志、`checks.json`、`mutations.json` 与只读场景复核报告；可重复运行的回归案例保存在 `cmd/charon/main_test.go`。

**D6 本轮仅完成设计审查与规格记录，未改功能。** 在当前工作树的临时副本中运行 `make fmt`、`make test`，隔离 HOME／XDG_CONFIG_HOME，避免格式化其他未完成的源文件。格式化与 vet 通过；六项复制／协议／Claude 地址相关测试另以 `-race -count=1` 重跑通过。全量测试未通过：catalog 的 `TestHiddenDuplicateDoesNotShadowVisible`、`TestEditingKeepsHidden` 失败，TUI 的协议下拉、配置摘要、底栏和详情等行数断言失败。本轮不修这些隐藏／布局问题，也不据此更新其交付状态。日志在 `/tmp/charon-copy-spec-lb7755rh/{fmt,test,copy-tests}.log`；没有运行真实网关、外部 CLI 会话、lint 或新建议交互的验收，COPY-04～07 的待确认项仍未实现。

### 证据索引

- 用户确认的 Pi endpoint 分组范围，以及当前 [README](../README.zh-CN.md)、[AGENTS](../AGENTS.md)。
- `cmd/charon/main.go`、`cmd/charon/commands.go`、`cmd/charon/main_test.go`。
- `internal/catalog/catalog.go`、`internal/catalog/bind.go`、`internal/catalog/project.go`、`internal/catalog/headers.go`、`internal/catalog/stash.go` 及相邻测试。
- `internal/tools/tool.go`、`internal/tools/pi.go`、`internal/tools/stash.go`、`internal/tools/off_diff_test.go`、`internal/tools/pi_groups_diff_test.go`。
- `internal/preview/`、`internal/edit/`、`internal/artifact/`、`internal/models/`、`internal/secret/`。
- [工程走查记录](engineering-walkthrough.md)、[测试体系](codebase/TESTING.md)、[风险清单](codebase/CONCERNS.md)、CI / Makefile / pre-push。
