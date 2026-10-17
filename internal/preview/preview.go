// Package preview runs a binding's apply against copies of a tool's config
// under a temporary home, and reports what it would change — nothing real
// is written.
package preview

import (
	"bytes"
	"os"
	"path/filepath"

	"charon/internal/tools"
)

// FileDiff is one config file's would-be change. Before is nil when the file
// does not exist yet; After is nil when the apply would remove it.
type FileDiff struct {
	Path   string // the tool's real config path, for display
	Before []byte
	After  []byte
}

// Changed reports whether the file would change.
func (d FileDiff) Changed() bool {
	return !bytes.Equal(d.Before, d.After)
}

// Diff applies spec for tool t (rebuilt under a temporary home that holds
// copies of t's config files) and returns what it would change. The real
// files are never touched. Files that would not change are left out.
//
// An absolute path a tool compares against its config (Codex's
// model_catalog_json) is made home-relative by the tools themselves, so the
// sandboxed run decides the same way the real one would.
func Diff(t *tools.Tool, spec tools.AuthSpec) ([]FileDiff, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	sandbox, err := os.MkdirTemp("", "charon-dryrun-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(sandbox) }()

	var diffs []FileDiff
	for _, real := range t.Files {
		rel, err := filepath.Rel(home, real)
		if err != nil || rel == ".." || filepath.IsAbs(rel) {
			continue // not under the home this process sees; nothing to copy
		}
		before, err := os.ReadFile(real)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil {
			dst := filepath.Join(sandbox, rel)
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return nil, err
			}
			info, err := os.Stat(real)
			perm := os.FileMode(0o600)
			if err == nil {
				perm = info.Mode().Perm()
			}
			if err := os.WriteFile(dst, before, perm); err != nil {
				return nil, err
			}
		}
		diffs = append(diffs, FileDiff{Path: real, Before: before})
	}

	// The apply runs with the sandbox as home, so the tool's paths (rebuilt
	// by tools.Find) resolve to the copies.
	oldHome, hadHome := os.LookupEnv("HOME")
	oldProfile, hadProfile := os.LookupEnv("USERPROFILE")
	setenv := func(v string) {
		_ = os.Setenv("HOME", v)
		_ = os.Setenv("USERPROFILE", v)
	}
	setenv(sandbox)
	applyErr := tools.Find(t.Name).ApplyAuth(spec)
	if hadHome {
		_ = os.Setenv("HOME", oldHome)
	} else {
		_ = os.Unsetenv("HOME")
	}
	if hadProfile {
		_ = os.Setenv("USERPROFILE", oldProfile)
	} else {
		_ = os.Unsetenv("USERPROFILE")
	}

	out := diffs[:0]
	for i, d := range diffs {
		rel, err := filepath.Rel(home, d.Path)
		if err != nil {
			continue
		}
		after, err := os.ReadFile(filepath.Join(sandbox, rel))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		// A sandboxed write may name its own home (Codex's model_catalog_json
		// points at the copy's path); show it as the real run would write it.
		after = bytes.ReplaceAll(after, []byte(sandbox), []byte(home))
		diffs[i].After = after
		if !bytes.Equal(d.Before, after) {
			out = append(out, diffs[i])
		}
	}
	return out, applyErr
}
