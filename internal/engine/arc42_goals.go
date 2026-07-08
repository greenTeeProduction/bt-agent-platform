package engine

// arc42 goal anchoring: the platform's architecture documentation
// (docs/arc42/go-bt-evolve-arc42.md, §1.2 "Quality Goals") is the single
// authoritative statement of WHAT the platform is trying to be good at —
// Q1 Correctness, Q2 Evolvability, Q3 Reliability as of 2026-07. Research
// direction used to come from static hardcoded topic lists with no tie to
// those goals, so scheduled research could drift into paper-chasing while
// the documented goals went unserved. Every research entry point (the
// scheduled researcher's query rotation, the goap-fusion grill, the Claude
// review fallback, the program seeder, and bt_fusion pattern research) now
// frames its work with the parsed goals and asks proposals to name the goal
// they advance.
//
// Parsing is best-effort by design: when the doc is missing or the table
// format drifts, everything degrades to the pre-anchoring behavior rather
// than failing a cycle. TestLoadArc42QualityGoalsAgainstRealRepoDoc pins the
// production doc's parseability so silent degradation is caught in CI.

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

type arc42Goal struct {
	ID         string // "Q1"
	Name       string // "Correctness"
	Motivation string // the table's motivation cell, markdown stripped
}

// arc42GoalsDocPaths lists candidate locations of the arc42 document, tried
// in order. The daemon runs with WorkingDirectory=<repo>, so the relative
// path resolves there; the absolute fallback covers bt-agent-cli invocations
// from other directories. Package var so tests can substitute a fixture.
var arc42GoalsDocPaths = []string{
	"docs/arc42/go-bt-evolve-arc42.md",
	"/home/nico/go-bt-evolve/docs/arc42/go-bt-evolve-arc42.md",
}

var arc42GoalRowRe = regexp.MustCompile(`(?m)^\|\s*(Q\d+)\s*\|\s*\*\*([^*|]+)\*\*\s*\|\s*([^|]+?)\s*\|\s*$`)

// loadArc42QualityGoals parses the §1.2 Quality Goals table. It returns nil
// when the doc is absent or the section/table cannot be found.
func loadArc42QualityGoals() []arc42Goal {
	var body string
	for _, p := range arc42GoalsDocPaths {
		if b, err := os.ReadFile(p); err == nil {
			body = string(b)
			break
		}
	}
	if body == "" {
		return nil
	}
	// Restrict matching to the §1.2 section so goal-shaped rows in other
	// tables (stakeholders, solution approaches) are never misparsed.
	start := strings.Index(body, "## 1.2 Quality Goals")
	if start < 0 {
		return nil
	}
	section := body[start:]
	if end := strings.Index(section[1:], "\n## "); end >= 0 {
		section = section[:end+1]
	}
	var goals []arc42Goal
	for _, m := range arc42GoalRowRe.FindAllStringSubmatch(section, -1) {
		goals = append(goals, arc42Goal{
			ID:         strings.TrimSpace(m[1]),
			Name:       strings.TrimSpace(m[2]),
			Motivation: strings.TrimSpace(strings.ReplaceAll(m[3], "`", "")),
		})
	}
	return goals
}

// arc42GoalsPromptBlock renders the quality goals as a prompt fragment that
// instructs the model to anchor proposals on them. Empty when unavailable so
// callers can embed it unconditionally.
func arc42GoalsPromptBlock() string {
	goals := loadArc42QualityGoals()
	if len(goals) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Platform quality goals (arc42 §1.2) — every proposal must advance at least one of these, and must name the quality goal it advances:\n")
	for _, g := range goals {
		fmt.Fprintf(&b, "- %s %s: %s\n", g.ID, g.Name, g.Motivation)
	}
	return b.String()
}

// arc42ResearchTopics derives one research question per quality goal, used
// as the scheduled researcher's idle rotation and bt_fusion's daily pattern
// question. Nil when the goals are unavailable.
func arc42ResearchTopics() []string {
	goals := loadArc42QualityGoals()
	if len(goals) == 0 {
		return nil
	}
	topics := make([]string, 0, len(goals))
	for _, g := range goals {
		motivation := g.Motivation
		if i := strings.Index(motivation, ". "); i > 0 {
			motivation = motivation[:i+1]
		}
		topics = append(topics, fmt.Sprintf(
			"State of the art techniques improving %s in autonomous behavior-tree agent platforms (%s) — concrete methods, reference implementations, and validation approaches; advances arc42 quality goal %s",
			g.Name, motivation, g.ID))
	}
	return topics
}
