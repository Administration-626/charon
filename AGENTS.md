# AGENTS.md

Guidance for AI coding agents (and humans) working in this repository.
`charon` is a small Go CLI that detects the Codex, Claude Code, OpenCode, Pi, Oh My
Pi (omp), and Grok CLIs and switches each one's **endpoint + credentials** between
named bindings.

## Golden rule: this tool edits real user credentials

`charon` reads and writes live config for other tools (`~/.codex`, `~/.claude`,
`~/.config/opencode`, `~/.local/share/opencode`, `~/.pi/agent`, `~/.grok`,
`~/.omp/agent`). It stores API keys of its own under `~/.config/charon/`.

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
internal/tools/     per-tool adapters
  tool.go           Tool struct, AuthSpec, registry (All/Find)
  providers.go      guards for the shared "charon" provider entry (codex/opencode)
  edit.go           JSON/TOML/YAML load-merge-write helpers (preserve unknown keys)
  codex.go / claude.go / opencode.go / pi.go / omp.go / grok.go   one file per tool
internal/catalog/   the store: providers, credentials, models, bindings, active pointer
internal/models/    fetch model lists from a provider API (openai/anthropic wire)
internal/secret/    masking
internal/tui/       bubbletea interactive menu
```

Layering (imports point left): `secret` ← `artifact` ← `tools` ← `catalog` ← `cmd`/`tui`.
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

`ApplyAuth` must **merge** into existing config (use the `edit.go` helpers) so
unrelated user settings survive; it must not rewrite the file wholesale. It must
refuse to modify a user-authored provider (`ensureOnlyCharonChanged`, managed
provider name `"charon"`).

### Model lists (`AuthSpec.AllModels`)

A binding's model list is what charon registers in the **tool's own** model picker
(Claude `modelPicker`, OpenCode `provider.charon.models`, pi's extension, omp
`providers.charon.models`, Grok `[model.charon-<slug>]`), so switching model
mid-session doesn't need charon. It is passed to `ApplyAuth` as
`AuthSpec.AllModels`. Two rules a new tool must honor:

- `AllModels` empty (nil) means **"keep what's registered"**, not "register nothing";
  this lets a direct `ApplyAuth` call that changes only a key or default model retain
  the picker. `Catalog.Project` always passes the binding's complete list, including a
  one-model list, so switching bindings replaces the picker and cannot retain another
  endpoint's models.
- Whatever key holds the list must be an **owned key** of the config artifact, so the
  list travels with the binding and one endpoint's models never leak into another's.

Codex has no config surface for a model list (`catalog.SingleModelTools`), so a
Codex binding carries exactly one model.

Which wire dialect an endpoint speaks is **not stored**. `models.Fetch` is called
twice when listing models — the tool's historical dialect first, then the other —
and the winner is not persisted.

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
