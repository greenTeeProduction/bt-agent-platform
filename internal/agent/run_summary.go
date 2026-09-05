package agent

import (
	"fmt"
	"strings"
)

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
	lines := strings.Split(output, "\n")
	for i := 0; i < len(lines); i++ {
		// Backticks are markdown affordances; the Telegram template renders
		// plain text, so they would show up literally.
		line := strings.ReplaceAll(strings.TrimSpace(lines[i]), "`", "")
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "```") {
			// Stray fence (one not owned by an empty-label fact below):
			// skip the whole block.
			i++
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				i++
			}
			continue
		}
		if h, ok := strings.CutPrefix(line, "## "); ok {
			if headline == "" {
				headline = strings.TrimSpace(h)
			}
			continue
		}
		if isFactLine(line) && len(facts) < 8 {
			fact := strings.Trim(line, "*")
			// An empty-value label ("Changed files:") followed by a fenced
			// block is a list rendered for the full report — fold the items
			// into the label's line so the notification doesn't end on a
			// dangling label (live bug: run 20260703T083801's Telegram
			// message ended with a bare "Changed files:").
			if strings.HasSuffix(fact, ":") {
				items, next := fencedItems(lines, i+1)
				if next > i {
					fact += " " + joinCapped(items, 4)
					i = next
				}
			}
			facts = append(facts, fact)
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

// fencedItems returns the content lines of a ``` block starting at or right
// after index from (skipping blank lines), and the index of the closing
// fence. Returns next <= from when no fence follows.
func fencedItems(lines []string, from int) (items []string, next int) {
	i := from
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
		return nil, from - 1
	}
	for i++; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "```") {
			return items, i
		}
		if line != "" {
			items = append(items, strings.Trim(line, "-* "))
		}
	}
	return items, i - 1
}

// joinCapped joins up to limit items with commas, appending "(+N more)".
func joinCapped(items []string, limit int) string {
	if len(items) <= limit {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:limit], ", ") + fmt.Sprintf(" (+%d more)", len(items)-limit)
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
