package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/nico/go-bt-evolve/internal/agent"
	"github.com/nico/go-bt-evolve/internal/research"
)

// cmdStatus prints a one-screen operational snapshot of the self-improvement
// loop: active program + milestone progress, recent cycle outcomes, the
// dead-letter backlog, and NotebookLM quota usage — the dashboard that
// previously had to be assembled by hand from programs.json, run.json,
// dead_letter_queue.json, and nlm-usage.json.
func cmdStatus() {
	home := agent.HomeDir()
	fmt.Println("bt-agent status —", time.Now().Format("2006-01-02 15:04"))
	fmt.Println()

	// Programs
	if ps, err := research.OpenPrograms(filepath.Join(home, "research", "programs.json")); err == nil {
		active := ps.Active()
		fmt.Printf("Programs: %d total, active: ", len(ps.Programs))
		if active == nil {
			fmt.Println("none (loop on single-cycle goals / self-seeding)")
		} else {
			idx, _ := active.NextMilestone()
			done := 0
			for _, m := range active.Milestones {
				if m.Status == "done" {
					done++
				}
			}
			fmt.Printf("%q\n  milestones %d/%d done, next %d/%d\n", active.Title, done, len(active.Milestones), idx+1, len(active.Milestones))
		}
	}
	fmt.Println()

	// Recent cycles
	hist, err := agent.NewHistory(agent.HistoryDir())
	if err == nil {
		for _, a := range []string{"goap-fusion-loop-runner", "goap-fusion-runner", "bt-fusion"} {
			recs := hist.List(a, 5)
			st := hist.Stats(a)
			fmt.Printf("%s: %d runs, %.0f%% ok — last 5: ", a, st.TotalRuns, st.SuccessRate*100)
			for _, r := range recs {
				mark := "✓"
				if r.Outcome != "success" {
					mark = "✗"
				}
				fmt.Print(mark)
			}
			fmt.Println()
		}
	}
	fmt.Println()

	// Dead-letter backlog
	if b, err := os.ReadFile(filepath.Join(home, "dead_letter_queue.json")); err == nil {
		var dlq []map[string]any
		if json.Unmarshal(b, &dlq) == nil {
			byAgent := map[string]int{}
			for _, e := range dlq {
				byAgent[fmt.Sprint(e["agent"])]++
			}
			fmt.Printf("Dead-letter queue: %d entries\n", len(dlq))
			type kv struct {
				a string
				n int
			}
			rows := make([]kv, 0, len(byAgent))
			for a, n := range byAgent {
				rows = append(rows, kv{a, n})
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].n > rows[j].n })
			for _, r := range rows {
				fmt.Printf("  %3d %s\n", r.n, r.a)
			}
		}
	}
	fmt.Println()

	// NotebookLM quota economy
	if b, err := os.ReadFile(filepath.Join(home, "research", "nlm-usage.json")); err == nil {
		var u struct {
			Day      string `json:"day"`
			Queries  int    `json:"queries"`
			Research int    `json:"research"`
		}
		if json.Unmarshal(b, &u) == nil {
			fmt.Printf("NotebookLM usage (%s): %d queries, %d web-research starts\n", u.Day, u.Queries, u.Research)
		}
	}
}
