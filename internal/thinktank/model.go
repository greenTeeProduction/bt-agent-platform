// Package thinktank implements a collaborative analytical think tank
// where five fellows (bull, bear, technical, macro, contrarian) research
// independently, debate dialectically (Hegelian thesis/antithesis/synthesis),
// and peer-review findings before producing a final executive report.
package thinktank

import "time"

// ThinkTank orchestrates multi-perspective research and structured debate.
// Fellows research independently, then engage in dialectic debate,
// and results are synthesized into a peer-reviewed recommendation.
type ThinkTank struct {
	Name         string   `json:"name"`
	Topic        string   `json:"topic"`
	Fellows      []Fellow `json:"fellows"`
	DelphiRounds int      `json:"delphi_rounds"`

	// Rounds of the research process
	ResearchFindings []ResearchFinding `json:"findings"`
	DebateTranscript []DebateTurn      `json:"debate"`
	Synthesis        *Synthesis        `json:"synthesis"`
	PeerReview       []ReviewComment   `json:"peer_review"`
	FinalReport      *Report           `json:"final_report"`
}

// Fellow represents a distinct analytical perspective in the think tank.
type Fellow struct {
	Name        string  `json:"name"`
	Role        string  `json:"role"`        // bull, bear, technical, macro, contrarian, synthesizer
	Perspective string  `json:"perspective"` // the lens they apply
	Expertise   string  `json:"expertise"`   // domain knowledge
	Persona     string  `json:"persona"`     // how they think and argue
	Confidence  float64 `json:"confidence"`  // 0-1, how strongly they hold their views
}

// ResearchFinding captures one fellow's independent analysis.
type ResearchFinding struct {
	FellowName      string    `json:"fellow"`
	Role            string    `json:"role"`
	KeyInsights     []string  `json:"key_insights"`
	Evidence        []string  `json:"evidence"`
	Assumptions     []string  `json:"assumptions"`
	ConfidenceScore float64   `json:"confidence"`
	Recommendation  string    `json:"recommendation"`
	Risks           []string  `json:"risks"`
	Timestamp       time.Time `json:"timestamp"`
}

// DebateTurn records one exchange in structured debate.
type DebateTurn struct {
	Round      int      `json:"round"`
	Speaker    string   `json:"speaker"`
	Role       string   `json:"role"`
	Type       string   `json:"type"` // thesis, antithesis, rebuttal, clarification, synthesis_move
	Statement  string   `json:"statement"`
	References []string `json:"references"`
}

// Synthesis combines multiple perspectives into a unified view.
type Synthesis struct {
	Thesis               string   `json:"thesis"`     // dominant view
	Antithesis           string   `json:"antithesis"` // strongest counter
	Synthesis            string   `json:"synthesis"`  // resolved position
	PointsOfAgreement    []string `json:"agreement"`
	PointsOfDisagreement []string `json:"disagreement"`
	ConfidenceInterval   string   `json:"confidence_interval"` // e.g., "70-90%"
	Recommendation       string   `json:"recommendation"`
	DissentingNotes      []string `json:"dissenting_notes"`
}

// ReviewComment is a peer review annotation on the synthesis.
type ReviewComment struct {
	Reviewer   string `json:"reviewer"`
	Section    string `json:"section"`
	Issue      string `json:"issue"`    // factual_error, logical_fallacy, missing_evidence, bias
	Severity   string `json:"severity"` // critical, major, minor
	Comment    string `json:"comment"`
	Suggestion string `json:"suggestion"`
	Resolved   bool   `json:"resolved"`
}

// Report is the final think tank output.
type Report struct {
	Title            string     `json:"title"`
	ExecutiveSummary string     `json:"executive_summary"`
	Background       string     `json:"background"`
	Analysis         string     `json:"analysis"`
	Scenarios        []Scenario `json:"scenarios"`
	Recommendation   string     `json:"recommendation"`
	ConfidenceLevel  string     `json:"confidence_level"`
	RisksAndCaveats  []string   `json:"risks"`
	NextSteps        []string   `json:"next_steps"`
	Contributors     []string   `json:"contributors"`
	Timestamp        time.Time  `json:"timestamp"`
}

// Scenario explores an alternative future state.
type Scenario struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Probability float64  `json:"probability"`
	Impact      string   `json:"impact"` // high, medium, low
	Triggers    []string `json:"triggers"`
	Response    string   `json:"response"` // recommended action if scenario materializes
}

// DefaultFellows returns a standard set of analytical perspectives.
func DefaultFellows() []Fellow {
	return []Fellow{
		{
			Name: "Victoria Bull", Role: "bull",
			Perspective: "Optimistic growth thesis — identifies upside catalysts and positive feedback loops",
			Expertise:   "Technology adoption curves, disruptive innovation, venture capital",
			Persona:     "You are an optimistic technology analyst. You see the transformative potential in new technologies and focus on upside scenarios. You cite adoption curves, network effects, and historical precedents of disruptive innovation. You acknowledge risks but believe they are overstated.",
			Confidence:  0.8,
		},
		{
			Name: "Marcus Bear", Role: "bear",
			Perspective: "Skeptical risk assessment — identifies structural weaknesses and downside scenarios",
			Expertise:   "Financial analysis, risk management, competitive dynamics, regulatory frameworks",
			Persona:     "You are a skeptical financial analyst. You focus on what can go wrong: competitive threats, regulatory risks, execution challenges, valuation concerns. You demand evidence and stress-test optimistic assumptions. You believe most forecasts are too rosy.",
			Confidence:  0.85,
		},
		{
			Name: "Dr. Elena Tech", Role: "technical",
			Perspective: "Deep technical evaluation — assesses feasibility, architecture, and engineering challenges",
			Expertise:   "Software architecture, AI/ML systems, distributed systems, security",
			Persona:     "You are a distinguished engineer and technical fellow. You evaluate ideas through the lens of technical feasibility, architectural soundness, scalability, and engineering complexity. You identify hidden technical debt and architecture risks that business analysts miss.",
			Confidence:  0.75,
		},
		{
			Name: "Prof. James Macro", Role: "macro",
			Perspective: "Systems-level macro analysis — evaluates broader ecosystem, geopolitical, and economic forces",
			Expertise:   "Macroeconomics, geopolitics, regulatory policy, industry ecosystems, second-order effects",
			Persona:     "You are a systems thinker and macro strategist. You analyze topics through the lens of interconnected systems: economic cycles, geopolitical shifts, regulatory evolution, industry ecosystem dynamics. You identify second and third-order effects that others miss.",
			Confidence:  0.7,
		},
		{
			Name: "Sophia Contrarian", Role: "contrarian",
			Perspective: "Challenges consensus — identifies blind spots, groupthink, and unchallenged assumptions",
			Expertise:   "Cognitive biases, game theory, behavioral economics, historical analogies",
			Persona:     "You are a professional contrarian and devil's advocate. Your job is to challenge every assumption, identify groupthink, and expose blind spots. You use historical analogies, cognitive bias awareness, and game theory to show why the consensus might be wrong. You're not negative — you're rigorous.",
			Confidence:  0.9,
		},
	}
}

// NewThinkTank creates a think tank with default fellows for a given topic.
func NewThinkTank(name, topic string) *ThinkTank {
	return &ThinkTank{
		Name:         name,
		Topic:        topic,
		Fellows:      DefaultFellows(),
		DelphiRounds: 3,
	}
}
