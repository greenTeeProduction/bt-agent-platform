package evolution

import (
	"math"
	"sort"
)

// MetaValidationDecision is the final acceptance decision produced by the
// meta-validator. The order is intentionally conservative: any hard structural
// or regression failure rejects the candidate even if other checks score well.
type MetaValidationDecision string

const (
	MetaAccept MetaValidationDecision = "accept"
	MetaWarn   MetaValidationDecision = "warn"
	MetaReject MetaValidationDecision = "reject"
)

// MetaValidationIssue captures one finding from a validation check.
type MetaValidationIssue struct {
	Check    string  `json:"check"`
	Severity string  `json:"severity"` // critical, major, minor, warning
	Message  string  `json:"message"`
	Weight   float64 `json:"weight"`
}

// MetaValidationReport is the explainable output of a meta-validation pass.
type MetaValidationReport struct {
	Decision        MetaValidationDecision `json:"decision"`
	Score           float64                `json:"score"` // 0-1, higher is safer
	Checks          []string               `json:"checks"`
	Issues          []MetaValidationIssue  `json:"issues,omitempty"`
	Warnings        []MetaValidationIssue  `json:"warnings,omitempty"`
	Recommendations []string               `json:"recommendations,omitempty"`
	NodeCount       int                    `json:"node_count"`
	Depth           int                    `json:"depth"`
	Composite       float64                `json:"composite,omitempty"`
	Regression      float64                `json:"regression,omitempty"`
}

// MetaValidatorConfig tunes the validation thresholds. Defaults are calibrated
// for evolved BTs: strict enough to catch broken trees but permissive enough to
// allow exploratory mutations that only produce minor warnings.
type MetaValidatorConfig struct {
	MinScore             float64
	WarnScore            float64
	MinComposite         float64
	MaxRegression        float64
	MaxNodes             int
	MaxDepth             int
	RequiredRootTypes    []string
	ArchetypeCategory    string
	RequireOutcomeSetter bool
}

// MetaValidator combines structural validation, expert-system checks,
// regression gates, and optional evaluator consensus into one P0 safety layer.
type MetaValidator struct {
	config  MetaValidatorConfig
	experts *ExpertKnowledge
}

// NewMetaValidator creates a validator with production-safe defaults.
func NewMetaValidator(config MetaValidatorConfig) *MetaValidator {
	if config.MinScore == 0 {
		config.MinScore = 0.70
	}
	if config.WarnScore == 0 {
		config.WarnScore = 0.85
	}
	if config.MinComposite == 0 {
		config.MinComposite = 0.30
	}
	if config.MaxRegression == 0 {
		config.MaxRegression = 0.20
	}
	if config.MaxNodes == 0 {
		config.MaxNodes = 120
	}
	if config.MaxDepth == 0 {
		config.MaxDepth = 8
	}
	if len(config.RequiredRootTypes) == 0 {
		config.RequiredRootTypes = []string{"Sequence", "Selector"}
	}
	if config.ArchetypeCategory == "" {
		config.ArchetypeCategory = "domain"
	}
	return &MetaValidator{config: config, experts: NewExpertKnowledge()}
}

// Validate checks a single candidate tree without a baseline regression score.
func (m *MetaValidator) Validate(candidate *SerializableNode, composite float64) MetaValidationReport {
	return m.ValidateMutation(nil, candidate, 0, composite)
}

// ValidateMutation checks a candidate against an optional baseline and returns an
// explainable decision. preComposite can be zero when no prior score exists.
func (m *MetaValidator) ValidateMutation(baseline, candidate *SerializableNode, preComposite, postComposite float64) MetaValidationReport {
	report := MetaValidationReport{
		Decision:  MetaAccept,
		Score:     1.0,
		Composite: postComposite,
		Checks: []string{
			"nil_tree", "root_type", "node_names", "structural_limits",
			"selector_shape", "retry_bounds", "expert_antipatterns",
			"archetype", "fitness_floor", "regression_gate",
		},
	}

	if candidate == nil {
		report.addIssue("nil_tree", "critical", "candidate tree is nil", 1.0)
		report.finalize(m.config.MinScore, m.config.WarnScore)
		return report
	}

	report.NodeCount = CountNodes(candidate)
	report.Depth = maxTreeDepth(candidate, 0)

	m.checkRoot(candidate, &report)
	m.checkNodeNames(candidate, &report)
	m.checkStructuralLimits(candidate, &report)
	m.checkSelectorShape(candidate, &report)
	m.checkRetryBounds(candidate, &report)
	m.checkExpertKnowledge(candidate, &report)
	m.checkArchetype(candidate, &report)
	m.checkFitness(preComposite, postComposite, &report)
	if baseline != nil {
		m.checkStructuralRegression(baseline, candidate, &report)
	}
	m.addRecommendations(candidate, &report)

	report.finalize(m.config.MinScore, m.config.WarnScore)
	return report
}

func (m *MetaValidator) checkRoot(candidate *SerializableNode, report *MetaValidationReport) {
	for _, typ := range m.config.RequiredRootTypes {
		if candidate.Type == typ {
			return
		}
	}
	report.addIssue("root_type", "critical", "root node must be Sequence or Selector", 0.35)
}

func (m *MetaValidator) checkNodeNames(candidate *SerializableNode, report *MetaValidationReport) {
	var walk func(*SerializableNode, string)
	walk = func(n *SerializableNode, path string) {
		if n == nil {
			return
		}
		if n.Type == "" {
			report.addIssue("node_names", "critical", "node at "+path+" has empty type", 0.35)
		}
		if n.Name == "" {
			report.addIssue("node_names", "major", "node at "+path+" has empty name", 0.15)
		}
		for i := range n.Children {
			walk(&n.Children[i], path+"/"+n.Children[i].Name)
		}
	}
	walk(candidate, candidate.Name)
}

func (m *MetaValidator) checkStructuralLimits(candidate *SerializableNode, report *MetaValidationReport) {
	if report.NodeCount == 0 {
		report.addIssue("structural_limits", "critical", "tree has zero nodes", 0.50)
	}
	if report.NodeCount > m.config.MaxNodes {
		report.addIssue("structural_limits", "major", "tree exceeds maximum node budget", 0.20)
	}
	if report.Depth > m.config.MaxDepth {
		report.addIssue("structural_limits", "major", "tree exceeds maximum depth budget", 0.20)
	}
}

func (m *MetaValidator) checkSelectorShape(candidate *SerializableNode, report *MetaValidationReport) {
	var walk func(*SerializableNode)
	walk = func(n *SerializableNode) {
		if n == nil {
			return
		}
		if (n.Type == "Sequence" || n.Type == "Selector") && len(n.Children) == 0 {
			report.addIssue("selector_shape", "major", n.Type+" node "+n.Name+" has no children", 0.20)
		}
		if n.Type == "Selector" && len(n.Children) == 1 {
			report.addWarning("selector_shape", "warning", "Selector "+n.Name+" has only one child", 0.05)
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(candidate)
}

func (m *MetaValidator) checkRetryBounds(candidate *SerializableNode, report *MetaValidationReport) {
	var walk func(*SerializableNode)
	walk = func(n *SerializableNode) {
		if n == nil {
			return
		}
		if n.Type == "Retry" && n.MaxRetries > 10 {
			report.addIssue("retry_bounds", "critical", "Retry "+n.Name+" exceeds bounded retry limit", 0.30)
		}
		for i := range n.Children {
			walk(&n.Children[i])
		}
	}
	walk(candidate)
}

func (m *MetaValidator) checkExpertKnowledge(candidate *SerializableNode, report *MetaValidationReport) {
	for _, anti := range m.experts.DetectAntiPatterns(candidate) {
		switch anti.Name {
		case "Dead Strategy Path":
			if hasExecutionPathFirst(candidate) {
				report.addIssue("expert_antipatterns", anti.Severity, anti.Name+": "+anti.Fix, 0.30)
			}
		case "Template-Only Execution":
			if hasNodeType(candidate, "ChainAction") && !hasAnySetupTools(candidate) {
				report.addIssue("expert_antipatterns", anti.Severity, anti.Name+": "+anti.Fix, 0.20)
			}
		case "Unbounded Retry Loop":
			if hasUnboundedRetry(candidate) {
				report.addIssue("expert_antipatterns", anti.Severity, anti.Name+": "+anti.Fix, 0.30)
			}
		case "Keyword Collision", "Missing Outcome Setter":
			// The expert KB currently exposes these as broad heuristic matches. Keep them
			// visible as recommendations rather than hard rejections until runtime route
			// evidence is available.
			report.addWarning("expert_antipatterns", "warning", anti.Name+": "+anti.Fix, 0.02)
		default:
			report.addWarning("expert_antipatterns", "warning", anti.Name+": "+anti.Fix, 0.03)
		}
	}
}

func (m *MetaValidator) checkArchetype(candidate *SerializableNode, report *MetaValidationReport) {
	fits, issues := m.experts.ValidateArchetype(candidate, m.config.ArchetypeCategory)
	if fits {
		return
	}
	sort.Strings(issues)
	for _, issue := range issues {
		report.addWarning("archetype", "warning", issue, 0.04)
	}
}

func (m *MetaValidator) checkFitness(preComposite, postComposite float64, report *MetaValidationReport) {
	if postComposite < m.config.MinComposite {
		report.addIssue("fitness_floor", "critical", "candidate composite is below minimum floor", 0.35)
	}
	if preComposite > 0 && postComposite < preComposite*(1-m.config.MaxRegression) {
		report.Regression = (preComposite - postComposite) / preComposite
		report.addIssue("regression_gate", "critical", "candidate exceeds maximum allowed fitness regression", 0.45)
	}
}

func (m *MetaValidator) checkStructuralRegression(baseline, candidate *SerializableNode, report *MetaValidationReport) {
	baseNodes := CountNodes(baseline)
	baseDepth := maxTreeDepth(baseline, 0)
	if baseNodes > 0 && report.NodeCount > int(math.Ceil(float64(baseNodes)*2.5)) {
		report.addWarning("structural_regression", "warning", "candidate grew more than 2.5x baseline nodes", 0.05)
	}
	if baseDepth > 0 && report.Depth > baseDepth+3 {
		report.addWarning("structural_regression", "warning", "candidate depth increased by more than three levels", 0.05)
	}
}

func (m *MetaValidator) addRecommendations(candidate *SerializableNode, report *MetaValidationReport) {
	patterns := m.experts.RecommendMutations(candidate)
	for _, pattern := range patterns {
		report.Recommendations = append(report.Recommendations, pattern.Name)
	}
	sort.Strings(report.Recommendations)
}

func hasExecutionPathFirst(node *SerializableNode) bool {
	if node == nil {
		return false
	}
	if node.Type == "Selector" && len(node.Children) > 1 && node.Children[0].Name == "ExecutionPath" {
		return true
	}
	for i := range node.Children {
		if hasExecutionPathFirst(&node.Children[i]) {
			return true
		}
	}
	return false
}

func hasAnySetupTools(node *SerializableNode) bool {
	return hasNodeMatching(node, "SetupDefaultTools") ||
		hasNodeMatching(node, "SetupDevTools") ||
		hasNodeMatching(node, "SetupResearchTools") ||
		hasNodeMatching(node, "SetupStartupTools") ||
		hasNodeMatching(node, "SetupFinanceTools") ||
		hasNodeMatching(node, "SetupTools")
}

func hasUnboundedRetry(node *SerializableNode) bool {
	if node == nil {
		return false
	}
	if node.Type == "Retry" && node.MaxRetries > 10 {
		return true
	}
	for i := range node.Children {
		if hasUnboundedRetry(&node.Children[i]) {
			return true
		}
	}
	return false
}

func (r *MetaValidationReport) addIssue(check, severity, message string, weight float64) {
	r.Issues = append(r.Issues, MetaValidationIssue{Check: check, Severity: severity, Message: message, Weight: weight})
}

func (r *MetaValidationReport) addWarning(check, severity, message string, weight float64) {
	r.Warnings = append(r.Warnings, MetaValidationIssue{Check: check, Severity: severity, Message: message, Weight: weight})
}

func (r *MetaValidationReport) finalize(minScore, warnScore float64) {
	penalty := 0.0
	critical := false
	for _, issue := range r.Issues {
		penalty += issue.Weight
		if issue.Severity == "critical" {
			critical = true
		}
	}
	for _, warning := range r.Warnings {
		penalty += warning.Weight
	}
	if penalty > 1 {
		penalty = 1
	}
	r.Score = 1 - penalty
	if critical || r.Score < minScore {
		r.Decision = MetaReject
		return
	}
	if len(r.Warnings) > 0 || r.Score < warnScore {
		r.Decision = MetaWarn
		return
	}
	r.Decision = MetaAccept
}
