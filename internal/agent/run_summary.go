package agent

import "strings"

// summaryMaxLen bounds {data.summary} to phone-notification size.
const summaryMaxLen = 600

// summaryMaxSteps is how many trailing node-path steps the summary keeps —
// the tail is where a run succeeded or died, the head is always preflight.
const summaryMaxSteps = 6

// buildRunActivitySummary distills a run's markdown output, failure reason,
// and executed node trail into the plain-text block the bt-task-complete
// Telegram webhook renders as {data.summary} (zero-LLM template — whatever
// this returns is exactly what the operator reads about the run).
func buildRunActivitySummary(output, failureReason, nodes string) string {
	var lines []string

	if failureReason != "" {
		lines = append(lines, "FAILED: "+failureReason)
	}

	headline, facts := salientOutputLines(output)
	if headline != "" {
		lines = append(lines, headline)
	}
	lines = append(lines, facts...)

	if trail := trimStepTrail(nodes); trail != "" {
		lines = append(lines, "Steps: "+trail)
	}

	if len(lines) == 0 {
		return "(no output)"
	}
	sum := strings.Join(lines, "\n")
	if len(sum) > summaryMaxLen {
		sum = sum[:summaryMaxLen-1] + "…"
	}
	return sum
}

// salientOutputLines extracts the first markdown headline and short
// "Key: value"-style fact lines (Run:, Commit:, Apply status:, **Status**:,
// …) from a run's output. Prose paragraphs are skipped: notifications need
// facts, the full report lives in the run artifacts.
func salientOutputLines(output string) (headline string, facts []string) {
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if h, ok := strings.CutPrefix(line, "## "); ok {
			if headline == "" {
				headline = strings.TrimSpace(h)
			}
			continue
		}
		if isFactLine(line) && len(facts) < 8 {
			facts = append(facts, strings.Trim(line, "*"))
			continue
		}
		// Fallback: no headline yet and no facts — keep the first plain
		// line so unstructured outputs still say something.
		if headline == "" && len(facts) == 0 {
			headline = line
		}
	}
	return headline, facts
}

// isFactLine reports whether a line looks like "Key: value" with a short key
// (or its bold "**Key**: value" variant) — the shape run reports use for
// run id, commit, apply status, and error facts.
func isFactLine(line string) bool {
	trimmed := strings.Trim(line, "*")
	key, _, ok := strings.Cut(trimmed, ":")
	if !ok {
		return false
	}
	key = strings.TrimSpace(key)
	return key != "" && len(key) <= 24 && !strings.Contains(key, " → ")
}

// trimStepTrail keeps the last summaryMaxSteps entries of an "a → b → c"
// node trail, prefixing an ellipsis when older steps were dropped.
func trimStepTrail(nodes string) string {
	nodes = strings.TrimSpace(nodes)
	if nodes == "" {
		return ""
	}
	steps := strings.Split(nodes, " → ")
	if len(steps) <= summaryMaxSteps {
		return nodes
	}
	return "… → " + strings.Join(steps[len(steps)-summaryMaxSteps:], " → ")
}
