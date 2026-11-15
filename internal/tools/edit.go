package tools

import (
	"encoding/json"
	"os"

	toml "github.com/pelletier/go-toml/v2"

	"charon/internal/edit"
)

// readConfig reads a config file for parsing, with a UTF-8 byte order mark
// trimmed: Windows editors (PowerShell's UTF8 encoding, Notepad) save one in
// front of a JSON or TOML file, the tools themselves read through it, and a
// bare parser refuses it. edit.Read does the same for the editors; these are the
// reads that never reach it.
func readConfig(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return edit.TrimBOM(data), nil
}

// loadJSONMap reads path as a JSON object, returning an empty map if absent.
func loadJSONMap(path string) (map[string]any, error) {
	m := map[string]any{}
	data, err := readConfig(path)
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

// loadTOMLMap reads path as a TOML table, returning an empty map if absent.
func loadTOMLMap(path string) (map[string]any, error) {
	m := map[string]any{}
	data, err := readConfig(path)
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

// subMap returns m[key] as a map, creating it if missing.
func subMap(m map[string]any, key string) map[string]any {
	if existing, ok := m[key].(map[string]any); ok {
		return existing
	}
	created := map[string]any{}
	m[key] = created
	return created
}
