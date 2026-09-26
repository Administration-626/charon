<h1 align="center">Charon</h1>

<p align="center">
  <em>在多个 endpoint 之间切换 AI 工具的配置。</em>
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

Charon 是一个用 Go 编写的 CLI，可检测 Codex、Claude Code、OpenCode、
Pi、Oh My Pi（omp）与 Grok，并在命名绑定（binding）之间切换各工具使用的
endpoint 和凭据。
一条绑定包含 endpoint、API key 和可供工具选择的模型，不保存工具配置中的其他内容。
切换绑定时，Charon 会将绑定中的设置写入工具配置，只修改由 Charon 管理的配置项。

<p align="center">
  <img src="https://raw.githubusercontent.com/Administration-626/charon/main/assets/screenshot.png" alt="Charon interactive menu" width="80%">
</p>

## 功能

- 一条命令，六个工具。用一个交互式菜单或可脚本化的 CLI 管理 Codex、Claude
  Code、OpenCode、Pi、Oh My Pi（omp）和 Grok。
- 命名绑定。保存 endpoint、API key 和该服务提供的模型，然后在不同绑定之间切换。
  同一个 endpoint 可供多个工具使用。
- 获取模型列表。提供 endpoint 和 API key 即可添加绑定；Charon 会获取模型列表供你选择，
  也可以手动输入模型 ID。
- 在工具内切换模型。勾选你常用的那几个模型，Charon 会把它们注册进工具自身的
  选择器（Claude Code 的 `/model`、OpenCode 的 `/models`、Pi 的 `/model`、Oh My Pi 的
  `/model`、Grok 的 `/model`），可以在会话中更换模型，无需返回 Charon。
- 单页表单。在同一屏新增或编辑绑定（Name、URL、Token、Model），可直接输入，
  并使用 [ Save ] / [ Cancel ] 按钮。
- 快速复制与搜索。按 c 复制绑定，按 x 复制到其他工具；在模型列表中输入文字即可实时模糊搜索。
- Unicode 名称。绑定名支持中文等文字；不允许空白或控制字符。
- 默认安全。写入采用原子替换。正在使用的绑定不能删除；删除前须切换到另一条绑定。
- 非破坏性。Charon 只修改每个工具配置中由它管理的 `charon` provider 条目，
  绝不碰你手写的 provider。

## 支持的工具

| 工具 | Endpoint | 凭据 |
|------|----------|------|
| Codex | `~/.codex/config.toml`（`model_provider` → `base_url`） | `~/.codex/config.toml`（`experimental_bearer_token`） |
| Claude Code | `~/.claude/settings.json`（`env.ANTHROPIC_BASE_URL`） | `settings.json` 的环境变量键 |
| OpenCode | `~/.config/opencode/opencode.jsonc`（`provider.*.options.baseURL`） | `opencode.jsonc`（`provider.charon.options.apiKey`） |
| Pi | `~/.pi/agent/extensions/charon.ts`（`baseUrl`） | `~/.pi/agent/extensions/charon.ts`（`apiKey`） |
| Oh My Pi（omp） | `~/.omp/agent/models.yml`（`providers.charon.baseUrl`） | `~/.omp/agent/models.yml`（`providers.charon.apiKey`） |
| Grok | `~/.grok/config.toml`（`[model.charon-*].base_url`） | `~/.grok/config.toml`（`[model.charon-*].api_key`） |

## 安装

### curl

无需安装 Go。安装脚本会下载对应平台的预编译二进制，校验 checksum，并安装到 `~/.local/bin`：

```sh
curl -fsSL https://github.com/Administration-626/charon/releases/latest/download/install.sh | sh
```

> 设置 `PREFIX=/usr/local` 可安装到系统目录；设置 `VERSION=v1.2.3` 可指定发行版。

<details>
<summary><b>其他方式</b>：下载二进制文件或从源码构建</summary>

预编译二进制 —— 从 [Releases 页面](https://github.com/Administration-626/charon/releases/latest)
下载你平台对应的压缩包（`charon_linux_{amd64,arm64}.tar.gz`），并对照随包
附带的 `checksums.txt` 校验：

```sh
curl -L https://github.com/Administration-626/charon/releases/latest/download/charon_linux_amd64.tar.gz | tar xz
sudo mv charon /usr/local/bin/
```

从源码构建 —— 需要 Go 1.24+：

```sh
make install                      # 构建 + 安装到 ~/.local/bin（PREFIX 可覆盖）
go build -o charon ./cmd/charon   # 或只在本目录构建
```

</details>

## 用法

### 交互式菜单

不带参数运行 `charon` 即可打开菜单：选择一个工具，再切换、添加、编辑或删除
绑定。随时按 `ctrl+c` 退出。

### CLI 参考

```sh
charon                       # 交互式菜单
charon status                # 显示各工具的实时配置与当前绑定（--json）
charon ls <tool>             # 列出已保存的绑定（--json）
charon models <tool>         # 列出某个 API 提供的模型（--key [--endpoint]）
charon add <tool>            # 添加并激活一条绑定（--name --key，并提供至少一个模型 id）
charon edit <tool> <b>       # 修改某绑定的 endpoint/key/model/models（--name 可改名）
charon rename <tool> <o> <n> # 重命名已保存的绑定
charon cp <tool> <src> <dst> # 复制已保存的绑定
charon cp <tool> <src> <tool> <dst> # 将已保存的绑定复制到另一个工具
charon switch <tool> <b>     # 将已保存的绑定应用到工具配置
charon rm <tool> <b>         # 删除一条绑定（正在使用的不能删）
charon completion <shell>    # 输出 bash/zsh/fish 补全脚本
charon update                # 升级 charon 到最新发行版
charon uninstall             # 卸载已安装的 charon 二进制
```

`status` 和 `ls` 支持 `--json`，便于脚本与编辑器集成。`status` 读工具的实时配置，
`ls` 读已保存的绑定。在工具里通过 `/model` 更改的模型，只要这条绑定仍是当前绑定就会保留；
应用另一条绑定时会替换模型列表。

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

### 从 endpoint + key 获取模型列表

在菜单里进入某个工具，选择 ＋ Add new binding…（或按 a）。单页表单会显示所有字段：

- Name：绑定名（支持 Unicode 文字）。
- API base URL：留空即采用该 provider 的默认值；Charon 不会预填真实地址。
- API key：输入内容会隐藏。
- Model：字段下方提供两个操作按钮：
  - [ Fetch & Pick Online Models ]：请求 `GET /v1/models`（OpenAI 兼容 API 使用
    `Authorization: Bearer`，Anthropic 用 `x-api-key`）并打开模型选择器。
  - [ Type Model IDs Manually ]：手动输入模型 ID，多个 ID 用逗号分隔，例如
    `kimi-k2, deepseek-v3`。适用于未提供 `/v1/models` 的中转服务或私有网关。在线获取失败时，
    Charon 会打开手动输入界面，并填入已有 ID。
  也可以直接在该输入框输入模型 slug。每条绑定至少需要一个模型 ID。

在模型选择器中，输入文字即可实时模糊搜索列表（Backspace 修改搜索内容，Esc 清空）：
- Space：勾选或取消勾选模型。已选模型旁显示 •，并会注册到工具的模型菜单中。
- Ctrl+A：全选或取消全选所有模型（处于搜索过滤状态时仅作用于匹配结果）。
- 勾选模型后，列表顶部会出现 Done — register these N model(s) 选项，副标题显示当前默认模型。选中该行并按 Enter 可确认选择并返回表单，默认模型保持不变。
- 在模型行按 Enter 可将该模型设为默认模型；若尚未勾选，该模型会自动加入注册列表。默认模型会显示勾号。
- 如果没有勾选模型，工具会注册获取到的全部模型。

按 Tab 选中 [ Save ] 并保存后，Charon 会将 endpoint、key 和 model 写入工具当前配置并激活该绑定。选择 [ Cancel ] 可放弃修改。Name、key 和至少一个模型为必填项；URL 留空时使用工具默认值。必填项缺失时，底部状态栏会显示提示。

### 在工具内切换模型

绑定中保存的模型会出现在工具自身的菜单中，因此可以在当前会话内更换模型：Claude Code
的 `/model`（通过 `modelPicker`）、OpenCode 的 `/models`（读取 `charon` provider 的模型
映射）、Pi 的 `/model`（由 Charon 生成的扩展）、Oh My Pi 的 `/model`（读取
`providers.charon` 的模型列表）以及 Grok 的 `/model`（一组 `[model.charon-<slug>]`
表）。切换绑定时，工具中的模型列表也会更新。
只有一个模型的绑定会将工具中的模型列表替换为该模型，不会保留上一条绑定的列表。Codex 是
例外：它的配置里没有注册额外模型的位置，所以一条 Codex 绑定只带一个模型
（`charon edit codex <b> --model ...`）。
在工具内更改模型不会更新 Charon 保存的绑定；只要这条绑定仍是当前绑定，Charon 会保留实况模型。
应用另一条绑定时会替换模型列表。

在菜单里按 c 可直接复制绑定，并命名为 `<name>-copy`；按 x 可选择其他工具并复制为
`<name>-copy`。副本与原绑定共用 API key，创建后不会自动激活。

### 编辑已有绑定

在某绑定上按 e 打开编辑表单，查看当前的 Name、URL、
Token（内容隐藏）与 Model。可直接修改任意字段；Model 字段下的
[ Fetch & Pick Online Models ] 与 [ Type Model IDs Manually ]
按钮可重新获取或手动调整模型。除非重新调整，绑定中保存的模型列表会保持不变；改名
或更换 key 后，模型列表也会保留。按 Tab 选中 [ Save ] 应用修改（系统会自动处理改名），或
[ Cancel ] 放弃。编辑一条非当前绑定只改目录，实时配置要等切换过去才变。
按 d 删除；正在使用的绑定不能删除，须先切换到其他绑定。

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
charon edit claude gateway --models glm-4.6   # 只在模型菜单中注册 glm-4.6
```

编辑时，如果 `--models` 不含当前默认模型，列表中的第一个模型会成为新的默认模型。
可用 `--model` 明确指定默认模型；该模型也会保留在工具的模型菜单中。

每个工具都会在它自己的配置格式里写入一个专属的 `charon` provider 条目
（Codex 的 `[model_providers.charon]`、Claude 的 `env.ANTHROPIC_*`、OpenCode 的一个
`@ai-sdk/openai-compatible` provider、Pi 的 `pi.registerProvider("charon", ...)`
扩展、Oh My Pi 的 `models.yml` 中一个 `providers.charon` 块、Grok 的每个模型一张
`[model.charon-<slug>]` 表）。因此切换到其他配置后再切回时，Charon 仍可重新应用该绑定。

示例：先运行 `charon add codex --name work-key --key sk-... --model gpt-5`，再运行
`charon add codex --name proxy --endpoint https://gateway.example/v1 --key sk-...
--model glm-4.6`，之后运行 `charon switch codex work-key` 切换绑定，也可以直接运行
`charon` 并在菜单中选择。

## 工作原理

- 存储：`~/.config/charon/`（遵循 `$XDG_CONFIG_HOME`）。数据保存在五个 JSON 文件中：
  - `providers.json`：服务配置的 id 和 base URL，可供多个工具共用。
  - `credentials.json`：服务配置对应的 API key，权限为 `0600`。
  - `models.json`：服务配置对应的模型 slug。
  - `bindings.json`：工具的绑定名称、凭据、默认模型，以及要在工具菜单中提供的模型 ID。
  - `active.json`：每个工具当前使用的绑定。
- 绑定中的模型和 API key 必须对应同一个服务配置。Codex 绑定只能包含一个模型。
- 切换绑定会更新 `active.json`，并将该绑定的设置写入工具配置。Charon 只修改由它管理的配置项。
- 写入是原子的（临时文件 → `rename`）。
- 首次打开时，会把旧版中保存了 endpoint、key 和 model 的可编辑档案导入为绑定；旧档案目录
  保留不动。只有配置快照的档案、备份和 OAuth 登录不会导入。

## 安全

绑定以未加密形式存储在磁盘上，文件权限为 `0600`，目录权限为 `0700`。目录权限中的
x 表示可以遍历该目录。各工具配置也采用相同的权限方式，例如 `~/.codex/config.toml` 和
`~/.claude/settings.json`。能够读取 `~/.config/charon` 的人也能读取这些配置。
相比之下，shell 配置文件（`~/.bashrc`、`~/.zshrc`）默认权限为 `0644`，其他本机用户也可读取；
将 API key 存在那里会扩大泄露风险。请限制对 `~/.config/charon` 的访问。文件通过临时文件和
`rename` 原子替换。Charon 不会将数据发送到本机以外。

## 项目结构

```
cmd/charon/          入口 + 子命令
internal/artifact/   原子写入
internal/tools/      各工具适配器（codex、claude、opencode、pi、grok）
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
构建与 golangci-lint。贡献者与 Agent 约定（包括测试时务必使用独立的 `HOME`，避免访问
真实凭据这条规则）见 [AGENTS.md](AGENTS.md)。

## 路线图

- 可选的 `--verify`：切换后做鉴权 ping 以确认凭据确实可用。
- 支持更多 AI CLI 工具。

## 贡献

欢迎提交 PR 和 issue。这是一个早期项目，仍有许多改进空间。功能建议和问题报告会影响后续开发。

- 发现问题？ [提交 issue](https://github.com/Administration-626/charon/issues/new)，请附上工具名、操作系统、预期结果和实际结果。
- 希望增加功能？ [提交功能建议](https://github.com/Administration-626/charon/issues/new)，说明所需的工具支持或界面修改。
- 提交修复或功能？ Fork、创建分支并提交 PR。提交前运行 `make fmt && make test`。开发约定见 [AGENTS.md](AGENTS.md)。

欢迎提交各类改进，包括修正错别字和增加功能。

## 许可证

基于 [MIT License](LICENSE) 发布。
