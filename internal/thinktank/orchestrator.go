package thinktank

import (
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/engine"
	"github.com/nico/go-bt-evolve/internal/llm"
)

// ThinkTankOrchestrator manages the full think tank analysis pipeline:
// research → debate → synthesis → peer review → report generation.
type ThinkTankOrchestrator struct {
	Tank *ThinkTank
	LLM  llm.LLM

	// chainState carries intermediate text results between phases so that
	// later phases can reference the output of earlier ones via bb.ChainState.
	chainState map[string]any
}

// NewOrchestrator creates a new orchestrator for the given think tank.
func NewOrchestrator(tank *ThinkTank, llm llm.LLM) *ThinkTankOrchestrator {
	return &ThinkTankOrchestrator{
		Tank:       tank,
		LLM:        llm,
		chainState: make(map[string]any),
	}
}

// newBlackboard creates a fresh blackboard for a tree execution phase.
func (o *ThinkTankOrchestrator) newBlackboard(task string) *engine.Blackboard {
	cs := make(map[string]any)
	cs["thinktank"] = o.Tank

	// Copy chain state so prior phase results are available to later phases
	for k, v := range o.chainState {
		cs[k] = v
	}

	return &engine.Blackboard{
		Task:       task,
		LLM:        o.LLM,
		ChainState: cs,
		ChainTools: []any{},
	}
}

// RunResearchRound runs the FellowResearchTree for each fellow independently,
// collecting their ResearchFindings into the tank.
func (o *ThinkTankOrchestrator) RunResearchRound() error {
	if o.Tank == nil {
		return fmt.Errorf("think tank is nil")
	}
	if o.LLM == nil {
		return fmt.Errorf("LLM is nil")
	}

	o.Tank.ResearchFindings = make([]ResearchFinding, 0, len(o.Tank.Fellows))

	for _, fellow := range o.Tank.Fellows {
		task := fmt.Sprintf("Research the topic '%s' from the perspective of %s (%s analyst with expertise in %s)",
			o.Tank.Topic, fellow.Name, fellow.Role, fellow.Expertise)

		bb := o.newBlackboard(task)
		bb.ChainState["current_fellow"] = fellow

		tree := FellowResearchTree(fellow, o.Tank.Topic)
		cmd := engine.BuildTree(tree, bb)
		result := engine.RunTask(bb, cmd)

		// Parse the result into a ResearchFinding
		finding := parseResearchFinding(result, fellow)
		finding.Timestamp = time.Now()
		o.Tank.ResearchFindings = append(o.Tank.ResearchFindings, finding)

		// Store the raw text for later phases to reference
		o.chainState[fmt.Sprintf("finding_%s", fellow.Name)] = result
	}

	return nil
}

// RunDebate runs the DebateTree with all fellows, collecting the debate
// transcript.
func (o *ThinkTankOrchestrator) RunDebate() error {
	if o.Tank == nil {
		return fmt.Errorf("think tank is nil")
	}
	if o.LLM == nil {
		return fmt.Errorf("LLM is nil")
	}
	if len(o.Tank.Fellows) == 0 {
		return fmt.Errorf("no fellows available for debate")
	}

	task := fmt.Sprintf("Conduct a structured dialectic debate on '%s' with %d analytical fellows",
		o.Tank.Topic, len(o.Tank.Fellows))

	bb := o.newBlackboard(task)
	bb.ChainState["research_findings"] = o.Tank.ResearchFindings

	tree := DebateTree(o.Tank.Fellows, o.Tank.Topic)
	cmd := engine.BuildTree(tree, bb)
	result := engine.RunTask(bb, cmd)

	// Parse debate transcript from the result
	o.Tank.DebateTranscript = parseDebateTranscript(result, o.Tank.Fellows)
	o.chainState["debate_result"] = result

	return nil
}

// RunSynthesis runs the SynthesisTree, combining research findings and debate
// into a unified Synthesis.
func (o *ThinkTankOrchestrator) RunSynthesis() error {
	if o.Tank == nil {
		return fmt.Errorf("think tank is nil")
	}
	if o.LLM == nil {
		return fmt.Errorf("LLM is nil")
	}

	task := fmt.Sprintf("Synthesize all research findings and debate transcripts on '%s'",
		o.Tank.Topic)

	bb := o.newBlackboard(task)
	bb.ChainState["research_findings"] = o.Tank.ResearchFindings
	bb.ChainState["debate_transcript"] = o.Tank.DebateTranscript

	tree := SynthesisTree()
	cmd := engine.BuildTree(tree, bb)
	result := engine.RunTask(bb, cmd)

	// Parse the synthesis from the result
	o.Tank.Synthesis = parseSynthesis(result)
	o.chainState["synthesis_text"] = result

	return nil
}

// RunPeerReview runs the PeerReviewTree, having each analytical perspective
// review the synthesis and produce ReviewComments.
func (o *ThinkTankOrchestrator) RunPeerReview() error {
	if o.Tank == nil {
		return fmt.Errorf("think tank is nil")
	}
	if o.LLM == nil {
		return fmt.Errorf("LLM is nil")
	}
	if o.Tank.Synthesis == nil {
		return fmt.Errorf("synthesis must be completed before peer review")
	}

	task := "Peer review the synthesis from multiple analytical perspectives"

	bb := o.newBlackboard(task)
	bb.ChainState["synthesis"] = o.Tank.Synthesis

	tree := PeerReviewTree()
	cmd := engine.BuildTree(tree, bb)
	result := engine.RunTask(bb, cmd)

	// Parse review comments
	o.Tank.PeerReview = parseReviewComments(result, o.Tank.Fellows)
	o.chainState["peer_review_text"] = result

	return nil
}

// RunReportGeneration runs the ReportGenerationTree, producing the final
// Report from the synthesis and peer review.
func (o *ThinkTankOrchestrator) RunReportGeneration() error {
	if o.Tank == nil {
		return fmt.Errorf("think tank is nil")
	}
	if o.LLM == nil {
		return fmt.Errorf("LLM is nil")
	}
	if o.Tank.Synthesis == nil {
		return fmt.Errorf("synthesis must be completed before report generation")
	}

	task := fmt.Sprintf("Generate the final report for the think tank analysis on '%s'",
		o.Tank.Topic)

	bb := o.newBlackboard(task)
	bb.ChainState["research_findings"] = o.Tank.ResearchFindings
	bb.ChainState["debate_transcript"] = o.Tank.DebateTranscript
	bb.ChainState["synthesis"] = o.Tank.Synthesis
	bb.ChainState["peer_review"] = o.Tank.PeerReview

	tree := ReportGenerationTree()
	cmd := engine.BuildTree(tree, bb)
	result := engine.RunTask(bb, cmd)

	// Parse the final report
	o.Tank.FinalReport = parseReport(result, o.Tank)

	return nil
}

// RunFullAnalysis runs all phases in sequence: research → debate → synthesis →
// peer review → report generation.
func (o *ThinkTankOrchestrator) RunFullAnalysis(topic string) error {
	if topic != "" {
		o.Tank.Topic = topic
	}

	phases := []struct {
		name string
		fn   func() error
	}{
		{"Research Round", o.RunResearchRound},
		{"Debate", o.RunDebate},
		{"Synthesis", o.RunSynthesis},
		{"Peer Review", o.RunPeerReview},
		{"Report Generation", o.RunReportGeneration},
	}

	for _, phase := range phases {
		if err := phase.fn(); err != nil {
			return fmt.Errorf("%s failed: %w", phase.name, err)
		}
	}

	return nil
}

// --- Result Parsers ---
// These parse the raw LLM output (text) into the thinktank model structs.
// In a production system these would use structured JSON output, but for
// the behavior tree simulation we parse the text heuristically.

func parseResearchFinding(raw string, fellow Fellow) ResearchFinding {
	finding := ResearchFinding{
		FellowName:      fellow.Name,
		Role:            fellow.Role,
		ConfidenceScore: fellow.Confidence,
	}

	finding.KeyInsights = extractListSection(raw, "KEY INSIGHTS")
	if len(finding.KeyInsights) == 0 {
		finding.KeyInsights = extractBulletPoints(raw)
	}

	finding.Evidence = extractListSection(raw, "EVIDENCE")
	if len(finding.Evidence) == 0 {
		finding.Evidence = extractListSection(raw, "SUPPORTING EVIDENCE")
	}

	finding.Assumptions = extractListSection(raw, "ASSUMPTIONS")

	finding.Recommendation = extractSection(raw, "RECOMMENDATION")
	if finding.Recommendation == "" {
		finding.Recommendation = extractSection(raw, "recommendation")
	}

	finding.Risks = extractListSection(raw, "RISKS")

	return finding
}

func parseDebateTranscript(raw string, fellows []Fellow) []DebateTurn {
	turns := make([]DebateTurn, 0)

	// Try to parse structured debate sections
	sections := []string{
		"OPENING STATEMENT",
		"OPENING",
		"CROSS-EXAMINATION",
		"CROSS EXAMINATION",
		"REBUTTAL",
		"SYNTHESIS MOVE",
		"VOTE",
	}

	for i, section := range sections {
		content := extractSection(raw, section)
		if content == "" {
			continue
		}
		fellow := fellows[i%len(fellows)]
		turnType := "statement"
		switch {
		case strings.Contains(section, "OPENING"):
			turnType = "thesis"
		case strings.Contains(section, "CROSS"):
			turnType = "clarification"
		case strings.Contains(section, "REBUTTAL"):
			turnType = "rebuttal"
		case strings.Contains(section, "SYNTHESIS"):
			turnType = "synthesis_move"
		case strings.Contains(section, "VOTE"):
			turnType = "synthesis_move"
		}
		turns = append(turns, DebateTurn{
			Round:     i + 1,
			Speaker:   fellow.Name,
			Role:      fellow.Role,
			Type:      turnType,
			Statement: truncateStr(content, 500),
		})
	}

	// Fallback: if no structured sections found, create a single turn per fellow
	if len(turns) == 0 {
		for i, f := range fellows {
			turns = append(turns, DebateTurn{
				Round:     i + 1,
				Speaker:   f.Name,
				Role:      f.Role,
				Type:      "thesis",
				Statement: truncateStr(raw, 300),
			})
		}
	}

	return turns
}

func parseSynthesis(raw string) *Synthesis {
	s := &Synthesis{}

	s.Thesis = extractSection(raw, "DOMINANT THESIS")
	if s.Thesis == "" {
		s.Thesis = extractSection(raw, "THESIS")
	}

	s.Antithesis = extractSection(raw, "ANTITHESIS")

	s.Synthesis = extractSection(raw, "SYNTHESIS")
	if s.Synthesis == "" {
		s.Synthesis = extractSection(raw, "SYNTHESIS POSITION")
	}

	s.PointsOfAgreement = extractListSection(raw, "POINTS OF AGREEMENT")
	if len(s.PointsOfAgreement) == 0 {
		s.PointsOfAgreement = extractListSection(raw, "AGREEMENT")
	}

	s.PointsOfDisagreement = extractListSection(raw, "POINTS OF DISAGREEMENT")
	if len(s.PointsOfDisagreement) == 0 {
		s.PointsOfDisagreement = extractListSection(raw, "DISAGREEMENT")
	}

	s.Recommendation = extractSection(raw, "RECOMMENDATION")
	s.ConfidenceInterval = extractSection(raw, "CONFIDENCE")
	s.DissentingNotes = extractListSection(raw, "DISSENTING NOTES")
	if len(s.DissentingNotes) == 0 {
		s.DissentingNotes = extractListSection(raw, "DISSENTING")
	}

	return s
}

func parseReviewComments(raw string, fellows []Fellow) []ReviewComment {
	comments := make([]ReviewComment, 0)

	sections := []struct {
		label    string
		issue    string
		severity string
	}{
		{"FACT CHECK", "factual_error", "critical"},
		{"LOGIC CHECK", "logical_fallacy", "major"},
		{"BIAS AUDIT", "bias", "major"},
		{"EVIDENCE GAP", "missing_evidence", "critical"},
	}

	for i, sec := range sections {
		content := extractSection(raw, sec.label)
		if content == "" {
			continue
		}
		reviewer := "Review Committee"
		if i < len(fellows) {
			reviewer = fellows[i].Name
		}
		issues := extractBulletPoints(content)
		for _, issue := range issues {
			comments = append(comments, ReviewComment{
				Reviewer:   reviewer,
				Section:    strings.ToLower(sec.label),
				Issue:      sec.issue,
				Severity:   sec.severity,
				Comment:    issue,
				Suggestion: "",
				Resolved:   false,
			})
		}
	}

	// Fallback: extract all bullet points as review comments
	if len(comments) == 0 {
		bullets := extractBulletPoints(raw)
		for i, b := range bullets {
			reviewer := "Review Committee"
			if i < len(fellows) {
				reviewer = fellows[i%len(fellows)].Name
			}
			comments = append(comments, ReviewComment{
				Reviewer:   reviewer,
				Section:    "synthesis",
				Issue:      "general",
				Severity:   "major",
				Comment:    b,
				Suggestion: "",
				Resolved:   false,
			})
		}
	}

	return comments
}

func parseReport(raw string, tank *ThinkTank) *Report {
	r := &Report{
		Title:        fmt.Sprintf("Think Tank Analysis: %s", tank.Topic),
		Timestamp:    time.Now(),
		Contributors: make([]string, len(tank.Fellows)),
	}
	for i, f := range tank.Fellows {
		r.Contributors[i] = f.Name
	}

	r.ExecutiveSummary = extractSection(raw, "EXECUTIVE SUMMARY")
	r.Background = extractSection(raw, "BACKGROUND")
	r.Analysis = extractSection(raw, "SCENARIO")
	if r.Analysis == "" {
		r.Analysis = extractSection(raw, "ANALYSIS")
	}
	r.Scenarios = parseScenarios(raw)
	r.Recommendation = extractSection(raw, "RECOMMENDATION")
	r.ConfidenceLevel = extractSection(raw, "CONFIDENCE")
	r.RisksAndCaveats = extractListSection(raw, "RISKS")
	if len(r.RisksAndCaveats) == 0 {
		r.RisksAndCaveats = extractListSection(raw, "RISKS AND CAVEATS")
	}
	r.NextSteps = extractListSection(raw, "NEXT STEPS")

	return r
}

func parseScenarios(raw string) []Scenario {
	scenarios := make([]Scenario, 0)
	section := extractSection(raw, "SCENARIO")
	if section == "" {
		section = extractSection(raw, "SCENARIOS")
	}
	if section == "" {
		section = raw
	}

	parts := splitByNumberedSections(section)
	for _, part := range parts {
		name := extractFirstLine(part)
		desc := part
		prob := 0.0
		impact := "medium"

		if p := extractSection(part, "PROBABILITY"); p != "" {
			prob = parseProbability(p)
		}
		if i := extractSection(part, "IMPACT"); i != "" {
			impact = strings.ToLower(i)
		}
		if name == "" && desc == "" {
			continue
		}

		scenarios = append(scenarios, Scenario{
			Name:        truncateStr(name, 80),
			Description: truncateStr(desc, 300),
			Probability: prob,
			Impact:      impact,
			Triggers:    extractListSection(part, "TRIGGERS"),
			Response:    extractSection(part, "RESPONSE"),
		})
	}

	// Fallback: at least one scenario
	if len(scenarios) == 0 && len(raw) > 0 {
		scenarios = append(scenarios, Scenario{
			Name:        "Base Case",
			Description: truncateStr(raw, 300),
			Probability: 0.5,
			Impact:      "medium",
		})
	}

	return scenarios
}

// --- Text parsing helpers ---

// extractSection finds a section label in text and returns the content
// following it. Case-insensitive.
func extractSection(raw, label string) string {
	rl := strings.ToLower(raw)
	ll := strings.ToLower(label)

	idx := strings.Index(rl, ll+":")
	if idx < 0 {
		idx = strings.Index(rl, ll)
	}
	if idx < 0 {
		return ""
	}

	start := idx + len(label)
	for start < len(raw) && (raw[start] == ':' || raw[start] == ' ' || raw[start] == '\n' || raw[start] == '\t') {
		start++
	}

	end := len(raw)
	if ns := findNextSection(raw[start:]); ns > 0 {
		end = start + ns
	}
	return strings.TrimSpace(raw[start:end])
}

// findNextSection finds the byte offset of the next section header in text,
// or 0 if none found.
func findNextSection(text string) int {
	common := []string{
		"KEY INSIGHTS:", "EVIDENCE:", "ASSUMPTIONS:", "RECOMMENDATION:",
		"RISKS:", "THESIS:", "ANTITHESIS:", "SYNTHESIS:", "AGREEMENT:",
		"DISAGREEMENT:", "DISSENTING:", "CONFIDENCE:", "EXECUTIVE SUMMARY:",
		"BACKGROUND:", "SCENARIO:", "NEXT STEPS:", "IMPACT:", "PROBABILITY:",
		"TRIGGERS:", "RESPONSE:", "RATIONALE:", "ALTERNATIVES:",
	}

	lower := strings.ToLower(text)
	best := len(text)

	for _, label := range common {
		ll := strings.ToLower(label)
		for i := 0; i < len(lower); i++ {
			if strings.HasPrefix(lower[i:], ll) {
				if i == 0 || lower[i-1] == '\n' || lower[i-1] == '.' || lower[i-1] == '!' {
					if i < best {
						best = i
					}
					break
				}
			}
		}
	}

	if best < len(text) {
		return best
	}
	return 0
}

// extractListSection extracts a list from a labeled section.
func extractListSection(raw, label string) []string {
	section := extractSection(raw, label)
	if section == "" {
		return nil
	}
	return extractBulletPoints(section)
}

// extractBulletPoints extracts items from bullet-point or numbered-list text.
func extractBulletPoints(text string) []string {
	var items []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		trimmed = strings.TrimPrefix(trimmed, "- ")
		trimmed = strings.TrimPrefix(trimmed, "* ")
		trimmed = strings.TrimPrefix(trimmed, "+ ")
		trimmed = strings.TrimPrefix(trimmed, "• ")
		trimmed = strings.TrimPrefix(trimmed, "> ")
		// Strip numbering like "1. ", "1) "
		for i := 0; i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9'; i++ {
			if i+1 < len(trimmed) {
				switch trimmed[i+1] {
				case '.', ')', ' ':
					trimmed = strings.TrimSpace(trimmed[i+2:])
					break
				}
			}
		}
		if trimmed != "" && len(trimmed) > 10 {
			items = append(items, trimmed)
		}
	}
	return items
}

// extractFirstLine returns the first non-empty line.
func extractFirstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// splitByNumberedSections splits text by markers like "Scenario 1:", "1.", etc.
func splitByNumberedSections(text string) []string {
	var parts []string
	current := ""
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		isNew := false
		if strings.HasPrefix(lower, "scenario") && len(trimmed) > 10 {
			isNew = true
		} else if len(trimmed) > 2 && trimmed[0] >= '1' && trimmed[0] <= '9' {
			if trimmed[1] == '.' || trimmed[1] == ')' {
				isNew = true
			}
		}
		if isNew && current != "" {
			parts = append(parts, strings.TrimSpace(current))
			current = trimmed + "\n"
		} else {
			current += trimmed + "\n"
		}
	}
	if current != "" {
		parts = append(parts, strings.TrimSpace(current))
	}
	return parts
}

// parseProbability extracts a numeric probability from text like "70%" or "0.7".
func parseProbability(text string) float64 {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, "%")
	var prob float64
	if _, err := fmt.Sscanf(text, "%f", &prob); err != nil {
		return 0.5
	}
	if prob > 1.0 {
		prob /= 100.0
	}
	if prob < 0 {
		prob = 0
	}
	if prob > 1.0 {
		prob = 1.0
	}
	return prob
}

// truncateStr limits a string to n characters, appending "..." if truncated.
func truncateStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
