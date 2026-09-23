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
**OpenCode** 与 **Pi** 这几个 CLI，并在命名的绑定（binding）之间切换各自的
**endpoint + 凭据**。一条绑定就是一个 endpoint、一把 key，以及它要提供的模型，
不是工具配置的快照。切换时把这条绑定重新渲染进工具，只覆盖 charon 自己的键。

<p align="center">
  <img src="https://raw.githubusercontent.com/Administration-626/charon/main/assets/screenshot.png" alt="Charon interactive menu" width="80%">
</p>

## 功能

- **一条命令，四个工具。** 用一个交互式菜单或可脚本化的 CLI 管理 Codex、Claude
  Code、OpenCode 和 Pi。
- **命名绑定。** 保存一个 endpoint、一把 key 和它提供的模型，然后在它们之间切换。
  同一个站点可以同时给多个工具用。
- **模型发现。** 给一个 endpoint + key 就能添加绑定；Charon 会拉取模型列表供你挑选，
  也可以直接手输模型 id。
- **在工具内切换模型。** 勾选你常用的那几个模型，Charon 会把它们注册进工具自身的
  选择器（Claude Code 的 `/model`、OpenCode 的 `/models`、Pi 的 `/model`），会话中
  换模型不必再回到 Charon。
- **单页表单。** 在同一屏完成绑定的新增或编辑（Name、URL、Token、Model），
  直接键入，配 `[ Save ]` / `[ Cancel ]` 按钮。
- **即时克隆与搜索。** 按 `c` 无需提示即可复制绑定；在模型列表里直接打字做
  实时模糊过滤。
- **Unicode 名称。** 绑定名支持中文等文字；不允许空白或控制字符。
- **默认安全。** 写入是原子的。正在使用的绑定不能删除——先切到另一条。
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
绑定。随时按 `ctrl+c` 退出。

### CLI 参考

```sh
charon                       # 交互式方向键菜单
charon status                # 显示各工具的实时配置与当前绑定（--json）
charon ls <tool>             # 列出已保存的绑定（--json）
charon models <tool>         # 列出某个 API 提供的模型（--key [--endpoint]）
charon add <tool>            # 添加并激活一条绑定（--name --key，并提供至少一个模型 id）
charon edit <tool> <b>       # 修改某绑定的 endpoint/key/model/models（--name 可改名）
charon rename <tool> <o> <n> # 重命名已保存的绑定
charon cp <tool> <src> <dst> # 复制已保存的绑定
charon switch <tool> <b>     # 把已保存的绑定渲染进工具
charon rm <tool> <b>         # 删除一条绑定（正在使用的不能删）
charon completion <shell>    # 打印 bash/zsh/fish 补全脚本
charon update                # 升级 charon 到最新发行版
charon uninstall             # 卸载已安装的 charon 二进制
```

`status` 和 `ls` 支持 `--json`，便于脚本与编辑器集成。`status` 读工具的实时配置，
`ls` 读已保存的绑定。在工具里用 `/model` 改的模型，下次重新渲染这条绑定时会被覆盖。

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
绑定名。

## 添加与编辑绑定

### 从 endpoint + key（带模型发现）

在菜单里进入某个工具，选 **＋ Add new binding…**（或按 `a`）。一个单页表单在同屏
收集所有信息：

- **Name** —— 绑定名（任意文字；Unicode 亦可）。
- **API base URL** —— 留空即采用该 provider 的默认值；真实值永远不会预填。
- **API key** —— 掩码输入。
- **Model**：字段下方提供两个操作按钮：
  - **`[ Fetch & Pick Online Models ]`**：请求 `GET /v1/models`（OpenAI 系 API 用
    `Authorization: Bearer`，Anthropic 用 `x-api-key`）并打开模型选择器。
  - **`[ Type Model IDs Manually ]`**：直接手动输入模型 ID（逗号分隔，如
    `kimi-k2, deepseek-v3`），适合未暴露 `/v1/models` 的中转站或私有网关。若在线
    拉取失败，Charon 也会自动降级到该手动录入界面并预填已有 ID。
  你也可以直接在该输入框键入 slug。一条绑定至少要有一个模型 id。

在模型选择器里，**直接打字即可实时模糊过滤**列表（Backspace 编辑查询，`Esc` 清空）：
- **`Space`**：勾选或取消勾选某个模型（标记为 `•`），加入注册给工具的候选列表。
- **`Ctrl+A`**：全选或取消全选所有模型（处于搜索过滤状态时仅作用于匹配结果）。
- 勾选模型后，列表顶部会出现 **`✔ Done — register these N model(s)`** 选项（副标题显示当前默认模型）。在 Done 行按 **`Enter`** 即可确认并返回表单，无需改动默认模型。
- 在任意模型行按 **`Enter`**：将其设为默认模型（标记为 `✓`，若尚未勾选会自动加入列表），并返回表单。
- 若一个都不勾选直接选定默认模型，整份拉取到的列表都会被注册给工具。
Tab 到 **`[ Save ]`** 即把 endpoint/key/model 写入工具实时配置并切换，或
**`[ Cancel ]`** 放弃。Name、key 和至少一个模型为必填；URL 留空会使用工具默认值。
提交时如有缺失会在底部红色状态栏标出。

### 在工具内切换模型

绑定注册的模型会出现在工具自身的菜单里，不必离开当前会话即可换模型：Claude Code
的 `/model`（走 `modelPicker`）、OpenCode 的 `/models`（走 `charon` provider 的模型
映射）、Pi 的 `/model`（走生成的扩展）。列表跟着绑定走——切换绑定时菜单也随之切换。
只有一个模型的绑定会把工具内列表替换为这一个模型，不沿用上一条绑定的列表。Codex 是
例外：它的配置里没有注册额外模型的位置，所以一条 Codex 绑定只带一个模型
（`charon edit codex <b> --model ...`）。
在工具内更改模型不会更新 Charon 保存的绑定；下次重新渲染这条绑定时，会重新应用其保存的
默认模型。

在菜单里按 **`c`** 可即时**克隆**某绑定为 `<name>-copy`，无任何提示——焦点跳到
新副本。副本共用同一把 key 和同一份模型列表，且不会被激活。

### 编辑已有绑定

在某绑定上按 **`e`** 打开编辑表单，同屏显示当前的 **Name**、**URL**、
**Token**（掩码）与 **Model**。直接在任意字段里键入；**Model** 字段下的
**`[ Fetch & Pick Online Models ]`** 与 **`[ Type Model IDs Manually ]`**
按钮支持重新拉取或手动调整模型。绑定中保存的模型列表除非你重新调整，否则保持不变，改名
或换 key 都会保留这条绑定自己的选择器。Tab 到 **`[ Save ]`** 应用改动（改名自动处理），或
**`[ Cancel ]`** 放弃。编辑一条非当前绑定只改目录，实时配置要等切换过去才变。
按 **`d`** 删除；正在使用的绑定会拒绝删除，先切走。

### 非交互式

```sh
charon models codex --endpoint https://openrouter.ai/api/v1 --key sk-...
charon add    codex --name openrouter --endpoint https://openrouter.ai/api/v1 \
                    --key sk-... --model openai/gpt-5.5
```

`--models` 会把一份列表注册进工具自身的选择器，之后就能在工具内部切换。省略
`--model` 时，列表里的第一个 id 会作为默认模型；列表随绑定存储，后续 `edit`
不传 `--models` 时保持不变：

```sh
charon add  claude --name gateway --endpoint https://gateway.example/v1 --key sk-... \
                   --models kimi-k2,glm-4.6,deepseek-v3
charon edit claude gateway --key sk-rotated   # 选择器列表原样保留
charon edit claude gateway --models glm-4.6   # 把菜单收窄
```

编辑时，如果 `--models` 不含当前默认模型，列表第一项会成为新的默认模型。
可用 `--model` 显式指定默认模型；该模型也会保留在选择器列表中。

每个工具都会在它自己的配置格式里写入一个专属的 `charon` provider 条目
（Codex 的 `[model_providers.charon]`、Claude 的 `env.ANTHROPIC_*`、OpenCode 的一个
`@ai-sdk/openai-compatible` provider、Pi 的 `pi.registerProvider("charon", ...)`
扩展），所以切走再切回都很干净。

典型流程：`charon add codex --name work-key --key sk-... --model gpt-5`，再
`charon add codex --name proxy --endpoint https://gateway.example/v1 --key sk-...
--model glm-4.6`，然后用 `charon switch codex work-key` 切换——或直接运行 `charon`
从菜单里选。

## 工作原理

- **存储：** `~/.config/charon/`（遵循 `$XDG_CONFIG_HOME`）。五个 JSON 文件，没有数据库：
  - `providers.json` —— 一个站点：id、base URL。多个工具共用。
  - `credentials.json` —— 某站点的一把 key。权限 `0600`。
  - `models.json` —— 某站点的一个模型 slug。
  - `bindings.json` —— 某工具的一条保存项：名字、凭据、默认模型，以及选择器要提供的模型 id。
  - `active.json` —— 每个工具当前渲染的是哪条绑定。
- 绑定的模型必须和它的 key 属于同一个站点。Codex 只能带一个模型。
- 切换会更新 `active.json` 并重新渲染该绑定。只覆盖 charon 自己的键。
- 写入是**原子**的（临时文件 → `rename`）。
- 首次打开时，会把旧版中保存了 endpoint、key 和 model 的可编辑档案导入为绑定；旧档案目录
  保留不动。只有配置快照的档案、备份和 OAuth 登录不会导入。

## 安全

绑定以**未加密**形式存于磁盘（文件 `0600`，目录 `0700`——目录上的 `x` 位表示"进入"而非"执行"，所以 `0700` 才是正确的做法）。这与各工具自身所用的权限模型一致
（`~/.codex/config.toml`、`~/.claude/settings.json` 等）——如果攻击者能读
`~/.config/charon`，那也能读这些文件。相比之下，**shell 配置文件**
（`~/.bashrc`、`~/.zshrc`）默认权限为 `0644`（所有人可读），把 API key 存在那里
要危险得多。请保持 `~/.config/charon` 私有。写入是**原子**的（临时文件 → `rename`）。
数据不会离开这台机器。

## 项目结构

```
cmd/charon/          入口 + 子命令
internal/artifact/   原子写入
internal/tools/      各工具适配器（codex、claude、opencode、pi）
internal/catalog/    provider、凭据、模型、绑定、当前指针
internal/models/     从 provider API 拉取模型列表（openai/anthropic 协议）
internal/tui/        bubbletea 交互式菜单（单页表单、模糊模型搜索）
internal/secret/     掩码
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
