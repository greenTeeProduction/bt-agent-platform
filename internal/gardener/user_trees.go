// Per-user gardener support (ADR-133 Phase 5): personal trees live in user
// workspaces (<usersRoot>/<user>/trees/tree-*.json), are evaluated strictly on
// their own reflection evidence, and evolve against the user's own experience
// bank so mutation priors learned on one user's trees never bleed into
// another's.
package gardener

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/persona"
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
	seenPaths := make(map[string]bool, len(r.entries))
	for i := range r.entries {
		seen[r.entries[i].Name] = true
		seenPaths[r.entries[i].FilePath] = true
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
			if seenPaths[path] {
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var tree evolution.SerializableNode
			if json.Unmarshal(data, &tree) != nil {
				continue
			}
			owner := user
			if declared, _ := tree.Metadata["user"].(string); declared != "" {
				if persona.SanitizeUserID(declared) != user {
					slog.Warn("personal tree owner does not match workspace", "path", path)
					continue
				}
				owner = declared
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
				TreeID:      tree.Name,
				Name:        entryName,
				Description: "Personal tree (user " + user + ")",
				Tree:        &tree,
				FilePath:    path,
				Active:      true,
				User:        owner,
			})
		}
	}
}

// recordsForEntry selects only this tree's owned evidence. Catalog aliases
// bridge the gardener's historical domain_name spelling and runtime's
// domain:name; personal entries use the real ID, never a collision-prefixed
// display name. Missing history stays missing for the evidence gate.
func recordsForEntry(allRecords []evolution.Record, entry TreeEntry) []evolution.Record {
	names := evidenceTreeNames(entry.Name)
	if id := runtimeTreeID(entry); id != "" && !slices.Contains(names, id) {
		names = append(names, id)
	}
	if entry.User != "" {
		names = []string{runtimeTreeID(entry)}
	}
	filtered := make([]evolution.Record, 0, len(allRecords))
	for _, name := range names {
		filtered = append(filtered, evolution.FilterByTreeOwner(allRecords, name, entry.User)...)
	}
	out := filtered[:0]
	for _, record := range filtered {
		if record.EvidenceKind != evolution.EvidenceCompilation {
			out = append(out, record)
		}
	}
	return out
}

// bankFor resolves the experience bank for a tree: the shared bank for
// builtin/shared trees, the user's own bank (<UserExperienceRoot>/<user>/
// experience, lazily opened and cached) for personal trees. Missing owner
// storage never grants access to shared learning state.
func (g *Gardener) bankFor(entry TreeEntry) *evolution.ExperienceBank {
	if entry.User == "" {
		return g.cfg.ExperienceBank
	}

	if g.cfg.UserExperienceRoot == "" {
		slog.Warn("personal experience unavailable: owner storage not configured", "user", entry.User)
		return nil
	}
	g.userBanksMu.Lock()
	defer g.userBanksMu.Unlock()
	if g.userBanks == nil {
		g.userBanks = make(map[string]*evolution.ExperienceBank)
	}
	if bank, ok := g.userBanks[entry.User]; ok {
		return bank
	}
	bankDir := filepath.Join(g.cfg.UserExperienceRoot, persona.SanitizeUserID(entry.User), "experience")
	if persona.SanitizeUserID(entry.User) != entry.User {
		// Distinct raw owner IDs can share a sanitized workspace name. Never
		// share their learning bank or guess ownership of the legacy bank.
		bankDir = filepath.Join(bankDir, fmt.Sprintf("owner-%x", sha256.Sum256([]byte(entry.User))))
	}
	bank, err := evolution.NewExperienceBank(bankDir)
	if err != nil {
		slog.Warn("personal experience unavailable", "user", entry.User, "error", err)
		return nil
	}
	g.userBanks[entry.User] = bank
	return bank
}
