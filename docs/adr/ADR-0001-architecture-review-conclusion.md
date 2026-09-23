# ADR-0001：架构深化评审结论 — 无需重构

- **状态**：已被后续 catalog 架构取代
- **日期**：2026-08-15
- **提出者**：架构评审（improve-codebase-architecture skill）

> 历史记录：本文结论针对当时基于快照的 `profile` 实现。后续版本改为 catalog 存储，
> 删除了快照、备份及对应的存储包；以下候选分析仅保留作当时的决策记录，不代表当前结构。

## 背景

对 charon 代码库进行了一次架构深化评审（architecture deepening review），
识别出 4 个候选重构方向，并逐一对每个候选进行了 grilling 和 peer review（通过 agy 工具）。

## 决策

**全部四个候选均被驳回，不做重构。**

## 候选及驳回理由

### 候选 1：Tool Describe 结构性重复

**判定**：驳回

四个工具的 `Describe()` 方法各自解析不同格式的配置文件（Codex 用 TOML，
Claude 用 JSON + keychain，OpenCode 用 JSON + agent fallback，Pi 用 JSON + regexp 扩展解析）。
虽然每个方法都遵循"读文件 → 解析 → 提取字段"的模式，但每个工具的提取逻辑是**真正不同**的，
不是代码组织问题。一个 `ConfigExtractor` 接口只会增加仪式感，不会减少每个工具需要的代码量。

### 候选 2：Store 单体结构

**判定**：驳回

`Store` 结构体虽然有 30+ 个方法，但已经在 5 个文件中按操作类型组织良好（`store.go` 生命周期/配置，
`snapshot.go` 捕获，`apply.go` 恢复，`backup.go` 安全，`manage.go` 增删改查）。
锁已经通过 build tag 分离到独立文件（`lock_unix.go` / `lock_other.go`），有 depth counter 确保可重入。
配置（active profile + OAuth fingerprint）只有 ~80 行代码。提取成独立类型需要修改 15+ 个调用点，
且不带来任何行为收益。成本为负。

### 候选 3：TUI model 中心化

**判定**：驳回

TUI 的 `model` 结构体虽然包含 30 个字段，`Update()` 方法是一个 100 行的 switch，但 bubbletea
框架天然鼓励这种模式。表单逻辑（462 行）和取模型逻辑（221 行）已经分离到独立文件
（`wizard.go` / `picker.go`）。提取子模型（`FormModel`、`PickerModel`）只是横向移动代码，
不改变本质复杂度。如果未来 TUI 继续增长，可以重新审视这个方向。

### 候选 4：edit.go 浅层辅助函数

**判定**：驳回

`edit.go` 提供的 4 个辅助函数（`loadJSONMap`、`writeJSONMap`、`loadTOMLMap`、`writeTOMLMap`）
是约 80 行的薄包装，稳定且有测试覆盖。只在工具配置被写入时（`ApplyAuth`）调用。
低优先级，不做清理。

## 架构评估

目前的代码库架构是健康的：

- **分层清晰**：`secret` ← `artifact` ← `tools` ← `profile` ← `cmd` / `tui`
- **模块深度好**：每个模块的接口都隐藏了实现复杂度（删除测试通过）
- **测试覆盖率高**：`internal/artifact`、`internal/profile`、`internal/tools` 测试完善
- **安全保证到位**：原子写入、`0600` 权限、每次切换前自动备份

## 后续建议

- 如果未来添加新的 tool 适配器，继续遵循 `internal/tools/<tool>.go` 的单文件模式
- 如果 TUI 继续增长到超过 3000 行，可以重新考虑子模型提取
- 此 ADR 的存在意味着未来的架构评审不应重复提出这些候选，除非上下文发生重大变化
