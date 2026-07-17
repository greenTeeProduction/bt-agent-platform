// Persisted runtime tree mutations (ADR-003): the full mutated tree is
// snapshotted per tree ID under ~/.go-bt-evolve/mutated_trees/ and consulted
// override-first by cmd/bt-agent's tree resolution, so persisted runtime
// mutations survive restarts even for code-defined domain trees.
package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

func mutatedTreesDir() string {
	if d := os.Getenv("BT_MUTATED_TREES_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".go-bt-evolve", "mutated_trees")
}

// sanitizeTreeID makes a tree ID filesystem-safe (IDs contain ':' and '/').
func sanitizeTreeID(id string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, id)
}

// SaveMutatedTree atomically snapshots tree as the persisted override for
// treeID. Wired into engine.PersistMutatedTreeFn from cmd/bt-agent.
func SaveMutatedTree(treeID string, tree *evolution.SerializableNode) error {
	dir := mutatedTreesDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	ts, err := evolution.NewTreeStore(dir)
	if err != nil {
		return err
	}
	return ts.SaveTo(tree, filepath.Join(dir, sanitizeTreeID(treeID)+".json"))
}

// LoadMutatedTreeOverride returns the persisted override for treeID, or nil
// when none exists (callers fall through to normal resolution).
func LoadMutatedTreeOverride(treeID string) *evolution.SerializableNode {
	dir := mutatedTreesDir()
	if dir == "" {
		return nil
	}
	data, err := os.ReadFile(filepath.Join(dir, sanitizeTreeID(treeID)+".json"))
	if err != nil {
		return nil
	}
	var tree evolution.SerializableNode
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil
	}
	return &tree
}
