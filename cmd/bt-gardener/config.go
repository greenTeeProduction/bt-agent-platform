package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/evolution"
	"github.com/nico/go-bt-evolve/internal/gardener"
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

	// Personal trees (ADR-010 Phase 5) live in per-user workspaces under
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
		// bank instead — users/<user>/experience (ADR-010 Phase 5).
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
	return cfg, v2Cfg
}
