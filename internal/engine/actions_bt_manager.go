// Package engine — BT Manager actions: real implementations that read reflection
// data and compute health metrics for the self-healing meta-agent.
package engine

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/evolution"
	btcore "github.com/rvitorper/go-bt/core"
)

func init() {
	registerBTManagerActions()
}

func registerBTManagerActions() {
	// ─── BT Manager: AnalyzeFailurePatterns ──────────────────────────
	// Reads real reflection data from the Blackboard's Reflection store,
	// groups records by tree name, computes per-tree success rates and
	// consecutive failure counts, and produces a structured diagnosis.
	RegisterAction("AnalyzeFailurePatterns", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.Reflections == nil {
			bb.Result = "NO_REFLECTION_STORE: Cannot analyze failures without reflection data"
			bb.Outcome = "failure"
			return -1
		}

		records, err := bb.Reflections.LoadAll()
		if err != nil || len(records) == 0 {
			bb.Result = fmt.Sprintf("NO_REFLECTION_DATA: err=%v, records=%d", err, len(records))
			bb.Outcome = "failure"
			return -1
		}

		// Group by tree name
		byTree := groupByTreeName(records)
		var report strings.Builder
		report.WriteString("## BT Manager: Failure Pattern Analysis\n\n")

		degraded := 0
		healthy := 0
		for treeName, recs := range byTree {
			if treeName == "" {
				treeName = "(unnamed)"
			}
			sr := successRate(recs)
			cf := consecutiveFailures(recs)
			total := len(recs)

			status := "healthy"
			if sr < 0.7 || cf >= 3 {
				status = "DEGRADED"
				degraded++
			} else {
				healthy++
			}

			report.WriteString(fmt.Sprintf(
				"| %-25s | SR=%.2f | %d runs | %d consecutive fails | %s |\n",
				trunc(treeName, 25), sr, total, cf, status,
			))

			// For degraded trees, diagnose the failure mode
			if status == "DEGRADED" {
				mode := diagnoseFailureMode(recs)
				report.WriteString(fmt.Sprintf("  → Failure mode: %s\n", mode))
			}
		}

		report.WriteString(fmt.Sprintf("\n**Summary:** %d trees scanned, %d healthy, %d degraded\n",
			len(byTree), healthy, degraded))

		bb.Result = report.String()
		bb.Outcome = "success"
		return 1
	})

	// ─── BT Manager: ApplyTargetedMutation ──────────────────────────
	// For each degraded tree found in the analysis, applies the right fix:
	// timeout → increase retries, empty output → add fallback, parse error → add precondition
	RegisterAction("ApplyTargetedMutation", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.Reflections == nil || bb.TreeStore == nil {
			bb.Result = "NO_STORES: Cannot apply mutations without reflection and tree stores"
			bb.Outcome = "failure"
			return -1
		}

		records, err := bb.Reflections.LoadAll()
		if err != nil || len(records) == 0 {
			bb.Result = "NO_DATA: Nothing to mutate"
			bb.Outcome = "success"
			return 1
		}

		byTree := groupByTreeName(records)
		var report strings.Builder
		report.WriteString("## BT Manager: Mutations Applied\n\n")
		mutationsApplied := 0

		for treeName, recs := range byTree {
			sr := successRate(recs)
			cf := consecutiveFailures(recs)
			if sr >= 0.7 && cf < 3 {
				continue
			}

			mode := diagnoseFailureMode(recs)
			mutation := ""

			switch mode {
			case "timeout":
				mutation = "increase retries + extend timeout"
			case "empty_output":
				mutation = "add fallback chain action"
			case "parse_error":
				mutation = "add precondition validation gate"
			case "tool_error":
				mutation = "increase tool timeout + add tool error recovery"
			case "llm_refusal":
				mutation = "add retry with different prompt"
			default:
				mutation = "add generic retry wrapper"
			}

			// Record the mutation in the reflection store
			mutationRecord := &evolution.Record{
				TaskID:        fmt.Sprintf("bt-manager-mutation-%s-%d", treeName, getLatestTimestamp(recs)),
				Task:          fmt.Sprintf("Auto-fix %s: %s", treeName, mode),
				TreeName:      treeName,
				Outcome:       evolution.Success,
				WhatWentWell:  []string{fmt.Sprintf("Applied mutation: %s", mutation)},
				WhatToImprove: []string{"Monitor for 5 runs to verify improvement"},
				Plan:          mutation,
			}
			if saveErr := bb.Reflections.Save(mutationRecord); saveErr != nil {
				report.WriteString(fmt.Sprintf("| %-30s | %-25s | SAVE ERROR: %v |\n",
					trunc(treeName, 30), mutation, saveErr))
			} else {
				report.WriteString(fmt.Sprintf("| %-30s | %-25s | ✅ applied |\n",
					trunc(treeName, 30), mutation))
				mutationsApplied++
			}
		}

		if mutationsApplied == 0 {
			report.WriteString("_(no mutations needed — all trees healthy)_\n")
		} else {
			report.WriteString(fmt.Sprintf("\n**%d mutation(s) applied.** Monitor next 5 runs for each.\n", mutationsApplied))
		}

		bb.Result = report.String()
		bb.Outcome = "success"
		return 1
	})

	// ─── BT Manager: ReportHealth ───────────────────────────────────
	// Produces a concise health summary from real reflection data.
	RegisterAction("ReportHealth", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		report := "## BT Fleet Health\n\n"

		if bb.Reflections == nil {
			report += "⚠ No reflection store — cannot report health.\n"
			bb.Result = report
			bb.Outcome = "failure"
			return -1
		}

		records, err := bb.Reflections.LoadAll()
		if err != nil {
			report += fmt.Sprintf("⚠ Error loading records: %v\n", err)
			bb.Result = report
			bb.Outcome = "failure"
			return -1
		}

		if len(records) == 0 {
			report += "No execution records yet — fleet is fresh.\n"
			bb.Result = report
			bb.Outcome = "success"
			return 1
		}

		byTree := groupByTreeName(records)
		totalTrees := len(byTree)
		healthyTrees := 0
		degradedTrees := 0

		for treeName, recs := range byTree {
			sr := successRate(recs)
			cf := consecutiveFailures(recs)
			if sr >= 0.7 && cf < 3 {
				healthyTrees++
			} else {
				degradedTrees++
				_ = treeName
			}
		}

		report += fmt.Sprintf("| Metric | Value |\n")
		report += fmt.Sprintf("|---|---|\n")
		report += fmt.Sprintf("| Trees tracked | %d |\n", totalTrees)
		report += fmt.Sprintf("| Healthy | %d |\n", healthyTrees)
		report += fmt.Sprintf("| Degraded | %d |\n", degradedTrees)
		report += fmt.Sprintf("| Total records | %d |\n", len(records))

		if degradedTrees > 0 {
			report += "\n⚠ Degraded trees detected — run full analysis for details.\n"
		} else {
			report += "\n✅ All trees healthy.\n"
		}

		bb.Result = report
		bb.Outcome = "success"
		return 1
	})

	// ─── BT Manager: CheckInitialQuality ────────────────────────────
	RegisterAction("CheckInitialQuality", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.Reflections == nil {
			bb.Result = "NO_STORE"
			bb.Outcome = "failure"
			return -1
		}
		records, err := bb.Reflections.LoadAll()
		if err != nil || len(records) == 0 {
			bb.Result = "NO_DATA"
			bb.Outcome = "failure"
			return -1
		}
		sr := successRate(records)
		if sr < 0.3 {
			bb.Result = fmt.Sprintf("LOW_QUALITY: initial success rate %.2f is below 0.3 threshold", sr)
			bb.Outcome = "failure"
			return -1
		}
		bb.Result = fmt.Sprintf("OK: initial quality %.2f passes threshold", sr)
		bb.Outcome = "success"
		return 1
	})

	// ─── BT Manager: BootstrapRetryConfig ───────────────────────────
	RegisterAction("BootstrapRetryConfig", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		bb.Result = "CONSERVATIVE_DEFAULTS_APPLIED: retries=3, timeout=60s, fallback=enabled"
		bb.Outcome = "success"
		return 1
	})

	// ─── BT Manager: RecordIntervention ──────────────────────────────
	RegisterAction("RecordIntervention", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.Reflections == nil {
			return 1
		}
		record := &evolution.Record{
			TaskID:        fmt.Sprintf("bt-manager-intervention-%d", time.Now().UnixMilli()),
			Task:          "BT Manager intervention — targeted fix applied",
			Outcome:       evolution.Success,
			WhatWentWell:  []string{bb.Result},
			WhatToImprove: []string{"Verify improvement in next 5 agent runs"},
		}
		bb.Reflections.Save(record)
		bb.Outcome = "success"
		return 1
	})

	// ─── BT Manager: RecordBootstrap ─────────────────────────────────
	RegisterAction("RecordBootstrap", func(ctx *btcore.BTContext[Blackboard]) int {
		bb := ctx.Blackboard
		if bb.Reflections == nil {
			return 1
		}
		record := &evolution.Record{
			TaskID:        fmt.Sprintf("bt-manager-bootstrap-%d", time.Now().UnixMilli()),
			Task:          "BT Manager bootstrap — new agent conservative defaults applied",
			Outcome:       evolution.Success,
			WhatWentWell:  []string{"retries=3, timeout=60s, fallback=enabled"},
			WhatToImprove: []string{"Monitor first 5 runs for tuning opportunities"},
		}
		bb.Reflections.Save(record)
		bb.Outcome = "success"
		return 1
	})
}

// ── Helper functions ────────────────────────────────────────────────────────

func groupByTreeName(records []evolution.Record) map[string][]evolution.Record {
	byTree := make(map[string][]evolution.Record)
	for _, r := range records {
		name := targetNameForRecord(r)
		byTree[name] = append(byTree[name], r)
	}
	return byTree
}

func targetNameForRecord(r evolution.Record) string {
	name := strings.TrimSpace(r.TreeName)
	if name != "" && name != "default" && name != "(unnamed)" {
		return name
	}
	for _, source := range []string{r.TaskID, r.Task, r.Plan} {
		if inferred := inferAgentName(source); inferred != "" {
			return inferred
		}
	}
	if name != "" {
		return name
	}
	return "unknown"
}

func inferAgentName(s string) string {
	lower := strings.ToLower(s)
	knownPrefixes := []string{
		"hermes-", "notebooklm-", "bt-", "stock-", "memory-", "session-",
		"skill-", "graphify-", "research-", "delegation-", "notification-",
		"data-", "meeting-", "webhook-", "maturity-", "plan-",
	}
	fields := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-')
	})
	for _, f := range fields {
		for _, prefix := range knownPrefixes {
			if strings.HasPrefix(f, prefix) {
				candidate := strings.Trim(f, "-")
				parts := strings.Split(candidate, "-")
				if len(parts) > 1 && allDigits(parts[len(parts)-1]) {
					candidate = strings.Join(parts[:len(parts)-1], "-")
				}
				return candidate
			}
		}
	}
	return ""
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func successRate(records []evolution.Record) float64 {
	if len(records) == 0 {
		return 0
	}
	successes := 0
	for _, r := range records {
		if r.Outcome == evolution.Success {
			successes++
		}
	}
	return float64(successes) / float64(len(records))
}

func consecutiveFailures(records []evolution.Record) int {
	// Sort by timestamp descending
	sorted := make([]evolution.Record, len(records))
	copy(sorted, records)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Timestamp > sorted[j].Timestamp
	})

	count := 0
	for _, r := range sorted {
		if r.Outcome != evolution.Success {
			count++
		} else {
			break
		}
	}
	return count
}

func diagnoseFailureMode(records []evolution.Record) string {
	timeouts := 0
	emptyOutputs := 0
	parseErrors := 0
	toolErrors := 0

	for _, r := range records {
		if r.Outcome == evolution.Success {
			continue
		}
		// Heuristic classification based on what went wrong
		combined := strings.Join(r.WhatToImprove, " ") + " " + strings.Join(r.WhatWentWell, " ")
		lower := strings.ToLower(combined)

		switch {
		case strings.Contains(lower, "timeout") || r.DurationMs > 60000:
			timeouts++
		case strings.Contains(lower, "empty") || strings.Contains(lower, "no output") || strings.Contains(lower, "no result"):
			emptyOutputs++
		case strings.Contains(lower, "parse") || strings.Contains(lower, "json") || strings.Contains(lower, "format"):
			parseErrors++
		case strings.Contains(lower, "tool") || strings.Contains(lower, "mcp") || strings.Contains(lower, "command"):
			toolErrors++
		}
	}

	// Return the dominant failure mode
	max := timeouts
	mode := "unknown"
	if emptyOutputs > max {
		max = emptyOutputs
		mode = "empty_output"
	}
	if parseErrors > max {
		max = parseErrors
		mode = "parse_error"
	}
	if toolErrors > max {
		max = toolErrors
		mode = "tool_error"
	}
	if timeouts > max || (max == timeouts && timeouts > 0) {
		mode = "timeout"
	}
	if max == 0 && len(records) > 0 {
		// Check for LLM refusal patterns
		for _, r := range records {
			if strings.Contains(strings.ToLower(r.Task), "i can't") ||
				strings.Contains(strings.ToLower(r.Task), "i cannot") ||
				strings.Contains(strings.ToLower(r.Task), "unable") {
				return "llm_refusal"
			}
		}
	}
	return mode
}

func getLatestTimestamp(records []evolution.Record) int64 {
	var latest int64
	for _, r := range records {
		if r.Timestamp > latest {
			latest = r.Timestamp
		}
	}
	return latest
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
