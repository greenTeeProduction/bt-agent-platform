package gardener

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

var ErrCandidateUnqualified = errors.New("candidate has not demonstrated task improvement")

func runtimeTreeID(entry TreeEntry) string {
	if entry.TreeID != "" {
		return entry.TreeID
	}
	if entry.Tree != nil {
		if strings.HasPrefix(entry.Tree.Name, "factory:") || entry.User != "" {
			return entry.Tree.Name
		}
	}
	for _, category := range []string{"domain", "finance", "research", "startup", "thinktank"} {
		if suffix, ok := strings.CutPrefix(entry.Name, category+"_"); ok {
			return category + ":" + suffix
		}
	}
	return entry.Name
}

func (r *Registry) releaseStore() *evolution.RuntimeReleaseStore {
	return evolution.NewRuntimeReleaseStore(filepath.Join(r.dir, "runtime-versions"))
}

// Called under the registry lock. A broken managed version disables this entry;
// stale proposal files cannot silently become its replacement.
func (r *Registry) loadRuntimeVersionsLocked() {
	for i := range r.entries {
		entry := &r.entries[i]
		tree, _, err := r.releaseStore().Resolve(runtimeTreeID(*entry), entry.User)
		if err != nil {
			entry.Active = false
			engine.Warn("gardener runtime version unavailable", "tree", entry.Name, "error", err)
			continue
		}
		if tree != nil {
			entry.Tree = tree
		}
	}
}

// PromoteCandidate is the production persistence boundary for every gardener
// pass. Historical or structural scores can suggest a trial; only fresh paired
// task outcomes can replace the active runtime version.
func (g *Gardener) PromoteCandidate(ctx context.Context, proposed TreeEntry) (*evolution.RuntimeQualification, error) {
	registry := g.cfg.Registry
	if registry == nil || proposed.Tree == nil {
		return nil, fmt.Errorf("registry and candidate required")
	}
	var prior TreeEntry
	registry.mu.RLock()
	for _, entry := range registry.entries {
		if entry.FilePath == proposed.FilePath {
			prior = entry
			prior.Tree = cloneTreeForGardener(entry.Tree)
			break
		}
	}
	registry.mu.RUnlock()
	if prior.Tree == nil {
		return nil, fmt.Errorf("candidate has no registered predecessor")
	}
	id := runtimeTreeID(prior)
	store := registry.releaseStore()
	active, _, err := store.Resolve(id, prior.User)
	if err != nil {
		return nil, err
	}
	if active != nil {
		prior.Tree = active
	}
	if info := engine.ValidateTreeFull(proposed.Tree); !info.Valid() {
		return nil, fmt.Errorf("candidate validation: %v", info.Errors)
	}
	model, err := benchmark.DefaultLLM()
	if err != nil {
		return nil, err
	}
	live, ok := model.(*benchmark.LiveModel)
	if !ok {
		return nil, fmt.Errorf("promotion requires the real benchmark provider")
	}
	qualification, err := benchmark.QualifyRuntimeCandidate(ctx, id, prior.User, prior.Tree, proposed.Tree, benchmark.SuiteForTree(prior.Name), live)
	if err != nil {
		if recordErr := store.RecordAttempt(qualification, err); recordErr != nil {
			return qualification, errors.Join(err, fmt.Errorf("retain rejected qualification: %w", recordErr))
		}
		return qualification, fmt.Errorf("%w: %v", ErrCandidateUnqualified, err)
	}
	if _, err := store.Promote(ctx, prior.Tree, proposed.Tree, qualification); err != nil {
		return qualification, err
	}
	registry.mu.Lock()
	if registry.qualifications == nil {
		registry.qualifications = make(map[string]*evolution.RuntimeQualification)
	}
	registry.qualifications[prior.FilePath] = qualification
	for i := range registry.entries {
		if registry.entries[i].FilePath == prior.FilePath {
			registry.entries[i].Tree = cloneTreeForGardener(proposed.Tree)
		}
	}
	registry.mu.Unlock()
	return qualification, nil
}

func (r *Registry) latestQualification(path string) *evolution.RuntimeQualification {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.qualifications[path]
}

func (g *Gardener) experienceScores(entry TreeEntry, tree *evolution.SerializableNode, batchSize int, before, after float64) (float64, float64, bool) {
	if g.candidateAcceptance != nil {
		return before, after, true
	}
	q := g.cfg.Registry.latestQualification(entry.FilePath)
	version, _ := evolution.TreeVersion(tree)
	// A multi-mutation comparison cannot attribute its complete gain to any
	// single operation. Keep that batch's evidence in the release ledger.
	if q == nil || batchSize != 1 || version != q.CandidateVersion {
		return 0, 0, false
	}
	b, a := q.PassCounts()
	return 100 * float64(b) / float64(len(q.Trials)), 100 * float64(a) / float64(len(q.Trials)), true
}

// applyMeasuredMetrics prevents estimated search scores from becoming claimed
// improvement evidence. No successful qualification means no measured delta.
func (g *Gardener) applyMeasuredMetrics(entry TreeEntry, metrics *CycleMetrics) {
	if g.candidateAcceptance != nil {
		return
	}
	metrics.Improved, metrics.Delta = false, 0
	version, _ := evolution.TreeVersion(entry.Tree)
	metrics.BaselineVersion = version
	// Per-version execution history remains useful for monitoring. It never
	// supplies a candidate's fitness before that candidate has executed.
	metrics.BaseFitness, metrics.NewFitness = 0, 0
	if g.cfg.RefStore != nil {
		records, err := g.cfg.RefStore.LoadAll()
		if err == nil {
			matching := evolution.FilterByTreeVersion(records, runtimeTreeID(entry), entry.User, version)
			if len(matching) > 0 {
				passed := 0
				for _, record := range matching {
					if record.Outcome == evolution.Success {
						passed++
					}
				}
				metrics.BaseFitness = 100 * float64(passed) / float64(len(matching))
				metrics.NewFitness = metrics.BaseFitness
			}
		}
	}
	q := g.cfg.Registry.latestQualification(entry.FilePath)
	if q == nil {
		return
	}
	before, after := q.PassCounts()
	metrics.BaselineVersion, metrics.CandidateVersion = q.BaselineVersion, q.CandidateVersion
	metrics.BaseFitness = 100 * float64(before) / float64(len(q.Trials))
	metrics.NewFitness = 100 * float64(after) / float64(len(q.Trials))
	metrics.Delta = metrics.NewFitness - metrics.BaseFitness
	metrics.Improved = metrics.Delta > 0
	_, release, err := g.cfg.Registry.releaseStore().Resolve(q.TreeID, q.User)
	if err == nil && release != nil {
		metrics.Qualification = release.Qualification
	}
}
