package doormate

// IntentSession represents an active user intent session.
type IntentSession struct {
	ID              string   `json:"id"`
	RawInput        string   `json:"raw_input"`
	Intent          string   `json:"intent"`
	SelectedBubbles []string `json:"selected_bubbles"`
	Bubbles         []string `json:"bubbles"`
	PageIDs         []string `json:"page_ids"`
	CreatedAt       int64    `json:"created_at"`
	UpdatedAt       int64    `json:"updated_at"`
}

// Block represents a reusable element in a generated page.
type Block struct {
	Type       string           `json:"type"` // e.g. "overview", "comparison", "list", "chart", "diagram", "timeline", "cards", "gallery", "decision_tree"
	Title      string           `json:"title,omitempty"`
	Content    string           `json:"content,omitempty"`
	Items      []string         `json:"items,omitempty"`
	Headers    []string         `json:"headers,omitempty"`     // For tables/comparisons
	Rows       [][]string       `json:"rows,omitempty"`        // For tables/comparisons
	DataPoints []ChartDataPoint `json:"data_points,omitempty"` // For charts
	Nodes      []DiagramNode    `json:"nodes,omitempty"`       // For diagrams
	Edges      []DiagramEdge    `json:"edges,omitempty"`       // For diagrams
}

type ChartDataPoint struct {
	Label string  `json:"label"`
	Value float64 `json:"value"`
}

type DiagramNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Type  string `json:"type,omitempty"` // "start", "decision", "action", "end"
}

type DiagramEdge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// PageSchema defines the strict dynamic rendering contract.
type PageSchema struct {
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	TemplateID string   `json:"template_id"` // "overview", "recommendation", "comparison", "guide"
	Blocks     []Block  `json:"blocks"`
	FollowUps  []string `json:"follow_ups"`
}

// GeneratedPage captures the generated response.
type GeneratedPage struct {
	ID         string     `json:"id"`
	SessionID  string     `json:"session_id"`
	Schema     PageSchema `json:"schema"`
	Bookmarked bool       `json:"bookmarked"`
	Rating     int        `json:"rating"` // 1-5, 0 for unrated
	CreatedAt  int64      `json:"created_at"`
}

// UserProfile aggregates learning indicators.
type UserProfile struct {
	ID             string   `json:"id"`
	PreferenceTags []string `json:"preference_tags"`
	BookmarkIDs    []string `json:"bookmark_ids"`
	PreferredStyle string   `json:"preferred_style"` // "visual", "minimal", "detailed"
	UpdatedAt      int64    `json:"updated_at"`
}

// FeedbackEvent log record.
type FeedbackEvent struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id,omitempty"`
	PageID    string `json:"page_id,omitempty"`
	Type      string `json:"type"` // "bubble_click", "bookmark", "rate", "follow_up_click"
	Value     string `json:"value"`
	Timestamp int64  `json:"timestamp"`
}
