// Package edit provides format-preserving editors for the config files that
// charon writes into: JSON/JSONC and TOML. Only the requested key changes;
// comments, ordering and indentation are left as they are.
//
// Adapted from magpie's internal/edit (github.com/yetone/magpie, MIT).
package edit

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"charon/internal/artifact"
)

// Read returns the file contents, or (nil, nil) when the file does not exist.
// A UTF-8 byte order mark at the start is not part of what is returned:
// Windows tools (PowerShell's UTF8 encoding, Notepad) put one in front of a
// settings.json or config.toml, which the tool reads through while a plain
// JSON or TOML parser refuses it. WriteAtomic puts it back, see keepBOM.
func Read(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return bytes.Clone(TrimBOM(b)), nil
}

// bom is the UTF-8 byte order mark.
var bom = []byte("\xef\xbb\xbf")

// TrimBOM is b without a UTF-8 byte order mark at its start, for a config
// read some other way than Read.
func TrimBOM(b []byte) []byte { return bytes.TrimPrefix(b, bom) }

// keepBOM is data with the byte order mark the file at path starts with,
// when it starts with one and data doesn't: the user's file keeps the
// encoding it was saved in, for the tools that wrote it that way, and the
// tool read it with the mark before.
func keepBOM(path string, data []byte) []byte {
	if len(data) == 0 || bytes.HasPrefix(data, bom) {
		return data
	}
	f, err := os.Open(path)
	if err != nil {
		return data
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, len(bom))
	if n, _ := io.ReadFull(f, head); n < len(bom) || !bytes.Equal(head, bom) {
		return data
	}
	return append(append(make([]byte, 0, len(bom)+len(data)), bom...), data...)
}

// WriteAtomic writes data to path through artifact's temp file + rename, so
// a crash can never leave a half-written config behind. The directory is
// created when missing. These configs carry credentials, so every write is
// 0600 — a file left more permissive by something else is tightened back.
func WriteAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return artifact.AtomicWrite(path, keepBOM(path, data), 0o600)
}
