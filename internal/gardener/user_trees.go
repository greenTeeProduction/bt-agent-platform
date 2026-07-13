// Per-user gardener support (ADR-010 Phase 5): personal trees live in user
// workspaces (<usersRoot>/<user>/trees/tree-*.json), are evaluated strictly on
// their own reflection evidence, and evolve against the user's own experience
// bank so mutation priors learned on one user's trees never bleed into
// another's.
package gardener

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// loadUserTreesLocked scans every user workspace under usersRoot and appends
// personal trees to the registry. Caller must hold r.mu.
//
// The registry entry name is the tree's root node name when present (the
// goal compiler sets it to the tree ID, e.g. "goal:automate_reports"), so
// reflection filtering and dynamic resolution agree on the identity; the
// sanitized file name is only a fallback. Entries are prefixed on collision
// so two users owning the same tree ID stay distinguishable.
func (r *Registry) loadUserTreesLocked() {
	if r.usersRoot == "" {
		return
	}
	users, err := os.ReadDir(r.usersRoot)
	if err != nil {
		return
	}

	seen := make(map[string]bool, len(r.entries))
	for i := range r.entries {
		seen[r.entries[i].Name] = true
	}

	for _, u := range users {
		if !u.IsDir() {
			continue
		}
		user := u.Name()
		treesDir := filepath.Join(r.usersRoot, user, "trees")
		files, err := os.ReadDir(treesDir)
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			if f.IsDir() || !strings.HasPrefix(name, "tree-") || !strings.HasSuffix(name, ".json") {
				continue
			}
			path := filepath.Join(treesDir, name)
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var tree evolution.SerializableNode
			if json.Unmarshal(data, &tree) != nil {
				continue
			}
			entryName := strings.TrimSpace(tree.Name)
			if entryName == "" {
				entryName = name[:len(name)-5] // strip .json
			}
			if seen[entryName] {
				entryName = user + "_" + entryName
			}
			seen[entryName] = true
			r.entries = append(r.entries, TreeEntry{
				Name:        entryName,
				Description: "Personal tree (user " + user + ")",
				Tree:        &tree,
				FilePath:    path,
				Active:      true,
				User:        user,
			})
		}
	}
}

// recordsForEntry selects the reflection evidence a tree is evaluated on.
// Personal trees use strict tree-name matching: the backward-compat fallback
// in FilterByTreeName (no match → all records) would score a personal tree on
// the global pool and blind the evidence gate to its missing history.
//
// Matching keys off the tree's real ID (entry.Tree.Name), not the registry's
// display Name: a colliding entry gets a disambiguating "<user>_" prefix on
// Name (see loadUserTreesLocked) but its underlying tree — and every
// reflection Record recorded against it — still carries the bare ID, so
// keying on Name would leave the renamed entry evidence-starved. Once
// matched by tree ID, records are further filtered down to the owning user
// (Record.User) so two users' trees sharing the same real ID never bleed
// evidence into each other; records with no User (pre-Phase-5 or seed
// reflections) still count for any owner, preserving backward compat.
func recordsForEntry(allRecords []evolution.Record, entry TreeEntry) []evolution.Record {
	if entry.User == "" {
		return evolution.FilterByTreeName(allRecords, entry.Name)
	}
	treeID := entry.Name
	if entry.Tree != nil && strings.TrimSpace(entry.Tree.Name) != "" {
		treeID = entry.Tree.Name
	}
	matched := evolution.FilterByTreeNameStrict(allRecords, treeID)
	filtered := make([]evolution.Record, 0, len(matched))
	for _, r := range matched {
		if r.User == "" || r.User == entry.User {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// bankFor resolves the experience bank for a tree: the shared bank for
// builtin/shared trees, the user's own bank (<UserExperienceRoot>/<user>/
// experience, lazily opened and cached) for personal trees. Falls back to the
// shared bank when the per-user bank cannot be opened or no root is
// configured, so evolution never silently loses experience recording.
func (g *Gardener) bankFor(entry TreeEntry) *evolution.ExperienceBank {
	if entry.User == "" || g.cfg.UserExperienceRoot == "" {
		return g.cfg.ExperienceBank
	}

	g.userBanksMu.Lock()
	defer g.userBanksMu.Unlock()
	if g.userBanks == nil {
		g.userBanks = make(map[string]*evolution.ExperienceBank)
	}
	if bank, ok := g.userBanks[entry.User]; ok {
		return bank
	}
	bank, err := evolution.NewExperienceBank(filepath.Join(g.cfg.UserExperienceRoot, entry.User, "experience"))
	if err != nil {
		// Do not cache: the open error may be transient (e.g. a path
		// temporarily blocked), and caching the shared bank here would
		// permanently strand the user on it even after the error clears.
		return g.cfg.ExperienceBank
	}
	g.userBanks[entry.User] = bank
	return bank
}
