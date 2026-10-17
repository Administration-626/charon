# Charon — feature roadmap

Candidate features, ranked by value vs. effort.

The store is a catalog, not snapshots. `~/.config/charon/` holds five JSON files:
`providers.json`, `credentials.json`, `models.json`, `bindings.json`, `active.json`.
A binding is an endpoint + key + model list for one tool; exactly one binding per
tool is active, and switching re-renders it through `ApplyAuth`. There is no
`default` binding, no backup, no drift flag, and no user-driven import/export.
First run locally migrates editable legacy profiles and leaves the old files in place.

## Done

### Catalog instead of snapshots
Providers are shared across tools. A binding references a credential and model
slugs rather than copying a tool's config file. `Project` renders a binding;
the binding's full model list is written, including a one-model list, so a
single-model endpoint cannot inherit the previous binding's models.

### In-tool model switching
Claude `/model`, OpenCode `/models`, and Pi `/model` get the binding's model list.
Codex's config has no such field, so a Codex binding carries exactly one model.

### Shell completions
`charon completion <bash|zsh|fish>` prints a script (dynamic binding-name
completion via the hidden `charon __profiles` command, retained for existing scripts).

### Pi endpoint aggregation
Activating a Pi binding registers the catalog's other Pi bindings as separate
providers in `models.json`, one per endpoint, each carrying the key, model list
and request protocol that binding saved. Every entry, the active binding's
included, is keyed `charon-<label>`, where the label is the host's first part
(full host, then full URL, when two endpoints would collide), so Pi's
`provider/id` label names the gateway instead of reading `charon`. `/model` therefore lists every endpoint
configured for Pi, and each request goes to the gateway that serves it. Endpoints
only other tools use are left out, and the active binding still sets the default
endpoint, the default model and its own list; `charon off` removes every
`charon*` provider.

### `--json` output
`charon status --json` and `charon ls <tool> --json` emit structured records
(secrets masked, never raw).

### CLI rename / edit / duplicate
`charon rename`, `charon edit`, and `charon cp` match the TUI. `c` (or `x`) in
the menu duplicates a binding: pick a destination tool — this tool leads the
list — then name the copy (prefilled `<name>-copy`). The duplicate shares the
source key and is not activated.

## Deferred / do not do

### Export / import / sync
Moving bindings across machines means moving real secrets. Not planned. The user
keeps their own sites and rotates keys themselves.

### More tools
Only add an adapter the user actually uses. Do not add Gemini, Aider, Cursor,
Continue, or Zed speculatively.

### Persisting model changes made inside the tool
A model picked inside the tool is not written back to its Charon binding. The
binding remains the source of truth, so rendering it again reapplies its saved
default. There is no `refresh` command.
