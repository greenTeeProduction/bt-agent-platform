package util

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// SaveJSONAtomic marshals v as indented JSON and writes it to path
// atomically with the historical 0644 file / 0755 directory permissions.
// See SaveJSONAtomicMode for the mechanics and the locking invariant that
// callers are responsible for.
func SaveJSONAtomic(path string, v any) error {
	return SaveJSONAtomicMode(path, v, 0o644, 0o755)
}

// SaveJSONAtomicMode marshals v as indented JSON and writes it to path
// atomically: it creates any missing parent directories with dirPerm, writes
// to a "path.tmp" sibling with filePerm, then renames it into place so
// readers never observe a partially-written file. Both permissions are
// subject to the process umask. If the rename fails the tmp sibling is
// removed, so a failed write never leaves a "path.tmp" file behind for
// directory globs (e.g. the named-tree and gardener registry scans) to pick
// up.
//
// The tmp sibling name is derived from path and is therefore NOT safe
// against concurrent writers on its own — two writers racing on the same
// path share one tmp name and can corrupt each other. Callers that share a
// path across goroutines or processes must hold
// reliability.AcquireFileLock(path) across the call. The lock deliberately
// lives outside this package: internal/reliability imports internal/util, so
// taking it here would create an import cycle.
func SaveJSONAtomicMode(path string, v any, filePerm, dirPerm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
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
