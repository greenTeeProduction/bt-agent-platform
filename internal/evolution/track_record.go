package evolution

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/nico/go-bt-evolve/internal/reliability"
	"github.com/nico/go-bt-evolve/internal/util"
)

// TrackRecord accumulates benchmark-gate outcomes (Q2 Evolvability) across
// evolution runs on a single base tree, shared across every benchmark-gated
// algorithm (bt_evolve_qd, bt_evolve_multiobjective, and future variants)
// instead of one archive per tool. RecommendedGenerations reads this history
// so a tree with recently regressed runs gets a larger generation budget
// instead of every call burning the same hardcoded default.
type TrackRecord struct {
	Runs []TrackRecordRun `json:"runs"`
}

// TrackRecordRun is one benchmarkGateEvolvedWinner outcome: whether the
// evolved winner regressed against the base tree's real benchmark success
// rate.
type TrackRecordRun struct {
	Rejected bool `json:"rejected"`
}

// trackRecordCap bounds Runs, mirroring ExpertKnowledge's learnedPatternCap
// and ExperienceBank's ADR-018 cap-500 pattern, so Record can't grow the
// archive unbounded across a long-lived tree's history.
const trackRecordCap = 500

// trackRecordWindow bounds how many of the most recent runs
// RecommendedGenerations weighs — old history shouldn't keep inflating the
// budget once a tree has stabilized.
const trackRecordWindow = 5

// NewTrackRecord returns an empty track record ready for Load/Record/Save.
func NewTrackRecord() *TrackRecord {
	return &TrackRecord{}
}

// Record appends outcome (true if benchmarkGateEvolvedWinner rejected the
// evolved winner as a regression) to the archive, evicting the oldest run
// once trackRecordCap is exceeded.
func (tr *TrackRecord) Record(rejected bool) {
	tr.Runs = append(tr.Runs, TrackRecordRun{Rejected: rejected})
	if len(tr.Runs) > trackRecordCap {
		tr.Runs = tr.Runs[len(tr.Runs)-trackRecordCap:]
	}
}

// RecommendedGenerations returns the generation budget a caller should use
// when it wasn't given an explicit "generations" argument. A cold or
// all-accepted recent history recommends defaultGenerations unchanged; a
// history with recent rejections (regressed evolved winners) scales the
// budget up proportional to the rejection rate over the most recent
// trackRecordWindow runs, so a tree that keeps regressing gets more search
// room instead of retrying with the same fixed compute every time.
func (tr *TrackRecord) RecommendedGenerations(defaultGenerations int) int {
	if tr == nil || len(tr.Runs) == 0 {
		return defaultGenerations
	}
	window := tr.Runs
	if len(window) > trackRecordWindow {
		window = window[len(window)-trackRecordWindow:]
	}
	rejections := 0
	for _, run := range window {
		if run.Rejected {
			rejections++
		}
	}
	if rejections == 0 {
		return defaultGenerations
	}
	rate := float64(rejections) / float64(len(window))
	return int(math.Round(float64(defaultGenerations) * (1 + rate)))
}

// WinRate returns the fraction of the most recent trackRecordWindow runs
// that were accepted (not rejected) by the benchmark gate — the same window
// RecommendedGenerations weighs — so callers can surface the adaptive budget
// driver alongside the recommendation itself instead of leaving it as
// internal state (Q2 Evolvability). A cold archive with no runs reports a
// win rate of 1, mirroring RecommendedGenerations' unpenalized treatment of
// a cold start.
func (tr *TrackRecord) WinRate() float64 {
	if tr == nil || len(tr.Runs) == 0 {
		return 1
	}
	window := tr.Runs
	if len(window) > trackRecordWindow {
		window = window[len(window)-trackRecordWindow:]
	}
	wins := 0
	for _, run := range window {
		if !run.Rejected {
			wins++
		}
	}
	return float64(wins) / float64(len(window))
}

// Save persists Runs as JSON at path, creating missing parent directories and
// writing atomically (temp file + rename) under the shared advisory flock so
// concurrent writers cannot interleave partial archives, mirroring
// ExpertKnowledge.Save and QTable.Save.
func (tr *TrackRecord) Save(path string) error {
	// The flock sidecar is created beside the archive, so the directory has
	// to exist before the lock is taken.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create track record archive dir: %w", err)
	}
	release, err := reliability.AcquireFileLock(path)
	if err != nil {
		return err
	}
	defer release()
	return util.SaveJSONAtomic(path, tr.Runs)
}

// Load warm-starts Runs by appending the archive at path onto the in-memory
// slice, mirroring ExpertKnowledge.Load. A missing archive is a silent cold
// start; a corrupt archive is an error that leaves the in-memory state
// untouched.
func (tr *TrackRecord) Load(path string) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	}
	release, err := reliability.AcquireFileLock(path)
	if err != nil {
		return err
	}
	defer release()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read track record archive: %w", err)
	}
	var runs []TrackRecordRun
	if err := json.Unmarshal(data, &runs); err != nil {
		return fmt.Errorf("parse track record archive %s: %w", path, err)
	}
	tr.Runs = append(tr.Runs, runs...)
	return nil
}
