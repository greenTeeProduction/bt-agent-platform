package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/gardener"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// TestBuildGardenerConfig_SafetyComponentsWired proves that the production config
// constructor wires all three safety components that the audit found missing.
func TestBuildGardenerConfig_SafetyComponentsWired(t *testing.T) {
	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}

	if cfg.Gate == nil {
		t.Error("Gate is nil — quality gate not wired into production config")
	}
	if cfg.SnapshotDir == "" {
		t.Error("SnapshotDir is empty — snapshot directory not wired into production config")
	}
	if cfg.CrisisDetector == nil {
		t.Error("CrisisDetector is nil — crisis detector not wired into production config")
	}
	if cfg.ValidationGate.EvidencePath != "/tmp/slo-evidence.json" {
		t.Errorf("ValidationGate.EvidencePath = %q — SLO evidence file not wired into production config (B1)",
			cfg.ValidationGate.EvidencePath)
	}
	// Production must allow trees without SLO evidence to persist: only 4 of
	// ~50 registry trees are executed by live agents, and strict fail-closed
	// froze evolution at 0 applied mutations for a month. Evidenced trees keep
	// full threshold enforcement.
	if !cfg.ValidationGate.AllowUnverified {
		t.Error("ValidationGate.AllowUnverified = false — unevidenced trees would fail closed forever")
	}
}

// TestBuildGardenerConfig_ExperienceBankSharedWithDaemon pins milestone 3/5 of
// the experience-grounded evolution program (Q2 Evolvability, Q3 Reliability):
// the production gardener config must carry a persistent ExperienceBank so
// RunCycleV2 records accepted mutations instead of discarding them, and that
// bank must default to the DAEMON'S experience directory —
// agent.HomeDir()/"experience", the exact dir bt-agent's experienceBankDir()
// resolves (see cmd/bt-agent/main.go) — so gardener and bt-agent accumulate
// into one shared on-disk bank. agent.HomeDir() honors BT_AGENT_HOME, which is
// both the configurability seam and what lets this test run against a temp
// dir instead of the real platform home.
func TestBuildGardenerConfig_ExperienceBankSharedWithDaemon(t *testing.T) {
	agentHome := t.TempDir()
	t.Setenv("BT_AGENT_HOME", agentHome)

	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}

	if cfg.ExperienceBank == nil {
		t.Fatal("ExperienceBank is nil — gardener runs experience-blind: RunCycleV2 discards accepted-mutation experience instead of recording it to the shared bank")
	}

	// NewExperienceBank MkdirAlls its directory, so the shared dir existing
	// under BT_AGENT_HOME proves the bank is rooted at the daemon's location
	// (agent.HomeDir()/experience) rather than some gardener-private path.
	sharedDir := filepath.Join(agentHome, "experience")
	info, statErr := os.Stat(sharedDir)
	if statErr != nil {
		t.Fatalf("shared experience dir %q was not created: %v — the gardener's bank must default to the daemon's experience dir (agent.HomeDir()/experience) so both binaries accumulate into one bank", sharedDir, statErr)
	}
	if !info.IsDir() {
		t.Errorf("shared experience path %q exists but is not a directory", sharedDir)
	}
}

// TestBuildGardenerConfig_SnapshotDirCreated proves that buildGardenerConfig
// creates the snapshot directory on disk (MkdirAll with 0700).
func TestBuildGardenerConfig_SnapshotDirCreated(t *testing.T) {
	baseDir := t.TempDir()
	snapDir := baseDir + "/snapshots"
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	_, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}

	info, statErr := os.Stat(snapDir)
	if statErr != nil {
		t.Fatalf("snapshot dir was not created: %v", statErr)
	}
	if !info.IsDir() {
		t.Errorf("snapshot path exists but is not a directory")
	}
	if perm := info.Mode().Perm(); perm != 0700 {
		t.Errorf("snapshot dir permissions = %04o, want 0700", perm)
	}
}

// TestBuildGardenerConfig_KnowledgeGraphWired pins Q2 Evolvability milestone
// 1/4: buildGardenerConfig must wire a real *knowledge.KnowledgeGraph into
// Config.KnowledgeGraph, otherwise RunCycleV2's treePriorityRanks()
// (internal/gardener/evolve_v2.go) always sees a nil graph and silently
// falls back to alphabetical tree ordering in the real daemon instead of
// prioritizing by KG analytics (bottlenecks / underbred-but-proven trees).
func TestBuildGardenerConfig_KnowledgeGraphWired(t *testing.T) {
	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}

	if cfg.KnowledgeGraph == nil {
		t.Fatal("KnowledgeGraph is nil — RunCycleV2's treePriorityRanks() falls back to alphabetical ordering instead of KG-driven prioritization in the real daemon")
	}
}

// TestBuildGardenerConfig_TranspositionTableWired pins Q2 Evolvability
// milestone 2/3: buildGardenerConfig must set Config.TranspositionTablePath,
// otherwise Gardener.transpositionTable() (internal/gardener/evolve_v2.go)
// always returns nil and evaluator.IterativeDeepening never runs outside
// tests.
func TestBuildGardenerConfig_TranspositionTableWired(t *testing.T) {
	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}

	if cfg.TranspositionTablePath == "" {
		t.Error("TranspositionTablePath is empty — transpositionTable() always returns nil, so the Stockfish-style deep-search apply path (evaluator.IterativeDeepening) never runs in production")
	}
}

// TestWireSelectorOrdering_SelectsStrategyFromEnv pins milestone 4/5 of the
// Selector-reordering consolidation program ("Consolidate the two competing
// Selector-reordering subsystems in internal/evolution into one canonical
// implementation"): evolution.OrderByIG/OrderByGini/OrderByHybrid
// (internal/evolution/selector_optimizer.go) have zero production callers —
// both real wiring sites (this function and
// internal/domains/tree_resolver.go's applyLearnedSelectorOrdering) hardcode
// evolution.OrderBySuccessRate — so they are pure dead code despite being
// fully implemented and unit-tested. wireSelectorOrdering must read
// BT_SELECTOR_ORDERING_STRATEGY and thread a real evolution.SelectorOrderingStrategy
// through EvolveV2Config so operators can actually select IG/Gini/Hybrid
// ordering in production. An unset (or unrecognized) env var must keep
// today's OrderBySuccessRate behavior — this pass already ships production
// telemetry (see TestWireSelectorOrdering_EnablesLearnedOrderingPass above),
// so silently changing its default ranking would be a behavior change for
// every existing opted-in deployment, not just an activation of dead code.
func TestWireSelectorOrdering_SelectsStrategyFromEnv(t *testing.T) {
	metricsDir := t.TempDir()

	t.Setenv("BT_SELECTOR_ORDERING_STRATEGY", "")
	_, v2Cfg := wireSelectorOrdering(gardener.Config{}, metricsDir)
	if v2Cfg.SelectorOrderingStrategy != evolution.OrderBySuccessRate {
		t.Errorf("default SelectorOrderingStrategy = %q, want %q (unset env must not change existing behavior)",
			v2Cfg.SelectorOrderingStrategy, evolution.OrderBySuccessRate)
	}

	t.Setenv("BT_SELECTOR_ORDERING_STRATEGY", "hybrid")
	_, v2Cfg = wireSelectorOrdering(gardener.Config{}, metricsDir)
	if v2Cfg.SelectorOrderingStrategy != evolution.OrderByHybrid {
		t.Errorf("SelectorOrderingStrategy = %q, want %q when BT_SELECTOR_ORDERING_STRATEGY=hybrid — OrderByHybrid is otherwise unreachable in production",
			v2Cfg.SelectorOrderingStrategy, evolution.OrderByHybrid)
	}
}

// TestWireDTOrdering_EnablesDTStatsPath pins the mirrored wiring for
// domain-tree (DT) reordering: wireDTOrdering must set Config.DTStatsPath
// under metricsDir and flip EvolveV2Config.DTOrdering on, mirroring
// wireSelectorOrdering above. main.go calls both wireSelectorOrdering and
// wireDTOrdering back-to-back on the same v2Cfg, so wireDTOrdering must also
// preserve the SelectorOrdering wiring it's handed rather than clobbering it
// with a freshly built EvolveV2Config. Without this function,
// applyDTOptimizerOrdering (internal/gardener/evolve_v2.go) only ever runs
// inside evolve_v2_test.go, never in the daemon or the langchain
// gardener_run_cycle tool.
func TestWireDTOrdering_EnablesDTStatsPath(t *testing.T) {
	metricsDir := t.TempDir()

	cfg, v2Cfg := wireSelectorOrdering(gardener.Config{}, metricsDir)
	cfg, v2Cfg = wireDTOrdering(cfg, v2Cfg, metricsDir)

	wantPath := filepath.Join(metricsDir, "dt-stats.json")
	if cfg.DTStatsPath != wantPath {
		t.Errorf("DTStatsPath = %q, want %q", cfg.DTStatsPath, wantPath)
	}
	if !v2Cfg.DTOrdering {
		t.Error("DTOrdering = false, want true — domain-tree reordering pass silently disabled in production")
	}
	if !v2Cfg.SelectorOrdering {
		t.Error("SelectorOrdering = false — wireDTOrdering must not clobber the existing Selector-ordering wiring already present in the EvolveV2Config it's handed")
	}
}

// TestBuildGardenerConfig_FeedbackPersistenceArmed pins Q2 Evolvability
// milestone 3/4: buildGardenerConfig must arm the KnowledgeGraph's debounced
// feedback writer (kg.ConfigureFeedbackPersistence) against the shared
// feedback file (agent.FeedbackFile()), the same file bt-agent's scheduler and
// bt-dashboard read. Without it, Config.KnowledgeGraph's persistence path
// stays empty, so FlushFeedback is a permanent no-op and the milestone-2
// write-back never survives a bt-gardener restart.
//
// The knowledge package's feedbackPersist state is unexported, so this proves
// the wiring behaviorally: register a tree, mark it dirty, force a flush, and
// confirm the shared feedback file actually landed on disk (mirrors
// internal/agent/scheduler_test.go's TestNewScheduler_RestoresAndConfiguresFeedback).
func TestBuildGardenerConfig_FeedbackPersistenceArmed(t *testing.T) {
	agentHome := t.TempDir()
	t.Setenv("BT_AGENT_HOME", agentHome)

	snapDir := t.TempDir()
	refDir := t.TempDir()
	metricsDir := t.TempDir()

	cfg, err := buildGardenerConfig(refDir, metricsDir, snapDir, "/tmp/slo-evidence.json")
	if err != nil {
		t.Fatalf("buildGardenerConfig returned error: %v", err)
	}
	if cfg.KnowledgeGraph == nil {
		t.Fatal("KnowledgeGraph is nil")
	}

	cfg.KnowledgeGraph.Register(&knowledge.TreeMeta{
		ID:       "tree:feedback-persist-armed-test",
		Name:     "Feedback Persist Armed Test",
		Category: "test",
	})
	cfg.KnowledgeGraph.MarkFeedbackDirty()
	if err := cfg.KnowledgeGraph.FlushFeedback(true); err != nil {
		t.Fatalf("FlushFeedback: %v", err)
	}

	feedbackPath := agent.FeedbackFile()
	if _, statErr := os.Stat(feedbackPath); statErr != nil {
		t.Fatalf("shared feedback file %q was not written after a forced flush: %v — buildGardenerConfig never armed ConfigureFeedbackPersistence with a non-empty path", feedbackPath, statErr)
	}
}
