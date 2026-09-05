package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/gardener"
	"github.com/nico/go-bt-evolve/internal/knowledge"
)

// feedbackFlushInterval throttles the knowledge graph's debounced feedback
// writer, matching internal/agent/scheduler.go's default
// (cfg.FeedbackFlushInterval fallback). The gardener has no equivalent
// config knob yet, so this is a fixed constant.
const feedbackFlushInterval = 30 * time.Second

const (
	// islandMigrationInterval is how many island-exploration passes apart the
	// IslandModel cross-pollinates domains (evolution.IslandModel.Migrate):
	// every 4th pass, so islands get a few generations of independent drift
	// before elites move between them.
	islandMigrationInterval = 4

	// islandMigrationRate is the fraction of a target island replaced by
	// migrating elites when a migration is due.
	islandMigrationRate = 0.2
)

// experienceBankDir resolves the on-disk directory backing the gardener's
// ExperienceBank. It deliberately matches bt-agent's experienceBankDir()
// (agent.HomeDir()/"experience") so both binaries accumulate mutation
// experience into one shared bank; agent.HomeDir() honors BT_AGENT_HOME,
// which is the configurability seam for redirecting it.
func experienceBankDir() string { return filepath.Join(agent.HomeDir(), "experience") }

// buildGardenerConfig constructs the production gardener.Config, wiring all
// safety components: Gate, SnapshotDir, CrisisDetector, and the SLO evidence
// file the validation gate reads (B1).
//
// Parameters are split out so the function is testable without touching the
// real home directory.
func buildGardenerConfig(refDir, metricsDir, snapDir, sloEvidencePath string) (gardener.Config, error) {
	if err := os.MkdirAll(snapDir, 0700); err != nil {
		return gardener.Config{}, fmt.Errorf("create snapshot dir %q: %w", snapDir, err)
	}

	refStore, err := evolution.NewStore(refDir)
	if err != nil {
		return gardener.Config{}, fmt.Errorf("open reflection store: %w", err)
	}

	// Personal trees (ADR-133 Phase 5) live in per-user workspaces under
	// agent.UsersDir(); scanning them here puts them into the same 24/7
	// evolution loop as shared trees, with per-user experience banks below.
	registry := gardener.NewRegistryWithUsers(refDir, agent.UsersDir())

	metricsTracker, err := gardener.NewMetricsTracker(metricsDir)
	if err != nil {
		return gardener.Config{}, fmt.Errorf("open metrics tracker: %w", err)
	}

	validationGate := gardener.DefaultValidationGateConfig()
	validationGate.EvidencePath = sloEvidencePath
	// Only the handful of trees executed by live agents have SLO evidence;
	// strict fail-closed for the rest froze evolution at zero applied
	// mutations. Unevidenced trees persist unverified (gardener output is not
	// consumed by running agents); evidenced trees keep threshold enforcement.
	validationGate.AllowUnverified = true

	expBank, err := evolution.NewExperienceBank(experienceBankDir())
	if err != nil {
		return gardener.Config{}, fmt.Errorf("open shared experience bank: %w", err)
	}

	// KG mirrors bt-agent's scheduler (internal/agent/scheduler.go): build the
	// full tree registry, then overlay persisted runtime feedback from the
	// same shared file so RunCycleV2's treePriorityRanks() sees real
	// Fitness/RunCount data instead of falling back to alphabetical ordering.
	kg := knowledge.BuildKnowledgeGraph()
	if err := kg.LoadFeedback(agent.FeedbackFile()); err != nil {
		return gardener.Config{}, fmt.Errorf("load knowledge graph feedback: %w", err)
	}
	// Write-side of feedback persistence (Q2 Evolvability milestone 3): arm the
	// debounced writer against the same shared file so the milestone-2
	// write-back (RunCycleV2 marking trees dirty on RecordRun) survives a
	// bt-gardener restart and is visible to bt-agent/bt-dashboard, which read
	// this identical path. Matches internal/agent/scheduler.go's NewScheduler
	// wiring of knowledge.GlobalGraph.
	kg.ConfigureFeedbackPersistence(agent.FeedbackFile(), feedbackFlushInterval)

	return gardener.Config{
		Registry:       registry,
		MetricsTracker: metricsTracker,
		RefStore:       refStore,
		Interval:       5 * time.Minute,
		MaxMutations:   2,
		UseRealLLM:     false,
		ValidationGate: validationGate,

		// Shared with bt-agent's daemon bank (agent.HomeDir()/experience) so
		// RunCycleV2 records accepted mutations into the same on-disk store
		// bt_evolve_genetic warm-starts from.
		ExperienceBank: expBank,
		// Personal trees record into (and bias against) the owning user's
		// bank instead — users/<user>/experience (ADR-133 Phase 5).
		UserExperienceRoot: agent.UsersDir(),

		// Safety components — wired by A1 remediation
		Gate:           evolution.NewQualityGate(snapDir),
		SnapshotDir:    snapDir,
		CrisisDetector: evolution.NewCrisisDetector(),

		// MetaValidator catches structurally broken mutations (empty
		// selectors, unbounded retries, expert antipatterns) that the
		// fitness/quality/SLO gates above never inspect. Production-safe
		// defaults (see evolution.NewMetaValidator).
		MetaValidator: evolution.NewMetaValidator(evolution.MetaValidatorConfig{}),

		// TranspositionTablePath enables the Stockfish-style deep-search apply
		// path (evaluator.IterativeDeepening, Q2 Evolvability milestone 2):
		// without it, Gardener.transpositionTable() always returns nil and
		// deep search never runs outside tests.
		TranspositionTablePath: filepath.Join(metricsDir, "transposition"),

		// KnowledgeGraph seeds treePriorityRanks() with KG-driven prioritization
		// (bottlenecks and underbred-but-proven trees first) instead of the
		// alphabetical fallback (Q2 Evolvability milestone 1).
		KnowledgeGraph: kg,

		// IslandModel activates RunCycleV2's periodic per-domain
		// population-exploration pass: without it the island model is reachable
		// only from bt-agent's MCP tools, so the 24/7 daemon never explores a
		// population and only ever mutates current-best. IslandInterval is left
		// at the gardener's own default (defaultIslandInterval — roughly hourly
		// at the 5-minute cycle interval above), since the pass evolves a whole
		// subpopulation per tree and is deliberately a side channel.
		//
		// Adoption of an island winner is a persist path, so it clears the Gate
		// and ValidationGate wired above before overwriting a live tree — the
		// pass runs before the per-tree loop, so those gates are the only thing
		// standing between a randomly bred individual and disk.
		IslandModel: evolution.NewIslandModel(islandMigrationInterval, islandMigrationRate),
	}, nil
}

// wireSelectorOrdering enables the milestone-4 learned-Selector-ordering pass
// (internal/gardener/evolve_v2.go) for production: it points cfg at a durable
// stats file under metricsDir and flips EvolveV2Config.SelectorOrdering on.
// Both DefaultEvolveV2Config() and buildGardenerConfig() leave this off by
// design (opt-in, see evolve_v2.go), so without this call the pass only ever
// ran inside evolve_v2_test.go — never in the daemon or the langchain
// gardener_run_cycle tool.
func wireSelectorOrdering(cfg gardener.Config, metricsDir string) (gardener.Config, gardener.EvolveV2Config) {
	cfg.SelectorStatsPath = filepath.Join(metricsDir, "selector-stats.json")

	v2Cfg := gardener.DefaultEvolveV2Config()
	v2Cfg.SelectorOrdering = true
	// BT_SELECTOR_ORDERING_STRATEGY lets operators opt into
	// OrderByIG/OrderByGini/OrderByHybrid; unset or unrecognized values keep
	// today's OrderBySuccessRate behavior (Selector-reordering consolidation
	// milestone 4).
	v2Cfg.SelectorOrderingStrategy = evolution.ParseSelectorOrderingStrategy(os.Getenv("BT_SELECTOR_ORDERING_STRATEGY"))
	return cfg, v2Cfg
}

// wireDTOrdering enables the domain-tree (DT) entropy/Gini-based reordering
// pass (internal/gardener/evolve_v2.go's applyDTOptimizerOrdering), mirroring
// wireSelectorOrdering above: it points cfg at a durable DTAnalyzer stats
// file under metricsDir and flips EvolveV2Config.DTOrdering on. It takes and
// returns the same v2Cfg threaded through wireSelectorOrdering so both passes
// can be wired back-to-back without clobbering each other.
func wireDTOrdering(cfg gardener.Config, v2Cfg gardener.EvolveV2Config, metricsDir string) (gardener.Config, gardener.EvolveV2Config) {
	cfg.DTStatsPath = filepath.Join(metricsDir, "dt-stats.json")
	v2Cfg.DTOrdering = true
	return cfg, v2Cfg
}
