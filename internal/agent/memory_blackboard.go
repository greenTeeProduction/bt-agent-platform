package agent

import (
	"fmt"
	"strings"

	"github.com/nico/go-bt-evolve/internal/blackboard"
)

const memoryBlackboardHint = `BLACKBOARD CONTEXT — Full memory/history is stored off-prompt. Use bb_read for:
  • memory/facts — high-priority agent facts
  • memory/pitfalls — known pitfalls
  • memory/patterns — learned patterns
  • history/runs — previous successful run outputs`

// seedMemoryToBlackboard loads agent memory and run history into the run-scoped
// blackboard and returns the task plus a short pointer hint (not the full payload).
func (d *RunDeps) seedMemoryToBlackboard(agentName, task string, prevLimit int, h *blackboard.Handle) string {
	if h == nil {
		return task
	}
	if prevLimit <= 0 {
		prevLimit = 2
	}

	mem, err := NewMemoryStore(MemoryDir(), agentName, 100)
	if err != nil {
		return task
	}

	seeded := false

	if block := exportMemoryCategory(mem, "fact", "high", 5); block != "" {
		_ = h.Set("memory/facts", block, "High-priority agent facts", "text")
		seeded = true
	}
	if block := exportMemoryCategory(mem, "pitfall", "high", 3); block != "" {
		_ = h.Set("memory/pitfalls", block, "Known pitfalls to avoid", "text")
		seeded = true
	}
	if block := exportMemoryCategory(mem, "pattern", "high", 3); block != "" {
		_ = h.Set("memory/patterns", block, "Learned patterns", "text")
		seeded = true
	}

	if d.History != nil {
		if block := exportPreviousRuns(d.History, agentName, prevLimit); block != "" {
			_ = h.Set("history/runs", block, fmt.Sprintf("Last %d successful runs", prevLimit), "text")
			seeded = true
		}
	}

	if !seeded {
		return task
	}
	return task + "\n\n" + memoryBlackboardHint
}

func exportMemoryCategory(mem *MemoryStore, category, priority string, limit int) string {
	entries := mem.Query(category, priority, limit)
	if len(entries) == 0 {
		return ""
	}
	var b strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&b, "%s: %s\n", e.Key, e.Value)
	}
	return strings.TrimSpace(b.String())
}

func exportPreviousRuns(history *History, agentName string, n int) string {
	runs := history.List(agentName, n+5)
	lines := make([]string, 0, len(runs))
	for _, r := range runs {
		if r.Outcome != "success" || r.Output == "" {
			continue
		}
		lines = append(lines, fmt.Sprintf(
			"=== Run (%s, %s) ===\nTask: %s\nOutput:\n%s",
			r.EndedAt.Format("2006-01-02 15:04"),
			r.Duration,
			r.Task,
			r.Output,
		))
		if len(lines) >= n {
			break
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n\n")
}
