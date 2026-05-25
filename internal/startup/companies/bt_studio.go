package companies

import (
	"time"
	"github.com/nico/go-bt-evolve/internal/startup"
)

// BTStudioCompany creates a startup building a Flutter-based behavior tree visual editor.
func BTStudioCompany() *startup.CompanyState {
	return &startup.CompanyState{
		Name:          "BT Studio Inc.",
		Founded:       time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Mission:       "Make behavior trees accessible to everyone through beautiful visual tooling",
		Industry:      "Developer Tools / AI Infrastructure",
		ProductName:   "BT Studio",
		ProductStage:  "alpha",
		Features:      []string{"visual_tree_editor", "drag_drop_nodes", "ollama_integration"},
		TechStack:     []string{"Flutter", "Dart", "Go", "PostgreSQL", "WebSocket", "gRPC"},
		TechnicalDebt: 15,

		Users:     85,
		MRR:       0,
		ARR:       0,
		ChurnRate: 0.0,
		CAC:       0,
		LTV:       0,

		Runway:        8,
		BurnRate:      35000,
		CashInBank:    280000,
		FundingRaised: 500000,
		FundingRound:  "pre-seed",
		Valuation:     2500000,

		TeamSize:       4,
		Engineers:      2,
		SalesPeople:    0,
		MarketingStaff: 1,

		CurrentSprint: 1,
		SprintGoal:    "Launch alpha with visual tree editor and drag-drop node creation",
		QuarterGoals:  []string{"Ship Flutter alpha", "Get 500 waitlist signups", "Close pre-seed extension"},
		Risks:         []string{"Flutter desktop performance on complex trees", "no revenue yet", "team too small"},
		Opportunities: []string{"growing BT adoption in AI agents", "no good visual BT editor exists", "Ollama ecosystem growth"},

		Metrics: map[string]interface{}{
			"waitlist_signups":    340,
			"github_stars":        120,
			"flutter_widget_count": 45,
			"tree_render_fps":      58.0,
		},
	}
}
