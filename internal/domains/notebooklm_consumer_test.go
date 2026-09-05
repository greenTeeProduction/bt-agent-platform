package domains

import (
	"strings"
	"testing"

	"github.com/nico/go-bt-evolve/internal/evolution"
)

// consumerChainPrompts collects the ChainAction prompts of the consumer tree.
func consumerChainPrompts(t *testing.T) string {
	t.Helper()
	tree := NotebookLMConsumerTree()
	if tree == nil {
		t.Fatal("NotebookLMConsumerTree returned nil")
	}
	var prompts []string
	walkTree(tree, func(n *evolution.SerializableNode) {
		if n.Type == "ChainAction" {
			prompts = append(prompts, n.Name)
		}
	})
	if len(prompts) == 0 {
		t.Fatal("consumer tree has no ChainAction prompts")
	}
	return strings.Join(prompts, "\n---\n")
}

// The research pipeline renamed its synthesis outputs on 2026-06-03: the
// producer (actions_notebooklm.go writeNotebookLMResearch) writes
// nlm-research-<date>.md, and the old notebooklm-*.md naming is retired.
// Watching the retired glob made the monitor report "research has stalled
// for 39 days" while fresh research sat in the same directory.
func TestNotebookLMConsumerTreeWatchesCurrentResearchNaming(t *testing.T) {
	prompts := consumerChainPrompts(t)
	if !strings.Contains(prompts, "nlm-research-") {
		t.Fatalf("consumer prompt must watch the current nlm-research-*.md naming:\n%s", prompts)
	}
}

// The retired glob also matched the monitor's own report file
// (notebooklm-pipeline-health.md), which — as the newest match — became the
// "newest synthesis" every run read and republished: a self-referential loop
// that kept re-sending the same stale report as fresh output.
func TestNotebookLMConsumerTreeDoesNotWatchRetiredGlobOrOwnOutput(t *testing.T) {
	prompts := consumerChainPrompts(t)
	if strings.Contains(prompts, "notebooklm-*.md") {
		t.Fatalf("consumer prompt still watches the retired notebooklm-*.md glob:\n%s", prompts)
	}
	if strings.Contains(prompts, "notebooklm-pipeline-health") {
		t.Fatalf("consumer prompt must not read its own report file back as input:\n%s", prompts)
	}
}
