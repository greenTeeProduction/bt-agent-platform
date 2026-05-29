package startup

import "github.com/nico/go-bt-evolve/internal/evolution"

// ---------------------------------------------------------------------------
// CEO Role Tree
// ---------------------------------------------------------------------------

// CEOTree returns the CEO behavior tree for a sequential quarterly workflow:
//
//	PreGate -> ReviewCompanyMetrics -> MakeStrategicDecisions ->
//	SetQuarterGoals -> CommunicateVision -> Reflect
func CEOTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "CEO_QuarterlyWorkflow",
		Children: []evolution.SerializableNode{
			// --- PreGate: validate state and populate tools ---
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{
						Type:        "Condition",
						Name:        "ValidateCompanyState",
						Description: "Check that CompanyState is initialized and non-nil",
					},
					{
						Type:        "Action",
						Name:        "SetupStartupTools",
						Description: "Populate bb.ChainTools with startup simulation tools (financial modeling, market analysis, etc.)",
					},
				},
			},
			// --- ReviewCompanyMetrics ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the CEO of a startup. Review the current company metrics: MRR={{.MRR}}, ARR={{.ARR}}, Runway={{.RunwayMonths}} months, Burn={{.BurnRateMonthly}}/month, Cash={{.CashInBank}}, Team={{.TeamSize}}, Users={{.Users}}, Churn={{.ChurnRate}}, NPS={{.NPS}}, ProductStage={{.ProductStage}}. Analyze the company's financial health, team composition, runway risks, and growth trajectory. Identify the top 3 risks and top 3 opportunities. Output a concise executive summary.",
			},
			// --- MakeStrategicDecisions ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the CEO. Based on the current state: MRR={{.MRR}}, Runway={{.RunwayMonths}}mo, Team={{.TeamSize}}, ProductStage={{.ProductStage}}, Risks={{.Risks}}, Opportunities={{.Opportunities}}. Evaluate strategic options: (1) hire more engineers, (2) raise next funding round, (3) pivot the product, (4) double-down on growth. Choose the best course of action with clear rationale. Consider runway, team bandwidth, market timing, and competitive landscape. Output a strategic decision in the format: CHOICE: <option>, RATIONALE: <reasoning>, ALTERNATIVES: <list>.",
			},
			// --- SetQuarterGoals ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the CEO. Define 3-5 OKRs for next quarter based on the strategic decision just made. Current metrics: MRR={{.MRR}}, Users={{.Users}}, ProductStage={{.ProductStage}}. Each OKR should have a measurable key result with a target number. Consider revenue growth, user acquisition, product milestones, team scaling, and fundraising. Output as a structured list: Objective 1: ..., KR1: ..., KR2: ...",
			},
			// --- CommunicateVision ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the CEO. Craft an internal company memo updating the team on the vision, strategy, and Q{{.Quarter}} goals. The memo should be inspiring, transparent about challenges (risks: {{.Risks}}), focused on opportunities ({{.Opportunities}}), and clearly communicate the strategic decision and OKRs. Tone: candid startup leadership. Keep it under 500 words.",
			},
			// --- Reflect ---
			{
				Type:        "Action",
				Name:        "Reflect",
				Description: "Generate reflection on the CEO workflow: what decisions were strong, where is more information needed, what should be revisited next quarter",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// CTO Role Tree
// ---------------------------------------------------------------------------

// CTOTree returns the CTO behavior tree for a sequential technical workflow:
//
//	PreGate -> ReviewArchitecture -> MakeTechDecisions ->
//	PlanEngineeringRoadmap -> TechDebtAssessment -> Reflect
func CTOTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "CTO_TechnicalWorkflow",
		Children: []evolution.SerializableNode{
			// --- PreGate ---
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{
						Type:        "Condition",
						Name:        "ValidateCompanyState",
						Description: "Check that CompanyState is initialized and non-nil",
					},
					{
						Type:        "Action",
						Name:        "SetupStartupTools",
						Description: "Populate bb.ChainTools with startup simulation tools",
					},
				},
			},
			// --- ReviewArchitecture ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the CTO of a startup. Evaluate the current technical architecture: TechStack={{.TechStack}}, ProductStage={{.ProductStage}}, Features={{.Features}}, TechnicalDebt={{.TechnicalDebt}}/100. Assess scalability risks, technology choices, infrastructure readiness, and system reliability. Identify architectural bottlenecks and single points of failure. Consider team size of {{.Engineers}} engineers. Output a concise architecture review with a health score (1-10) and key findings.",
			},
			// --- MakeTechDecisions ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the CTO. Based on the architecture review, make key technical decisions for the next quarter. Consider: (1) refactoring critical paths, (2) adopting new tools/infrastructure, (3) scaling strategy, (4) build vs. buy decisions, (5) hiring priorities. Current stack: {{.TechStack}}, Engineers: {{.Engineers}}, Debt: {{.TechnicalDebt}}. Output each decision with rationale and trade-offs.",
			},
			// --- PlanEngineeringRoadmap ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the CTO. Define the engineering roadmap for next quarter with technical milestones. Current sprint: {{.CurrentSprint}}, ProductStage: {{.ProductStage}}, Team: {{.Engineers}} engineers. Plan 3-5 milestones with timelines, dependencies, and success criteria. Include: infrastructure work, feature development, tech debt reduction targets, reliability improvements. Align with product goals from the CEO/PM.",
			},
			// --- TechDebtAssessment ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the CTO. Perform a detailed tech debt assessment. Current debt score: {{.TechnicalDebt}}/100. Review the codebase areas contributing to this: architecture decisions, test coverage gaps, documentation, dependency freshness, monitoring gaps. Prioritize tech debt items by risk and remediation effort. Output a prioritized list with estimated engineering weeks per item and a recommended reduction target for next quarter.",
			},
			// --- Reflect ---
			{
				Type:        "Action",
				Name:        "Reflect",
				Description: "Generate reflection on the CTO workflow: what technical decisions are sound, what risks were underweighted, what needs more data",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Product Manager Role Tree
// ---------------------------------------------------------------------------

// PMTree returns the Product Manager behavior tree for a sequential product workflow:
//
//	PreGate -> ReviewUserFeedback -> PrioritizeFeatures ->
//	WriteSpecs -> CompetitiveAnalysis -> Reflect
func PMTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "PM_ProductWorkflow",
		Children: []evolution.SerializableNode{
			// --- PreGate ---
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{
						Type:        "Condition",
						Name:        "ValidateCompanyState",
						Description: "Check that CompanyState is initialized and non-nil",
					},
					{
						Type:        "Action",
						Name:        "SetupStartupTools",
						Description: "Populate bb.ChainTools with startup simulation tools",
					},
				},
			},
			// --- ReviewUserFeedback ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the Product Manager. Review user feedback and product metrics: Users={{.Users}}, NPS={{.NPS}}, Churn={{.ChurnRate}}, ActivationRate={{index .Metrics \"activation_rate\"}}, SupportTickets={{index .Metrics \"support_tickets\"}}, DAU={{index .Metrics \"daily_active_users\"}}, MRR={{.MRR}}, ProductStage={{.ProductStage}}. Analyze the voice of the customer: what are users loving, where are they struggling, what are churn signals? Identify top 3 user pain points and top 3 feature requests. Output a concise product health report.",
			},
			// --- PrioritizeFeatures ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the Product Manager. Prioritize features for the next sprint/quarter. Current features: {{.Features}}, Sprint: {{.CurrentSprint}}, Goal: \"{{.SprintGoal}}\". Based on user feedback analysis, rank candidate features by impact vs. effort (RICE: Reach, Impact, Confidence, Effort). Consider: user needs, business goals (MRR growth, retention), technical feasibility (engineers: {{.Engineers}}, debt: {{.TechnicalDebt}}). Output a prioritized backlog with top 5 items and reasoning.",
			},
			// --- WriteSpecs ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the Product Manager. Write feature specifications for the top 2 prioritized features. For each feature, provide: (1) problem statement and user story, (2) acceptance criteria, (3) technical scope and constraints, (4) success metrics, (5) wireframe/textual UI description. Keep each spec concise but actionable for {{.Engineers}} engineers. Include edge cases and error states. Output both specs with clear separation.",
			},
			// --- CompetitiveAnalysis ---
			{
				Type: "ChainAction",
				Name: "llm_call:You are the Product Manager. Perform a competitive analysis for the current market position. Product: {{.ProductName}}, Industry: {{.Industry}}, Stage: {{.ProductStage}}. Analyze competitive landscape: identify 3-5 key competitors or substitutes, their recent moves, and how our product differentiates. Assess threats and opportunities from competitor actions. Consider pricing, features, positioning. Output a competitive matrix and strategic recommendations.",
			},
			// --- Reflect ---
			{
				Type:        "Action",
				Name:        "Reflect",
				Description: "Generate reflection on the PM workflow: what user signals were strongest, which features may need re-prioritization, what competitive moves were missed",
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Role Registry
// ---------------------------------------------------------------------------

// Roles returns a map of role names to their behavior tree factory functions.
func Roles() map[string]*evolution.SerializableNode {
	return map[string]*evolution.SerializableNode{
		"ceo": CEOTree(),
		"cto": CTOTree(),
		"pm":  PMTree(),
	}
}

// RoleDescriptions returns human-readable descriptions for each role.
func RoleDescriptions() map[string]string {
	return map[string]string{
		"ceo": "CEO: Strategic leadership — reviews company metrics, makes high-level decisions, sets quarterly OKRs, and communicates vision.",
		"cto": "CTO: Technical leadership — reviews architecture, makes tech decisions, plans engineering roadmap, and assesses tech debt.",
		"pm":  "Product Manager: Product leadership — reviews user feedback, prioritizes features, writes specs, and analyzes competition.",
	}
}
