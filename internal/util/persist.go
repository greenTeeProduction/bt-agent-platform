package util

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// SaveJSONAtomic writes private state (0600 files, 0750 new directories).
func SaveJSONAtomic(path string, v any) error {
	return SaveJSONAtomicMode(path, v, 0o600, 0o750)
}

// persistenceParent anchors operations in the configured storage directory.
// Paths are operator configuration, not arbitrary request paths: callers must
// constrain request-derived names to their storage root before calling us.
// Reject traversal BEFORE cleaning. Existing directory symlinks are supported
// (operators relocate storage to SSD); after OpenRoot, relative operations
// cannot escape that directory, even through symlinks or concurrent renames.
func persistenceParent(path string, create bool, perm os.FileMode) (*os.Root, string, error) {
	if path == "" || strings.ContainsRune(path, 0) || strings.HasSuffix(path, string(os.PathSeparator)) {
		return nil, "", fmt.Errorf("invalid persistence path %q", path)
	}
	if slices.Contains(strings.Split(filepath.ToSlash(path), "/"), "..") {
		return nil, "", fmt.Errorf("parent traversal in persistence path %q", path)
	}
	name := filepath.Base(path)
	if !filepath.IsLocal(name) || name == "." {
		return nil, "", fmt.Errorf("invalid persistence filename %q", name)
	}
	dir := filepath.Dir(path)
	root, err := os.OpenRoot(dir)
	if err == nil || !create || !os.IsNotExist(err) {
		return root, name, err
	}
	// Find an existing configured ancestor and create missing parents through
	// its directory handle rather than unchecked path-based MkdirAll.
	ancestor := dir
	for {
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return nil, "", err
		}
		ancestor = parent
		root, err = os.OpenRoot(ancestor)
		if err == nil {
			break
		}
		if !os.IsNotExist(err) {
			return nil, "", err
		}
	}
	defer root.Close()
	rel, err := filepath.Rel(ancestor, dir)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, "", fmt.Errorf("invalid persistence directory %q", dir)
	}
	if err := root.MkdirAll(rel, perm); err != nil {
		return nil, "", err
	}
	child, err := root.OpenRoot(rel)
	return child, name, err
}

// EnsurePersistenceParent creates a private parent before a caller acquires
// its existing sidecar lock. It applies the same validation as the write.
func EnsurePersistenceParent(path string) error {
	root, _, err := persistenceParent(path, true, 0o750)
	if err != nil {
		return err
	}
	return root.Close()
}

// SaveJSONAtomicMode writes an exclusively-created random sibling and renames
// it within one directory handle. Neither a preplanted .tmp symlink nor a
// concurrent writer can redirect/truncate the temporary file. Callers still
// need their sidecar lock across read/merge/write transactions.
func SaveJSONAtomicMode(path string, v any, filePerm, dirPerm os.FileMode) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	root, name, err := persistenceParent(path, true, dirPerm)
	if err != nil {
		return fmt.Errorf("create dir for %s: %w", path, err)
	}
	defer root.Close()
	tmp := ".persist-" + rand.Text() + ".tmp"
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	defer func() { _ = root.Remove(tmp) }()
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil {
		return fmt.Errorf("write %s: %w", path, writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close %s: %w", path, closeErr)
	}
	if err := root.Rename(tmp, name); err != nil {
		return fmt.Errorf("commit %s: %w", path, err)
	}
	return nil
}

// ReadPersistenceFile reads only within the configured parent directory.
func ReadPersistenceFile(path string) ([]byte, error) {
	root, name, err := persistenceParent(path, false, 0)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.ReadFile(name)
}

// LoadJSON leaves dest untouched for a missing file (a silent cold start).
func LoadJSON(path string, dest any) error {
	data, err := ReadPersistenceFile(path)
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
