# AGENTS.md

Guidance for AI coding agents (and humans) working in this repository.
`charon` is a small Go CLI that detects the Codex, Claude Code, OpenCode, Pi,
and Grok CLIs and switches each one's **endpoint + credentials** between
named bindings.

## Primary user workflow: TUI

The user primarily uses the interactive TUI. Treat CLI commands as secondary
automation/compatibility entry points, not the required path for normal use or
user acceptance. Core user-facing actions must be reachable and understandable
in the TUI; a CLI-only implementation is not complete delivery for this user.
Exercise the TUI's key-message flow and rendered state in tests, alongside
catalog/config assertions. Do not remove existing CLI support without explicit
approval, and do not invent more CLI commands instead of completing the UI.

**Latest approved direction:** merge the behavior into `h`, grouped by endpoint
address for Pi. All saved Pi bindings at that address hide/show together;
different endpoints remain independent even if they share a key. Hide/show must
update the managed provider immediately without switching or resetting the current
model. Refuse hiding the endpoint in use; keep `d` scoped to the selected binding.
The experimental `p` menu, independent key policy, and its disable-key/enable-key
commands have been removed. Group-based `h` is implemented; preserve its endpoint
scope, immediate provider update, live-model preservation and active-endpoint
refusals. See `docs/spec.md` for the behavior contract and
`docs/next-session-handoff.md` for the approved cases and historical evidence.

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
(`.github/workflows/ci.yml`) runs fmt-check, vet, `-race` tests, and build on
Linux + macOS, plus golangci-lint on Linux; keep all of them green.

## CodeGraph code search

When the `codegraph` CLI and this project's local `.codegraph/` index are
available, prefer CodeGraph for cross-file navigation, caller discovery, and
change-impact exploration. Simple text lookups can still use `rg` directly.
Run commands from the repository root:

- `codegraph status --strict` checks index readiness and freshness. If source
  changes are pending, run `codegraph sync` before querying; do not assume a
  background watcher is running.
- `codegraph query <symbol>` locates definitions.
- `codegraph callers <symbol>` finds direct callers.
- `codegraph explore --max-files 3 '<symbols or code terms>'` returns related
  source and call paths. For example:
  `codegraph explore --max-files 3 'SetBindingHidden PreparePiEndpointVisibility'`.

Use symbol names, file names, or English code terms: search is lexical, not
meaning-based, and Chinese natural-language queries are unreliable. The graph
can miss relationships; verify completeness with `rg`, source, and tests before
removing code or claiming a complete impact analysis. If the CLI or index is
missing or fails, fall back to `rg` + file reads. Do not automatically install
software, initialize an index, or change global agent configuration.

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
internal/tools/     per-tool adapters
  tool.go           Tool struct, AuthSpec, registry (All/Find)
  edit.go           JSON/TOML read helpers (loadJSONMap/loadTOMLMap)
  stash.go          Tool.Owned/Created declarations and `charon off` removal;
                    catalog/stash.go keeps stash.json with pre-charon values
  codex.go / claude.go / opencode.go / pi.go / grok.go   one file per tool
internal/catalog/   the store: providers, credentials, models, bindings, active pointer
internal/models/    fetch model lists from a provider API (openai/anthropic wire)
internal/secret/    masking
internal/tui/       bubbletea interactive menu
  tui.go            entry point (Run)
  model.go          model type, key bindings, construction
  load.go           loading rows for each screen
  update.go         message loop and search typing
  actions.go        key actions on the highlighted row
  views.go          rendering, detail pane, status line
  wizard.go         add/edit flow and delete confirmation
  picker.go         fetch-and-choose-a-model screen
  window.go         context-window preset editor and custom input
  table.go / theme.go / banner.go   shared table cells, styles, wordmark
```

Layering (imports point left): `secret` ← `artifact` ← `edit` ← `tools` ← `catalog` ← `cmd`/`tui`;
`models` is used by `tools`/`catalog`/`tui`, and `preview` builds on `tools` for `cmd`.
Binding names are validated in `internal/catalog` (`validateName`); a name is a label,
never a path segment, so do not join one into a filesystem path.

Data lives under `~/.config/charon/` (`$XDG_CONFIG_HOME` respected):
`providers.json`, `credentials.json`, `models.json`, `bindings.json`, `active.json`,
and `stash.json` (written on the first switch; `charon off` restores from it).

### What `charon off` restores

The stash records a tool's config before charon first writes to it, and off puts
it back **byte for byte**: comments, key order, indentation, a CRLF or BOM
encoding, a missing final newline. Three rules keep that promise:

- `OwnedFile.JSON`, `TOMLTop` and `TOMLKeys` are restorable (captured with their
  written form and, for JSON, the previous sibling that anchors them back into
  place). Tables, table-name prefixes and JSON key prefixes are charon's own
  namespace: off deletes them and never restores them, so a fixture must not
  pre-exist one.
- An object charon created is recorded (`StashVal{}` with `Absent`) and removed
  once the entries inside it are gone — but only when nothing is left in it, so a
  key the user added while charon was active survives. `FileStash.Absent` marks a
  file charon created, removed if nothing but an empty object remains.
- A file's final line break is the file's, not the edit's: `joinLinesLike` in
  `internal/edit` re-adds the empty last element a deletion consumed and strips a
  line break the file never had. Nothing else may decide how a file ends.

A file that held only blank lines comes back empty rather than with its exact
count of newlines; anything else restores exactly.

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
Pi `models.json` → `providers.charon-<label>.models`, Grok `[model.charon-<slug>]`), so switching model
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
TUI and projected to `providers.charon-<label>.api`. An absent value means
`openai-completions`; edits and copies within Pi must preserve explicitly saved
types. Pi 1.0.4 does not support a separate Responses Compact API, so never offer
it in the dropdown or write it as an API value.

### Hidden bindings (`Binding.Hidden`)

`h` on the bindings screen keeps bindings in the catalog but leaves their endpoint
out of the tool's config. For Pi, all bindings at the same normalized endpoint
share one visibility state. `Catalog.SetBindingHidden` updates that group and uses
`tools.PreparePiEndpointVisibility` to add or remove only its managed provider
immediately, without reapplying the active binding or changing Pi settings;
`Catalog.endpointGroups` honors the same flags on later applies. Hiding is refused
when the group contains the active binding or the on-disk default uses the
endpoint. Activating any binding in a hidden group shows the whole group; adding,
editing and duplicating preserve or inherit the group state. A tool without
`MultiEndpoint` has no list to hide from, and the setter refuses.

### Pi key policy (retired)

The experimental `p` menu, `disable-key`/`enable-key` commands and
`Credential.PiDisabled` policy were rejected by the user and removed. Do not
reintroduce a separate key policy or a write barrier in projection, activation,
stash restoration, header handling, CLI or TUI. Hiding is an endpoint concern,
not a key concern: hiding one endpoint must not disable the same key on another.
The supported replacement is endpoint-grouped hiding through `h`. Historical
evidence and the approved acceptance cases stay in `docs/next-session-handoff.md`.

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
  families. Pi tool generation merges them into `providers.charon-<label>.models` entries so each model
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

Behavior contracts and acceptance scenarios live in `docs/spec.md`. Update the
relevant rule when changing behavior; keep `[ASK USER]` decisions explicit rather
than treating the current implementation as the only valid product requirement.

- Standard Go style: `gofmt`/`goimports`, tabs, error wrapping with `%w`,
  table-driven tests, small focused packages, exported identifiers documented.
- Keep `cmd/` thin — logic belongs in `internal/` packages so it stays testable.
- Never log or print full secrets; route them through `secret.Mask`.
- Prefer standard library. Current third-party deps are intentionally minimal:
  `bubbletea`/`bubbles`/`lipgloss` (TUI), `sahilm/fuzzy` (model search),
  `pelletier/go-toml/v2` (Codex/Grok TOML), `tidwall/gjson` + `tidwall/jsonc`
  (JSONC-aware edits), and `golang.org/x/sys/unix` (flock). Discuss before adding more.
- Platform-specific code goes behind build tags (`_darwin.go` / `_other.go`),
  never runtime `runtime.GOOS` branching for the keychain.

## Testing expectations

- Any new behavior needs a test. Keep coverage on `internal/models`,
  `internal/catalog`, and `internal/tools` from regressing.
- Use `httptest` for anything hitting the network (see `fetch_test.go`); never
  make real API calls in tests.
- The TUI has table-driven tests for pure helpers and rendered views; keep
  new logic in testable helpers, and avoid brittle end-to-end terminal tests.
- `internal/edit` has fuzz targets (`go test ./internal/edit -fuzz=FuzzPatchJSON`)
  and a fixture corpus in `testdata/` compared against golden files. Regenerate
  goldens with `-update` only after reading the diff.
- `internal/edit/diff_test.go` checks each edit differentially: the same change
  is made to a decoded copy of the document (`map[string]any`, whole-document
  rewrite) and the two are compared **by value**. Bytes are expected to differ —
  preserving comments, key order and indentation is the point of the package —
  so a difference means the edit changed the document's meaning. A new edit
  operation or a new config shape belongs there as a case; keep the oracle a
  stub, never a second implementation of the editor.
- `internal/tools/off_diff_test.go` does the same for `charon off`: every tool's
  config is generated across the layouts a hand-edited file really has (2/4
  spaces, tabs, compact, CRLF, BOM, no final newline) and with charon's entries
  already present in each position of the object, then captured, applied and
  removed — the bytes must match. A new owned path or layout belongs in that
  matrix, and every off-behaviour fix gets a focused test next to it.
- `internal/tools/pi_groups_diff_test.go` covers the one multi-endpoint tool the
  other way round: an endpoint registered as a `Groups` entry must come out
  identical to applying that endpoint alone (labels aside, which the test
  normalizes and checks against each other). The two paths share model/header
  value builders, so protocol resolution, window defaults, thinking presets and header projection
  cannot drift between them. Mutation-check it before trusting it: a case only
  counts when breaking the group path makes it fail.
- `make vuln` runs `govulncheck`; `make hooks` points git at `.githooks/pre-push`
  (test + lint + vuln, skipping tools that are not installed).

## Out of scope / do not do

- Don't commit built binaries, `dist/`, or coverage files (see `.gitignore`).
- Don't send config or secrets to any external service. No user-driven import/export or sync.
- Don't weaken file permissions.
- Don't snapshot a tool's config, capture OAuth logins, or auto-create a `default`
  binding. Official logins are the tool's own business.
- Don't add a tool adapter the user does not use.
