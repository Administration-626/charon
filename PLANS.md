# Charon — feature roadmap

Candidate features, ranked by value vs. effort. These lean on the existing
`Tool`/`Artifact`/`Store` abstractions, so most are additive and low-risk.

## High value, low effort

### Account-named backups ✅ (done)
`charon save <tool>` with no name snapshots the current OAuth login and names the
profile after its account (Codex `id_token` email, Claude `~/.claude.json`), so a
user with several ChatGPT/Claude accounts can capture and hop between each. In the
TUI, **`b`** backs up the highlighted profile: a login is captured under its email
(non-editable), while an API-proxy profile is duplicated to an editable `name-2`.

### More tools
The whole point of the `Tool`/`Artifact` design is cheap additions. Each new
tool is one `internal/tools/<tool>.go` returning a `*Tool` plus a
`TestXxxDescribeAndApply` case — the store, CLI, and TUI are already generic.

Targets: Gemini CLI, Aider, Cursor, Continue, Zed.

### Backup pruning ✅ (done)
Every switch/add/undo now backs up first; retention keeps the newest 10 per tool
automatically, with `charon prune <tool> [--keep N]` to trim on demand.

### `charon undo` ✅ (done)
`charon undo <tool>` reverts to the most recent backup (restoring the active
pointer too) and snapshots the current state first, so undo is itself reversible.

### Shell completions ✅ (done)
`charon completion <bash|zsh|fish>` prints a script (dynamic profile-name
completion via the hidden `charon __profiles`); goreleaser generates and bundles
them into release archives and installs them through the Homebrew formula.

## Medium

### Drift detection ✅ (done)
`Store.Drift` compares the active profile's snapshot against the live artifacts;
`status` shows `(modified)` and the TUI flags the active profile/tool with ⚠ when
the live config changed outside charon.

### `--json` output ✅ (done)
`charon status --json` and `charon ls <tool> --json` emit structured records
(secrets masked, never raw) for scripting and editor integrations.

### CLI rename / edit ✅ (done)
`charon rename <tool> <old> <new>` and `charon edit <tool> <p>
[--endpoint --key --model --name]` bring the CLI to parity with the TUI.

## Deferred / handle with care

### Profile export / import / sync
Moving profiles across machines means moving real secrets, which cuts against
the "never send secrets anywhere" guarantee (see AGENTS.md). If pursued, limit
to encrypted local export — no network sync.

---

## Known issues

### TUI wizard: step-by-step back navigation & explicit Options UI across all wizard steps (API URL, Key, Model, Name) ✅ (done)
In the profile creation wizard, users need clear Option guidance and back navigation at every stage:
- **Explicit Options UI block**: Rendered directly in every input step (`viewAddEndpoint`, `viewAddKey`, `viewAddCustomModel`, `viewAddName`), displaying clear action items (e.g. `• [ Enter ] Continue to Fetch Models` / `• [ Esc ] ← Back to API Base URL`).
- **`Esc` key back navigation**: Pressing `Esc` at any step steps back to the previous input step while preserving all already-entered data (URL, Key, Model).
- **`← Back` list item**: Placed prominently at the top of the model-picker list (`viewPickModel`), allowing immediate keyboard navigation back to the API Key step.

### TUI model picker: manual custom model ID entry ✅ (done)
The model-picker step (`viewPickModel`) now includes a **`✎ Enter custom model ID...`**
selectable item. Selecting it opens a text input step (`viewAddCustomModel`),
allowing users to type any non-standard or unlisted model slug (e.g. `gpt-4o`,
`claude-3-7-sonnet`, `glm-4-flash-custom`) directly in the TUI without relying solely
on the fetched `/v1/models` API response.

### Profile name rejects Unicode (Chinese characters) ✅ (done)
`charon add codex --name codex公益站` now supports Unicode letters and digits, so
Chinese profile names (and other non-ASCII names) work natively.

### Single-page Form UI refactor for Profile Creation (Add/Edit Flow) ✅ (done)
Replaced the multi-step wizard with a unified Single-page Form view (reusing `viewEditForm` mechanics) matching mature TUI standards (e.g. Claude Code, GitUI):
- **Direct typing**: Moving focus (`↑`/`↓`) to Name, URL, Token, or Model allows typing/backspacing directly on the row without pressing extra activation keys (`e`/`Enter`).
- **On-demand model fetching**: Press `m` / `Ctrl+M` on the Model row to fetch and pick from `/v1/models`.
- **Standard Save & Cancel buttons**: Distinct `[ Save Profile ]` and `[ Cancel ]` buttons at the bottom. `Ctrl+S` or `[ Save Profile ]` + `Enter` submits; `Esc` or `[ Cancel ]` + `Enter` cancels and exits.

### Profile snapshots store redundant config data ("插槽" refactor)
Each profile stores a full copy of the tool's config file (e.g.
`config.toml`), even though only 3–4 owned keys differ between profiles
(`model`, `model_provider`, `model_providers`, `model_reasoning_effort`).
Non-owned keys (`sandbox_mode`, `projects`, `approval_policy`, …) are dead
weight — they are overwritten by live values on restore anyway.

Ideal design: a profile is just its `Spec` (`{endpoint, key, model}`); switching
calls `ApplyAuth(Spec)` directly, with no file-level snapshot or merge needed.
The one open question is how to persist in-session model changes (e.g. `/model`
inside the tool) back to `Spec.Model` without an explicit `charon refresh`.

_Context: the refactor/hygiene items (A/B) and the throttled model-fetch
loading screen are already implemented; this file tracks the remaining feature
work._
