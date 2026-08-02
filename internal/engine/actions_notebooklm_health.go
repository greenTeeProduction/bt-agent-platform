package engine

// actions_notebooklm_health.go — an honest outcome for the NotebookLM pipeline
// monitor.
//
// 2026-08-01: notebooklm-pipeline-monitor recorded outcome=success on all 35 of
// its runs since 07-30 while its own report said the pipeline was broken —
// "Source Count: 3 (target 40)", "LOCK_TIMEOUT on research query after 120s",
// a write lock held ~9h, "NEW PLANS NEEDED: YES". "success" was true of the
// MONITOR (it did produce a report) and false of the thing it monitors, and the
// outcome — not the prose — is what the notification throttle, the circuit
// breaker and the dashboard read. The report went nowhere and nothing acted.
//
// The verdict here is derived from the producer's own synthesis files, NOT by
// parsing the monitor's prose: that report is LLM output whose wording changes
// run to run, while the files carry the facts. This action only REFINES the
// outcome; it never fails the tree.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	btcore "github.com/rvitorper/go-bt/core"
)

// nlmHealthSynthesesDir is the producer's output directory. Var for tests.
var nlmHealthSynthesesDir = goapFusionSynthesesDir

// nlmHealthStaleAfter matches the threshold the monitor's own prompt states
// ("research is STALE only if the newest nlm-research file is older than 48
// hours"), so the deterministic verdict and the report agree.
const nlmHealthStaleAfter = 48 * time.Hour

// nlmHealthMinSourceRatio is how far below its own stated target the newest
// synthesis may fall before the run counts as degraded. The live decline was
// 19 -> 2 -> 2 -> 2 -> 3 against a target of 40; half the target is a generous
// floor that still catches that by a wide margin.
const nlmHealthMinSourceRatio = 0.5

// nlmHealthFailureMarkers are the producer's own self-labelled failures. These
// are written BY the research action into the synthesis, so they are facts
// about the pipeline rather than model wording.
var nlmHealthFailureMarkers = []string{
	"LOCK_TIMEOUT",
	"import blocked",
	"Status: STALE",
	"RESOURCE_EXHAUSTED",
	"INVALID_ARGUMENT",
}

// nlmSourceCountRe reads "Source Count: 3 (target 40)" — the producer's own
// header line. Both numbers are optional-tolerant: without a target the count
// alone cannot be judged, so no verdict is drawn from it.
var nlmSourceCountRe = regexp.MustCompile(`(?i)source count:\s*(\d+)\s*\(target\s*(\d+)\)`)

// newestNLMResearchFile returns the most recently modified nlm-research-*.md in
// dir. Only that prefix counts: the monitor writes its OWN summaries into the
// same vault, and counting one as fresh research is precisely the self-read that
// produced 39 days of false "research is fine" verdicts in the 2026-07-15
// stale-glob incident.
func newestNLMResearchFile(dir string) (path string, modTime time.Time, ok bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", time.Time{}, false
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "nlm-research-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if !ok || info.ModTime().After(modTime) {
			path, modTime, ok = filepath.Join(dir, e.Name()), info.ModTime(), true
		}
	}
	return path, modTime, ok
}

// assessNotebookLMPipelineHealth returns the degradation reasons for the newest
// research synthesis. An empty slice means healthy.
func assessNotebookLMPipelineHealth(dir string, now time.Time) []string {
	path, modTime, ok := newestNLMResearchFile(dir)
	if !ok {
		return []string{"no nlm-research-*.md synthesis exists — the producer has never written output here"}
	}
	var reasons []string
	if age := now.Sub(modTime); age > nlmHealthStaleAfter {
		reasons = append(reasons, fmt.Sprintf("newest research %s is %s old (stale after %s)",
			filepath.Base(path), age.Truncate(time.Hour), nlmHealthStaleAfter))
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return append(reasons, fmt.Sprintf("newest research %s is unreadable: %v", filepath.Base(path), err))
	}
	text := string(body)
	for _, marker := range nlmHealthFailureMarkers {
		if strings.Contains(text, marker) {
			reasons = append(reasons, fmt.Sprintf("newest research self-reports %q", marker))
		}
	}
	if m := nlmSourceCountRe.FindStringSubmatch(text); m != nil {
		count, cerr := strconv.Atoi(m[1])
		target, terr := strconv.Atoi(m[2])
		if cerr == nil && terr == nil && target > 0 &&
			float64(count) < float64(target)*nlmHealthMinSourceRatio {
			reasons = append(reasons, fmt.Sprintf("source count %d is far below the producer's own target %d", count, target))
		}
	}
	return reasons
}

func init() {
	// Placed after the consumer chain agent and before the OutcomeSelector, so
	// the monitor still writes its report and still succeeds as an agent — the
	// refinement only changes how the RUN is recorded. "degraded" is a healthy
	// terminal outcome (no breaker trip, no dead-letter) that the routine
	// notification throttle does not suppress, because it only throttles
	// no_change.
	RegisterAction("AssessNotebookLMPipelineHealth", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		reasons := assessNotebookLMPipelineHealth(nlmHealthSynthesesDir, time.Now())
		if len(reasons) == 0 {
			return 1
		}
		bb.OutcomeRefinement = "degraded"
		bb.Result += "\n\n## Pipeline Health: DEGRADED\n\nDerived from the producer's own synthesis files, independently of the report above:\n- " +
			strings.Join(reasons, "\n- ")
		Warn("notebooklm pipeline degraded", "reasons", strings.Join(reasons, "; "))
		return 1
	})
}
