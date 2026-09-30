package main

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"charon/internal/artifact"
	"charon/internal/catalog"
	"charon/internal/models"
	"charon/internal/secret"
	"charon/internal/tools"
)

// requireTool resolves a tool-name argument, erroring with the supported names.
func requireTool(name string) (*tools.Tool, error) {
	if t := tools.Find(name); t != nil {
		return t, nil
	}
	var names []string
	for _, t := range tools.All() {
		names = append(names, t.Name)
	}
	if name == "" {
		return nil, fmt.Errorf("missing tool name (want %s)", strings.Join(names, ", "))
	}
	return nil, fmt.Errorf("unknown tool %q (want %s)", name, strings.Join(names, ", "))
}

// splitTool returns the first non-flag arg (the tool) and the remaining args.
func splitTool(args []string) (tool string, rest []string) {
	for _, a := range args {
		if tool == "" && !strings.HasPrefix(a, "-") {
			tool = a
			continue
		}
		rest = append(rest, a)
	}
	return tool, rest
}

func printJSON(v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

type statusRow struct {
	Tool     string `json:"tool"`
	Title    string `json:"title"`
	Detected bool   `json:"detected"`
	Active   string `json:"active,omitempty"` // last binding Charon confirmed; kept for JSON compatibility
	AuthMode string `json:"authMode,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
	Account  string `json:"account,omitempty"`
	Secret   string `json:"secret,omitempty"` // masked; never the raw value
}

func cmdStatus(cat *catalog.Catalog, args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	if err := fs.Parse(args); err != nil {
		return err
	}

	var rows []statusRow
	for _, t := range tools.All() {
		r := statusRow{Tool: t.Name, Title: t.Title}
		if b, found, err := cat.Active(t.Name); err != nil {
			return err
		} else if found {
			r.Active = b.Name
		}
		if t.Detected != nil && t.Detected() {
			info, _ := t.Describe()
			r.Detected = true
			r.AuthMode = info.AuthMode
			r.Endpoint = info.Endpoint
			r.Model = info.Model
			r.Effort = info.Effort
			r.Account = info.Account
			r.Secret = secret.Mask(info.Secret)
		}
		rows = append(rows, r)
	}

	if *asJSON {
		return printJSON(rows)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "TOOL\tLAST CONFIRMED\tLIVE AUTH\tLIVE ENDPOINT\tLIVE MODEL\tLIVE EFFORT\tLIVE SECRET")
	for _, r := range rows {
		active := r.Active
		if active == "" {
			active = "—"
		}
		if !r.Detected {
			fmt.Fprintf(w, "%s\t%s\t(not detected)\t\t\t\t\n", r.Title, active)
			continue
		}
		model, effort := r.Model, r.Effort
		if model == "" {
			model = "—"
		}
		if effort == "" {
			effort = "—"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Title, active, r.AuthMode, r.Endpoint, model, effort, r.Secret)
	}
	return w.Flush()
}

type bindingRow struct {
	Name     string `json:"name"`
	Active   bool   `json:"active"`
	Endpoint string `json:"endpoint,omitempty"`
	Model    string `json:"model,omitempty"`
	Extra    int    `json:"extra"`
}

func cmdList(cat *catalog.Catalog, args []string) error {
	toolName, rest := splitTool(args)
	if toolName == "" {
		return fmt.Errorf("usage: charon ls <tool> [--json]")
	}
	t, err := requireTool(toolName)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "machine-readable JSON output")
	if err := fs.Parse(rest); err != nil {
		return err
	}

	active, _, err := cat.Active(t.Name)
	if err != nil {
		return err
	}
	bs, err := cat.Bindings(t.Name)
	if err != nil {
		return err
	}
	var rows []bindingRow
	for _, b := range bs {
		r := bindingRow{Name: b.Name, Active: b.ID == active.ID}
		if view, err := viewOf(cat, b); err == nil {
			r.Endpoint = view.endpoint
			r.Model = view.slug
			r.Extra = view.extra
		}
		rows = append(rows, r)
	}

	if *asJSON {
		return printJSON(rows)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "\tNAME\tENDPOINT\tMODEL\tEXTRA")
	for _, r := range rows {
		marker := "  "
		if r.Active {
			marker = "* "
		}
		model := r.Model
		if model == "" {
			model = "—"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", marker, r.Name, r.Endpoint, model, r.Extra)
	}
	return w.Flush()
}

// viewOf is the display shape of a binding: its provider's URL, default slug,
// and how many picker entries sit beside that default.
type shown struct {
	endpoint string
	slug     string
	slugs    []string
	extra    int
}

func viewOf(cat *catalog.Catalog, b catalog.Binding) (shown, error) {
	cr, err := cat.Credential(b.CredentialID)
	if err != nil {
		return shown{}, err
	}
	p, err := cat.Provider(cr.ProviderID)
	if err != nil {
		return shown{}, err
	}
	slug, err := cat.ModelSlug(b.ModelID)
	if err != nil {
		return shown{}, err
	}
	slugs, err := cat.ModelSlugs(b.Models)
	if err != nil {
		return shown{}, err
	}
	extra := len(slugs) - 1
	if extra < 0 {
		extra = 0
	}
	return shown{endpoint: p.BaseURL, slug: slug, slugs: slugs, extra: extra}, nil
}

func cmdSwitch(cat *catalog.Catalog, args []string) error {
	return cmdApplyBinding(cat, args, false)
}

func cmdReapply(cat *catalog.Catalog, args []string) error {
	return cmdApplyBinding(cat, args, true)
}

func cmdApplyBinding(cat *catalog.Catalog, args []string, reapply bool) error {
	if len(args) < 2 {
		if reapply {
			return fmt.Errorf("usage: charon reapply <tool> <binding>")
		}
		return fmt.Errorf("usage: charon switch <tool> <binding>")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	b, found, err := cat.BindingByName(t.Name, args[1])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no %s binding named %q", t.Title, args[1])
	}
	if reapply {
		_, err = cat.Reapply(b.ID)
	} else {
		_, err = cat.Activate(b.ID)
	}
	if err != nil {
		return err
	}
	if reapply {
		fmt.Printf("Reapplied %s → %s\n", t.Title, args[1])
	} else {
		fmt.Printf("Switched %s → %s\n", t.Title, args[1])
	}
	return nil
}

func cmdModels(cat *catalog.Catalog, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: charon models <tool> --key <key> [--endpoint <url>]")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	endpoint := fs.String("endpoint", "", "API base URL")
	key := fs.String("key", "", "API key")
	local := fs.Bool("local", false, "list models saved in charon's catalog")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *local {
		return listLocalModels(cat, t)
	}
	if err := tools.ValidateKey(*key); err != nil {
		return err
	}
	if err := tools.ValidateEndpoint(*endpoint); err != nil {
		return err
	}
	resolvedEndpoint := t.ResolveEndpoint(*endpoint)
	warnEndpointV1(t, resolvedEndpoint)
	list, err := probeModels(t, resolvedEndpoint, *key)
	if err != nil {
		return err
	}
	for _, m := range list {
		if w := models.DefaultContextWindow(m.ID); w > 0 {
			fmt.Printf("%s\t%d\n", m.ID, w)
		} else {
			fmt.Println(m.ID)
		}
	}
	return nil
}

func listLocalModels(cat *catalog.Catalog, t *tools.Tool) error {
	bindings, err := cat.Bindings(t.Name)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	writer := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	for _, binding := range bindings {
		for _, modelID := range binding.Models {
			m, err := cat.Model(modelID)
			if err != nil {
				return err
			}
			if seen[m.Slug] {
				continue
			}
			seen[m.Slug] = true
			window := "unknown"
			if m.ContextWindow > 0 {
				window = strconv.Itoa(m.ContextWindow)
			}
			fmt.Fprintf(writer, "%s\t%s\n", m.Slug, window)
		}
	}
	return writer.Flush()
}

func cmdSetContext(cat *catalog.Catalog, args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("usage: charon set-context <tool> <binding> <model> <tokens|unknown>")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	binding, found, err := cat.BindingByName(t.Name, args[1])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no %s binding named %q", t.Title, args[1])
	}
	slug := strings.TrimSpace(args[2])
	if slug == "" {
		return fmt.Errorf("model is required")
	}
	value := strings.TrimSpace(args[3])
	window := 0
	if value != "unknown" {
		window, err = strconv.Atoi(value)
		if err != nil || window <= 0 {
			return fmt.Errorf("context window must be a positive whole number or unknown")
		}
	}
	var model catalog.Model
	for _, modelID := range binding.Models {
		candidate, err := cat.Model(modelID)
		if err != nil {
			return err
		}
		if candidate.Slug == slug {
			model = candidate
			break
		}
	}
	if model.ID == "" {
		return fmt.Errorf("model %q is not registered in binding %q", slug, binding.Name)
	}
	if _, err := cat.SetModelWindow(model.ID, window); err != nil {
		return err
	}
	if _, err := cat.ProjectIfActive(binding.ID); err != nil {
		return err
	}
	if window == 0 {
		fmt.Printf("Set context window for %s to unknown\n", slug)
	} else {
		fmt.Printf("Set context window for %s to %d\n", slug, window)
	}
	return nil
}

func cmdSetEffort(cat *catalog.Catalog, args []string) error {
	if len(args) < 4 {
		return fmt.Errorf("usage: charon set-effort <tool> <binding> <model> <low|medium|high|xhigh|max|ultra>")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	if t.Name != "codex" {
		return fmt.Errorf("%s does not support reasoning effort", t.Title)
	}
	binding, found, err := cat.BindingByName(t.Name, args[1])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no %s binding named %q", t.Title, args[1])
	}
	slug := strings.TrimSpace(args[2])
	if slug == "" {
		return fmt.Errorf("model is required")
	}
	effort := strings.ToLower(strings.TrimSpace(args[3]))
	if err := tools.ValidateCodexEffort(effort); err != nil {
		return err
	}
	var model catalog.Model
	for _, modelID := range binding.Models {
		candidate, err := cat.Model(modelID)
		if err != nil {
			return err
		}
		if candidate.Slug == slug {
			model = candidate
			break
		}
	}
	if model.ID == "" {
		return fmt.Errorf("model %q is not registered in binding %q", slug, binding.Name)
	}
	if _, err := cat.SetModelEffort(model.ID, effort); err != nil {
		return err
	}
	if _, err := cat.ProjectIfActive(binding.ID); err != nil {
		return err
	}
	fmt.Printf("Set effort for %s to %s\n", slug, effort)
	return nil
}

// probeModels asks the endpoint for its model list in the tool's historical dialect
// first, then the other. Which dialect answered is not stored: the same site often
// speaks both, and the next call can try again.
func probeModels(t *tools.Tool, endpoint, key string) ([]models.Info, error) {
	first := models.Provider(t.Provider)
	list, err := models.Fetch(first, endpoint, key)
	if err == nil {
		return list, nil
	}
	other := models.OpenAI
	if first == models.OpenAI {
		other = models.Anthropic
	}
	if list, err2 := models.Fetch(other, endpoint, key); err2 == nil {
		return list, nil
	}
	return nil, err
}

// splitModels parses a --models value ("a, b ,c") into ids, dropping blanks so a
// trailing comma or an empty flag yields no list rather than an empty-string id.
func splitModels(list string) []string {
	var ids []string
	for _, id := range strings.Split(list, ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// describeModels summarizes the slugs actually stored, for the confirmation line.
func describeModels(slug string, slugs []string) string {
	if slug == "" {
		return "no model"
	}
	if extra := len(slugs) - 1; extra > 0 {
		return fmt.Sprintf("%s +%d more in the tool's picker", slug, extra)
	}
	return slug
}

func cmdAdd(cat *catalog.Catalog, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: charon add <tool> --name <b> --key <k> [--endpoint <url>] [--model <id>] [--models <id,...>]")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	endpoint := fs.String("endpoint", "", "API base URL")
	key := fs.String("key", "", "API key")
	model := fs.String("model", "", "model id")
	modelList := fs.String("models", "", "comma-separated model ids to offer in the tool's own picker")
	name := fs.String("name", "", "binding name")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if t.ApplyAuth == nil {
		return fmt.Errorf("%s does not support add", t.Title)
	}
	if *name == "" {
		return fmt.Errorf("--name is required")
	}
	if err := tools.ValidateKey(*key); err != nil {
		return err
	}
	if err := tools.ValidateEndpoint(*endpoint); err != nil {
		return err
	}
	warnEndpointV1(t, t.ResolveEndpoint(*endpoint))
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "models" {
			explicit = true
		}
	})
	b, err := catalog.StoreBinding(cat, t, nil, *name, *endpoint, *key, *model, splitModels(*modelList), explicit, nil)
	if err != nil {
		return err
	}
	if _, err := cat.Activate(b.ID); err != nil {
		return err
	}
	view, err := viewOf(cat, b)
	if err != nil {
		return err
	}
	fmt.Printf("Added and activated %s binding %q (%s · %s)\n", t.Title, b.Name, view.endpoint, describeModels(view.slug, view.slugs))
	return nil
}

func cmdEdit(cat *catalog.Catalog, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: charon edit <tool> <binding> [--endpoint --key --model --models --name]")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	name := args[1]
	cur, found, err := cat.BindingByName(t.Name, name)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no %s binding named %q", t.Title, name)
	}
	view, err := viewOf(cat, cur)
	if err != nil {
		return err
	}
	cr, err := cat.Credential(cur.CredentialID)
	if err != nil {
		return err
	}

	// Flags default to the current values, so an unset flag leaves that field unchanged.
	// --models is not defaulted: whether the user passed it decides if the picker is rewritten.
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	endpoint := fs.String("endpoint", view.endpoint, "API base URL")
	key := fs.String("key", cr.Key, "API key")
	model := fs.String("model", view.slug, "model id")
	modelList := fs.String("models", "", "comma-separated model ids to offer in the tool's own picker")
	newName := fs.String("name", "", "rename the binding")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	explicit := false
	modelExplicit := false
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "models":
			explicit = true
		case "model":
			modelExplicit = true
		}
	})
	modelID := strings.TrimSpace(*model)
	if explicit && !modelExplicit {
		requested := splitModels(*modelList)
		if len(requested) > 0 && !slices.Contains(requested, modelID) {
			// Match add: when a new picker list excludes the old default, its first
			// entry becomes the new default unless --model was explicitly supplied.
			modelID = requested[0]
		}
	}
	target := name
	if *newName != "" {
		target = *newName
	}
	if err := tools.ValidateKey(*key); err != nil {
		return err
	}
	if err := tools.ValidateEndpoint(*endpoint); err != nil {
		return err
	}
	warnEndpointV1(t, t.ResolveEndpoint(*endpoint))

	b, err := catalog.StoreBinding(cat, t, &cur, target, *endpoint, *key, modelID, splitModels(*modelList), explicit, nil)
	if err != nil {
		return err
	}
	// The active check and projection are serialized with switch so the tool config
	// cannot end up describing a binding that the active pointer no longer names.
	if _, err := cat.ProjectIfActive(b.ID); err != nil {
		return err
	}
	stored, err := viewOf(cat, b)
	if err != nil {
		return err
	}
	fmt.Printf("Updated %s binding %q (%s · %s)\n", t.Title, b.Name, stored.endpoint, describeModels(stored.slug, stored.slugs))
	return nil
}

func warnEndpointV1(t *tools.Tool, endpoint string) {
	if tools.EndpointHasClaudeV1(t, endpoint) {
		fmt.Fprintln(os.Stderr, "warning: endpoint includes /v1; Claude Code requests /v1/messages, so confirm the gateway's documented base URL")
		return
	}
	if tools.EndpointNeedsV1Hint(t, endpoint) {
		fmt.Fprintln(os.Stderr, "warning: endpoint does not contain /v1; model discovery tries /v1/models, but actual requests depend on the gateway documentation")
	}
}

func cmdRename(cat *catalog.Catalog, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: charon rename <tool> <old> <new>")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	b, found, err := cat.BindingByName(t.Name, args[1])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no %s binding named %q", t.Title, args[1])
	}
	slugs, err := cat.ModelSlugs(b.Models)
	if err != nil {
		return err
	}
	slug, err := cat.ModelSlug(b.ModelID)
	if err != nil {
		return err
	}
	if _, err := cat.UpdateBinding(b.ID, args[2], b.CredentialID, slug, slugs); err != nil {
		return err
	}
	fmt.Printf("Renamed %s binding %q → %q\n", t.Title, args[1], args[2])
	return nil
}

func cmdDuplicate(cat *catalog.Catalog, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: charon cp <tool> <src> <dst> | charon cp <tool> <src> <tool> <dst>")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	dstTool := t
	dstName := args[2]
	if len(args) >= 4 {
		dstTool, err = requireTool(args[2])
		if err != nil {
			return err
		}
		dstName = args[3]
	}
	b, found, err := cat.BindingByName(t.Name, args[1])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no %s binding named %q", t.Title, args[1])
	}
	slugs, err := cat.ModelSlugs(b.Models)
	if err != nil {
		return err
	}
	slug, err := cat.ModelSlug(b.ModelID)
	if err != nil {
		return err
	}
	cr, err := cat.Credential(b.CredentialID)
	if err != nil {
		return err
	}
	p, err := cat.Provider(cr.ProviderID)
	if err != nil {
		return err
	}
	if _, err := catalog.StoreBinding(cat, dstTool, nil, dstName, p.BaseURL, cr.Key, slug, slugs, true, nil); err != nil {
		return err
	}
	fmt.Printf("Copied %s binding %q → %s %q\n", t.Title, args[1], dstTool.Title, dstName)
	return nil
}

func cmdRemove(cat *catalog.Catalog, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: charon rm <tool> <binding>")
	}
	t, err := requireTool(args[0])
	if err != nil {
		return err
	}
	b, found, err := cat.BindingByName(t.Name, args[1])
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("no %s binding named %q", t.Title, args[1])
	}
	active, activeFound, err := cat.Active(t.Name)
	if err != nil {
		return err
	}
	if activeFound && active.ID == b.ID {
		return fmt.Errorf("binding %q is active for %s; switch to another binding before deleting it", args[1], t.Title)
	}
	if err := cat.RemoveBinding(b.ID); err != nil {
		return err
	}
	fmt.Printf("Removed binding %q for %s\n", args[1], t.Title)
	return nil
}

func cmdCompletion(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: charon completion [bash|zsh|fish]")
	}
	switch args[0] {
	case "bash":
		fmt.Print(bashCompletion)
	case "zsh":
		fmt.Print(zshCompletion)
	case "fish":
		fmt.Print(fishCompletion)
	default:
		return fmt.Errorf("unsupported shell %q (want bash, zsh, or fish)", args[0])
	}
	return nil
}

// cmdProfiles prints one binding name per line for a tool; used by shell completion.
func cmdProfiles(cat *catalog.Catalog, args []string) error {
	if len(args) < 1 {
		return nil
	}
	t := tools.Find(args[0])
	if t == nil {
		return nil
	}
	bs, err := cat.Bindings(t.Name)
	if err != nil {
		return err
	}
	for _, b := range bs {
		fmt.Println(b.Name)
	}
	return nil
}

// cmdUninstall removes the running charon binary.
func cmdUninstall() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate running binary: %w", err)
	}
	fmt.Printf("Removing charon binary at %s ...\n", exe)
	if err := os.Remove(exe); err != nil {
		return fmt.Errorf("failed to remove binary: %w. Try running with sudo if needed", err)
	}
	fmt.Println("Charon binary uninstalled successfully.")
	fmt.Println("Note: Your saved bindings at ~/.config/charon remain intact.")
	fmt.Println("To completely remove them, run: rm -rf ~/.config/charon")
	return nil
}

const releaseBaseURL = "https://github.com/Administration-626/charon/releases/latest/download"

// cmdUpdate downloads and verifies the release binary without executing a remote script.
func cmdUpdate() error {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return fmt.Errorf("self-update is supported only on linux and darwin")
	}
	archiveName := fmt.Sprintf("charon_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	tmpDir, err := os.MkdirTemp("", "charon-update-")
	if err != nil {
		return fmt.Errorf("creating update directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	archivePath := filepath.Join(tmpDir, archiveName)
	if err := downloadRelease(releaseBaseURL+"/"+archiveName, archivePath); err != nil {
		return fmt.Errorf("downloading %s: %w", archiveName, err)
	}
	checksumsPath := filepath.Join(tmpDir, "checksums.txt")
	if err := downloadRelease(releaseBaseURL+"/checksums.txt", checksumsPath); err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	expected, err := checksumFor(checksumsPath, archiveName)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(archivePath)
	if err != nil {
		return fmt.Errorf("reading downloaded archive: %w", err)
	}
	actual := fmt.Sprintf("%x", sha256.Sum256(data))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s (expected %s, got %s)", archiveName, expected, actual)
	}
	binary, err := extractReleaseBinary(archivePath)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating current binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("resolving current binary: %w", err)
	}
	if err := artifact.AtomicWrite(exe, binary, 0o755); err != nil {
		return fmt.Errorf("installing updated binary: %w", err)
	}
	fmt.Printf("Updated %s from verified release %s\n", exe, archiveName)
	return nil
}

func downloadRelease(url, path string) error {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url) //nolint:gosec // URL is fixed to the project's release host.
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %s", resp.Status)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func checksumFor(path, archiveName string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading checksums.txt: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.TrimPrefix(fields[1], "*") == archiveName {
			if len(fields[0]) != sha256.Size*2 {
				return "", fmt.Errorf("invalid checksum for %s", archiveName)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", archiveName)
}

func extractReleaseBinary(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening release archive: %w", err)
	}
	defer func() { _ = f.Close() }()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("reading release archive: %w", err)
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading release archive: %w", err)
		}
		if header.Typeflag == tar.TypeReg && strings.TrimPrefix(header.Name, "./") == "charon" {
			if header.Size <= 0 || header.Size > 100<<20 {
				return nil, fmt.Errorf("invalid charon entry size %d", header.Size)
			}
			return io.ReadAll(io.LimitReader(tr, header.Size))
		}
	}
	return nil, fmt.Errorf("release archive does not contain charon binary")
}
