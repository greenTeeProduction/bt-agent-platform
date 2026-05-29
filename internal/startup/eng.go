package startup

import "github.com/nico/go-bt-evolve/internal/evolution"

// EngineerTree models a software engineering sprint workflow.
//
// Flow: PreGate → SprintPlanning → BuildFeature → WriteTests → FixBugs → CodeReview → DeployRelease → Reflect
// Each core step is a ChainAction "agent:" node, simulating an engineer reasoning about the task.
func EngineerTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Engineer_Main",
		Children: []evolution.SerializableNode{
			// --- PreGate: validate and set up tools ---
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty sprint task"},
					{Type: "Condition", Name: "IsEngineeringTask", Description: "Detect engineering/dev keywords"},
					{Type: "Action", Name: "SetupDevTools", Description: "Populate bb.ChainTools with go_build, go_test, go_vet, web_search"},
				},
			},
			// --- SprintPlanning: plan current sprint tasks ---
			{
				Type: "ChainAction",
				Name: "llm_call:Plan the current sprint. Review the sprint goal {{.Task}}, assess the feature backlog, tech debt ({{.Result}}), and team capacity. Decide which feature to build this sprint and estimate effort.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a senior software engineer planning a sprint. Consider feature priorities, technical debt, team velocity, and dependencies. Produce a concise sprint plan.",
					"tools":      []any{"web_search"},
				},
			},
			// --- BuildFeature: build the top-priority feature ---
			{
				Type: "ChainAction",
				Name: "llm_call:Build the top-priority feature from the product backlog. Write production-quality code. Consider the existing tech stack, architecture patterns, and testability. Implement the feature end-to-end.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a software engineer implementing a feature. Write clean, idiomatic Go code. Consider error handling, logging, and edge cases. Output the implementation plan and key code decisions.",
					"tools":      []any{"go_build", "web_search"},
				},
			},
			// --- WriteTests: write tests for the new feature ---
			{
				Type: "ChainAction",
				Name: "llm_call:Write comprehensive tests for the newly built feature. Cover happy path, edge cases, error handling, and integration points. Use table-driven tests where appropriate.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a software engineer writing tests. Use table-driven tests, cover edge cases, and ensure high coverage. Focus on behavioral testing, not implementation details.",
					"tools":      []any{"go_test", "go_build"},
				},
			},
			// --- FixBugs: fix reported bugs and reduce technical_debt ---
			{
				Type: "ChainAction",
				Name: "llm_call:Fix any reported bugs from the bug tracker. Review and reduce technical debt where possible. Refactor legacy code that's causing maintenance issues. Prioritize by severity and impact.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a software engineer fixing bugs and reducing tech debt. Triage by severity, write regression tests, and refactor where it meaningfully reduces complexity.",
					"tools":      []any{"go_build", "go_test", "go_vet"},
				},
			},
			// --- CodeReview: review teammate's code ---
			{
				Type: "ChainAction",
				Name: "llm_call:Review the code changes from this sprint. Check for correctness, style, security issues, performance problems, and adherence to team conventions. Provide actionable feedback.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a senior engineer doing code review. Be thorough but constructive. Check for bugs, security issues, performance, readability, and idiomatic patterns. Suggest concrete improvements.",
				},
			},
			// --- DeployRelease: deploy to staging/production ---
			{
				Type: "ChainAction",
				Name: "llm_call:Deploy the sprint's changes to staging and then production. Verify health checks, run smoke tests, monitor error rates, and be ready to rollback if needed. Document the release.",
				Metadata: map[string]any{
					"max_tokens": float64(8),
					"system_msg": "You are a DevOps engineer deploying a release. Check health endpoints, monitor dashboards, verify feature flags, and ensure the deployment is clean. Document what was deployed.",
					"tools":      []any{"go_build", "go_test"},
				},
			},
			// --- Reflect ---
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect on sprint outcomes: what went well, what to improve"},
			// --- Outcome selector ---
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3,
						Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix and retry"}},
					},
				},
			},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve on failures"},
		},
	}
}

// MarketingTree models a marketing team workflow.
//
// Flow: PreGate → ContentStrategy → SEOOptimization → CommunityEngagement → CampaignAnalysis → Reflect
func MarketingTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Marketing_Main",
		Children: []evolution.SerializableNode{
			// --- PreGate ---
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty marketing task"},
					{Type: "Condition", Name: "IsMarketingTask", Description: "Detect marketing/growth/content keywords"},
					{Type: "Action", Name: "SetupResearchTools", Description: "Populate bb.ChainTools with web_search, knowledge_graph, calculator"},
				},
			},
			// --- ContentStrategy: plan blog posts, social media, developer content ---
			{
				Type: "ChainAction",
				Name: "llm_call:Plan the content strategy for this sprint. Identify key topics, target audiences, content formats (blog, social, video, docs), and publishing cadence. Align with product roadmap and company goals.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a marketing strategist planning content. Consider SEO keywords, developer audience, thought leadership, and competitive differentiation. Output a content calendar.",
					"tools":      []any{"web_search"},
				},
			},
			// --- SEOOptimization: improve SEO for target keywords ---
			{
				Type: "ChainAction",
				Name: "llm_call:Optimize SEO for target keywords. Analyze current rankings, identify keyword gaps, improve on-page SEO (titles, meta, headings, internal linking), and plan backlink strategy. Prioritize high-ROI keywords.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are an SEO specialist. Focus on technical SEO, keyword research, on-page optimization, and content gaps. Consider search intent and competitive landscape.",
					"tools":      []any{"web_search"},
				},
			},
			// --- CommunityEngagement: engage on GitHub, Discord, Twitter ---
			{
				Type: "ChainAction",
				Name: "llm_call:Plan and execute community engagement. Engage on GitHub (issues, PRs, discussions), Discord (answer questions, foster community), and Twitter/LinkedIn (share updates, amplify user content). Build developer relations.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a developer relations and community manager. Be authentic, helpful, and focused on building genuine relationships. Highlight community contributions and user success stories.",
					"tools":      []any{"web_search"},
				},
			},
			// --- CampaignAnalysis: analyze CAC, conversion rates, ROI ---
			{
				Type: "ChainAction",
				Name: "llm_call:Analyze marketing campaign performance. Calculate CAC by channel, conversion rates through the funnel, ROI per campaign, and LTV/CAC ratio. Identify the best and worst performing channels. Recommend budget allocation changes.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a growth analyst. Quantify campaign performance with hard numbers. Compare channels, identify trends, and make data-driven budget recommendations. Flag underperforming spend.",
					"tools":      []any{"calculator", "web_search"},
				},
			},
			// --- Reflect ---
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect on marketing outcomes"},
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3,
						Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix and retry"}},
					},
				},
			},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve on failures"},
		},
	}
}

// SalesTree models a sales team workflow.
//
// Flow: PreGate → LeadQualification → DemoPreparation → PricingStrategy → CloseDeals → Reflect
func SalesTree() *evolution.SerializableNode {
	return &evolution.SerializableNode{
		Type: "Sequence",
		Name: "Sales_Main",
		Children: []evolution.SerializableNode{
			// --- PreGate ---
			{
				Type: "Sequence",
				Name: "PreGate",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "ValidateInput", Description: "Non-empty sales task"},
					{Type: "Condition", Name: "IsSalesTask", Description: "Detect sales/deal/pipeline/revenue keywords"},
					{Type: "Action", Name: "SetupResearchTools", Description: "Populate bb.ChainTools with web_search, knowledge_graph, calculator"},
				},
			},
			// --- LeadQualification: qualify inbound leads from pipeline ---
			{
				Type: "ChainAction",
				Name: "llm_call:Qualify inbound leads from the sales pipeline. Score each lead by BANT (Budget, Authority, Need, Timeline). Identify the top 3 leads to focus on. Research each lead's company, pain points, and use case fit.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a sales development representative qualifying leads. Use BANT framework. Research company size, industry, tech stack, and potential deal size. Rank leads by close probability.",
					"tools":      []any{"web_search"},
				},
			},
			// --- DemoPreparation: prepare custom demo for top lead ---
			{
				Type: "ChainAction",
				Name: "llm_call:Prepare a custom demo for the top-qualified lead. Tailor the demo to their specific use case, industry, and pain points. Prepare talking points, anticipate objections, and craft a compelling narrative.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a solutions engineer preparing a product demo. Focus on the prospect's specific pain points. Show the workflow that matters to them. Prepare answers for common objections.",
					"tools":      []any{"web_search"},
				},
			},
			// --- PricingStrategy: evaluate pricing vs competitors ---
			{
				Type: "ChainAction",
				Name: "llm_call:Evaluate pricing strategy. Research competitor pricing, assess willingness-to-pay for target segments, model different pricing tiers (freemium, per-seat, usage-based). Recommend optimal pricing for current deals and long-term positioning.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are a pricing strategist. Analyze competitor pricing, value-based pricing models, and segment willingness-to-pay. Consider discount strategies for enterprise deals. Maximize LTV while staying competitive.",
					"tools":      []any{"web_search", "calculator"},
				},
			},
			// --- CloseDeals: send proposals, negotiate, close ---
			{
				Type: "ChainAction",
				Name: "llm_call:Close qualified deals. Send proposals to top leads, negotiate terms, address final objections, and move deals to closed-won. Track deal stages and forecast revenue. Escalate blockers to leadership.",
				Metadata: map[string]any{
					"max_tokens": float64(10),
					"system_msg": "You are an account executive closing deals. Be persistent but respectful. Address objections with data. Know when to offer discounts and when to hold firm. Aim for mutual close plans.",
					"tools":      []any{"calculator"},
				},
			},
			// --- Reflect ---
			{Type: "Action", Name: "ReflectOnOutcome", Description: "Reflect on sales outcomes"},
			{
				Type: "Selector",
				Name: "OutcomeSelector",
				Children: []evolution.SerializableNode{
					{Type: "Condition", Name: "WasSuccessful"},
					{
						Type: "Retry", Name: "RetrySelfCorrect", MaxRetries: 3,
						Children: []evolution.SerializableNode{{Type: "Action", Name: "SelfCorrect", Description: "Fix and retry"}},
					},
				},
			},
			{Type: "Action", Name: "UpdateBehaviorTree", Description: "Evolve on failures"},
		},
	}
}

// StartupTrees returns all startup operational trees keyed by name.
func StartupTrees() map[string]*evolution.SerializableNode {
	return map[string]*evolution.SerializableNode{
		"engineer":  EngineerTree(),
		"marketing": MarketingTree(),
		"sales":     SalesTree(),
	}
}
