# AGENTS.md

Guidance for AI coding agents (and humans) working in this repository.
`charon` is a small Go CLI that detects the Codex, Claude Code, OpenCode, Pi,
and Grok CLIs and switches each one's **endpoint + credentials** between
named bindings.

## Golden rule: this tool edits real user credentials

`charon` reads and writes live config for other tools (`~/.codex`, `~/.claude`,
`~/.config/opencode`, `~/.local/share/opencode`, `~/.pi/agent`, `~/.grok`).
It stores API keys of its own under `~/.config/charon/`.

- **Never** run `charon add`, `charon switch`, `charon edit`, or the interactive menu
  against your real `$HOME` while developing. Always sandbox:
  ```sh
  HOME=$(mktemp -d) go run ./cmd/charon status
  ```
- Tests must never touch real config. Use `t.Setenv("HOME", t.TempDir())` and
  `t.Setenv("XDG_CONFIG_HOME", t.TempDir())`. See `internal/tools/tools_test.go`
  and `internal/catalog/catalog_test.go` for the pattern.
- Do not add tests that read or write the real Keychain.
- Preserve the safety guarantees: **atomic writes** (temp file + rename) and
  `0600` on credential files / `0700` on dirs. Don't regress these.

## Commands

```sh
make build      # build ./charon
make test       # go vet + go test -race ./...
make cover      # coverage summary
make lint       # golangci-lint run
make fmt        # gofmt -w .
make run        # build + open the interactive menu (sandbox your HOME first)
```

Always run `make fmt` and `make test` before finishing a change. CI
(`.github/workflows/ci.yml`) runs fmt-check, vet, `-race` tests, build, and
golangci-lint on Linux + macOS; keep all of them green.

## Architecture

```
cmd/charon/         CLI entrypoint (thin; no business logic)
  main.go           main, subcommand dispatch, usage
  commands.go       one cmd* func per subcommand + requireTool
internal/artifact/  atomic writes
internal/edit/      format-preserving JSON/JSONC + TOML editors (adapted from
                    magpie's internal/edit, MIT): set/del key paths and whole
                    tables by name, comments/order/indentation kept; edits are
                    validated before any write
internal/preview/   dry-run previews: apply a binding against copies of the
                    tool's config under a temporary home, diff, write nothing
stash               Tool.Owned/Created declare what ApplyAuth owns per file;
                    `charon off` (catalog.Off) removes it and restores the
                    stashed pre-charon values (catalog stash.json)
internal/tools/     per-tool adapters
  tool.go           Tool struct, AuthSpec, registry (All/Find)
  edit.go           JSON/TOML read helpers (loadJSONMap/loadTOMLMap)
  codex.go / claude.go / opencode.go / pi.go / grok.go   one file per tool
internal/catalog/   the store: providers, credentials, models, bindings, active pointer
internal/models/    fetch model lists from a provider API (openai/anthropic wire)
internal/secret/    masking
internal/tui/       bubbletea interactive menu
```

Layering (imports point left): `secret` ← `artifact` ← `edit` ← `tools` ← `catalog`/`preview` ← `cmd`/`tui`.
Binding names are validated in `internal/catalog` (`validateName`); a name is a label,
never a path segment, so do not join one into a filesystem path.

Data lives under `~/.config/charon/` (`$XDG_CONFIG_HOME` respected):
`providers.json`, `credentials.json`, `models.json`, `bindings.json`, `active.json`.
On first open, editable legacy profiles with an endpoint, key, and model are imported;
the old `profiles/` tree remains untouched. Snapshot-only profiles, backups, and OAuth
logins are not imported.

### Store locking

- **Mutations are serialized by an advisory lock.** Every mutating `Catalog` method
  takes an exclusive flock on `~/.config/charon/.lock` (via `golang.org/x/sys/unix`
  on Linux/macOS; a no-op elsewhere). A competing mutation waits, then runs against
  the latest files, so its result becomes active without interleaving another
  operation. The process-local mutex also serializes goroutines using one Catalog.
  Keep the lock around the full read-modify-write operation, including rendering an
  active binding; don't add a mutation without wrapping it the same way.

### How to add a new tool

1. Add `internal/tools/<tool>.go` returning a `*Tool` with: `Name`, `Title`,
   `Provider` (`openai`/`anthropic`), `DefaultEndpoint`, `Detected`, `Describe`,
   and `ApplyAuth`.
2. Register it in `All()` in `tool.go`.
3. Add a `TestXxxDescribeAndApply` in `tools_test.go` using a sandboxed `$HOME`.
   Everything else (catalog, CLI, TUI) is generic and needs no changes.

`ApplyAuth` writes through `internal/edit` (`SetJSON`/`ApplyJSON` for JSON
and JSONC, `SetTOMLTable`/`SetTOMLKey`/`SetTOMLTop`/`PatchTOMLTop`/`SetTOMLTables`
for TOML), which changes only the named key paths and tables: comments, key
order, indentation and every user-authored entry survive as they are, and the
edited bytes are validated before anything reaches the disk. Reads may still use
the `edit.go` load helpers. Never round-trip a tool's config through
marshal/unmarshal — that reorders keys and strips comments.

### Model lists (`AuthSpec.Models`)

A binding's model list is what charon registers in the **tool's own** model picker
(Codex `custom_models.json`, Claude `modelPicker`, OpenCode `provider.charon.models`,
Pi `models.json` → `providers.charon.models`, Grok `[model.charon-<slug>]`), so switching model
mid-session doesn't need charon. It is passed to `ApplyAuth` as
`AuthSpec.Models`. Two rules a new tool must honor:

- `Models` empty (nil) means **"keep what's registered"**, not "register nothing";
  this lets a direct `ApplyAuth` call that changes only a key or default model retain
  the picker. `Catalog.Project` always passes the binding's complete list, including a
  one-model list, so switching bindings replaces the picker and cannot retain another
  endpoint's models.
- Whatever key holds the list must be an **owned key** of the config artifact, so the
  list travels with the binding and one endpoint's models never leak into another's.

Codex supports multiple models through `model_catalog_json`. Each generated entry
has its own context window and reasoning effort; multi-model bindings must not set
the global `model_context_window`, which would override those per-model windows.
A single built-in OpenAI model at the official endpoint still uses Codex's native
catalog when no explicit window override is needed.

The model-discovery wire dialect is **not stored**. `models.Fetch` is called
twice when listing models — the tool's historical dialect first, then the other —
and the winner is not persisted.
Pi bindings separately store an optional `piApi` request protocol, selected in the
TUI and projected to `providers.charon.api`. An absent value means
`openai-completions`; edits and copies within Pi must preserve explicitly saved
types. Pi 1.0.4 does not support a separate Responses Compact API, so never offer
it in the dropdown or write it as an API value.

### Built-in context windows (`internal/models/builtin.go`)

- Model context window sizes resolve in order of precedence:
  1. Manual override (`catalog.WindowManual`), which takes priority and is never overwritten.
  2. API response value (`catalog.WindowAPI`), when returned and > 0.
  3. Built-in lookup table (`catalog.WindowBuiltin`), derived from **WorkBuddy** configurations and official provider specifications.
  4. Fallback limit (`models.FallbackContextWindow`, 500,000 tokens) when unrecognized.
- When extending or updating built-in rules, reference WorkBuddy model presets or official vendor specifications.
  Use `scripts/extract_workbuddy_models.py --mainstream --format go` to extract up-to-date mappings from WorkBuddy's installation archive (`docs/workbuddy-context-sync.md`).

### Built-in thinking presets (`internal/models/thinking.go`)

- `LookupThinkingPreset` returns a vendor's verified thinking-level map plus OpenAI-completions
  compat flags (thinking format, `max_tokens` field, reasoning-content tagging) for recognized
  families. Pi tool generation merges them into `providers.charon.models` entries so each model
  offers exactly the levels its vendor supports (and pi sends the right request shape through a
  gateway, where its baseUrl fingerprinting cannot detect the vendor).
- Presets are curated from WorkBuddy (`scripts/extract_workbuddy_models.py --thinking`); official
  vendor channels win over hosts and aggregators. Claude presets bind to `anthropic-messages`
  bindings only (their official maps are dialect-specific and adaptive-thinking requires an
  explicit compat flag pi cannot infer at a gateway); MiniMax and Muse stay unpreset (no
  authoritative OpenAI-compatible dialect data); MiMo is a toggle-only preset (compat flags
  only, no level map); Grok presets carry an explicit `supportsReasoningEffort: false` since pi
  only knows xAI's rejection behind its endpoint fingerprint.
- Manual per-model maps (`catalog.Model.ThinkingLevelMap`, set via the TUI thinking-map field)
  always win over presets; unknown models keep the generic off/xhigh/max fallback.

## Conventions

- Standard Go style: `gofmt`/`goimports`, tabs, error wrapping with `%w`,
  table-driven tests, small focused packages, exported identifiers documented.
- Keep `cmd/` thin — logic belongs in `internal/` packages so it stays testable.
- Never log or print full secrets; route them through `secret.Mask`.
- Prefer standard library. Current third-party deps are intentionally minimal:
  `bubbletea`/`bubbles`/`lipgloss` (TUI) and `pelletier/go-toml/v2` (Codex TOML).
  Discuss before adding more.
- Platform-specific code goes behind build tags (`_darwin.go` / `_other.go`),
  never runtime `runtime.GOOS` branching for the keychain.

## Testing expectations

- Any new behavior needs a test. Keep coverage on `internal/models`,
  `internal/catalog`, and `internal/tools` from regressing.
- Use `httptest` for anything hitting the network (see `fetch_test.go`); never
  make real API calls in tests.
- The TUI is verified by compile + `go vet`; extract pure logic into testable
  helpers rather than testing the bubbletea event loop.

## Out of scope / do not do

- Don't commit built binaries, `dist/`, or coverage files (see `.gitignore`).
- Don't send config or secrets to any external service. No user-driven import/export or sync.
- Don't weaken file permissions.
- Don't snapshot a tool's config, capture OAuth logins, or auto-create a `default`
  binding. Official logins are the tool's own business.
- Don't add a tool adapter the user does not use.
