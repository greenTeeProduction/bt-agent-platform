package persona

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStore_LoadReturnsDefaultForNewUser(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Load("nico")
	if err != nil {
		t.Fatalf("Load new user: %v", err)
	}
	if p.ID != "nico" {
		t.Errorf("ID = %q, want nico", p.ID)
	}
	if p.Approval.AutoApproveAutomations {
		t.Error("default profile must require HITL approval")
	}
	if p.Approval.MaxAutoCreatedAgents != defaultMaxAutoCreatedAgents {
		t.Errorf("MaxAutoCreatedAgents = %d, want %d", p.Approval.MaxAutoCreatedAgents, defaultMaxAutoCreatedAgents)
	}
}

func TestStore_SaveLoadRoundtrip(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Load("nico")
	if err != nil {
		t.Fatal(err)
	}
	p.PreferredStyle = "minimal"
	p.PreferenceTags = []string{"golang", "finance"}
	p.PromptHints = []string{"answer in German"}
	if err := s.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := s.Load("nico")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PreferredStyle != "minimal" || len(loaded.PreferenceTags) != 2 || len(loaded.PromptHints) != 1 {
		t.Errorf("roundtrip mismatch: %+v", loaded)
	}
	if _, err := os.Stat(filepath.Join(s.Root(), "nico", "profile.json")); err != nil {
		t.Errorf("profile.json not at expected workspace path: %v", err)
	}
}

func TestStore_AddPreferenceTagIsIdempotent(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddPreferenceTag("nico", "golang"); err != nil {
		t.Fatal(err)
	}
	p, err := s.AddPreferenceTag("nico", "GoLang")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.PreferenceTags) != 1 {
		t.Errorf("case-insensitive duplicate tag added: %v", p.PreferenceTags)
	}
}

func TestSanitizeUserID(t *testing.T) {
	cases := map[string]string{
		"nico":            "nico",
		"user:with/slash": "user_with_slash",
		"  spaced name  ": "spaced_name",
		"":                "_anonymous",
	}
	for in, want := range cases {
		if got := SanitizeUserID(in); got != want {
			t.Errorf("SanitizeUserID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProfile_ContextBlock(t *testing.T) {
	p := &Profile{
		ID:             "nico",
		PreferredStyle: "detailed",
		PreferenceTags: []string{"golang"},
		PromptHints:    []string{"prefer tables"},
	}
	block := p.ContextBlock()
	for _, want := range []string{"User Profile (nico)", "detailed", "golang", "prefer tables"} {
		if !strings.Contains(block, want) {
			t.Errorf("ContextBlock missing %q:\n%s", want, block)
		}
	}
	if (&Profile{ID: "empty"}).ContextBlock() != "" {
		t.Error("empty profile must render an empty context block")
	}
}

func TestLog_AppendAllSince(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	log, err := NewLog(s.Workspace("nico"))
	if err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-30 * 24 * time.Hour).Unix()
	if err := log.Append(Interaction{Task: "old task", Timestamp: old}); err != nil {
		t.Fatal(err)
	}
	if err := log.Append(Interaction{Task: "summarize weekly sales report", TreeID: "finance:report", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}

	all, err := log.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("All: expected 2 records, got %d", len(all))
	}

	recent, err := log.Since(time.Now().Add(-14 * 24 * time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 1 || recent[0].Task != "summarize weekly sales report" {
		t.Fatalf("Since: expected only the recent record, got %+v", recent)
	}
}

func minerInput(now time.Time) []Interaction {
	day := func(d int) int64 { return now.Add(-time.Duration(d) * 24 * time.Hour).Unix() }
	return []Interaction{
		{Task: "summarize weekly sales report for the team", TreeID: "finance:report", Timestamp: day(10)},
		{Task: "review my go code for bugs", TreeID: "domain:code_review", Timestamp: day(9)},
		{Task: "summarize the weekly sales report numbers", TreeID: "finance:report", Timestamp: day(6)},
		{Task: "please summarize weekly sales report again", TreeID: "finance:report", Timestamp: day(2)},
		{Task: "completely unrelated question about weather", Timestamp: day(1)},
		// Outside the 14-day window — must not count.
		{Task: "summarize weekly sales report from march", Timestamp: day(40)},
	}
}

func TestHabitMiner_DetectsRecurringPattern(t *testing.T) {
	now := time.Now()
	patterns := NewHabitMiner().Mine(minerInput(now), now)
	if len(patterns) != 1 {
		t.Fatalf("expected exactly 1 recurring pattern, got %d: %+v", len(patterns), patterns)
	}
	p := patterns[0]
	if p.Count != 3 {
		t.Errorf("Count = %d, want 3 (window must exclude the 40-day-old task)", p.Count)
	}
	if !strings.Contains(p.Representative, "summarize weekly sales report again") {
		t.Errorf("Representative should be the most recent example, got %q", p.Representative)
	}
	if len(p.TreeIDs) == 0 || p.TreeIDs[0] != "finance:report" {
		t.Errorf("dominant tree should be finance:report, got %v", p.TreeIDs)
	}
	if !strings.HasPrefix(p.SuggestedGoal, "Automate recurring task:") {
		t.Errorf("SuggestedGoal = %q", p.SuggestedGoal)
	}
}

func TestHabitMiner_BelowThresholdYieldsNothing(t *testing.T) {
	now := time.Now()
	interactions := []Interaction{
		{Task: "summarize weekly sales report", Timestamp: now.Unix()},
		{Task: "summarize weekly sales report", Timestamp: now.Unix()},
	}
	if got := NewHabitMiner().Mine(interactions, now); got != nil {
		t.Errorf("2 occurrences must not form a pattern, got %+v", got)
	}
}

func TestHabitMiner_EmbeddingFailureFallsBackToKeywords(t *testing.T) {
	now := time.Now()
	m := NewHabitMiner()
	m.Embed = func(string) ([]float64, error) { return nil, errors.New("ollama down") }
	patterns := m.Mine(minerInput(now), now)
	if len(patterns) != 1 || patterns[0].Count != 3 {
		t.Fatalf("keyword fallback should still find the pattern, got %+v", patterns)
	}
}

func TestHabitMiner_UsesEmbeddingsWhenAvailable(t *testing.T) {
	now := time.Now()
	m := NewHabitMiner()
	m.Threshold = 0.9
	// Fake embedder: sales-report tasks share one axis, everything else another.
	m.Embed = func(text string) ([]float64, error) {
		if strings.Contains(text, "sales report") {
			return []float64{1, 0}, nil
		}
		return []float64{0, 1}, nil
	}
	patterns := m.Mine(minerInput(now), now)
	if len(patterns) != 1 || patterns[0].Count != 3 {
		t.Fatalf("embedding clustering should find the sales-report pattern, got %+v", patterns)
	}
}
