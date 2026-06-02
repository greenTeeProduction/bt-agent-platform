package a2a

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/nico/go-bt-evolve/internal/evolution"
)

// TestBTAgentClient_New verifies the client constructor.
func TestBTAgentClient_New(t *testing.T) {
	c := NewBTAgentClient()
	if c == nil {
		t.Fatal("NewBTAgentClient returned nil")
	}
	if c.Timeout != 120*time.Second {
		t.Errorf("expected default timeout 120s, got %v", c.Timeout)
	}
}

// TestBTAgentClient_SendTask_NoServer tests that SendTask fails gracefully
// when the target URL is unreachable.
func TestBTAgentClient_SendTask_NoServer(t *testing.T) {
	c := NewBTAgentClient()
	c.Timeout = 500 * time.Millisecond // fast timeout for test

	_, err := c.SendTask(context.Background(), "http://127.0.0.1:19899/nonexistent", "test task")
	if err == nil {
		t.Error("expected error when target URL is unreachable")
	}
}

// TestBTAgentClient_SendTask_EmptyURL tests that an empty URL fails.
func TestBTAgentClient_SendTask_EmptyURL(t *testing.T) {
	c := NewBTAgentClient()

	_, err := c.SendTask(context.Background(), "", "test task")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

// safetyGetMessageText tests — these don't need network.
func TestSafetyGetMessageText(t *testing.T) {
	tests := []struct {
		name     string
		msg      *a2a.Message
		expected string
	}{
		{"nil message", nil, "no status message"},
		{"empty parts", a2a.NewMessage(a2a.MessageRoleAgent), ""},
		{"with text part", a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("hello")), "hello"},
		{"multiple parts, first text",
			func() *a2a.Message {
				// Create message with multiple text parts via constructor
				return a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("first"), a2a.NewTextPart("second"))
			}(), "first"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := safetyGetMessageText(tt.msg)
			if got != tt.expected {
				t.Errorf("safetyGetMessageText() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestBTAgentClient_DiscoverAgents_NoServer tests discovery against unreachable server.
func TestBTAgentClient_DiscoverAgents_NoServer(t *testing.T) {
	c := NewBTAgentClient()
	c.Timeout = 500 * time.Millisecond

	_, err := c.DiscoverAgents(context.Background(), "http://127.0.0.1:19899/")
	if err == nil {
		t.Error("expected error when discovering unreachable server")
	}
}

// TestBTAgentClient_DiscoverAgents_EmptyURL tests discovery with empty URL.
func TestBTAgentClient_DiscoverAgents_EmptyURL(t *testing.T) {
	c := NewBTAgentClient()

	_, err := c.DiscoverAgents(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty URL")
	}
}

func TestBTAgentClient_TreeSkillName(t *testing.T) {
	tests := []struct {
		treeID   string
		expected string
	}{
		{"domain:code_review", "code review (domain)"},
		{"research:deep_research", "deep research (research)"},
		{"finance:pitch_agent", "pitch agent (finance)"},
		{"simple-tree", "simple-tree"},
		{"", ""},
		{"a:b", "b (a)"},
	}

	for _, tt := range tests {
		got := treeSkillName(tt.treeID)
		if got != tt.expected {
			t.Errorf("treeSkillName(%q) = %q, want %q", tt.treeID, got, tt.expected)
		}
	}
}

func TestBTAgentClient_TreeTags(t *testing.T) {
	tests := []struct {
		treeID   string
		expected []string
	}{
		{"domain:code_review", []string{"domain", "code", "review"}},
		{"research:deep_research", []string{"research", "deep", "research"}},
		{"finance:pitch_agent", []string{"finance", "pitch", "agent"}},
		{"simple-tree", []string{"simple-tree"}},
		{"", []string{""}},
	}

	for _, tt := range tests {
		got := treeTags(tt.treeID)
		if len(got) != len(tt.expected) {
			t.Errorf("treeTags(%q) length = %d, want %d", tt.treeID, len(got), len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("treeTags(%q)[%d] = %q, want %q", tt.treeID, i, got[i], tt.expected[i])
			}
		}
	}
}

// TestA2AServerHealth tests the health endpoint path.
func TestA2AServer_HandleHealth_Direct(t *testing.T) {
	// Test handleHealth directly with a minimal Server instance
	srv := &Server{
		Port:    0,
		BaseURL: "http://localhost:8686",
	}

	handler := http.HandlerFunc(srv.handleHealth)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET health failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// TestA2AServer_HandleWellKnown tests that well-known discovery returns 404.
func TestA2AServer_HandleWellKnown(t *testing.T) {
	srv := &Server{
		Port:    0,
		BaseURL: "http://localhost:8686",
	}

	handler := http.HandlerFunc(srv.handleWellKnown)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("GET well-known failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

// TestA2AServer_Stop verifies Stop handles nil server gracefully.
func TestA2AServer_Stop(t *testing.T) {
	srv := &Server{}
	err := srv.Stop()
	if err != nil {
		t.Errorf("Stop() on nil httpSrv should not error, got: %v", err)
	}
}

// TestA2AServer_HandleAgentEndpoint_EmptyName verifies listing agents when no path.
func TestA2AServer_HandleAgentEndpoint_EmptyName(t *testing.T) {
	srv := &Server{}
	handler := http.HandlerFunc(srv.handleAgentEndpoint)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/agents/")
	if err != nil {
		t.Fatalf("GET agent endpoint failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("expected 200 for empty agent name, got %d", resp.StatusCode)
	}
}

// TestA2AServer_HandleAgentEndpoint_UnknownAgent verifies 404 for unknown agent.
func TestA2AServer_HandleAgentEndpoint_UnknownAgent(t *testing.T) {
	srv := &Server{
		CardCache: map[string]*a2a.AgentCard{},
	}
	handler := http.HandlerFunc(srv.handleAgentEndpoint)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/agents/unknown-agent")
	if err != nil {
		t.Fatalf("GET unknown agent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 404 {
		t.Errorf("expected 404 for unknown agent, got %d", resp.StatusCode)
	}
}

// TestTreeSkillName_EdgeCases covers edge cases for treeSkillName.
func TestTreeSkillName_EdgeCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"a_b:c_d_e", "c d e (a_b)"},
		{"multiple:colons:here", "colons:here (multiple)"},
		{"no_underscore", "no_underscore"},
		{"  spaces  ", "  spaces  "},
		{"special:chars!", "chars! (special)"},
	}

	for _, tt := range tests {
		got := treeSkillName(tt.input)
		if got != tt.expected {
			t.Errorf("treeSkillName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// TestTreeTags_EdgeCases covers edge cases for treeTags.
func TestTreeTags_EdgeCases(t *testing.T) {
	tests := []struct {
		input  string
		minLen int
	}{
		{"a:b_c_d", 3},
		{"only_prefix", 1},
		{"prefix:", 2},
	}

	for _, tt := range tests {
		got := treeTags(tt.input)
		if len(got) < tt.minLen {
			t.Errorf("treeTags(%q) length = %d, want >= %d", tt.input, len(got), tt.minLen)
		}
	}
}

// TestSetTreeResolver verifies the SetTreeResolver function.
func TestSetTreeResolver(t *testing.T) {
	result := resolveTreeByID("test")
	if result != nil {
		t.Error("expected default resolveTreeByID to return nil")
	}

	// Set custom resolver
	called := false
	SetTreeResolver(func(id string) *evolution.SerializableNode {
		called = true
		if id != "custom" {
			t.Errorf("expected id 'custom', got %q", id)
		}
		return nil
	})
	resolveTreeByID("custom")
	if !called {
		t.Error("expected custom resolver to be called")
	}
}
