package domains

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/benchmark"
	"github.com/nico/go-bt-evolve/internal/engine"
)

// singleTaskSuite builds a minimal Suite with one task.
func singleTaskSuite(name, task string, shouldSucceed bool) benchmark.Suite {
	return benchmark.Suite{
		Name: name,
		Tasks: []benchmark.TaskCase{
			{Task: task, ShouldSucceed: shouldSucceed, MinResultLen: 10},
		},
	}
}

func TestCodeReviewTree(t *testing.T) {
	tree := CodeReviewTree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("code_review_smoke", "find bugs in this code", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.SuccessRate < 0.5 {
		t.Errorf("CodeReviewTree success rate too low: %.2f (want >= 0.5)", metrics.SuccessRate)
	}
	t.Logf("CodeReviewTree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

func TestDevOpsTree(t *testing.T) {
	tree := DevOpsCITree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("devops_ci_smoke", "build the project", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.Successes == 0 {
		t.Error("DevOpsCITree task should succeed")
	}
	t.Logf("DevOpsCITree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

func TestAgentMonitor(t *testing.T) {
	tree := AgentMonitorTree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("agent_monitor_smoke", "check health of all agents", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.Successes == 0 {
		t.Error("AgentMonitorTree task should succeed")
	}
	t.Logf("AgentMonitorTree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

func TestCrashInvestigator(t *testing.T) {
	tree := CrashInvestigatorTree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("crash_investigator_smoke", "parse this stack trace for crash", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.Successes == 0 {
		t.Error("CrashInvestigatorTree task should succeed")
	}
	t.Logf("CrashInvestigatorTree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

func TestGameAI(t *testing.T) {
	tree := GameAITree()
	mock := benchmark.DefaultMock()
	suite := singleTaskSuite("game_ai_smoke", "game: patrol the area", true)
	metrics := benchmark.RunSuite(tree, suite, mock)

	if metrics.Successes == 0 {
		t.Error("GameAITree task should succeed")
	}
	t.Logf("GameAITree: %d/%d passed, rate=%.2f, avgDur=%dms",
		metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
}

// tasksForTree returns a representative smoke task for each domain tree.
func tasksForTree() map[string]string {
	return map[string]string{
		"code_review":        "find bugs in this code",
		"devops_ci":          "build the project",
		"agent_monitor":      "check health of all agents",
		"refactoring":        "refactor this code to be cleaner",
		"security_audit":     "audit this code for vulnerabilities",
		"data_pipeline":      "extract data from source and transform",
		"meeting_notes":      "summarize this meeting transcript",
		"crash_investigator": "parse this stack trace for crash",
		"game_ai":            "game: patrol the area",
		"trading_signal":     "calculate trading signals for AAPL",
		"alert_router":       "critical disk alert: sda1 at 95%",
		"goap_planning":      "plan the steps to deploy a new service",
		"goap_research":      "research best practices for Go microservices",
		"goap_devops":        "diagnose why the CI pipeline is failing",
		// Arc42 documentation trees
		"arc42:section1":  "generate arc42 introduction and goals",
		"arc42:section2":  "generate arc42 constraints section",
		"arc42:section3":  "generate arc42 context and scope",
		"arc42:section4":  "generate arc42 solution strategy",
		"arc42:section5":  "generate arc42 building block view",
		"arc42:section6":  "generate arc42 runtime view",
		"arc42:section7":  "generate arc42 deployment view",
		"arc42:section8":  "generate arc42 crosscutting concepts",
		"arc42:section9":  "generate arc42 architecture decisions",
		"arc42:section10": "generate arc42 quality requirements",
		"arc42:section11": "generate arc42 risks and technical debt",
		"arc42:section12": "generate arc42 glossary",
		"arc42:assemble":  "assemble final arc42 document",
	}
}

func TestAllDomainTrees(t *testing.T) {
	all := AllDomainTrees()
	tasks := tasksForTree()
	mock := benchmark.DefaultMock()

	if len(all) != 27 {
		t.Errorf("expected 27 domain trees, got %d", len(all))
	}

	for name, tree := range all {
		task, ok := tasks[name]
		if !ok {
			t.Errorf("no smoke task defined for tree %q", name)
			continue
		}
		// Arc42 trees require graphify + LLM + shell access. Smoke-test
		// structural validity only: verify BuildTree doesn't panic.
		if strings.HasPrefix(name, "arc42:") {
			bb := &engine.Blackboard{
				Task: task,
				LLM:  mock,
			}
			cmd := engine.BuildTree(tree, bb)
			if cmd == nil {
				t.Errorf("arc42 tree %q: BuildTree returned nil", name)
			}
			t.Logf("  %s: structure OK (skip runtime — needs graphify + LLM)", name)
			continue
		}

		suite := singleTaskSuite(name+"_smoke", task, true)
		metrics := benchmark.RunSuite(tree, suite, mock)

		if metrics.Successes == 0 {
			t.Errorf("tree %q failed its smoke task %q (0/%d passed)", name, task, metrics.TotalTasks)
		}

		t.Logf("  %s: %d/%d passed, rate=%.2f, avgDur=%dms",
			name, metrics.Successes, metrics.TotalTasks, metrics.SuccessRate, int64(metrics.AvgDurationMs))
	}
}
