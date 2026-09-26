package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	toml "github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"

	"charon/internal/artifact"
)

// loadJSONMap reads path as a JSON object, returning an empty map if absent.
func loadJSONMap(path string) (map[string]any, error) {
	m := map[string]any{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// writeJSONMap writes m to path (0600) as indented JSON, atomically.
func writeJSONMap(path string, m map[string]any, perm os.FileMode) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return artifact.AtomicWrite(path, append(data, '\n'), perm)
}

// loadTOMLMap reads path as a TOML table, returning an empty map if absent.
func loadTOMLMap(path string) (map[string]any, error) {
	m := map[string]any{}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := toml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// writeTOMLMap writes m to path (0644) as TOML, atomically.
func writeTOMLMap(path string, m map[string]any, perm os.FileMode) error {
	data, err := toml.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return artifact.AtomicWrite(path, data, perm)
}

// subMap returns m[key] as a map, creating it if missing.
func subMap(m map[string]any, key string) map[string]any {
	if existing, ok := m[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	m[key] = created
	return created
}

// loadYAMLDoc reads path as a YAML document, returning an empty mapping when
// the file is absent. Editing the parsed node tree keeps key order and
// comments intact across a load-merge-write cycle.
func loadYAMLDoc(path string) (*yaml.Node, error) {
	empty := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return empty, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	if _, err := yamlDocMap(&doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &doc, nil
}

// yamlDocMap returns the top-level mapping of a YAML document.
func yamlDocMap(doc *yaml.Node) (*yaml.Node, error) {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("refusing to write config: top level is not a mapping")
	}
	return doc.Content[0], nil
}

// yamlMapEntry returns the value node stored under key in a mapping, nil when
// the key is absent or holds an explicit null.
func yamlMapEntry(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		v := m.Content[i+1]
		if v.Kind == yaml.ScalarNode && v.Tag == "!!null" {
			return nil
		}
		return v
	}
	return nil
}

// yamlMapChild returns the mapping stored under key, creating one when the key
// is absent. A key holding a non-mapping value is reported, not overwritten.
func yamlMapChild(m *yaml.Node, key string) (*yaml.Node, error) {
	if v := yamlMapEntry(m, key); v != nil {
		if v.Kind != yaml.MappingNode {
			return nil, fmt.Errorf("refusing to write config: %q is not a mapping", key)
		}
		return v, nil
	}
	created := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	yamlSetValue(m, key, created)
	return created, nil
}

// yamlSetValue replaces, or inserts, the value stored under key.
func yamlSetValue(m *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = value
			return
		}
	}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, value)
}

// writeYAMLDoc writes doc to path (perm) as YAML, atomically.
func writeYAMLDoc(path string, doc *yaml.Node, perm os.FileMode) error {
	data, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return artifact.AtomicWrite(path, data, perm)
}
