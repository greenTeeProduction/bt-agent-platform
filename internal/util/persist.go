package util

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SaveJSONAtomic marshals v as indented JSON and writes it to path
// atomically: it creates any missing parent directories, writes to a
// "path.tmp" sibling, then renames it into place so readers never observe a
// partially-written file. It centralizes only the write/read mechanics —
// callers that need cross-process exclusion (e.g. concurrent writers to the
// same path) must acquire their own lock around the call.
func SaveJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("commit %s: %w", path, err)
	}
	return nil
}

// LoadJSON reads path and unmarshals it into dest. A missing file is a
// silent cold start: dest is left untouched and nil is returned. Any other
// read or parse error is returned to the caller.
func LoadJSON(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, dest); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}
