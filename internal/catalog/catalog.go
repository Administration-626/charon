// Package catalog stores charon's data as four flat tables plus one active pointer.
//
//	providers.json     id, base_url                       a site, shared across tools
//	credentials.json   id, provider_id, key               a key, belonging to one site
//	models.json        id, provider_id, slug              a model slug, belonging to one site
//	bindings.json      id, name, tool, credential_id,     one tool's saved choice
//	                   model_id, models
//	active.json        tool -> binding_id                 which binding a tool renders
//
// A binding references a credential and models; it never copies them, so one key can
// back Claude and Codex at once. The one integrity rule: every model a binding names
// must belong to the same provider as its credential — a slug from one site paired
// with a key for another can never be stored.
//
// Catalog methods hold these rows. project.go renders a binding into its tool's
// live config through the tool adapter.
package catalog

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode"

	"charon/internal/artifact"
)

// Provider is one API site. It carries no wire dialect: a single endpoint can speak
// both the OpenAI and the Anthropic dialect, so which one to try is decided per
// request, not stored.
type Provider struct {
	ID      string `json:"id"`
	BaseURL string `json:"baseUrl"`
}

// Credential is one API key for a provider.
type Credential struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	Key        string `json:"key"`
}

// Model is one model slug offered by a provider.
type Model struct {
	ID         string `json:"id"`
	ProviderID string `json:"providerId"`
	Slug       string `json:"slug"`
}

// Binding is one saved choice for one tool: which credential, which model is the
// default, and which models the tool's own picker should offer. Name is the label
// the user sees and types.
type Binding struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Tool         string   `json:"tool"`
	CredentialID string   `json:"credentialId"`
	ModelID      string   `json:"modelId"`
	Models       []string `json:"models"`
}

// Catalog is the on-disk store, rooted at ~/.config/charon.
type Catalog struct {
	Root string

	// lockFile/lockMu serialize each complete mutation across processes (advisory
	// flock on a .lock file) and goroutines in this process.
	lockFile *os.File
	lockMu   sync.Mutex
}

// ErrNotFound is returned when a row a caller asked for by id does not exist.
var ErrNotFound = errors.New("not found")

// SingleModelTools are tools whose config has no field for a model list, so a
// binding for one may carry only its default model. Codex's model_providers table
// has no such field; its /model menu lists built-in presets only.
var SingleModelTools = map[string]bool{"codex": true}

// Open returns the catalog rooted at $XDG_CONFIG_HOME/charon (default ~/.config/charon).
func Open() (*Catalog, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		base = filepath.Join(h, ".config")
	}
	root := filepath.Join(base, "charon")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	c := &Catalog{Root: root}
	if err := c.openLock(); err != nil {
		return nil, err
	}
	if err := c.migrateLegacyProfiles(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Catalog) lock() error {
	c.lockMu.Lock()
	if err := c.acquireOSLock(); err != nil {
		c.lockMu.Unlock()
		return fmt.Errorf("acquiring catalog lock: %w", err)
	}
	return nil
}

func (c *Catalog) unlock() {
	_ = c.releaseOSLock()
	c.lockMu.Unlock()
}

func (c *Catalog) providers() ([]Provider, error) {
	return readTable[Provider](c.table("providers.json"))
}
func (c *Catalog) credentials() ([]Credential, error) {
	return readTable[Credential](c.table("credentials.json"))
}
func (c *Catalog) models() ([]Model, error) { return readTable[Model](c.table("models.json")) }
func (c *Catalog) bindings() ([]Binding, error) {
	return readTable[Binding](c.table("bindings.json"))
}

func (c *Catalog) table(name string) string { return filepath.Join(c.Root, name) }

// Providers returns every provider, ordered by id.
func (c *Catalog) Providers() ([]Provider, error) {
	ps, err := c.providers()
	if err != nil {
		return nil, err
	}
	sort.Slice(ps, func(i, j int) bool { return ps[i].ID < ps[j].ID })
	return ps, nil
}

// ProviderByURL returns the provider serving baseURL, matched with trailing slashes
// ignored, or found=false when none does.
func (c *Catalog) ProviderByURL(baseURL string) (Provider, bool, error) {
	ps, err := c.providers()
	if err != nil {
		return Provider{}, false, err
	}
	want := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	for _, p := range ps {
		if strings.TrimRight(p.BaseURL, "/") == want {
			return p, true, nil
		}
	}
	return Provider{}, false, nil
}

// PutProvider inserts a provider or, when one with the same base URL already
// exists, returns that one untouched. Providers are keyed by where they point, not
// by a name the user has to invent.
func (c *Catalog) PutProvider(baseURL string) (Provider, error) {
	if err := c.lock(); err != nil {
		return Provider{}, err
	}
	defer c.unlock()
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return Provider{}, fmt.Errorf("provider base URL is required")
	}
	ps, err := c.providers()
	if err != nil {
		return Provider{}, err
	}
	for _, p := range ps {
		if strings.TrimRight(p.BaseURL, "/") == baseURL {
			return p, nil
		}
	}
	p := Provider{ID: c.nextID(providerIDs(ps), "p"), BaseURL: baseURL}
	return p, writeTable(c.table("providers.json"), append(ps, p))
}

// PutCredential inserts a key for a provider, or returns the existing row when that
// provider already has that exact key. The key is stored once however many bindings
// reference it.
func (c *Catalog) PutCredential(providerID, key string) (Credential, error) {
	if err := c.lock(); err != nil {
		return Credential{}, err
	}
	defer c.unlock()
	key = strings.TrimSpace(key)
	if key == "" {
		return Credential{}, fmt.Errorf("credential key is required")
	}
	if err := c.mustProvider(providerID); err != nil {
		return Credential{}, err
	}
	cs, err := c.credentials()
	if err != nil {
		return Credential{}, err
	}
	for _, cr := range cs {
		if cr.ProviderID == providerID && cr.Key == key {
			return cr, nil
		}
	}
	cr := Credential{ID: c.nextID(credentialIDs(cs), "k"), ProviderID: providerID, Key: key}
	return cr, writeTable(c.table("credentials.json"), append(cs, cr))
}

// PutModel inserts a slug for a provider, or returns the existing row when that
// provider already has that slug.
func (c *Catalog) PutModel(providerID, slug string) (Model, error) {
	if err := c.lock(); err != nil {
		return Model{}, err
	}
	defer c.unlock()
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return Model{}, fmt.Errorf("model slug is required")
	}
	if err := c.mustProvider(providerID); err != nil {
		return Model{}, err
	}
	ms, err := c.models()
	if err != nil {
		return Model{}, err
	}
	for _, m := range ms {
		if m.ProviderID == providerID && m.Slug == slug {
			return m, nil
		}
	}
	m := Model{ID: c.nextID(modelIDs(ms), "m"), ProviderID: providerID, Slug: slug}
	return m, writeTable(c.table("models.json"), append(ms, m))
}

// AddBinding stores a new binding for a tool. models are the slugs its picker
// should offer; defaultSlug is the one selected on render and must be among them.
// Every slug must belong to the credential's provider, and a tool in
// SingleModelTools may name only one.
func (c *Catalog) AddBinding(tool, name, credentialID, defaultSlug string, models []string) (Binding, error) {
	if err := c.lock(); err != nil {
		return Binding{}, err
	}
	defer c.unlock()
	b, err := c.prepareBinding("", "", tool, name, credentialID, defaultSlug, models)
	if err != nil {
		return Binding{}, err
	}
	bs, err := c.bindings()
	if err != nil {
		return Binding{}, err
	}
	b.ID = c.nextID(bindingIDs(bs), "b")
	return b, writeTable(c.table("bindings.json"), append(bs, b))
}

// UpdateBinding replaces the editable fields on an existing binding. Its tool is
// fixed; a credential can be shared by adding another binding for a different tool.
func (c *Catalog) UpdateBinding(id, name, credentialID, defaultSlug string, models []string) (Binding, error) {
	if err := c.lock(); err != nil {
		return Binding{}, err
	}
	defer c.unlock()
	bs, err := c.bindings()
	if err != nil {
		return Binding{}, err
	}
	idx := indexBinding(bs, id)
	if idx < 0 {
		return Binding{}, fmt.Errorf("binding %q: %w", id, ErrNotFound)
	}
	b, err := c.prepareBinding(id, bs[idx].Name, bs[idx].Tool, name, credentialID, defaultSlug, models)
	if err != nil {
		return Binding{}, err
	}
	b.ID = id
	bs[idx] = b
	if err := writeTable(c.table("bindings.json"), bs); err != nil {
		return Binding{}, err
	}
	return b, nil
}

// prepareBinding validates and assembles a binding, resolving each slug to its id.
// selfID is the binding being updated, excluded from the per-tool name uniqueness
// check; "" when adding. oldName lets an existing legacy label survive unchanged.
func (c *Catalog) prepareBinding(selfID, oldName, tool, name, credentialID, defaultSlug string, models []string) (Binding, error) {
	var zero Binding
	if selfID == "" || name != oldName {
		if err := validateName(name); err != nil {
			return zero, err
		}
	}
	if strings.TrimSpace(tool) == "" {
		return zero, fmt.Errorf("binding tool is required")
	}
	cr, err := c.mustCredential(credentialID)
	if err != nil {
		return zero, err
	}
	slugs := cleanSlugs(append(models, defaultSlug))
	if len(slugs) == 0 {
		return zero, fmt.Errorf("a binding needs at least one model")
	}
	if SingleModelTools[tool] && len(slugs) > 1 {
		return zero, fmt.Errorf("%s can register only one model, got %d", tool, len(slugs))
	}
	if defaultSlug = strings.TrimSpace(defaultSlug); defaultSlug == "" {
		defaultSlug = slugs[0]
	}
	ms, err := c.models()
	if err != nil {
		return zero, err
	}
	ids := make([]string, 0, len(slugs))
	defaultID := ""
	for _, slug := range slugs {
		found := false
		for _, m := range ms {
			if m.ProviderID == cr.ProviderID && m.Slug == slug {
				ids = append(ids, m.ID)
				if slug == defaultSlug {
					defaultID = m.ID
				}
				found = true
				break
			}
		}
		if !found {
			return zero, fmt.Errorf("model %q is not registered for this credential's provider", slug)
		}
	}
	if defaultID == "" {
		return zero, fmt.Errorf("default model %q is not among the binding's models", defaultSlug)
	}
	bs, err := c.bindings()
	if err != nil {
		return zero, err
	}
	for _, b := range bs {
		if b.ID != selfID && b.Tool == tool && b.Name == name {
			return zero, fmt.Errorf("a %s binding named %q already exists", tool, name)
		}
	}
	return Binding{Name: name, Tool: tool, CredentialID: credentialID, ModelID: defaultID, Models: ids}, nil
}

// BindingByName returns the binding of that name for a tool.
func (c *Catalog) BindingByName(tool, name string) (Binding, bool, error) {
	bs, err := c.bindings()
	if err != nil {
		return Binding{}, false, err
	}
	for _, b := range bs {
		if b.Tool == tool && b.Name == name {
			return b, true, nil
		}
	}
	return Binding{}, false, nil
}

// Bindings returns a tool's bindings, ordered by name.
func (c *Catalog) Bindings(tool string) ([]Binding, error) {
	bs, err := c.bindings()
	if err != nil {
		return nil, err
	}
	var out []Binding
	for _, b := range bs {
		if b.Tool == tool {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// RemoveBinding deletes a binding and clears it from the active pointer if it was
// the one rendered. The credential and models it referenced stay; other bindings
// may still use them.
func (c *Catalog) RemoveBinding(id string) error {
	if err := c.lock(); err != nil {
		return err
	}
	defer c.unlock()
	bs, err := c.bindings()
	if err != nil {
		return err
	}
	idx := indexBinding(bs, id)
	if idx < 0 {
		return fmt.Errorf("binding %q: %w", id, ErrNotFound)
	}
	tool := bs[idx].Tool
	bs = append(bs[:idx], bs[idx+1:]...)
	if err := writeTable(c.table("bindings.json"), bs); err != nil {
		return err
	}
	active, err := c.readActive()
	if err != nil {
		return err
	}
	if active[tool] == id {
		delete(active, tool)
		return c.writeActive(active)
	}
	return nil
}

func (c *Catalog) setActiveForTool(tool, id string) error {
	active, err := c.readActive()
	if err != nil {
		return err
	}
	active[tool] = id
	return c.writeActive(active)
}

// ClearActive forgets which binding a tool renders, leaving the tool unconfigured
// by charon.
func (c *Catalog) ClearActive(tool string) error {
	if err := c.lock(); err != nil {
		return err
	}
	defer c.unlock()
	active, err := c.readActive()
	if err != nil {
		return err
	}
	if _, ok := active[tool]; !ok {
		return nil
	}
	delete(active, tool)
	return c.writeActive(active)
}

// Active returns the binding a tool currently renders, or found=false when the tool
// has none.
func (c *Catalog) Active(tool string) (Binding, bool, error) {
	active, err := c.readActive()
	if err != nil {
		return Binding{}, false, err
	}
	id, ok := active[tool]
	if !ok {
		return Binding{}, false, nil
	}
	bs, err := c.bindings()
	if err != nil {
		return Binding{}, false, err
	}
	if idx := indexBinding(bs, id); idx >= 0 {
		return bs[idx], true, nil
	}
	return Binding{}, false, nil
}

// Credential returns a credential by id.
func (c *Catalog) Credential(id string) (Credential, error) {
	cs, err := c.credentials()
	if err != nil {
		return Credential{}, err
	}
	for _, cr := range cs {
		if cr.ID == id {
			return cr, nil
		}
	}
	return Credential{}, fmt.Errorf("credential %q: %w", id, ErrNotFound)
}

// Model returns a model by id.
func (c *Catalog) Model(id string) (Model, error) {
	ms, err := c.models()
	if err != nil {
		return Model{}, err
	}
	for _, m := range ms {
		if m.ID == id {
			return m, nil
		}
	}
	return Model{}, fmt.Errorf("model %q: %w", id, ErrNotFound)
}

// Provider returns a provider by id.
func (c *Catalog) Provider(id string) (Provider, error) {
	ps, err := c.providers()
	if err != nil {
		return Provider{}, err
	}
	for _, p := range ps {
		if p.ID == id {
			return p, nil
		}
	}
	return Provider{}, fmt.Errorf("provider %q: %w", id, ErrNotFound)
}

// ModelSlug resolves a model id to its slug.
func (c *Catalog) ModelSlug(id string) (string, error) {
	m, err := c.Model(id)
	if err != nil {
		return "", err
	}
	return m.Slug, nil
}

// ModelSlugs resolves model ids to slugs, in the same order.
func (c *Catalog) ModelSlugs(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		slug, err := c.ModelSlug(id)
		if err != nil {
			return nil, err
		}
		out = append(out, slug)
	}
	return out, nil
}

func (c *Catalog) mustProvider(id string) error {
	ps, err := c.providers()
	if err != nil {
		return err
	}
	for _, p := range ps {
		if p.ID == id {
			return nil
		}
	}
	return fmt.Errorf("provider %q: %w", id, ErrNotFound)
}

func (c *Catalog) mustCredential(id string) (Credential, error) {
	cs, err := c.credentials()
	if err != nil {
		return Credential{}, err
	}
	for _, cr := range cs {
		if cr.ID == id {
			return cr, nil
		}
	}
	return Credential{}, fmt.Errorf("credential %q: %w", id, ErrNotFound)
}

func (c *Catalog) readActive() (map[string]string, error) {
	var active map[string]string
	data, err := os.ReadFile(c.table("active.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]string{}, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &active); err != nil {
		return nil, fmt.Errorf("reading active.json: %w", err)
	}
	if active == nil {
		active = map[string]string{}
	}
	return active, nil
}

func (c *Catalog) writeActive(active map[string]string) error {
	data, err := json.MarshalIndent(active, "", "  ")
	if err != nil {
		return err
	}
	return artifact.AtomicWrite(c.table("active.json"), append(data, '\n'), 0o600)
}

// nextID returns the lowest "<prefix><n>" not present in taken, starting at 1.
func (c *Catalog) nextID(taken map[string]bool, prefix string) string {
	for n := 1; ; n++ {
		id := fmt.Sprintf("%s%d", prefix, n)
		if !taken[id] {
			return id
		}
	}
}

func providerIDs(ps []Provider) map[string]bool {
	out := make(map[string]bool, len(ps))
	for _, p := range ps {
		out[p.ID] = true
	}
	return out
}

func credentialIDs(cs []Credential) map[string]bool {
	out := make(map[string]bool, len(cs))
	for _, cr := range cs {
		out[cr.ID] = true
	}
	return out
}

func modelIDs(ms []Model) map[string]bool {
	out := make(map[string]bool, len(ms))
	for _, m := range ms {
		out[m.ID] = true
	}
	return out
}

func bindingIDs(bs []Binding) map[string]bool {
	out := make(map[string]bool, len(bs))
	for _, b := range bs {
		out[b.ID] = true
	}
	return out
}

func indexBinding(bs []Binding, id string) int {
	for i, b := range bs {
		if b.ID == id {
			return i
		}
	}
	return -1
}

// cleanSlugs trims, drops blanks, and de-duplicates while keeping first-seen order.
func cleanSlugs(in []string) []string {
	seen := make(map[string]bool, len(in))
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// validateName rejects names that cannot be safely typed as one CLI argument.
func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("name is required")
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return fmt.Errorf("invalid name %q (no whitespace)", name)
		}
	}
	return nil
}

// readTable loads a JSON array, treating a missing file as an empty table.
func readTable[T any](path string) ([]T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var rows []T
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	return rows, nil
}

// writeTable writes a table atomically. Credentials live in credentials.json, so
// every table is written 0600 — a provider list is not secret, but one mode keeps
// the whole catalog equally unreadable.
func writeTable[T any](path string, rows []T) error {
	if rows == nil {
		rows = []T{}
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}
	return artifact.AtomicWrite(path, append(data, '\n'), 0o600)
}
