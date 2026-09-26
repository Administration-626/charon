<h1 align="center">Charon</h1>

<p align="center">
  <em>Switch your AI tools between endpoints.</em>
</p>

<p align="center">
  <a href="https://github.com/Administration-626/charon/releases/latest"><img src="https://img.shields.io/github/v/release/Administration-626/charon?style=flat-square&color=6c47ff" alt="Latest Release"></a>
  <a href="https://github.com/Administration-626/charon/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/Administration-626/charon/ci.yml?branch=main&style=flat-square&label=CI" alt="CI"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/Administration-626/charon?style=flat-square" alt="MIT License"></a>
  <a href="https://github.com/Administration-626/charon/issues"><img src="https://img.shields.io/github/issues/Administration-626/charon?style=flat-square" alt="Open Issues"></a>
</p>

<p align="center">
  <b>English</b> · <a href="README.zh-CN.md">简体中文</a>
</p>

Charon is a tiny Go CLI that detects the Codex, Claude Code,
OpenCode, Pi, Oh My Pi (omp), and Grok CLIs and switches each one's endpoint +
credentials between named bindings. A binding is an endpoint, an API key, and
the models it should offer — not a snapshot of the tool's config. Switching
re-renders that binding into the tool, overwriting only the keys charon owns.

<p align="center">
  <img src="https://raw.githubusercontent.com/Administration-626/charon/main/assets/screenshot.png" alt="Charon interactive menu" width="80%">
</p>

## Features

- One command, six tools. Manage Codex, Claude Code, OpenCode, Pi, Oh My Pi
  (omp), and Grok from a single interactive menu or a scriptable CLI.
- Named bindings. Save an endpoint, an API key, and its models, then switch
  between bindings. One endpoint can serve several tools.
- Model discovery. Add a binding from an endpoint + key; Charon fetches the
  model list and lets you pick — or type model ids directly.
- Switch models inside the tool. Check off the models you actually use and
  Charon registers them in the tool's own picker (Claude Code's `/model`, OpenCode's
  `/models`, Pi's `/model`, Oh My Pi's `/model`, Grok's `/model`), so changing
  model mid-session never means going back through Charon.
- Single-page form. Add or edit a binding on one screen (Name, URL, Token,
  Model) with direct typing and [ Save ] / [ Cancel ] buttons.
- Instant clone & cross-tool copy. Press c to duplicate a binding, or x to
  copy it to another tool; type to fuzzy-filter the model list in real time.
- Unicode names. Binding names support any script, including Chinese; spaces
  and control characters are rejected.
- Safe by default. Writes are atomic. Deleting the binding a tool is
  currently using is refused — switch to another one first.
- Non-destructive. Charon only ever touches its own `charon` provider entry
  in each tool's config, never your hand-authored providers.

## Supported tools

| Tool | Endpoint | Credentials |
|------|----------|-------------|
| Codex | `~/.codex/config.toml` (`model_provider` → `base_url`) | `~/.codex/config.toml` (`experimental_bearer_token`) |
| Claude Code | `~/.claude/settings.json` (`env.ANTHROPIC_BASE_URL`) | `settings.json` env key |
| OpenCode | `~/.config/opencode/opencode.jsonc` (`provider.*.options.baseURL`) | `opencode.jsonc` (`provider.charon.options.apiKey`) |
| Pi | `~/.pi/agent/extensions/charon.ts` (`baseUrl`) | `~/.pi/agent/extensions/charon.ts` (`apiKey`) |
| Oh My Pi (omp) | `~/.omp/agent/models.yml` (`providers.charon.baseUrl`) | `~/.omp/agent/models.yml` (`providers.charon.apiKey`) |
| Grok | `~/.grok/config.toml` (`[model.charon-*].base_url`) | `~/.grok/config.toml` (`[model.charon-*].api_key`) |

## Installation

### curl

No Go needed — downloads the prebuilt binary for your platform, verifies its
checksum, and installs to `~/.local/bin`:

```sh
curl -fsSL https://github.com/Administration-626/charon/releases/latest/download/install.sh | sh
```

> Prepend `PREFIX=/usr/local` to install system-wide, or `VERSION=v1.2.3` to pin a release.

<details>
<summary><b>Other methods</b> — manual binary · build from source</summary>

Pre-built binary — grab your platform's archive from the
[Releases page](https://github.com/Administration-626/charon/releases/latest)
(`charon_linux_{amd64,arm64}.tar.gz`) and verify it against the included
`checksums.txt`:

```sh
curl -L https://github.com/Administration-626/charon/releases/latest/download/charon_linux_amd64.tar.gz | tar xz
sudo mv charon /usr/local/bin/
```

From source — requires Go 1.24+:

```sh
make install                      # build + install to ~/.local/bin (PREFIX to override)
go build -o charon ./cmd/charon   # or just build here
```

</details>

## Usage

### Interactive menu

Run `charon` with no arguments to open an arrow-key menu: pick a tool, then
switch, add, edit, or delete bindings. Quit any time with `ctrl+c`.

### CLI reference

```sh
charon                       # interactive arrow-key menu
charon status                # show each tool's live config and active binding (--json)
charon ls <tool>             # list saved bindings (--json)
charon models <tool>         # list models offered by an API (--key [--endpoint])
charon add <tool>            # add + activate a binding (--name --key and at least one model id)
charon edit <tool> <b>       # change a binding's endpoint/key/model/models (--name to rename)
charon rename <tool> <o> <n> # rename a saved binding
charon cp <tool> <src> <dst> # duplicate a saved binding
charon cp <tool> <src> <tool> <dst> # copy a saved binding to another tool
charon switch <tool> <b>     # render a saved binding into the tool
charon rm <tool> <b>         # delete a binding (refused while it is the active one)
charon completion <shell>    # print a bash/zsh/fish completion script
charon update                # upgrade charon to the latest release
charon uninstall             # remove the installed charon binary
```

`status` and `ls` accept `--json` for scripting and editor integrations.
`status` reads the tool's live config; `ls` reads the saved bindings. A model
change made with the tool's own `/model` is kept while that binding stays
active; rendering a different binding replaces the model list.

### Shell completions

 Completions ship in the release archives. To enable them manually:

```sh
# bash — add to ~/.bashrc
source <(charon completion bash)
# zsh — add to ~/.zshrc (ensure `compinit` runs)
source <(charon completion zsh)
# fish
charon completion fish | source
```

They complete subcommands, tool names, and — for `switch`/`edit`/`rename`/`cp`/`rm`
— saved binding names.

## Adding & editing bindings

### From an endpoint + key (with model discovery)

In the menu, drill into a tool and choose ＋ Add new binding… (or press a).
A single-page form collects everything on one screen:

- Name — a binding name (any script; Unicode is fine).
- API base URL — leave blank to accept the provider default; a real value is
  never prefilled.
- API key — masked input.
- Model — two action buttons underneath give you full control:
  - [ Fetch & Pick Online Models ] hits `GET /v1/models` (using `Authorization:
    Bearer` for OpenAI-style APIs and `x-api-key` for Anthropic) and opens the
    model picker.
  - [ Type Model IDs Manually ] lets you type model IDs directly (comma-separated,
    e.g. `kimi-k2, deepseek-v3`) for endpoints or gateways that do not expose
    `/v1/models`. If an online fetch fails, Charon falls back to this manual screen
    automatically with existing IDs prefilled.
  You can also type a model slug directly into the field. If you leave it blank,
  the first model in the registered list becomes the default.

In the model picker, just start typing to fuzzy-filter the list in real time
(Backspace edits the query, Esc clears it):
- Space checks/unchecks a model (marked •) into the curated list registered
  with the tool.
- Ctrl+A selects or deselects all models (or all matching search results).
- Once models are checked, a Done — register these N model(s) row appears at the
  top (showing the active default model). Press Enter on it to return to the
  form without re-picking the default.
- Press Enter on any model row to set it as the default,
  automatically adding it to the registered list if not already checked, and return
  to the form.
- Check nothing and the whole fetched list is registered.
Tab to [ Save ] to write the endpoint/key/model into the tool's live
config and switch to it, or [ Cancel ] to discard. Name, key, and at least
one model are required. Leave URL blank to use the tool's default endpoint; a red
status bar flags missing required values on submit.

### Switching model inside the tool

The models a binding registers land in the tool's own menu, so you can change model
without leaving your session: Claude Code's `/model` (via `modelPicker`), OpenCode's
`/models` (via the `charon` provider's model map), Pi's `/model` (via the
generated extension), Oh My Pi's `/model` (via the `providers.charon` model
list), and Grok's `/model` (via `[model.charon-<slug>]` tables).
The list travels with the binding — switch bindings and the menu switches too.
Codex is the exception: its config has no place to register extra models, so a
Codex binding carries exactly one (`charon edit codex <b> --model ...`).
A binding with one model replaces the previous binding's list with that one model.
Changing the model in the tool does not update the saved binding, and Charon
keeps the live model while that binding stays active. Rendering another binding
replaces the model list.

In the menu, press c on a binding to clone it instantly into `<name>-copy`;
press x to pick another tool and copy it there as `<name>-copy`. Both copies
share the source key and are not activated.

### Editing an existing binding

Press e on a binding to open its edit form, showing the current Name,
URL, Token (masked), and Model on a single screen. Type directly into
any field; the Model field's [ Fetch & Pick Online Models ] and
[ Type Model IDs Manually ] buttons let you update or re-fetch the registered
models. The model list saved on this binding stays as-is unless you curate a new list,
so a rename or key rotation keeps the binding's own picker. Tab to [ Save ]
to apply the changes — renaming is handled automatically — or [ Cancel ] to
discard. Editing a binding that is not the active one updates only the catalog;
the live config changes when you switch to it. Press d to delete; the active
binding is refused until you switch away.

### Non-interactively

```sh
charon models codex --endpoint https://openrouter.ai/api/v1 --key sk-...
charon add    codex --name openrouter --endpoint https://openrouter.ai/api/v1 \
                    --key sk-... --model openai/gpt-5.5
```

`--models` registers a list in the tool's own picker, so you can switch between them
from inside the tool. The first id becomes the default when `--model` is omitted, and
the list is stored with the binding, so a later `edit` that doesn't pass `--models`
leaves it intact:

```sh
charon add  claude --name gateway --endpoint https://gateway.example/v1 --key sk-... \
                   --models kimi-k2,glm-4.6,deepseek-v3
charon edit claude gateway --key sk-rotated   # picker list survives untouched
charon edit claude gateway --models glm-4.6   # curate the menu down
```

When `--models` excludes the current default, its first id becomes the new default.
Pass `--model` to choose another default explicitly; that id is also kept in the
picker list.

Each tool gets a dedicated `charon` provider entry written into its own config
format (Codex `[model_providers.charon]`, Claude `env.ANTHROPIC_*`, OpenCode an
`@ai-sdk/openai-compatible` provider, Pi a `pi.registerProvider("charon", ...)`
extension, Oh My Pi a `providers.charon` block in `models.yml`, Grok a
`[model.charon-<slug>]` table per model), so Charon can reapply a binding after
you switch to another one.

For example, run `charon add codex --name work-key --key sk-... --model gpt-5`,
then `charon add codex --name proxy --endpoint https://gateway.example/v1 --key sk-...
--model glm-4.6`. Switch back with `charon switch codex work-key`, or run `charon`
and choose a binding from the menu.

## How it works

- Storage: `~/.config/charon/` (`$XDG_CONFIG_HOME` respected). Five JSON files,
  no database:
  - `providers.json` — a site: id, base URL. Reused across tools.
  - `credentials.json` — a key for one site. Mode `0600`.
  - `models.json` — a model slug for one site.
  - `bindings.json` — one tool's saved choice: name, credential, default model,
    and the model ids its picker should offer.
  - `active.json` — which binding each tool currently renders.
- A binding's models must belong to the same site as its key. Codex carries
  exactly one model.
- Switching updates `active.json` and re-renders that binding. Only the keys
  charon owns are overwritten.
- Writes are atomic (temp file → `rename`).
- On first open, saved legacy profiles with endpoint, key, and model data are imported
  as bindings. The old profile tree is kept; snapshot-only profiles, backups, and OAuth
  logins are not imported.

## Security

Bindings are stored unencrypted on disk (`0600` for files, `0700` for
directories — the x bit on directories means "enter", not "execute", so
`0700` is the correct way to allow access). This is the same permission model
used by the tools themselves (`~/.codex/config.toml`, `~/.claude/settings.json`,
etc.) — if an attacker can read `~/.config/charon`, they can also read those
files. Shell config files (`~/.bashrc`, `~/.zshrc`), by contrast, default to
`0644` (world-readable), so storing API keys there is considerably less secure.
Keep `~/.config/charon` private. Writes are atomic (temp file → `rename`).
Nothing is sent off the machine.

## Project layout

```
cmd/charon/          entrypoint + subcommands
internal/artifact/   atomic writes
internal/tools/      per-tool adapters (codex, claude, opencode, pi, grok)
internal/catalog/    providers, credentials, models, bindings, active pointer
internal/models/     fetch model lists from a provider API (openai/anthropic wire)
internal/tui/        bubbletea interactive menu (single-page forms, fuzzy model search)
internal/secret/     masking
```

## Development

```sh
make build   # build ./charon
make test    # go vet + go test -race ./...
make cover   # coverage summary
make lint    # golangci-lint run
make fmt     # gofmt -w .
make run     # build + interactive menu (sandbox HOME first!)
```

CI (`.github/workflows/ci.yml`) runs formatting checks, vet, race tests, build,
and golangci-lint on Linux. Contributor and agent conventions —
including the rule to always sandbox `HOME` when testing so real credentials
are never touched — live in [AGENTS.md](AGENTS.md).

## Roadmap

- Optional `--verify` post-switch auth ping to confirm credentials actually work.
- Support for more AI CLI tools.

## Contributing

PRs and issues are welcome. This is an early project, and feedback helps guide
future changes.

- Found a bug? [Open an issue](https://github.com/Administration-626/charon/issues/new) with the tool name, operating system, expected behavior, and actual behavior.
- Have a feature request? [Open an issue](https://github.com/Administration-626/charon/issues/new) describing the requested tool support or interface change.
- Sending a fix or feature? Fork the repository, create a branch, and open a PR. Run `make fmt && make test` before pushing. See [AGENTS.md](AGENTS.md) for the conventions.

Contributions include typo fixes, bug fixes, and new features.

## License

Released under the [MIT License](LICENSE).
