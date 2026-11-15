# 已撤回试验的工程走查留档：Pi 独立 key 停供

检查日期：2026-11-11。对象是仓库 `/home/tan/charon` 当时未提交的工作树，Git HEAD `305beab`。

> **状态：已被用户否决，相关实现已精准撤回。** 本文件只保存一次工程走查的历史结论，不是当前产品合同，也不能作为重新引入该功能的依据。当前产品结论见[规格](spec.md)与[新 session 接棒](next-session-handoff.md)。

## 1. 当时试过的设计

需求样本是“停止向 Pi 提供某把已保存的 key”，并尝试了：

- 在 Pi 绑定列表按 `p`，选择 Disable key／Enable key，再进入确认页；
- 辅助 CLI `charon disable-key pi <binding>`／`charon enable-key pi <binding>`；
- 在 `Credential` 上保存 `PiDisabled`，按 key 字面值跨 Pi binding／provider 生效；
- 在投影、激活、编辑、stash 恢复与标头处理中加入政策屏障；
- 清理失败时保持禁用并在 TUI 结果页重试，恢复不自动 apply／activate。

这些实现曾通过按键、渲染、临时配置字节比较与 80×24 PTY 自动验证，证据保存在 `/tmp/charon-tui-key-_zn2u3tt/`。自动测试通过不等于产品被接受。

## 2. 为什么否决

用户试用后明确拒绝“`h` 隐藏某条方案 + `p` 独立停供 key + 恢复后还要 apply”的双层设计，表示宁可不实现也不接受它。随后确认改为按 endpoint 地址把隐藏合并到 `h`：同地址的 Pi 方案一起隐藏／显示，不同地址即使共用同一把 key 也互不连带，隐藏／显示立即更新托管 provider 且不切换或重置模型。

因此，独立 key 政策不是隐藏的替代实现，也不应继续留在后台。仅隐藏菜单而保留政策与屏障同样不符合决定。

## 3. 撤回范围

已删除：

- `Credential.PiDisabled`、`AuthSpec.PiDisabledKeys` 及新凭据继承标记；
- `disable-key`／`enable-key` 的 CLI dispatch、usage、shell completion 与专用测试；
- `p` 菜单、确认页、失败结果页、`[keyDisabled]`／`[keyUnknown]` 标记与专用 UI 状态；
- 投影、active 编辑、激活、stash、Pi 聚合与标头中的政策屏障；
- `SetPiKeyDisabled`、`PreparePiKeyRemoval`、`CheckPiStashKeys` 等仅服务该试验的 helper 与测试；
- README、AGENTS、spec 与 codebase 文档中把该版称为有效产品的内容。

保留：

- 原有 `h` 隐藏入口与隐藏复制修复；
- header 存储／投影／继承，格式保留编辑器与差分／fuzz／off 测试；
- `cmdEdit --help`／参数错误不泄露保存 key 的独立安全修复；
- 其他与本试验无关的工作树改动与既有 CLI 支持。

## 4. 历史证据与边界

- 被否决实现的完整阶段证据：`/tmp/charon-tui-key-_zn2u3tt/`（源码拷贝、before-hashes、变异、PTY 脚本与原始记录）。
- 帮助泄露修复证据：`/tmp/charon-help-fix-di397ba1/`。
- 隐藏复制修复证据：`/tmp/charon-tui-copy-6ut6dpsl/`。
- 试验前工作树补丁：`/tmp/charon-before-pi-disable.patch`（可在临时副本重建比较，不应覆盖当前工作树）。
- 撤回前完整快照：`/tmp/charon-endpoint-h-uihawoin/`。

这些是本地临时证据，不是仓库依赖，也不是对当前工作树全部行为的保证。该历史快照中按 endpoint 分组的新 `h` 尚未实现；当前行为与验收见[规格](spec.md) 8.2，原始方案与回退边界保留在[接棒文档](next-session-handoff.md)。
