package doormate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// mockLLM implements llm.LLM interface for testing.
type mockLLM struct {
	reply string
	err   error
}

func (m *mockLLM) Generate(prompt string) (string, error) {
	return m.reply, m.err
}

func (m *mockLLM) GenerateCtx(ctx context.Context, prompt string) (string, error) {
	return m.reply, m.err
}

func (m *mockLLM) GenerateWithTimeout(prompt string, timeout time.Duration) (string, error) {
	return m.reply, m.err
}

func (m *mockLLM) AnalyzeComplexity(task string) string {
	return ""
}

func (m *mockLLM) GeneratePlan(task, complexity string) string {
	return ""
}

func (m *mockLLM) Reflect(task, outcome, plan string) (string, string) {
	return "", ""
}

// TestAgentMockFallback verifies the mock fallback templates for different intents.
func TestAgentMockFallback(t *testing.T) {
	agent := NewPageAgent(nil) // nil LLM triggers mock fallback

	tests := []struct {
		name           string
		input          string
		expectedIntent string
		titleContains  string
	}{
		{
			name:           "Security Intent",
			input:          "setup high security door lock",
			expectedIntent: "security",
			titleContains:  "security",
		},
		{
			name:           "Design Intent",
			input:          "modern gateway design",
			expectedIntent: "design",
			titleContains:  "design",
		},
		{
			name:           "Automation Intent",
			input:          "smart home automation control",
			expectedIntent: "automation",
			titleContains:  "automation",
		},
		{
			name:           "Default Intent",
			input:          "something else entirely",
			expectedIntent: "general",
			titleContains:  "personal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := &UserProfile{ID: "test-user", PreferenceTags: []string{}}
			sess, page, err := agent.Process(tt.input, profile)
			if err != nil {
				t.Fatalf("Process failed: %v", err)
			}

			if sess.Intent != tt.expectedIntent {
				t.Errorf("expected intent %q, got %q", tt.expectedIntent, sess.Intent)
			}

			if !strings.Contains(strings.ToLower(page.Schema.Title), tt.titleContains) {
				t.Errorf("expected title to contain %q, got %q", tt.titleContains, page.Schema.Title)
			}

			if page.SessionID != sess.ID {
				t.Errorf("expected page.SessionID %q to match sess.ID %q", page.SessionID, sess.ID)
			}
		})
	}
}

// TestAgentLLMGeneration verifies dynamic page generation using the mock LLM.
func TestAgentLLMGeneration(t *testing.T) {
	expectedSchema := PageSchema{
		Title:      "Dynamic LLM Title",
		Summary:    "Dynamic Summary from LLM",
		TemplateID: "overview",
		Blocks: []Block{
			{
				Type:    "overview",
				Title:   "LLM Block",
				Content: "Content generated dynamically by LLM",
			},
		},
		FollowUps: []string{"Follow up 1", "Follow up 2"},
	}

	schemaBytes, err := json.Marshal(expectedSchema)
	if err != nil {
		t.Fatalf("failed to marshal expected schema: %v", err)
	}

	// Wrap in markdown json code block to test cleaning logic as well
	mockReply := "```json\n" + string(schemaBytes) + "\n```"

	mock := &mockLLM{reply: mockReply}
	agent := NewPageAgent(mock)

	profile := &UserProfile{ID: "test-user", PreferenceTags: []string{}}
	sess, page, err := agent.Process("generate a custom page", profile)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if sess.Intent != "general" {
		t.Errorf("expected intent 'general', got %q", sess.Intent)
	}

	if page.Schema.Title != expectedSchema.Title {
		t.Errorf("expected title %q, got %q", expectedSchema.Title, page.Schema.Title)
	}

	if len(page.Schema.Blocks) != 1 || page.Schema.Blocks[0].Content != expectedSchema.Blocks[0].Content {
		t.Errorf("expected block content %q, got %q", expectedSchema.Blocks[0].Content, page.Schema.Blocks[0].Content)
	}
}

// TestBubbleRecommendation verifies bubble prediction logic and profile tag integration.
func TestBubbleRecommendation(t *testing.T) {
	agent := NewPageAgent(nil)

	// User profile with custom preference tags
	profile := &UserProfile{
		ID:             "test-user",
		PreferenceTags: []string{"custom-tag-1", "custom-tag-2"},
	}

	sess, _, err := agent.Process("setup high security door lock", profile)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	// Check that default bubbles for security are present
	hasBiometric := false
	hasCustomTag := false
	for _, b := range sess.Bubbles {
		if b == "Biometric Lock" {
			hasBiometric = true
		}
		if b == "Tag: custom-tag-1" {
			hasCustomTag = true
		}
	}

	if !hasBiometric {
		t.Errorf("expected predicted bubbles to contain 'Biometric Lock', got %v", sess.Bubbles)
	}

	if !hasCustomTag {
		t.Errorf("expected predicted bubbles to contain profile tag 'Tag: custom-tag-1', got %v", sess.Bubbles)
	}
}
