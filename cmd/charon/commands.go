package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"strings"
	"text/tabwriter"

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
	Active   string `json:"active,omitempty"`
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
		if t.Detected != nil && t.Detected() {
			info, _ := t.Describe()
			r.Detected = true
			if b, found, err := cat.Active(t.Name); err == nil && found {
				r.Active = b.Name
			}
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
	fmt.Fprintln(w, "TOOL\tACTIVE\tAUTH\tENDPOINT\tMODEL\tEFFORT\tSECRET")
	for _, r := range rows {
		if !r.Detected {
			fmt.Fprintf(w, "%s\t—\t(not detected)\t\t\t\t\n", r.Title)
			continue
		}
		active := r.Active
		if active == "" {
			active = "—"
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
	if len(args) < 2 {
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
	if _, err := cat.Activate(b.ID); err != nil {
		return err
	}
	fmt.Printf("Switched %s → %s\n", t.Title, args[1])
	return nil
}

func cmdModels(args []string) error {
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
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if err := tools.ValidateKey(*key); err != nil {
		return err
	}
	if err := tools.ValidateEndpoint(*endpoint); err != nil {
		return err
	}
	list, err := probeModels(t, t.ResolveEndpoint(*endpoint), *key)
	if err != nil {
		return err
	}
	for _, m := range list {
		fmt.Println(m)
	}
	return nil
}

// probeModels asks the endpoint for its model list in the tool's historical dialect
// first, then the other. Which dialect answered is not stored: the same site often
// speaks both, and the next call can try again.
func probeModels(t *tools.Tool, endpoint, key string) ([]string, error) {
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
	explicit := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "models" {
			explicit = true
		}
	})
	b, err := catalog.StoreBinding(cat, t, nil, *name, *endpoint, *key, *model, splitModels(*modelList), explicit)
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

	b, err := catalog.StoreBinding(cat, t, &cur, target, *endpoint, *key, modelID, splitModels(*modelList), explicit)
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
		return fmt.Errorf("usage: charon cp <tool> <src> <dst>")
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
	if _, err := cat.AddBinding(b.Tool, args[2], b.CredentialID, slug, slugs); err != nil {
		return err
	}
	fmt.Printf("Copied %s binding %q → %q\n", t.Title, args[1], args[2])
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

// cmdUpdate runs the online install.sh script to upgrade the binary.
func cmdUpdate() error {
	fmt.Println("Checking for updates and upgrading charon ...")

	// Download the install script to a temp file instead of piping curl to sh directly,
	// so we can verify the download and show the user what will be executed.
	scriptURL := "https://github.com/Administration-626/charon/releases/latest/download/install.sh"
	tmpFile, err := os.CreateTemp("", "charon-install-*.sh")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	resp, err := http.Get(scriptURL) //nolint:noctx
	if err != nil {
		return fmt.Errorf("downloading install script: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed with status %s", resp.Status)
	}

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("saving install script: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}

	// Basic sanity check: the script must start with a shebang.
	header, err := os.ReadFile(tmpPath)
	if err != nil {
		return fmt.Errorf("reading downloaded script: %w", err)
	}
	if len(header) > 20 {
		header = header[:20]
	}
	if !strings.HasPrefix(string(header), "#!/") {
		return fmt.Errorf("downloaded file does not look like a shell script (header: %q)", string(header))
	}

	fmt.Printf("Downloaded install script to %s\n", tmpPath)
	fmt.Println("The script will:")
	fmt.Println("  - Detect your OS and architecture")
	fmt.Println("  - Download the latest charon binary")
	fmt.Println("  - Verify its checksum")
	fmt.Println("  - Install it to ~/.local/bin/charon (or $PREFIX/bin)")
	fmt.Println()
	fmt.Println("Executing install script ...")

	cmd := exec.Command("sh", tmpPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}
	return nil
}
