# DoorMate Page-First AI Assistant Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build "DoorMate," a revolutionary, page-first AI assistant integrated directly into the `bt-dashboard` as a persistent tab, replacing traditional chat streams with dynamically generated web pages built from a reusable template library, backed by an intent-predicting bubble engine, voice/video interaction, and a persistent personalized user profile loop.

**Architecture:** 
- **Backend (`internal/doormate`)**: Go-based domain service containing an Intent Parser/Bubble Engine, Page Agent (leveraging `llm.LLM` / Ollama with a robust static fallback), and a lightweight JSON-file persistence layer storing Sessions, Pages, Profiles, and Feedback.
- **Frontend (`cmd/bt-dashboard/static/js/tabs/doormate.js`)**: Dynamic HTML5/CSS3 interface featuring a beautiful "Intent Canvas" (picture-phrase background), interactive bubble selections, voice/video capture cues, and a Page Template Library that renders JSON schemas into elegant, interactive dashboard pages.

**Tech Stack:** Go (Standard Library), Vanilla HTML5/CSS3/JavaScript (dynamic template rendering, SVG/CSS-based charts and diagrams), JSON File Persistence.

---

## Chunk 1: Backend Domain Models & File-Based Storage

We will build the domain layer and persistent JSON storage to track intent sessions, user profiles, generated pages, and feedback.

### Task 1: Domain Structs
**Files:**
- Create: `internal/doormate/models.go`
- Create: `internal/doormate/models_test.go`

- [ ] **Step 1: Write tests for models serialization**
```go
package doormate

import (
	"encoding/json"
	"testing"
)

func TestModelSerialization(t *testing.T) {
	session := IntentSession{
		ID:        "sess-123",
		RawInput:  "setup smart lock",
		Intent:    "configure_lock",
		Bubbles:   []string{"smart lock", "zigbee", "keyless"},
		PageIDs:   []string{"page-123"},
		CreatedAt: 1780651400,
	}
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("failed to marshal session: %v", err)
	}
	var deserialized IntentSession
	if err := json.Unmarshal(data, &deserialized); err != nil {
		t.Fatalf("failed to unmarshal session: %v", err)
	}
	if deserialized.RawInput != session.RawInput {
		t.Errorf("expected input %q, got %q", session.RawInput, deserialized.RawInput)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test ./internal/doormate/...`
Expected: FAIL due to missing types/package.

- [ ] **Step 3: Implement domain models**
```go
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
	Type        string            `json:"type"` // e.g. "overview", "comparison", "list", "chart", "diagram", "timeline", "cards", "gallery", "decision_tree"
	Title       string            `json:"title,omitempty"`
	Content     string            `json:"content,omitempty"`
	Items       []string          `json:"items,omitempty"`
	Headers     []string          `json:"headers,omitempty"` // For tables/comparisons
	Rows        [][]string        `json:"rows,omitempty"`    // For tables/comparisons
	DataPoints  []ChartDataPoint  `json:"data_points,omitempty"` // For charts
	Nodes       []DiagramNode     `json:"nodes,omitempty"`       // For diagrams
	Edges       []DiagramEdge     `json:"edges,omitempty"`       // For diagrams
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
	ID              string     `json:"id"`
	SessionID       string     `json:"session_id"`
	Schema          PageSchema `json:"schema"`
	Bookmarked      bool       `json:"bookmarked"`
	Rating          int        `json:"rating"` // 1-5, 0 for unrated
	CreatedAt       int64      `json:"created_at"`
}

// UserProfile aggregates learning indicators.
type UserProfile struct {
	ID                 string            `json:"id"`
	PreferenceTags     []string          `json:"preference_tags"`
	BookmarkIDs        []string          `json:"bookmark_ids"`
	PreferredStyle     string            `json:"preferred_style"` // "visual", "minimal", "detailed"
	UpdatedAt          int64             `json:"updated_at"`
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
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test ./internal/doormate/...`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git add internal/doormate/models.go internal/doormate/models_test.go
git commit -m "feat(doormate): add domain models for assistant"
```

---

### Task 2: JSON Persistence Store
We will build a JSON file-based store following the platform's atomic-rename conventions.

**Files:**
- Create: `internal/doormate/store.go`
- Create: `internal/doormate/store_test.go`

- [ ] **Step 1: Write store integration tests**
```go
package doormate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorePersistence(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}

	// Test profile
	profile := &UserProfile{ID: "default", PreferenceTags: []string{"smart", "security"}}
	if err := store.SaveProfile(profile); err != nil {
		t.Fatalf("save profile: %v", err)
	}

	loaded, err := store.LoadProfile("default")
	if err != nil {
		t.Fatalf("load profile: %v", err)
	}
	if len(loaded.PreferenceTags) != 2 || loaded.PreferenceTags[0] != "smart" {
		t.Errorf("profile mismatch")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test ./internal/doormate/...`
Expected: FAIL due to missing store functions.

- [ ] **Step 3: Implement Store with atomic writes and individual file groupings**
```go
package doormate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Store struct {
	mu  sync.RWMutex
	dir string
}

func NewStore(dir string) (*Store, error) {
	subdirs := []string{"sessions", "pages", "profiles", "feedback"}
	for _, sub := range subdirs {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0750); err != nil {
			return nil, fmt.Errorf("create subdir %s: %w", sub, err)
		}
	}
	return &Store{dir: dir}, nil
}

func (s *Store) atomicWrite(path string, data interface{}) error {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, bytes, 0640); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func (s *Store) SaveSession(sess *IntentSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess.UpdatedAt = time.Now().Unix()
	if sess.CreatedAt == 0 {
		sess.CreatedAt = sess.UpdatedAt
	}
	return s.atomicWrite(filepath.Join(s.dir, "sessions", sess.ID+".json"), sess)
}

func (s *Store) LoadSession(id string) (*IntentSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.dir, "sessions", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sess IntentSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *Store) SavePage(page *GeneratedPage) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if page.CreatedAt == 0 {
		page.CreatedAt = time.Now().Unix()
	}
	return s.atomicWrite(filepath.Join(s.dir, "pages", page.ID+".json"), page)
}

func (s *Store) LoadPage(id string) (*GeneratedPage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.dir, "pages", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var page GeneratedPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, err
	}
	return &page, nil
}

func (s *Store) SaveProfile(prof *UserProfile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prof.UpdatedAt = time.Now().Unix()
	return s.atomicWrite(filepath.Join(s.dir, "profiles", prof.ID+".json"), prof)
}

func (s *Store) LoadProfile(id string) (*UserProfile, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	path := filepath.Join(s.dir, "profiles", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Return a clean default profile
			return &UserProfile{ID: id, PreferenceTags: []string{}, PreferredStyle: "visual"}, nil
		}
		return nil, err
	}
	var prof UserProfile
	if err := json.Unmarshal(data, &prof); err != nil {
		return nil, err
	}
	return &prof, nil
}

func (s *Store) LogFeedback(evt *FeedbackEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	evt.Timestamp = time.Now().Unix()
	if evt.ID == "" {
		evt.ID = fmt.Sprintf("feed-%d", time.Now().UnixNano())
	}
	return s.atomicWrite(filepath.Join(s.dir, "feedback", evt.ID+".json"), evt)
}
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test ./internal/doormate/...`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git add internal/doormate/store.go internal/doormate/store_test.go
git commit -m "feat(doormate): implement JSON storage persistence layer"
```

---

## Chunk 2: AI Intent Parsing & Page Generation (LLM + Fallback)

This chunk implements the intelligence engine: parsing intents into structured bubbles and generating pages using `llm.LLM` (with a highly customized prompt requesting JSON), along with a rich, beautifully designed mock generator that serves as an immediate fallback when Ollama is unavailable.

### Task 3: Intent Parser and Page Agent
**Files:**
- Create: `internal/doormate/agent.go`
- Create: `internal/doormate/agent_test.go`

- [ ] **Step 1: Write test for intent parsing and page generation**
```go
package doormate

import (
	"context"
	"strings"
	"testing"
	"time"
)

type mockLLM struct {
	reply string
	err   error
}

func (m *mockLLM) Generate(prompt string) (string, error) { return m.reply, m.err }
func (m *mockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) { return m.reply, m.err }
func (m *mockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) { return m.reply, m.err }
func (m *mockLLM) AnalyzeComplexity(task string) string { return "" }
func (m *mockLLM) GeneratePlan(task, complexity string) string { return "" }
func (m *mockLLM) Reflect(task, outcome, plan string) (string, string) { return "", "" }

func TestAgentGeneration(t *testing.T) {
	// Let's test the mock fallback since Ollama might be skipped
	agent := NewPageAgent(nil) // nil LLM triggers mock fallback
	sess, page, err := agent.Process("setup high security door lock", &UserProfile{ID: "test"})
	if err != nil {
		t.Fatalf("agent process failed: %v", err)
	}
	if sess.Intent != "security" {
		t.Errorf("expected security intent, got %q", sess.Intent)
	}
	if !strings.Contains(strings.ToLower(page.Schema.Title), "security") {
		t.Errorf("expected security in title, got %s", page.Schema.Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test ./internal/doormate/...`
Expected: FAIL due to missing NewPageAgent.

- [ ] **Step 3: Implement Page Agent with dynamic LLM generation and a comprehensive static template fallback library**
```go
package doormate

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nico/go-bt-evolve/internal/llm"
)

type PageAgent struct {
	llm llm.LLM
}

func NewPageAgent(llmClient llm.LLM) *PageAgent {
	return &PageAgent{llm: llmClient}
}

// Process parses intent and generates structured page schema.
func (pa *PageAgent) Process(input string, profile *UserProfile) (*IntentSession, *GeneratedPage, error) {
	sessionID := fmt.Sprintf("sess-%d", time.Now().UnixNano())
	pageID := fmt.Sprintf("page-%d", time.Now().UnixNano())

	intent, bubbles := pa.parseIntentAndBubbles(input, profile)

	var schema PageSchema
	var err error

	if pa.llm != nil {
		schema, err = pa.generatePageWithLLM(input, intent, bubbles, profile)
		if err != nil {
			// Fallback to beautiful mock page
			schema = pa.generateBeautifulMockPage(input, intent, bubbles, profile)
		}
	} else {
		schema = pa.generateBeautifulMockPage(input, intent, bubbles, profile)
	}

	session := &IntentSession{
		ID:              sessionID,
		RawInput:        input,
		Intent:          intent,
		SelectedBubbles: []string{},
		Bubbles:         bubbles,
		PageIDs:         []string{pageID},
	}

	page := &GeneratedPage{
		ID:        pageID,
		SessionID: sessionID,
		Schema:    schema,
	}

	return session, page, nil
}

func (pa *PageAgent) parseIntentAndBubbles(input string, profile *UserProfile) (string, []string) {
	lower := strings.ToLower(input)
	var intent string
	var bubbles []string

	// Simple trigger checks for high reliability
	switch {
	case strings.Contains(lower, "security") || strings.Contains(lower, "lock") || strings.Contains(lower, "alarm"):
		intent = "security"
		bubbles = []string{"Biometric Lock", "Smart Deadbolt", "Door Sensors", "Video Doorbell"}
	case strings.Contains(lower, "design") || strings.Contains(lower, "color") || strings.Contains(lower, "style") || strings.Contains(lower, "look"):
		intent = "design"
		bubbles = []string{"Modern Minimalist", "Classic Wood", "Industrial Steel", "Bold Custom Color"}
	case strings.Contains(lower, "automation") || strings.Contains(lower, "smart") || strings.Contains(lower, "assistant") || strings.Contains(lower, "control"):
		intent = "automation"
		bubbles = []string{"Zigbee Controller", "Apple HomeKit", "Home Assistant", "Voice Prompts"}
	default:
		intent = "general"
		bubbles = []string{"Interactive Guides", "System Diagnostic", "Energy Efficiency", "Material Durability"}
	}

	// Mix in profile elements if any
	for _, tag := range profile.PreferenceTags {
		if len(bubbles) < 6 {
			bubbles = append(bubbles, "Tag: "+tag)
		}
	}

	return intent, bubbles
}

func (pa *PageAgent) generatePageWithLLM(input, intent string, bubbles []string, profile *UserProfile) (PageSchema, error) {
	prompt := fmt.Sprintf(`You are DoorMate, an advanced Page-First AI Assistant. Your task is to output a single, valid, raw JSON object matching the PageSchema.
DO NOT return any markdown wrapping, chat text, or explanation. Only return raw JSON.

Input: %s
Intent: %s
Top Predicted Options: %s
User Preferences: %s

The JSON structure must match this schema:
{
  "title": "Page Title",
  "summary": "Brief summary",
  "template_id": "overview" (or "recommendation", "comparison", "guide"),
  "blocks": [
    {
      "type": "overview" (or "comparison", "list", "chart", "diagram", "timeline", "cards", "gallery", "decision_tree"),
      "title": "Block Title",
      "content": "Paragraph content",
      "items": ["list item 1", "list item 2"],
      "headers": ["column 1", "column 2"],
      "rows": [["row 1 col 1", "row 1 col 2"]],
      "data_points": [{"label": "A", "value": 75}],
      "nodes": [{"id": "n1", "label": "Start", "type": "start"}],
      "edges": [{"from": "n1", "to": "n2", "label": "Yes"}]
    }
  ],
  "follow_ups": ["Next step bubble 1", "Next step bubble 2"]
}

Make sure you include:
1. An elegant title.
2. A clear summary.
3. At least one "list", "cards", or "comparison" block.
4. At least one SVG-ready structural "chart" or flowchart "diagram" block.
`, input, intent, strings.Join(bubbles, ", "), strings.Join(profile.PreferenceTags, ", "))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reply, err := pa.llm.GenerateCtx(ctx, prompt)
	if err != nil {
		return PageSchema{}, err
	}

	// Clean code blocks if returned
	reply = strings.TrimPrefix(reply, "```json")
	reply = strings.TrimPrefix(reply, "```")
	reply = strings.TrimSuffix(reply, "```")
	reply = strings.TrimSpace(reply)

	var schema PageSchema
	if err := json.Unmarshal([]byte(reply), &schema); err != nil {
		return PageSchema{}, err
	}
	return schema, nil
}

func (pa *PageAgent) generateBeautifulMockPage(input, intent string, bubbles []string, profile *UserProfile) PageSchema {
	switch intent {
	case "security":
		return PageSchema{
			Title:      "Smart Lock & Door Security Blueprint",
			Summary:    fmt.Sprintf("Your request '%s' indicates a primary focus on door security. This blueprint outlines biometric deadbolts, remote controls, and automated locking parameters.", input),
			TemplateID: "recommendation",
			Blocks: []Block{
				{
					Type:    "overview",
					Title:   "Advanced Security Assessment",
					Content: "Securing your physical gateway is the first line of defense. Modern biometric authentication (fingerprint + 3D facial scanning) combined with heavy-duty Grade 1 physical deadbolts prevents both manual forced entry and cyber-attacks.",
				},
				{
					Type:  "cards",
					Title: "Top-Rated Biometric Security Options",
					Items: []string{
						"Apex Bio-Lock: Fingerprint + Bluetooth, Grade 1 certified.",
						"Sentinel SmartGuard: Built-in 2K camera with on-device AI facial recognition.",
						"CipherShield Pro: PIN code with anti-peep decoy mode + physical backup key.",
					},
				},
				{
					Type:    "comparison",
					Title:   "Comparison of Secure Entrance Methods",
					Headers: []string{"Feature", "Biometric (Fingerprint)", "Pin Code", "Smart Card / NFC"},
					Rows: [][]string{
						{"Speed", "Under 0.5s", "Moderate (2-3s)", "Instant (0.1s)"},
						{"Security", "Highest (uncloneable)", "High (can be shared)", "Medium (card can be lost)"},
						{"Convenience", "No keys needed", "Needs memory", "Requires card"},
					},
				},
				{
					Type:  "chart",
					Title: "Intrusion Attempt Deterrence Rate by Feature",
					DataPoints: []ChartDataPoint{
						{Label: "Biometric Lock", Value: 98.2},
						{Label: "Video Intercom", Value: 85.5},
						{Label: "Visible Alarm Sensor", Value: 72.0},
						{Label: "Standard Key Lock", Value: 12.0},
					},
				},
				{
					Type:  "diagram",
					Title: "Smart Security Authentication & Lockdown Flow",
					Nodes: []DiagramNode{
						{ID: "start", Label: "User Approaches Door", Type: "start"},
						{ID: "scan", Label: "Scan Biometrics", Type: "action"},
						{ID: "decide", Label: "Is Authorized?", Type: "decision"},
						{ID: "unlock", Label: "Unlock & Welcome", Type: "end"},
						{ID: "alarm", Label: "Lockout & Alert App", Type: "end"},
					},
					Edges: []DiagramEdge{
						{From: "start", To: "scan"},
						{From: "scan", To: "decide"},
						{From: "decide", To: "unlock", Label: "Yes"},
						{From: "decide", To: "alarm", Label: "No (3 attempts)"},
					},
				},
			},
			FollowUps: []string{
				"What happens if internet is cut?",
				"Show lock power consumption.",
				"Integrate video doorbell flow.",
			},
		}
	case "design":
		return PageSchema{
			Title:      "Modern Gateway & Material Design Board",
			Summary:    fmt.Sprintf("Design-first options tailored for '%s'. Highlighting material selection, aesthetic integration, and architectural styles.", input),
			TemplateID: "guide",
			Blocks: []Block{
				{
					Type:    "overview",
					Title:   "Architectural Visual Concept",
					Content: "An entry gateway sets the aesthetic tone of the entire structure. Mixing warm, certified rustic oak with sleek, powder-coated aerospace steel creates a beautiful, mid-century modern look that is both inviting and structurally superior.",
				},
				{
					Type:  "cards",
					Title: "Aesthetic Inspiration Elements",
					Items: []string{
						"Warm Timber Finish: Sustainable red cedar.",
						"Anodized Charcoal Framework: Non-reflective matte texture.",
						"Flush Biometric Bezel: Integrated directly into the organic grain.",
					},
				},
				{
					Type:  "chart",
					Title: "Visual Preference Ranking (Architect Polls)",
					DataPoints: []ChartDataPoint{
						{Label: "Mid-Century Modern", Value: 42},
						{Label: "Industrial Minimalist", Value: 28},
						{Label: "Rustic Heritage", Value: 18},
						{Label: "Art Deco Chic", Value: 12},
					},
				},
			},
			FollowUps: []string{
				"Explore cedar wood options.",
				"What are custom colors available?",
				"Show me minimal smart bezels.",
			},
		}
	default:
		return PageSchema{
			Title:      "Personalized Gateway Consultation Overview",
			Summary:    fmt.Sprintf("Welcome to DoorMate. Custom analysis generated for your query: '%s'.", input),
			TemplateID: "overview",
			Blocks: []Block{
				{
					Type:    "overview",
					Title:   "Seamless AI Navigation Layer",
					Content: "DoorMate operates as a page-first agent. It predicts your requirements and designs interactive schemas rather than engaging in verbose chat, helping you configure, visualize, and command your Smart Gateway quickly.",
				},
				{
					Type:  "list",
					Title: "Your Gateway Options Roadmap",
					Items: []string{
						"Analyze structural material parameters and environmental resistance.",
						"Configure real-time monitoring and event-driven camera feeds.",
						"Design custom smart alerts and profile-based guest keys.",
					},
				},
			},
			FollowUps: []string{
				"Configure door security locks.",
				"Browse visual material styles.",
				"How does dynamic profile learning work?",
			},
		}
	}
}
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test ./internal/doormate/...`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git add internal/doormate/agent.go internal/doormate/agent_test.go
git commit -m "feat(doormate): implement PageAgent and mock template generation engine"
```

---

## Chunk 3: REST API Handlers & Routing Integration

This chunk exposes the system as fully secure HTTP endpoints, integrated with the dashboard's CSRF/session middleware, and updates the OpenAPI schema using actual platform-compliant syntax.

### Task 4: Handler Actions
**Files:**
- Create: `internal/doormate/handlers.go`
- Create: `internal/doormate/handlers_test.go`

- [ ] **Step 1: Write handler tests**
```go
package doormate

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleIntentREST(t *testing.T) {
	storeDir := t.TempDir()
	store, _ := NewStore(storeDir)
	agent := NewPageAgent(nil)

	handler := NewHandler(store, agent)

	reqBody, _ := json.Marshal(map[string]string{"input": "lock security"})
	req := httptest.NewRequest("POST", "/api/doormate/intent", bytes.NewReader(reqBody))
	rr := httptest.NewRecorder()

	handler.HandleIntent(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}

	var res map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if res["intent"] != "security" {
		t.Errorf("expected security intent, got %v", res["intent"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**
Run: `go test ./internal/doormate/...`
Expected: FAIL due to missing Handler code.

- [ ] **Step 3: Implement Handler actions**
```go
package doormate

import (
	"encoding/json"
	"net/http"
	"strings"
)

type Handler struct {
	store *Store
	agent *PageAgent
}

func NewHandler(store *Store, agent *PageAgent) *Handler {
	return &Handler{store: store, agent: agent}
}

func (h *Handler) getProfile(r *http.Request) *UserProfile {
	// Simple identifier, expandable via sessions
	profile, err := h.store.LoadProfile("default_user")
	if err != nil {
		return &UserProfile{ID: "default_user", PreferenceTags: []string{}, PreferredStyle: "visual"}
	}
	return profile
}

func (h *Handler) HandleIntent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	profile := h.getProfile(r)
	sess, page, err := h.agent.Process(body.Input, profile)
	if err != nil {
		http.Error(w, "generation failed", http.StatusInternalServerError)
		return
	}

	_ = h.store.SaveSession(sess)
	_ = h.store.SavePage(page)

	// Lightweight profile update based on intent
	hasTag := false
	for _, t := range profile.PreferenceTags {
		if t == sess.Intent {
			hasTag = true
			break
		}
	}
	if !hasTag && sess.Intent != "general" {
		profile.PreferenceTags = append(profile.PreferenceTags, sess.Intent)
		_ = h.store.SaveProfile(profile)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"session_id": sess.ID,
		"intent":     sess.Intent,
		"bubbles":    sess.Bubbles,
		"page":       page,
		"profile":    profile,
	})
}

func (h *Handler) HandleBookmark(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		PageID string `json:"page_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	page, err := h.store.LoadPage(body.PageID)
	if err != nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}

	page.Bookmarked = !page.Bookmarked
	_ = h.store.SavePage(page)

	profile := h.getProfile(r)
	if page.Bookmarked {
		profile.BookmarkIDs = append(profile.BookmarkIDs, page.ID)
		_ = h.store.LogFeedback(&FeedbackEvent{Type: "bookmark", Value: page.ID, PageID: page.ID})
	} else {
		newList := []string{}
		for _, b := range profile.BookmarkIDs {
			if b != page.ID {
				newList = append(newList, b)
			}
		}
		profile.BookmarkIDs = newList
		_ = h.store.LogFeedback(&FeedbackEvent{Type: "unbookmark", Value: page.ID, PageID: page.ID})
	}
	_ = h.store.SaveProfile(profile)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "bookmarked": page.Bookmarked})
}

func (h *Handler) HandleRate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		PageID string `json:"page_id"`
		Rating int    `json:"rating"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	page, err := h.store.LoadPage(body.PageID)
	if err != nil {
		http.Error(w, "page not found", http.StatusNotFound)
		return
	}

	page.Rating = body.Rating
	_ = h.store.SavePage(page)

	_ = h.store.LogFeedback(&FeedbackEvent{Type: "rate", Value: fmt.Sprintf("%d", body.Rating), PageID: page.ID})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"status": "success", "rating": page.Rating})
}

func (h *Handler) HandleProfile(w http.ResponseWriter, r *http.Request) {
	profile := h.getProfile(r)

	if r.Method == http.MethodPost {
		var body struct {
			Tags  []string `json:"tags"`
			Style string   `json:"style"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			profile.PreferenceTags = body.Tags
			profile.PreferredStyle = body.Style
			_ = h.store.SaveProfile(profile)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(profile)
}
```

- [ ] **Step 4: Run test to verify it passes**
Run: `go test ./internal/doormate/...`
Expected: PASS

- [ ] **Step 5: Commit**
```bash
git add internal/doormate/handlers.go internal/doormate/handlers_test.go
git commit -m "feat(doormate): implement handler endpoints for intents, pages, bookmarks"
```

---

### Task 5: Routing Registration
We will plug the handlers into the central web server.

**Files:**
- Modify: `cmd/bt-dashboard/main.go`
- Modify: `internal/api/openapi.go`

- [ ] **Step 1: Write integration compilation confirmation**
Make sure Go can build with no linter errors.

- [ ] **Step 2: Add Handlers and routes to `cmd/bt-dashboard/main.go`**
Search for references to `sharedLLM` or `InitStore` in `cmd/bt-dashboard/main.go` to locate registration context.
Modify `cmd/bt-dashboard/main.go`:
```go
// Add import:
// "github.com/nico/go-bt-evolve/internal/doormate"

// Under OTLP/LLM/Session configuration initialization, setup DoorMate package:
home, _ := os.UserHomeDir()
doormateStore, err := doormate.NewStore(filepath.Join(home, ".go-bt-evolve", "doormate"))
if err != nil {
	slog.Error("Failed to initialize DoorMate storage", "error", err)
}
doormateAgent := doormate.NewPageAgent(sharedLLM)
doormateHandler := doormate.NewHandler(doormateStore, doormateAgent)

// Register endpoints using the existing sessionAuth wrapping:
mux.HandleFunc("/api/doormate/intent", sessionAuth(doormateHandler.HandleIntent))
mux.HandleFunc("/api/doormate/bookmark", sessionAuth(doormateHandler.HandleBookmark))
mux.HandleFunc("/api/doormate/rate", sessionAuth(doormateHandler.HandleRate))
mux.HandleFunc("/api/doormate/profile", sessionAuth(doormateHandler.HandleProfile))
```

- [ ] **Step 3: Document Routes in `internal/api/openapi.go`**
Register the DoorMate routes inside `openapi.go`'s schema generator using standard `NewRoute` specifications.
Modify `internal/api/openapi.go` in `DashboardRoutes()` (add these routes before returning the routes list):
```go
		NewRoute("/api/doormate/intent", POST).
			Summary("Submit user query or bubble click to DoorMate Page Agent").
			Tags("DoorMate").
			OperationID("handleDoorMateIntent").
			JSONResponse(200, "Intent analyzed, predicted options returned along with a fully populated Page Schema object", ObjectSchema(map[string]*Schema{
				"session_id": StringSchema("Session ID"),
				"intent":     StringSchema("Analyzed intent"),
				"bubbles":    ArraySchema(StringSchema("Predicted bubbles"), "Predicted bubbles"),
				"page":       ObjectSchema(map[string]*Schema{}),
				"profile":    ObjectSchema(map[string]*Schema{}),
			})).Build(),

		NewRoute("/api/doormate/bookmark", POST).
			Summary("Toggle bookmark state of a DoorMate page").
			Tags("DoorMate").
			OperationID("handleDoorMateBookmark").
			JSONResponse(200, "Success confirmation and state", ObjectSchema(map[string]*Schema{
				"status":     StringSchema("success"),
				"bookmarked": BoolSchema("New bookmark state"),
			})).Build(),

		NewRoute("/api/doormate/rate", POST).
			Summary("Submit feedback rating for a DoorMate page").
			Tags("DoorMate").
			OperationID("handleDoorMateRate").
			JSONResponse(200, "Success confirmation", ObjectSchema(map[string]*Schema{
				"status": StringSchema("success"),
				"rating": IntSchema("Submitted rating"),
			})).Build(),

		NewRoute("/api/doormate/profile", GET).
			Summary("Retrieve personalized DoorMate user profile").
			Tags("DoorMate").
			OperationID("handleDoorMateGetProfile").
			JSONResponse(200, "User preference profile data", ObjectSchema(map[string]*Schema{
				"id":              StringSchema("User ID"),
				"preference_tags": ArraySchema(StringSchema("Preference tag"), "Tags"),
				"bookmark_ids":    ArraySchema(StringSchema("Page ID"), "Bookmark IDs"),
				"preferred_style": StringSchema("Interface style preference"),
			})).Build(),
```

- [ ] **Step 4: Verify Compilation**
Run: `go build ./cmd/bt-dashboard`
Expected: Compile success without error.

- [ ] **Step 5: Commit**
```bash
git add cmd/bt-dashboard/main.go internal/api/openapi.go
git commit -m "feat(doormate): register rest routes and document in openapi schema"
```

---

## Chunk 4: Page-First Web Interface & Page Template Library

We will write the complete JS code for the "DoorMate" tab. It implements the "Intent Canvas" visual loop, Predicted Bubble buttons, a simulated voice/video indicator, and a complete dynamic HTML rendering library for all required template layouts following the non-module, sequentially loaded script standards of the codebase.

### Task 6: Tab Layout & JS Template Renderer
**Files:**
- Create: `cmd/bt-dashboard/static/js/tabs/doormate.js`
- Modify: `cmd/bt-dashboard/static/js/app.js`
- Modify: `cmd/bt-dashboard/static/index.html`
- Modify: `cmd/bt-dashboard/static/css/base.css`

- [ ] **Step 1: Create `cmd/bt-dashboard/static/js/tabs/doormate.js`**
Write the complete frontend file defining the DoorMate tab layout and page template library.
```javascript
// DoorMate Page-First AI Assistant Tab Controller & Rendering Library
// Globally loaded sequential script. No ES module import or export.

let activePageID = null;

function renderDoormate() {
  return `
    <div class="doormate-layout">
      <!-- Left Column: Intent Canvas & Interactive Loop -->
      <div class="intent-column">
        <div class="intent-canvas-box">
          <div class="intent-canvas-bg" id="doormate-canvas-bg"></div>
          <div class="intent-canvas-content">
            <h2 class="canvas-title">DoorMate Gateway Assistant</h2>
            <p class="canvas-subtitle">Personalized AI Page Design</p>
            
            <!-- Voice & Video Interface Indicator -->
            <div class="av-interface-row">
              <button class="av-btn" id="doormate-mic-btn" title="Toggle Voice Interaction">
                <span class="av-icon">🎙️</span>
                <span class="av-text" id="mic-status">Voice Idle</span>
              </button>
              <button class="av-btn" id="doormate-cam-btn" title="Toggle Video Calibration">
                <span class="av-icon">📷</span>
                <span class="av-text" id="cam-status">Video Idle</span>
              </button>
            </div>

            <!-- Predicted Interactive Bubbles -->
            <div class="bubble-container" id="predicted-bubbles">
              <span class="bubble-placeholder-text">Enter a request to bootstrap your gateway.</span>
            </div>

            <!-- Input / Phrase Area (WhatsApp Style Send) -->
            <div class="whatsapp-send-bar">
              <input type="text" id="doormate-input" placeholder="Type a concept, design style, or security standard..." />
              <button id="doormate-send-btn" title="Send Request">
                <svg viewBox="0 0 24 24" width="20" height="20">
                  <path fill="currentColor" d="M2,21L23,12L2,3V10L17,12L2,14V21Z" />
                </svg>
              </button>
            </div>
          </div>
        </div>

        <!-- Personalization & Personal Profile Tags -->
        <div class="profile-box">
          <h3>Your Personalization Signals</h3>
          <div class="profile-tags-container" id="doormate-profile-tags">
            <!-- Dynamically populated preference tags -->
          </div>
        </div>
      </div>

      <!-- Right Column: Structured Generated Page Workspace -->
      <div class="page-workspace" id="doormate-workspace">
        <div class="workspace-empty-state">
          <div class="empty-icon">🚪</div>
          <h4>No Generated Page Blueprint Active</h4>
          <p>Provide a gateway phrase or click an interactive bubble on the left to dynamically compile a structured web page response.</p>
        </div>
      </div>
    </div>
  `;
}

function initDoormateTab() {
  const sendBtn = document.getElementById('doormate-send-btn');
  const inputEl = document.getElementById('doormate-input');
  if (!sendBtn || !inputEl) return;

  sendBtn.addEventListener('click', () => sendPhrase());
  inputEl.addEventListener('keydown', (e) => {
    if (e.key === 'Enter') sendPhrase();
  });

  // AV Simulation
  const micBtn = document.getElementById('doormate-mic-btn');
  const camBtn = document.getElementById('doormate-cam-btn');
  let micActive = false;
  let camActive = false;

  if (micBtn && camBtn) {
    micBtn.addEventListener('click', () => {
      micActive = !micActive;
      micBtn.classList.toggle('active', micActive);
      document.getElementById('mic-status').textContent = micActive ? "Listening..." : "Voice Idle";
    });

    camBtn.addEventListener('click', () => {
      camActive = !camActive;
      camBtn.classList.toggle('active', camActive);
      document.getElementById('cam-status').textContent = camActive ? "Calibrating..." : "Video Idle";
    });
  }

  // Initial Profile Load
  loadProfile();
}

async function loadProfile() {
  try {
    const res = await apiFetch('/api/doormate/profile');
    if (res) renderProfileTags(res.preference_tags || []);
  } catch (err) {
    console.error('load profile error', err);
  }
}

function renderProfileTags(tags) {
  const container = document.getElementById('doormate-profile-tags');
  if (!container) return;
  if (tags.length === 0) {
    container.innerHTML = `<span class="no-tags-notice">No signals learned yet. Submit security or design intents to bootstrap learning.</span>`;
    return;
  }
  container.innerHTML = tags.map(tag => `<span class="profile-tag-chip">✓ ${tag}</span>`).join('');
}

async function sendPhrase(customText = null) {
  const inputEl = document.getElementById('doormate-input');
  const text = customText || (inputEl ? inputEl.value.trim() : "");
  if (!text) return;

  if (!customText && inputEl) inputEl.value = '';

  const workspace = document.getElementById('doormate-workspace');
  if (workspace) {
    workspace.innerHTML = `
      <div class="workspace-loading">
        <div class="spinner"></div>
        <p>Compiling structured page from template library...</p>
      </div>
    `;
  }

  try {
    const res = await apiFetch('/api/doormate/intent', {
      method: 'POST',
      body: JSON.stringify({ input: text })
    });

    if (res && res.page) {
      activePageID = res.page.id;
      renderBubbles(res.bubbles || []);
      renderProfileTags(res.profile ? res.profile.preference_tags : []);
      renderGeneratedPage(res.page);
    } else {
      if (workspace) workspace.innerHTML = `<div class="render-error">Error compiling template.</div>`;
    }
  } catch (err) {
    if (workspace) workspace.innerHTML = `<div class="render-error">Service exception: ${err.message}</div>`;
  }
}

function renderBubbles(bubbles) {
  const container = document.getElementById('predicted-bubbles');
  if (!container) return;
  container.innerHTML = bubbles.map(b => `
    <button class="predicted-bubble-btn" data-bubble="${b}">
      ${b}
    </button>
  `).join('');

  // Register bubble click actions to simulate sequential refinement
  container.querySelectorAll('.predicted-bubble-btn').forEach(btn => {
    btn.addEventListener('click', () => {
      sendPhrase(btn.getAttribute('data-bubble'));
    });
  });
}

// Complete Page Template Rendering Library
function renderGeneratedPage(page) {
  const container = document.getElementById('doormate-workspace');
  if (!container) return;

  const schema = page.schema;
  const isBookmarked = page.bookmarked;
  const rating = page.rating || 0;

  let pageContent = `
    <div class="generated-page-container theme-${schema.template_id}">
      <!-- Page Header & Action Controls -->
      <div class="generated-page-header">
        <div class="title-section">
          <span class="template-label-badge">${schema.template_id.toUpperCase()} TEMPLATE</span>
          <h2>${schema.title}</h2>
          <p class="summary-paragraph">${schema.summary}</p>
        </div>
        <div class="header-action-controls">
          <button class="page-action-btn ${isBookmarked ? 'active' : ''}" id="btn-bookmark" title="Bookmark Board">
            ${isBookmarked ? '★ Bookmarked' : '☆ Bookmark'}
          </button>
          
          <!-- Rating System -->
          <div class="page-rating-row">
            ${[1, 2, 3, 4, 5].map(star => `
              <span class="star-rating-icon ${star <= rating ? 'active' : ''}" data-star="${star}">★</span>
            `).join('')}
          </div>
        </div>
      </div>

      <!-- Dynamic Content Blocks Section -->
      <div class="generated-page-body">
        ${schema.blocks.map(block => renderBlock(block)).join('')}
      </div>

      <!-- Follow-up Bubble Actions (Implicit Rail) -->
      ${schema.follow_ups && schema.follow_ups.length > 0 ? `
        <div class="follow-up-actions-rail">
          <h4>Continuous Interaction Follow-ups:</h4>
          <div class="follow-up-bubbles-row">
            ${schema.follow_ups.map(f => `
              <button class="follow-up-bubble-pill" data-phrase="${f}">
                ➔ ${f}
              </button>
            `).join('')}
          </div>
        </div>
      ` : ''}
    </div>
  `;

  container.innerHTML = pageContent;

  // Setup interaction event handlers
  document.getElementById('btn-bookmark').addEventListener('click', () => toggleBookmark());
  container.querySelectorAll('.star-rating-icon').forEach(icon => {
    icon.addEventListener('click', () => {
      const star = parseInt(icon.getAttribute('data-star'));
      ratePage(star);
    });
  });
  container.querySelectorAll('.follow-up-bubble-pill').forEach(pill => {
    pill.addEventListener('click', () => {
      sendPhrase(pill.getAttribute('data-phrase'));
    });
  });

  // Dynamic canvas-bg change matching template choice
  const canvasBg = document.getElementById('doormate-canvas-bg');
  if (canvasBg) {
    if (schema.template_id === 'recommendation') {
      canvasBg.className = "intent-canvas-bg bg-security";
    } else if (schema.template_id === 'guide') {
      canvasBg.className = "intent-canvas-bg bg-design";
    } else {
      canvasBg.className = "intent-canvas-bg bg-general";
    }
  }
}

// Block Renderer matching standard schemas
function renderBlock(block) {
  switch (block.type) {
    case 'overview':
      return `
        <div class="rendered-block block-overview">
          <h3>${block.title || 'Summary Details'}</h3>
          <p>${block.content || ''}</p>
        </div>
      `;
    case 'cards':
    case 'gallery':
      return `
        <div class="rendered-block block-cards">
          <h3>${block.title || 'Options Blueprint'}</h3>
          <div class="cards-grid">
            ${(block.items || []).map(item => `
              <div class="ui-blueprint-card">
                <div class="card-indicator-node"></div>
                <p>${item}</p>
              </div>
            `).join('')}
          </div>
        </div>
      `;
    case 'list':
      return `
        <div class="rendered-block block-list">
          <h3>${block.title || 'Implementation Tasks'}</h3>
          <ul class="blueprint-list">
            ${(block.items || []).map(item => `<li><span class="bullet-indicator">▪</span> ${item}</li>`).join('')}
          </ul>
        </div>
      `;
    case 'comparison':
      return `
        <div class="rendered-block block-comparison">
          <h3>${block.title || 'Structured Comparison Matrix'}</h3>
          <div class="comparison-table-wrapper">
            <table>
              <thead>
                <tr>
                  ${(block.headers || []).map(h => `<th>${h}</th>`).join('')}
                </tr>
              </thead>
              <tbody>
                ${(block.rows || []).map(row => `
                  <tr>
                    ${row.map(cell => `<td>${cell}</td>`).join('')}
                  </tr>
                `).join('')}
              </tbody>
            </table>
          </div>
        </div>
      `;
    case 'chart':
      const maxVal = Math.max(...(block.data_points || []).map(dp => dp.value), 100);
      return `
        <div class="rendered-block block-chart">
          <h3>${block.title || 'Performance Metrics'}</h3>
          <div class="svg-chart-container">
            <svg viewBox="0 0 500 200" width="100%" height="100%">
              <!-- Bar chart -->
              ${(block.data_points || []).map((dp, i) => {
                const x = 50 + (i * 110);
                const height = (dp.value / maxVal) * 140;
                const y = 170 - height;
                return `
                  <rect x="${x}" y="${y}" width="60" height="${height}" rx="4" fill="var(--accent)" />
                  <text x="${x + 30}" y="190" text-anchor="middle" font-size="10" fill="var(--text-secondary)">${dp.label}</text>
                  <text x="${x + 30}" y="${y - 8}" text-anchor="middle" font-size="11" font-weight="bold" fill="var(--text-primary)">${dp.value}%</text>
                `;
              }).join('')}
              <line x1="30" y1="170" x2="480" y2="170" stroke="var(--border-standard)" stroke-width="2" />
            </svg>
          </div>
        </div>
      `;
    case 'diagram':
      return `
        <div class="rendered-block block-diagram">
          <h3>${block.title || 'Process Workflow Logic'}</h3>
          <div class="svg-diagram-container">
            <svg viewBox="0 0 600 150" width="100%" height="100%">
              <!-- Quick horizontal node-link flow -->
              ${(block.nodes || []).map((node, i) => {
                const x = 60 + (i * 120);
                const y = 60;
                let color = "var(--bg-surface)";
                let stroke = "var(--border-standard)";
                if (node.type === 'start') { color = "rgba(40,167,69,0.15)"; stroke = "#27a644"; }
                else if (node.type === 'decision') { color = "rgba(255,193,7,0.15)"; stroke = "#f59e0b"; }
                else if (node.type === 'end') { color = "rgba(0,123,255,0.15)"; stroke = "#3b82f6"; }
                return `
                  <rect x="${x}" y="${y}" width="90" height="40" rx="6" fill="${color}" stroke="${stroke}" stroke-width="2"/>
                  <text x="${x + 45}" y="${y + 24}" text-anchor="middle" font-size="9" fill="var(--text-primary)">${node.label}</text>
                `;
              }).join('')}
              
              <!-- Draw simplistic link lines between sequential elements -->
              ${(block.edges || []).map((edge, i) => {
                // Find node indices
                const fromIndex = (block.nodes || []).findIndex(n => n.id === edge.from);
                const toIndex = (block.nodes || []).findIndex(n => n.id === edge.to);
                if (fromIndex === -1 || toIndex === -1) return '';
                const x1 = 150 + (fromIndex * 120);
                const x2 = 60 + (toIndex * 120);
                const y = 80;
                return `
                  <line x1="${x1}" y1="${y}" x2="${x2}" y2="${y}" stroke="var(--text-tertiary)" stroke-width="1.5" stroke-dasharray="4,4" />
                  <polygon points="${x2},${y} ${x2-5},${y-3} ${x2-5},${y+3}" fill="var(--text-tertiary)"/>
                  ${edge.label ? `<text x="${(x1+x2)/2}" y="${y - 4}" text-anchor="middle" font-size="8" fill="var(--text-tertiary)">${edge.label}</text>` : ''}
                `;
              }).join('')}
            </svg>
          </div>
        </div>
      `;
    default:
      return '';
  }
}

async function toggleBookmark() {
  if (!activePageID) return;
  try {
    const res = await apiFetch('/api/doormate/bookmark', {
      method: 'POST',
      body: JSON.stringify({ page_id: activePageID })
    });
    if (res && res.status === "success") {
      const btn = document.getElementById('btn-bookmark');
      if (res.bookmarked) {
        btn.classList.add('active');
        btn.textContent = '★ Bookmarked';
      } else {
        btn.classList.remove('active');
        btn.textContent = '☆ Bookmark';
      }
    }
  } catch (err) {
    console.error('bookmark toggle failed', err);
  }
}

async function ratePage(starCount) {
  if (!activePageID) return;
  try {
    const res = await apiFetch('/api/doormate/rate', {
      method: 'POST',
      body: JSON.stringify({ page_id: activePageID, rating: starCount })
    });
    if (res && res.status === "success") {
      const starIcons = document.querySelectorAll('.star-rating-icon');
      starIcons.forEach(icon => {
        const index = parseInt(icon.getAttribute('data-star'));
        if (index <= starCount) {
          icon.classList.add('active');
        } else {
          icon.classList.remove('active');
        }
      });
    }
  } catch (err) {
    console.error('rating submission failed', err);
  }
}
```

- [ ] **Step 2: Modify `cmd/bt-dashboard/static/js/app.js` to register tab**
Modify `cmd/bt-dashboard/static/js/app.js` to append `'doormate'` as the last tab item and call the initialization routine.
```javascript
// Find the TAB_KEYS array and append 'doormate' at the end:
const TAB_KEYS = ['overview', 'thinktank', 'company', 'tasks', 'trees', 'mindmap', 'evolution', 'traces', 'agents', 'scalability', 'doormate'];

// In renderTab(tab) function, add the case for doormate:
    case 'scalability': main.innerHTML = renderScalability(); break;
    case 'doormate': main.innerHTML = renderDoormate(); setTimeout(initDoormateTab, 100); break;
```

- [ ] **Step 3: Modify `cmd/bt-dashboard/static/index.html` to add script and nav items**
Modify `cmd/bt-dashboard/static/index.html` to load our script sequentially and add navigation button:
```html
<!-- Inside <div class="nav-links"> after scalability button: -->
      <button class="nav-item" data-tab="doormate"><span class="icon">🚪</span> DoorMate</button>

<!-- Inside script loadings, before app.js: -->
<script src="/static/js/tabs/doormate.js"></script>
<script src="/static/js/app.js"></script>
```

- [ ] **Step 4: Append styles to `cmd/bt-dashboard/static/css/base.css`**
Append styles directly to support DoorMate columns, canvas box, predicted bubbles, template classes:
```css
/* ─── DoorMate Layout ─── */
.doormate-layout {
  display: grid;
  grid-template-columns: 350px 1fr;
  gap: var(--space-4);
  height: calc(100vh - 120px);
  overflow: hidden;
}

.intent-column {
  display: flex;
  flex-direction: column;
  gap: var(--space-4);
  height: 100%;
}

.intent-canvas-box {
  position: relative;
  border: 1px solid var(--border-standard);
  border-radius: var(--radius-lg);
  overflow: hidden;
  height: 400px;
  background: var(--bg-panel);
}

.intent-canvas-bg {
  position: absolute;
  top: 0; left: 0; right: 0; bottom: 0;
  opacity: 0.15;
  background-size: cover;
  background-position: center;
  transition: background 0.5s ease;
}

.intent-canvas-bg.bg-security { background-image: url('https://images.unsplash.com/photo-1558002038-1055907df827?auto=format&fit=crop&w=400&q=80'); }
.intent-canvas-bg.bg-design { background-image: url('https://images.unsplash.com/photo-1513694203232-719a280e022f?auto=format&fit=crop&w=400&q=80'); }
.intent-canvas-bg.bg-general { background-image: url('https://images.unsplash.com/photo-1486406146926-c627a92ad1ab?auto=format&fit=crop&w=400&q=80'); }

.intent-canvas-content {
  position: relative;
  z-index: 2;
  padding: var(--space-4);
  display: flex;
  flex-direction: column;
  height: 100%;
  justify-content: space-between;
}

.canvas-title { font-size: 16px; margin: 0; color: var(--text-primary); }
.canvas-subtitle { font-size: 12px; margin: 0; color: var(--text-tertiary); }

.av-interface-row {
  display: flex;
  gap: var(--space-2);
  margin-top: var(--space-2);
}

.av-btn {
  flex: 1;
  background: var(--bg-surface);
  border: 1px solid var(--border-standard);
  color: var(--text-secondary);
  padding: var(--space-2);
  border-radius: var(--radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  cursor: pointer;
  font-size: 11px;
}

.av-btn.active {
  border-color: var(--red);
  background: rgba(239, 68, 68, 0.1);
  color: var(--text-primary);
}

.bubble-container {
  flex: 1;
  margin: var(--space-4) 0;
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-2);
  align-content: flex-start;
  overflow-y: auto;
}

.bubble-placeholder-text { font-size: 11px; color: var(--text-quaternary); text-align: center; width: 100%; margin-top: var(--space-6); }

.predicted-bubble-btn {
  background: rgba(255,255,255,0.03);
  border: 1px solid var(--border-subtle);
  color: var(--text-secondary);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-full);
  font-size: 11px;
  cursor: pointer;
  transition: all 0.2s ease;
}

.predicted-bubble-btn:hover {
  background: var(--accent-bg);
  border-color: var(--accent);
  color: var(--text-primary);
}

.whatsapp-send-bar {
  display: flex;
  gap: var(--space-2);
  background: var(--bg-surface);
  border-radius: var(--radius-md);
  padding: var(--space-2);
  border: 1px solid var(--border-standard);
}

.whatsapp-send-bar input {
  flex: 1;
  background: transparent;
  border: none;
  color: var(--text-primary);
  font-size: 12px;
  outline: none;
}

.whatsapp-send-bar button {
  background: var(--accent-bg);
  border: none;
  color: var(--text-primary);
  width: 32px;
  height: 32px;
  border-radius: var(--radius-md);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.profile-box {
  border: 1px solid var(--border-standard);
  border-radius: var(--radius-lg);
  padding: var(--space-4);
  background: var(--bg-panel);
}

.profile-box h3 { font-size: 12px; margin: 0 0 var(--space-2) 0; color: var(--text-primary); }

.profile-tags-container {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-1);
}

.profile-tag-chip {
  background: rgba(16, 185, 129, 0.1);
  color: var(--green-bright);
  font-size: 10px;
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
}

.no-tags-notice { font-size: 10px; color: var(--text-quaternary); }

/* Workspace & Renderer Styling */
.page-workspace {
  border: 1px solid var(--border-standard);
  border-radius: var(--radius-lg);
  background: var(--bg-panel);
  overflow-y: auto;
  padding: var(--space-5);
  height: 100%;
}

.workspace-empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: var(--text-tertiary);
  text-align: center;
}

.empty-icon { font-size: 40px; margin-bottom: var(--space-3); }

.workspace-loading {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
}

.spinner {
  width: 24px;
  height: 24px;
  border: 2px solid var(--border-standard);
  border-top-color: var(--accent);
  border-radius: 50%;
  animation: doormate-spin 0.8s linear infinite;
  margin-bottom: var(--space-3);
}

@keyframes doormate-spin { to { transform: rotate(360deg); } }

.generated-page-container {
  display: flex;
  flex-direction: column;
  gap: var(--space-5);
}

.generated-page-header {
  display: flex;
  justify-content: space-between;
  border-bottom: 1px solid var(--border-standard);
  padding-bottom: var(--space-4);
}

.template-label-badge {
  font-size: 9px;
  background: var(--accent-bg);
  color: var(--text-primary);
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-sm);
  font-weight: bold;
}

.header-action-controls {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: var(--space-2);
}

.page-action-btn {
  background: var(--bg-surface);
  border: 1px solid var(--border-standard);
  color: var(--text-secondary);
  padding: var(--space-2) var(--space-4);
  border-radius: var(--radius-md);
  cursor: pointer;
}

.page-action-btn.active {
  background: var(--accent-bg);
  border-color: var(--accent);
  color: var(--text-primary);
}

.page-rating-row {
  display: flex;
  gap: var(--space-1);
}

.star-rating-icon {
  font-size: 16px;
  color: var(--text-quaternary);
  cursor: pointer;
}

.star-rating-icon.active { color: var(--amber); }

.rendered-block {
  background: var(--bg-surface);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  padding: var(--space-4);
  margin-bottom: var(--space-4);
}

.rendered-block h3 { font-size: 13px; margin: 0 0 var(--space-3) 0; color: var(--text-primary); }

.cards-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: var(--space-3);
}

.ui-blueprint-card {
  background: rgba(255,255,255,0.01);
  border: 1px solid var(--border-standard);
  border-radius: var(--radius-md);
  padding: var(--space-3);
  position: relative;
}

.card-indicator-node {
  position: absolute;
  top: var(--space-3); right: var(--space-3);
  width: 6px; height: 6px;
  border-radius: 50%;
  background: var(--accent);
}

.blueprint-list { list-style: none; padding: 0; margin: 0; }
.blueprint-list li { font-size: 12px; margin-bottom: var(--space-2); display: flex; align-items: center; gap: var(--space-2); }
.bullet-indicator { color: var(--accent); }

.comparison-table-wrapper { overflow-x: auto; }
.comparison-table-wrapper table { width: 100%; border-collapse: collapse; text-align: left; }
.comparison-table-wrapper th, .comparison-table-wrapper td { padding: var(--space-2) var(--space-3); border-bottom: 1px solid var(--border-subtle); font-size: 11px; }
.comparison-table-wrapper th { color: var(--text-tertiary); font-weight: bold; }

.follow-up-actions-rail {
  border-top: 1px solid var(--border-standard);
  padding-top: var(--space-4);
}

.follow-up-actions-rail h4 { font-size: 11px; color: var(--text-tertiary); margin: 0 0 var(--space-2) 0; }
.follow-up-bubbles-row { display: flex; flex-wrap: wrap; gap: var(--space-2); }

.follow-up-bubble-pill {
  background: rgba(255,255,255,0.02);
  border: 1px solid var(--border-standard);
  color: var(--text-primary);
  border-radius: var(--radius-full);
  font-size: 11px;
  padding: var(--space-2) var(--space-4);
  cursor: pointer;
  transition: all 0.2s ease;
}

.follow-up-bubble-pill:hover {
  background: var(--accent-hover);
  color: var(--text-primary);
}
```

- [ ] **Step 5: Commit**
```bash
git add cmd/bt-dashboard/static/js/tabs/doormate.js cmd/bt-dashboard/static/js/app.js cmd/bt-dashboard/static/index.html cmd/bt-dashboard/static/css/base.css
git commit -m "feat(doormate): create beautiful frontend and template rendering engine"
```

---

## Chunk 5: E2E Playwright Verification

We will run E2E testing using Playwright to navigate to the new tab, perform query inputs, trigger predicted bubbles, and click ratings/bookmarks.

### Task 7: E2E Visual Verification Run
**Files:**
- Create: `tests/e2e/doormate_test.js`

- [ ] **Step 1: Write Playwright tests**
```javascript
// Verify that the DoorMate tab exists, responds to clicks, and loads beautiful templates.
const { test, expect } = require('@playwright/test');

test('DoorMate Tab Workflow Verification', async ({ page }) => {
  await page.goto('http://localhost:9800');
  
  // Click on the DoorMate tab (the last tab element)
  await page.click('[data-tab="doormate"]');
  
  // Verify canvas presence
  await expect(page.locator('.intent-canvas-box')).toBeVisible();
  
  // Enter intent in input
  await page.fill('#doormate-input', 'advanced fingerprint door lock');
  await page.click('#doormate-send-btn');
  
  // Verify workspace changes from empty state to loading/rendered page
  await page.waitForTimeout(1000);
  await expect(page.locator('.generated-page-container')).toBeVisible();
  
  // Test bookmark button toggle
  await page.click('#btn-bookmark');
  await page.waitForTimeout(500);
  
  // Capture snapshot for verification
  await page.screenshot({ path: '.playwright-mcp/doormate_snapshot.png' });
});
```

- [ ] **Step 2: Run test to verify it succeeds**
Run: `npx playwright test tests/e2e/doormate_test.js` (or trigger via Playwright MCP actions).
Expected: PASS and produces verification screenshot.

- [ ] **Step 3: Commit**
```bash
git add tests/e2e/doormate_test.js
git commit -m "test(doormate): implement Playwright integration test"
```

---

## Execution Checklist & Completion

Once Chunk 5 is completed, execute verification sweeps using the `verification-before-completion` skill rules:
1. Run linter and tests for compilation safety.
2. Confirm live routes respond correctly.
3. Deliver the final PR/diff to the user.
