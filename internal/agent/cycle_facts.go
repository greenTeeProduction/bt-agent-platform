package agent

import (
	"regexp"
	"strings"
)

// extractCycleFacts distills the operationally useful markers a goap-fusion
// cycle leaves in its result text into structured log fields, so the
// "scheduler: cycle complete" event carries what the cycle actually did
// without a human reading run.json. Absent markers are simply omitted.
func extractCycleFacts(output string) map[string]any {
	facts := map[string]any{}
	if m := cycleCommitRe.FindStringSubmatch(output); m != nil {
		facts["commit"] = m[1]
	}
	if m := cycleApplyRe.FindStringSubmatch(output); m != nil {
		facts["apply"] = m[1]
	}
	if strings.Contains(output, "PARTIAL LANDING") {
		facts["partial_landing"] = true
	}
	if strings.Contains(output, "Backlog Seeded") {
		facts["seeded_program"] = true
	}
	if m := cycleMilestoneRe.FindStringSubmatch(output); m != nil {
		facts["program_milestone"] = m[1]
	}
	if strings.Contains(output, "arc42 sync:") && !strings.Contains(output, "no documentation impact") {
		facts["arc42_synced"] = true
	}
	if strings.Contains(output, "No New Research") || strings.Contains(output, "no saved plan") {
		facts["noop"] = true
	}
	return facts
}

var (
	cycleCommitRe    = regexp.MustCompile(`(?i)Commit:\s*` + "`?" + `([0-9a-f]{7,40})`)
	cycleApplyRe     = regexp.MustCompile(`(?i)Apply status:\s*` + "`?" + `(\w+)`)
	cycleMilestoneRe = regexp.MustCompile(`milestone (\d+/\d+)`)
)

func truncateForLog(s string, n int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " | ")
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}
