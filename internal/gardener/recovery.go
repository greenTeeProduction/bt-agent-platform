package gardener

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

// outcomeRecoveryOnly recognizes the observed destructive pruning signature.
// It deliberately does not classify a small tree as damaged merely by size.
func outcomeRecoveryOnly(tree *evolution.SerializableNode) bool {
	if tree == nil {
		return false
	}
	recovery := false
	var walk func(*evolution.SerializableNode) bool
	walk = func(n *evolution.SerializableNode) bool {
		if len(n.Children) == 0 {
			switch {
			case n.Type == "Condition" && n.Name == "WasSuccessful":
				return true
			case n.Type == "Action" && (n.Name == "SelfCorrect" || n.Name == "EscalateToDeepSeek"):
				recovery = true
				return true
			default:
				return false
			}
		}
		if n.Type != "Sequence" && n.Type != "Selector" && n.Type != "Retry" {
			return false
		}
		for i := range n.Children {
			if !walk(&n.Children[i]) {
				return false
			}
		}
		return true
	}
	return walk(tree) && recovery
}

type TreeRecoveryItem struct {
	Operation     string `json:"operation"`
	TreeID        string `json:"tree_id"`
	File          string `json:"file"`
	BeforeHash    string `json:"before_sha256"`
	BeforeVersion string `json:"before_version"`
	AfterVersion  string `json:"after_version"`
	BeforeNodes   int    `json:"before_nodes"`
	AfterNodes    int    `json:"after_nodes"`
	Status        string `json:"status"`
	Backup        string `json:"backup,omitempty"`
	Archived      string `json:"archived,omitempty"`
	Error         string `json:"error,omitempty"`
	original      []byte
	authored      *evolution.SerializableNode
}

type TreeRecoveryReport struct {
	Directory      string             `json:"directory"`
	SourceRevision string             `json:"source_revision"`
	CreatedAt      time.Time          `json:"created_at"`
	Applied        bool               `json:"applied"`
	Manifest       string             `json:"manifest,omitempty"`
	Items          []TreeRecoveryItem `json:"items"`
	Restored       int                `json:"restored"`
	Quarantined    int                `json:"quarantined"`
	Scope          string             `json:"scope"`
	plannedDigest  string
}

// PlanTreeRecovery matches exact registered filenames to fresh authored trees.
// It never uses a damaged root name, reverse-sanitizes an ID, or loads generated
// or personal definitions as the authority for another tree.
func PlanTreeRecovery(dir, revision string) (*TreeRecoveryReport, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	// An empty in-memory registry has exactly the compiled catalog, without
	// consulting persisted state, active versions or runtime reorder hooks.
	catalog := &Registry{}
	catalog.addCatalogBuiltins()
	// Retired by the arc42 documentation consolidation; restoring its removed
	// actions would revive an obsolete workflow, not repair an authored tree.
	catalog.entries = append(catalog.entries, TreeEntry{TreeID: "domain:arc42:assemble", FilePath: "tree-domain_arc42:assemble.json"})
	report := &TreeRecoveryReport{Directory: dir, SourceRevision: revision, CreatedAt: time.Now().UTC(), Items: []TreeRecoveryItem{}, Scope: "Offline restoration of authored task logic; structural validation is not measured task improvement. All writers must remain stopped during apply."}
	for _, entry := range catalog.entries {
		name := filepath.Base(entry.FilePath)
		data, err := root.ReadFile(name)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}
		var current evolution.SerializableNode
		if err := json.Unmarshal(data, &current); err != nil {
			return nil, fmt.Errorf("inspect %s: %w", name, err)
		}
		if !outcomeRecoveryOnly(&current) {
			continue
		}
		if owner, _ := current.Metadata["user"].(string); owner != "" {
			return nil, fmt.Errorf("shared filename %s contains personal ownership", name)
		}
		if _, active, err := evolution.NewRuntimeReleaseStore(filepath.Join(dir, "runtime-versions")).Resolve(entry.TreeID, ""); err != nil {
			return nil, err
		} else if active != nil {
			continue // managed versions have their own qualified rollback authority
		}
		operation, after, afterNodes := "quarantine_retired", "", 0
		if entry.Tree != nil {
			if info := engine.ValidateTreeFull(entry.Tree); !info.Valid() {
				return nil, fmt.Errorf("authored %s fails validation: %v", entry.TreeID, info.Errors)
			}
			operation, afterNodes = "restore_authored", evolution.CountNodes(entry.Tree)
			after, err = evolution.TreeVersion(entry.Tree)
			if err != nil {
				return nil, err
			}
		}
		before, err := evolution.TreeVersion(&current)
		if err != nil {
			return nil, err
		}
		report.Items = append(report.Items, TreeRecoveryItem{Operation: operation, TreeID: entry.TreeID, File: name, BeforeHash: fmt.Sprintf("%x", sha256.Sum256(data)), BeforeVersion: before, AfterVersion: after, BeforeNodes: evolution.CountNodes(&current), AfterNodes: afterNodes, Status: "planned", original: data, authored: entry.Tree})
	}
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	report.plannedDigest = fmt.Sprintf("%x", sha256.Sum256(data))
	return report, nil
}

// ApplyTreeRecovery requires quiescent legacy writers: they do not participate
// in this recovery lock. Per-file byte checks refuse changes since planning.
// Backups and the prepared manifest precede every overwrite. A partial commit
// reports exactly what completed, without undoing or replaying completed work.
func ApplyTreeRecovery(ctx context.Context, report *TreeRecoveryReport) error {
	if report == nil || report.Directory == "" || report.Applied || report.Manifest != "" {
		return fmt.Errorf("fresh in-process recovery plan required")
	}
	data, err := json.Marshal(report)
	if err != nil || report.plannedDigest == "" || report.plannedDigest != fmt.Sprintf("%x", sha256.Sum256(data)) {
		return fmt.Errorf("recovery plan changed since inspection")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	unlock, err := reliability.AcquireFileLockWithContext(ctx, filepath.Join(report.Directory, ".tree-recovery"))
	if err != nil {
		return err
	}
	defer unlock()
	root, err := os.OpenRoot(report.Directory)
	if err != nil {
		return err
	}
	defer root.Close()
	// Validate the complete plan before writing any backup or tree.
	for _, item := range report.Items {
		if (item.authored == nil && item.Operation != "quarantine_retired") || len(item.original) == 0 || filepath.Base(item.File) != item.File {
			return fmt.Errorf("untrusted recovery plan")
		}
		data, err := root.ReadFile(item.File)
		if err != nil {
			return err
		}
		if !bytes.Equal(data, item.original) {
			return fmt.Errorf("%s changed since planning", item.File)
		}
		if item.authored != nil {
			version, err := evolution.TreeVersion(item.authored)
			if err != nil || version != item.AfterVersion {
				return fmt.Errorf("authored definition changed")
			}
		}
		if _, active, err := evolution.NewRuntimeReleaseStore(filepath.Join(report.Directory, "runtime-versions")).Resolve(item.TreeID, ""); err != nil {
			return err
		} else if active != nil {
			return fmt.Errorf("%s acquired managed authority", item.TreeID)
		}
	}
	if len(report.Items) == 0 {
		report.Applied = true
		return nil
	}
	dir := filepath.Join(report.Directory, "recovery", time.Now().UTC().Format("20060102T150405Z")+"-"+rand.Text())
	report.Manifest = filepath.Join(dir, "manifest.json")
	for i := range report.Items {
		item := &report.Items[i]
		item.Backup = filepath.Join(dir, "originals", item.File)
		if item.Operation == "quarantine_retired" {
			item.Archived = filepath.Join(dir, "retired", item.File)
			if err := util.EnsurePersistenceParent(item.Archived); err != nil {
				return err
			}
		}
		if err := util.SavePersistenceFile(item.Backup, item.original); err != nil {
			return err
		}
		item.Status = "backed_up"
	}
	if err := util.SaveJSONAtomic(report.Manifest, report); err != nil {
		return err
	}
	for i := range report.Items {
		item := &report.Items[i]
		err := ctx.Err()
		if err == nil {
			var data []byte
			data, err = root.ReadFile(item.File)
			if err == nil && !bytes.Equal(data, item.original) {
				err = fmt.Errorf("tree changed during recovery")
			}
		}
		if err == nil {
			if item.Operation == "quarantine_retired" {
				var target string
				target, err = filepath.Rel(report.Directory, item.Archived)
				if err == nil {
					err = root.Rename(item.File, target)
				}
			} else {
				err = util.SaveJSONAtomic(filepath.Join(report.Directory, item.File), item.authored)
			}
		}
		if err != nil {
			item.Status, item.Error = "failed", err.Error()
			if saveErr := util.SaveJSONAtomic(report.Manifest, report); saveErr != nil {
				return fmt.Errorf("recovery: %w; manifest: %v", err, saveErr)
			}
			return err
		}
		if item.Operation == "quarantine_retired" {
			item.Status = "quarantined"
			report.Quarantined++
		} else {
			item.Status = "restored"
			report.Restored++
		}
		if err := util.SaveJSONAtomic(report.Manifest, report); err != nil {
			return fmt.Errorf("tree restored but manifest acknowledgement failed: %w", err)
		}
	}
	report.Applied = true
	return util.SaveJSONAtomic(report.Manifest, report)
}
