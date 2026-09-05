package agent

import (
	"strings"
	"testing"
)

// The Telegram bt-task-complete webhook renders {data.summary} verbatim
// (zero-LLM template), so this function is the whole story of what the
// operator sees about a run. It must surface the substance — headline,
// run/commit facts, failure reason, executed steps — without dumping the
// full markdown report into a phone notification.

func TestBuildRunActivitySummarySuccessfulSuperpowersRun(t *testing.T) {
	output := "## Superpowers Implementation Complete\n\n" +
		"Run: `20260703T073337-8f8576fe`\n" +
		"Artifacts: `/home/nico/go-bt-evolve/docs/superpowers/runs/20260703T073337-8f8576fe`\n" +
		"Apply status: `committed`\n" +
		"Commit: `aedcce9`\n\n" +
		"Some long trailing prose that should not need to appear in full.\n"
	sum := buildRunActivitySummary(output, "", "GrillMe → ResearchRouter → Implement → Verify")

	for _, want := range []string{
		"Superpowers Implementation Complete",
		"Run: 20260703T073337-8f8576fe",
		"Commit: aedcce9",
		"Apply status: committed",
		"Steps: GrillMe → ResearchRouter → Implement → Verify",
	} {
		if !strings.Contains(sum, want) {
			t.Errorf("summary missing %q:\n%s", want, sum)
		}
	}
	if strings.Contains(sum, "trailing prose") {
		t.Errorf("summary should keep key facts, not prose:\n%s", sum)
	}
}

func TestBuildRunActivitySummaryFailureLeadsWithReason(t *testing.T) {
	sum := buildRunActivitySummary("## Verification Failed\n\nBUILD FAILED (exit status 1)", "agent outcome: failure", "")
	if !strings.HasPrefix(sum, "FAILED: agent outcome: failure") {
		t.Errorf("failure summary must lead with the reason:\n%s", sum)
	}
	if !strings.Contains(sum, "Verification Failed") {
		t.Errorf("failure summary must keep the output headline:\n%s", sum)
	}
}

func TestBuildRunActivitySummaryFallsBackToFirstLinesAndTruncates(t *testing.T) {
	long := strings.Repeat("word ", 300)
	sum := buildRunActivitySummary("plain output without headers\n"+long, "", "")
	if !strings.Contains(sum, "plain output without headers") {
		t.Errorf("fallback must include the first line:\n%s", sum)
	}
	if len(sum) > 700 {
		t.Errorf("summary must stay notification-sized, got %d chars", len(sum))
	}
}

func TestBuildRunActivitySummaryTrimsLongStepTrails(t *testing.T) {
	nodes := strings.Join([]string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}, " → ")
	sum := buildRunActivitySummary("## Done", "", nodes)
	if strings.Contains(sum, "a → b") {
		t.Errorf("long step trails must be trimmed to the tail:\n%s", sum)
	}
	if !strings.Contains(sum, "… → ") || !strings.Contains(sum, "→ j") {
		t.Errorf("trimmed trail must keep the final steps with ellipsis:\n%s", sum)
	}
}

func TestBuildRunActivitySummaryEmptyEverything(t *testing.T) {
	if sum := buildRunActivitySummary("", "", ""); sum != "(no output)" {
		t.Errorf("empty run summary = %q, want (no output)", sum)
	}
}

// TestBuildRunActivitySummaryFoldsFencedListAfterEmptyLabel uses the exact
// live output of run 20260703T083801: "Changed files:" is followed by a
// fenced block of paths, which rendered on Telegram as a dangling label with
// nothing after it. The fenced items must fold into the label's line, and
// backticks must not leak into the plain-text notification.
func TestBuildRunActivitySummaryFoldsFencedListAfterEmptyLabel(t *testing.T) {
	output := "## Superpowers Implementation Complete\n\n" +
		"Run: `20260703T083801-8f8576fe`\n" +
		"Commit: `0ede946`\n" +
		"Changed files:\n```\ninternal/engine/actions_superpowers.go\ninternal/engine/superpowers_runtime_contract_test.go\n```"
	sum := buildRunActivitySummary(output, "", "")

	if !strings.Contains(sum, "Changed files: internal/engine/actions_superpowers.go, internal/engine/superpowers_runtime_contract_test.go") {
		t.Errorf("fenced list must fold into its label line:\n%s", sum)
	}
	if strings.Contains(sum, "`") {
		t.Errorf("backticks must not leak into plain-text summary:\n%s", sum)
	}
	if strings.Contains(sum, "```") {
		t.Errorf("fences must not appear:\n%s", sum)
	}
}

func TestBuildRunActivitySummaryCapsLongFencedLists(t *testing.T) {
	output := "## Done\nChanged files:\n```\na.go\nb.go\nc.go\nd.go\ne.go\nf.go\n```"
	sum := buildRunActivitySummary(output, "", "")
	if !strings.Contains(sum, "Changed files: a.go, b.go, c.go, d.go (+2 more)") {
		t.Errorf("long fenced lists must cap with +N more:\n%s", sum)
	}
}
