package evolution

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/util"
)

// TreeFileName returns the canonical on-disk file name for a generated tree ID
// (e.g. "core:automate_reports" → "tree-core_automate_reports.json").
//
// Tree IDs may contain characters that are invalid in file names (":" on
// Windows, "/" everywhere), so the ID is sanitized to a stable, deterministic
// name. The same transformation is applied on save and on load, which means
// resolution never needs to reverse the mapping. The file name keeps the
// "tree-" prefix so the gardener registry picks generated trees up for
// evolution (it scans for tree-*.json).
func TreeFileName(id string) string {
	return "tree-" + sanitizeTreeID(id) + ".json"
}

// sanitizeTreeID maps a tree ID to a cross-platform-safe file name fragment.
func sanitizeTreeID(id string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// SaveNamed persists a generated tree under its canonical per-ID file in the
// store directory (ADR-133 Phase 0). Unlike Save, which owns the single
// tree.json used for the agent's active self-mutating tree, SaveNamed writes
// tree-<id>.json so generated trees are resolvable by ID and visible to the
// gardener registry.
func (ts *TreeStore) SaveNamed(id string, tree *SerializableNode) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("save named tree: empty id")
	}
	if tree == nil {
		return "", fmt.Errorf("save named tree %q: nil tree", id)
	}
	path := filepath.Join(ts.dir, TreeFileName(id))
	if err := ts.SaveTo(tree, path); err != nil {
		return "", err
	}
	return path, nil
}

// LoadNamed reads a generated tree by ID from the store directory.
// Returns (nil, nil) when no tree with that ID has been persisted.
func (ts *TreeStore) LoadNamed(id string) (*SerializableNode, error) {
	return LoadNamedTree(ts.dir, id)
}

// SaveNamedTree persists a generated tree as tree-<id>.json in an arbitrary
// directory (created if needed) with an atomic write. It is the store-free
// counterpart of TreeStore.SaveNamed for per-user workspaces (ADR-133
// Phase 5: users/<user>/trees/).
func SaveNamedTree(dir, id string, tree *SerializableNode) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("save named tree: empty id")
	}
	if dir == "" {
		return "", fmt.Errorf("save named tree %q: empty dir", id)
	}
	if tree == nil {
		return "", fmt.Errorf("save named tree %q: nil tree", id)
	}
	path := filepath.Join(dir, TreeFileName(id))
	if err := util.SaveJSONAtomic(path, tree); err != nil {
		return "", fmt.Errorf("save tree %q: %w", id, err)
	}
	return path, nil
}

// QuarantineNamedTree renames a generated tree's on-disk file aside (adding a
// ".rejected" suffix) so it can no longer be resolved by ID via LoadNamedTree
// or the gardener's tree-*.json registry scan. Used when a proposed
// automation is rejected: the compiled tree must stop being resolvable even
// though nothing else in its lifecycle has changed. A no-op if the file does
// not exist (idempotent under repeated rejection or a tree that was never
// persisted).
func QuarantineNamedTree(dir, id string) error {
	if strings.TrimSpace(id) == "" || dir == "" {
		return nil
	}
	path := filepath.Join(dir, TreeFileName(id))
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("quarantine tree %q: %w", id, err)
	}
	if err := os.Rename(path, path+".rejected"); err != nil {
		return fmt.Errorf("quarantine tree %q: %w", id, err)
	}
	return nil
}

// LoadNamedTree reads tree-<id>.json from dir. Returns (nil, nil) when the
// file does not exist, so callers can distinguish "not generated" from a
// genuinely broken file.
func LoadNamedTree(dir, id string) (*SerializableNode, error) {
	if strings.TrimSpace(id) == "" || dir == "" {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, TreeFileName(id)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read named tree %q: %w", id, err)
	}
	var tree SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("unmarshal named tree %q: %w", id, err)
	}
	return &tree, nil
}
