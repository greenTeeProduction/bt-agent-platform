package knowledge

import "github.com/nico/go-bt-evolve/internal/evolution"

// SelectorChildOutcome is one terminal child tick under a Selector, as
// delivered by the engine's per-run tick log (Blackboard.ChildTicks).
type SelectorChildOutcome struct {
	Selector string
	Child    string
	Status   string
}

// RecordSelectorChildOutcomes merges terminal child outcomes into the durable
// per-tree Selector telemetry at path. A fresh optimizer records only this
// batch and SaveSelectorStats sums it onto whatever is already on disk (under
// the sidecar flock), so successive runs accumulate. Outcomes missing a
// selector or child name are skipped, and an all-skipped batch writes nothing
// — the stats dir never gains empty files.
func RecordSelectorChildOutcomes(path string, outcomes []SelectorChildOutcome) error {
	opt := evolution.NewSelectorOptimizer(evolution.OrderBySuccessRate)
	recorded := false
	for _, o := range outcomes {
		if o.Selector == "" || o.Child == "" {
			continue
		}
		opt.Record(o.Selector, evolution.NodeExecutionRecord{NodeName: o.Child, Outcome: o.Status})
		recorded = true
	}
	if !recorded {
		return nil
	}
	return opt.SaveSelectorStats(path)
}

// DecisionTreeChildOutcome is one terminal child tick under a Selector, as
// delivered by the engine's per-run tick log (Blackboard.ChildTicks),
// enriched with the child's decision-tree condition (evolution.extractCondition
// via evolution.SelectorChildConditions).
type DecisionTreeChildOutcome struct {
	Selector  string
	Child     string
	Condition string
	Status    string
}

// RecordDecisionTreeChildOutcomes merges terminal child outcomes into the
// durable per-tree DTAnalyzer telemetry at path — the DTAnalyzer-side sibling
// of RecordSelectorChildOutcomes. Unlike SaveSelectorStats, DTAnalyzer.Save
// does not merge onto disk, so the bridge Loads whatever is already
// persisted before recording this batch's hits, so successive runs
// accumulate (under the sidecar flock). Outcomes missing a selector or child
// name are skipped, and an all-skipped batch writes nothing.
func RecordDecisionTreeChildOutcomes(path string, outcomes []DecisionTreeChildOutcome) error {
	valid := make([]DecisionTreeChildOutcome, 0, len(outcomes))
	for _, o := range outcomes {
		if o.Selector == "" || o.Child == "" {
			continue
		}
		valid = append(valid, o)
	}
	if len(valid) == 0 {
		return nil
	}
	da := evolution.NewDTAnalyzer()
	if err := da.Load(path); err != nil {
		return err
	}
	for _, o := range valid {
		da.RecordHit(o.Selector, o.Child, o.Condition, o.Status == "success")
	}
	return da.Save(path)
}
