<h1 align="center">Charon</h1>

<p align="center">
  <em>在多个 endpoint 之间摆渡你的 AI 工具。</em>
</p>

<p align="center">
  <a href="https://github.com/Administration-626/charon/releases/latest"><img src="https://img.shields.io/github/v/release/Administration-626/charon?style=flat-square&color=6c47ff" alt="Latest Release"></a>
  <a href="https://github.com/Administration-626/charon/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/Administration-626/charon/ci.yml?branch=main&style=flat-square&label=CI" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Administration-626/charon?style=flat-square" alt="MIT License"></a>
  <a href="https://github.com/Administration-626/charon/issues"><img src="https://img.shields.io/github/issues/Administration-626/charon?style=flat-square" alt="Open Issues"></a>
</p>

<p align="center">
  <a href="README.md">English</a> · <b>简体中文</b>
</p>

Charon 是一个用 Go 编写的小型 CLI，能够检测 **Codex**、**Claude Code**、
**OpenCode** 与 **Pi** 这几个 CLI，并在命名的配置（profile）之间切换各自的
**endpoint + 凭据**。每个 profile 都是工具鉴权状态的完整快照，因此既适用于
API-key 登录，也适用于 OAuth/ChatGPT 会话——切换出去再切回来始终干净、可逆。

<p align="center">
  <img src="https://raw.githubusercontent.com/Administration-626/charon/main/assets/screenshot.png" alt="Charon interactive menu" width="80%">
</p>

## 功能

- **一条命令，四个工具。** 用一个交互式菜单或可脚本化的 CLI 管理 Codex、Claude
  Code、OpenCode 和 Pi。
- **命名 profile。** 对工具完整的鉴权状态做快照，在 endpoint/key 之间即时切换。
- **模型发现。** 只给一个 endpoint + key 就能添加 profile；Charon 会拉取模型列表
  供你挑选——也可以直接手输自定义模型 slug。
- **单页表单。** 在同一屏完成 profile 的新增或编辑（Name、URL、Token、Model），
  直接键入，配 `[ Save Profile ]` / `[ Cancel ]` 按钮——没有多步向导、不跳页。
- **即时克隆与搜索。** 按 `c` 无需提示即可复制 profile；在模型列表里直接打字做
  实时模糊过滤。
- **Unicode 名称。** profile 名接受任意脚本的字母与数字，中文名（及其他非 ASCII 名）
  原生可用。
- **默认安全。** 每次切换前都先备份，写入是原子的，并自动捕获一个 `default`
  profile，让你随时可以回滚。
- **非破坏性。** Charon 只会动每个工具配置里它自己的 `charon` provider 条目，
  绝不碰你手写的 provider。

## 支持的工具

| 工具 | Endpoint | 凭据 |
|------|----------|------|
| **Codex** | `~/.codex/config.toml`（`model_provider` → `base_url`） | `~/.codex/config.toml`（`experimental_bearer_token`） |
| **Claude Code** | `~/.claude/settings.json`（`env.ANTHROPIC_BASE_URL`） | `settings.json` 的 env key |
| **OpenCode** | `~/.config/opencode/opencode.jsonc`（`provider.*.options.baseURL`） | `opencode.jsonc`（`provider.charon.options.apiKey`） |
| **Pi** | `~/.pi/agent/extensions/charon.ts`（`baseUrl`） | `~/.pi/agent/extensions/charon.ts`（`apiKey`） |

## 安装

### curl（Linux）

无需 Go——下载对应平台的预编译二进制、校验 checksum，并安装到 `~/.local/bin`：

```sh
curl -fsSL https://github.com/Administration-626/charon/releases/latest/download/install.sh | sh
```

> 前置 `PREFIX=/usr/local` 可装到系统目录；前置 `VERSION=v1.2.3` 可锁定某个发行版。

<details>
<summary><b>其它方式</b> · 手动二进制 · 从源码构建</summary>

**预编译二进制** —— 从 [Releases 页面](https://github.com/Administration-626/charon/releases/latest)
下载你平台对应的压缩包（`charon_linux_{amd64,arm64}.tar.gz`），并对照随包
附带的 `checksums.txt` 校验：

```sh
curl -L https://github.com/Administration-626/charon/releases/latest/download/charon_linux_amd64.tar.gz | tar xz
sudo mv charon /usr/local/bin/
```

**从源码构建** —— 需要 Go 1.24+：

```sh
make install                      # 构建 + 安装到 ~/.local/bin（PREFIX 可覆盖）
go build -o charon ./cmd/charon   # 或只在本目录构建
```

</details>

## 用法

### 交互式菜单

不带参数运行 `charon` 即可打开方向键菜单：选一个工具，再切换、添加、编辑或删除
profile。随时按 `ctrl+c` 退出。

### CLI 参考

```sh
charon                       # 交互式方向键菜单
charon status                # 显示各工具当前 profile、endpoint 与鉴权（--json）
charon ls <tool>             # 列出已保存的 profile（--json）
charon save <tool> [name]    # 对当前实时配置做快照（省略 name 则用登录账号命名）
charon refresh <tool>        # 把会话内的变更（model、effort）写回当前 profile
charon models <tool>         # 列出某个 API 提供的模型（--key [--endpoint]）
charon add <tool>            # 添加并激活一个 profile（--name --key [--endpoint --model]）
charon edit <tool> <p>       # 修改某 profile 的 endpoint/key/model（--name 可改名）
charon rename <tool> <o> <n> # 重命名已保存的 profile
charon cp <tool> <src> <dst> # 复制已保存的 profile
charon switch <tool> <p>     # 应用某 profile（先备份当前配置）
charon restore <tool>        # 回到自动捕获的 default
charon undo <tool>           # 回到最近一次切换前的备份
charon prune <tool>          # 删除旧备份，保留最新的（--keep N，默认 10）
charon rm <tool> <p>         # 删除一个 profile
charon completion <shell>    # 打印 bash/zsh/fish 补全脚本
charon update                # 升级 charon 到最新发行版
charon uninstall             # 卸载已安装的 charon 二进制
```

`status` 和 `ls` 支持 `--json`，便于脚本与编辑器集成。`status` 还会在某个工具的
实时配置被 Charon 之外改动过（例如刚执行过 `claude login`）时，标记
**`(modified)`**，让过期的当前 profile 一目了然。

### Shell 补全

 补全脚本随发行版压缩包提供。手动启用方式：

```sh
# bash —— 加到 ~/.bashrc
source <(charon completion bash)
# zsh —— 加到 ~/.zshrc（确保运行了 compinit）
source <(charon completion zsh)
# fish
charon completion fish | source
```

补全覆盖子命令、工具名，以及 `switch`/`edit`/`rename`/`cp`/`rm` 的已保存
profile 名。

## 添加与编辑 profile

### 从 endpoint + key（带模型发现）

在菜单里进入某个工具，选 **＋ Add new profile…**（或按 `a`）。一个单页表单在同屏
收集所有信息：

- **Name** —— profile 名（任意脚本的字母/数字；Unicode 亦可）。
- **API base URL** —— 留空即采用该 provider 的默认值；真实值永远不会预填。
- **API key** —— 掩码输入。
- **Model** —— **Fetch** 按钮会请求 `GET /v1/models`（OpenAI 系 API 用
  `Authorization: Bearer`，Anthropic 用 `x-api-key`）并打开模型选择器。你也可以
  直接手输自定义 slug，或留空采用工具默认值。

在模型选择器里，**直接打字即可实时模糊过滤**列表（Backspace 编辑查询，`Esc`
清空）。Tab 到 **`[ Save Profile ]`** 即把 endpoint/key/model 写入工具实时配置并
切换，或 **`[ Cancel ]`** 放弃。Name、URL、key 为必填；提交时如有缺失会在底部
红色状态栏标出。

### 备份已登录账号

已经用真实账号登录了 Codex 或 Claude Code？Charon 可以对该会话做快照，并**自动用
账号邮箱命名 profile**：

```sh
codex login              # 用工作账号登录
charon save codex        # → 保存并激活 profile "you@work.com"

codex login              # 用第二个账号登录
charon save codex        # → 保存并激活 profile "you@personal.com"

charon switch codex you@work.com   # 即刻切回
```

邮箱读自工具自身的配置——Codex 的 `id_token`、Claude Code 的 `~/.claude.json`——
仅用于命名 profile；该文件只读、绝不修改。这类登录备份**不可编辑**（没有
endpoint/key 可改）；重新执行 `charon save` 会刷新快照。API-key 登录没有账号，所以
`charon save` 仍需显式指定 name。

在菜单里按 **`c`** 可即时**克隆**某 profile 为 `<name>-copy`，无任何提示——焦点跳到
新副本，它是正常的可编辑 profile，可改名、编辑、删除。

### 编辑已有 profile

在某 profile 上按 **`e`** 打开编辑表单，同屏显示当前的 **Name**、**URL**、
**Token**（掩码）与 **Model**。直接在任意字段里键入；**Model** 字段的 **Fetch**
按钮会重新拉取该 endpoint 的模型列表供你挑选（或手输 slug）。Tab 到
**`[ Save Profile ]`** 应用改动并切换到该 profile（改名自动处理），或
**`[ Cancel ]`** 放弃。自动捕获的 **`default`** profile 与登录备份（无
endpoint/key）受保护，不可编辑。

### 非交互式

```sh
charon models codex --endpoint https://openrouter.ai/api/v1 --key sk-...
charon add    codex --name openrouter --endpoint https://openrouter.ai/api/v1 \
                    --key sk-... --model openai/gpt-5.5
```

每个工具都会在它自己的配置格式里写入一个专属的 `charon` provider 条目
（Codex 的 `[model_providers.charon]`、Claude 的 `env.ANTHROPIC_*`、OpenCode 的一个
`@ai-sdk/openai-compatible` provider、Pi 的 `pi.registerProvider("charon", ...)`
扩展），所以切走再切回都很干净。

典型流程：正常登录某工具，`charon save codex work-key`；换一个 endpoint/key 登录，
`charon save codex proxy`；然后用 `charon switch codex work-key` 在两者间切换——
或直接运行 `charon` 从菜单里选。`restore` 始终回到 Charon 首次运行时捕获的原始配置。

## 工作原理

- **存储：** `~/.config/charon/`（遵循 `$XDG_CONFIG_HOME`）。
  - `profiles/<tool>/<name>/` —— 快照文件 + `manifest.json`。
  - `backups/<tool>/<timestamp>/` —— 每次切换、添加或 undo 前自动备份。`charon undo`
    回到最新的；每个工具保留最近 10 份（可用 `charon prune <tool> --keep N` 调整）。
  - `config.json` —— 每个工具的当前 profile。
- **`default`** 在检测到某工具时自动捕获，因此随时可回滚，且永不被覆盖。
- 写入是**原子**的（临时文件 → `rename`）。

## 安全

profile 以**未加密**形式存于磁盘（文件 `0600`，目录 `0700`——目录上的 `x` 位表示"进入"而非"执行"，所以 `0700` 才是正确的做法）。这与各工具自身所用的权限模型一致
（`~/.codex/config.toml`、`~/.claude/settings.json` 等）——如果攻击者能读
`~/.config/charon`，那也能读这些文件。相比之下，**shell 配置文件**
（`~/.bashrc`、`~/.zshrc`）默认权限为 `0644`（所有人可读），把 API key 存在那里
要危险得多。请保持 `~/.config/charon` 私有。写入是**原子**的（临时文件 → `rename`）。

## 项目结构

```
cmd/charon/          入口 + 子命令
internal/artifact/   快照/恢复原语（Artifact 接口及其实现）
internal/tools/      各工具适配器（codex、claude、opencode、pi）
internal/profile/    快照存储（按关注点拆分：snapshot、apply、backup、manage）
internal/models/     从 provider API 拉取模型列表（openai/anthropic 协议）
internal/tui/        bubbletea 交互式菜单（单页表单、模糊模型搜索）
internal/secret/     掩码 + 平台 keychain 访问
```

## 开发

```sh
make build   # 构建 ./charon
make test    # go vet + go test -race ./...
make cover   # 覆盖率概览
make lint    # golangci-lint run
make fmt     # gofmt -w .
make run     # 构建 + 交互式菜单（先 sandbox HOME！）
```

CI（`.github/workflows/ci.yml`）在 Linux 上运行格式检查、vet、race 测试、
构建与 golangci-lint。贡献者与 Agent 约定（包括**测试时务必 sandbox `HOME` 以免触碰
真实凭据**这条规则）见 [AGENTS.md](AGENTS.md)。

## 路线图

- 可选的 `--verify`：切换后做鉴权 ping 以确认凭据确实可用。
- 支持更多 AI CLI 工具。

## 贡献

**欢迎提 PR 和 issue。** 这是个早期项目，还有大量成长空间——你的想法和 bug 报告会
切实影响它下一步的方向。

- 🐛 **发现 bug？** [提个 issue](https://github.com/Administration-626/charon/issues/new)，附上工具名、系统、期望与实际行为。
- 💡 **有想法？** [开个讨论](https://github.com/Administration-626/charon/issues/new)——新工具支持、UX 调整，什么都行。
- 🔧 **提交修复或功能？** Fork → branch → PR。推送前跑 `make fmt && make test`。约定见 [AGENTS.md](AGENTS.md)。

再小的贡献也欢迎——改个错别字和加个功能同样感激。

## 许可证

基于 [MIT License](LICENSE) 发布。
