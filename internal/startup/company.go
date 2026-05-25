package startup

import "time"

// CompanyState holds the simulated startup's state — the shared blackboard for all role trees.
type CompanyState struct {
	Name        string    `json:"name"`
	Founded     time.Time `json:"founded"`
	Mission     string    `json:"mission"`
	Industry    string    `json:"industry"`

	// Product
	ProductName    string   `json:"product_name"`
	ProductStage   string   `json:"product_stage"` // idea, mvp, beta, launched, scaling
	Features       []string `json:"features"`
	TechStack      []string `json:"tech_stack"`
	TechnicalDebt  float64  `json:"technical_debt"` // 0-100

	// Metrics
	Users          int     `json:"users"`
	MRR            float64 `json:"mrr"`
	ARR            float64 `json:"arr"`
	ChurnRate      float64 `json:"churn_rate"`
	CAC            float64 `json:"cac"`
	LTV            float64 `json:"ltv"`
	NPS            float64 `json:"nps"`

	// Financials
	Runway         int     `json:"runway_months"`
	BurnRate       float64 `json:"burn_rate_monthly"`
	CashInBank     float64 `json:"cash_in_bank"`
	FundingRaised  float64 `json:"funding_raised"`
	FundingRound   string   `json:"funding_round"` // pre-seed, seed, series-a, series-b
	Valuation      float64 `json:"valuation"`

	// Team
	TeamSize       int     `json:"team_size"`
	Engineers      int     `json:"engineers"`
	SalesPeople    int     `json:"sales_people"`
	MarketingStaff int     `json:"marketing_staff"`

	// Strategy
	CurrentSprint  int                    `json:"current_sprint"`
	SprintGoal     string                 `json:"sprint_goal"`
	QuarterGoals   []string               `json:"quarter_goals"`
	Risks          []string               `json:"risks"`
	Opportunities  []string               `json:"opportunities"`
	Decisions      []Decision             `json:"decisions"`
	Metrics        map[string]interface{} `json:"custom_metrics"`
}

// Decision records a strategic choice made by the CEO or leadership.
type Decision struct {
	Topic       string    `json:"topic"`
	Choice      string    `json:"choice"`
	Rationale   string    `json:"rationale"`
	Alternatives []string  `json:"alternatives"`
	Timestamp   time.Time `json:"timestamp"`
}

// SprintResult captures the outcome of one development sprint.
type SprintResult struct {
	SprintNum    int      `json:"sprint_num"`
	Goal         string   `json:"goal"`
	Completed    []string `json:"completed"`
	Deferred     []string `json:"deferred"`
	BugsFixed    int      `json:"bugs_fixed"`
	Velocity     float64  `json:"velocity"`
	TechDebtDelta float64 `json:"tech_debt_delta"`
}

// QuarterResult captures quarterly business review.
type QuarterResult struct {
	Quarter      int     `json:"quarter"`
	Revenue      float64 `json:"revenue"`
	Growth       float64 `json:"growth_pct"`
	UsersAdded   int     `json:"users_added"`
	Churn        float64 `json:"churn"`
	CashBurned   float64 `json:"cash_burned"`
	Highlights   []string `json:"highlights"`
	Lowlights    []string `json:"lowlights"`
	OKRProgress  map[string]float64 `json:"okr_progress"`
}

// NewDefaultCompany creates a seed-stage SaaS startup as default simulation.
func NewDefaultCompany() *CompanyState {
	return &CompanyState{
		Name:          "HermesAI",
		Founded:       time.Now().AddDate(-1, 0, 0),
		Mission:       "Democratize AI agent development with behavior trees",
		Industry:      "AI/Developer Tools",
		ProductName:   "BT Studio",
		ProductStage:  "beta",
		Features:      []string{"visual_bt_editor", "mcp_integration", "ollama_backend"},
		TechStack:     []string{"Go", "React", "TypeScript", "PostgreSQL", "Redis"},
		TechnicalDebt: 25,

		Users:     1200,
		MRR:       18000,
		ARR:       216000,
		ChurnRate: 0.03,
		CAC:       150,
		LTV:       5000,

		Runway:        14,
		BurnRate:      45000,
		CashInBank:    630000,
		FundingRaised: 1200000,
		FundingRound:  "seed",
		Valuation:     6000000,

		TeamSize:       8,
		Engineers:      4,
		SalesPeople:    1,
		MarketingStaff: 1,

		CurrentSprint: 12,
		SprintGoal:    "Launch enterprise SSO + audit logging",
		QuarterGoals:  []string{"Reach $25k MRR", "Close 3 enterprise pilots", "Ship v1.0"},
		Risks:         []string{"competitor launched similar product", "key engineer considering offer"},
		Opportunities: []string{"enterprise procurement pipeline", "OpenAI plugin marketplace"},

		Metrics: map[string]interface{}{
			"daily_active_users":  340,
			"activation_rate":     0.45,
			"nps_score":           42,
			"support_tickets":     18,
		},
	}
}
