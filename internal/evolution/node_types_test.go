package evolution

import (
	"strings"
	"testing"
)

// TestSerializableNodeValidate_MaxTokensFloor pins Validate()'s expected
// behavior for ChainAction nodes whose max_tokens metadata is implausibly
// small: milestone 1 (generateWithRetry) and milestone 2 (Budget node) both
// now honor max_tokens for real, so a node authored with e.g. max_tokens=1
// silently truncates every LLM response to a couple of words instead of
// erroring loudly. Validate() should catch that at tree-authoring time for
// the chain kinds that actually issue an LLM call (llm_call, rag_query,
// structured_output) — not for tool_call/agent/conversation nodes, which
// may legitimately need few or no output tokens.
func TestSerializableNodeValidate_MaxTokensFloor(t *testing.T) {
	tests := []struct {
		name      string
		node      SerializableNode
		wantFlag  bool
		wantWords []string // substrings the flagging error must contain, if wantFlag
	}{
		{
			name: "llm_call below floor is flagged",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "llm_call:Summarize the last five customer emails in detail.",
				Metadata: map[string]any{
					"max_tokens": float64(8),
				},
			},
			wantFlag:  true,
			wantWords: []string{"max_tokens", "llm_call"},
		},
		{
			name: "rag_query below floor is flagged",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "rag_query:Find supporting passages for the claim.",
				Metadata: map[string]any{
					"max_tokens": float64(1),
				},
			},
			wantFlag:  true,
			wantWords: []string{"max_tokens", "rag_query"},
		},
		{
			name: "structured_output below floor is flagged",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "structured_output:Extract the invoice fields as JSON.",
				Metadata: map[string]any{
					"max_tokens": float64(4),
				},
			},
			wantFlag:  true,
			wantWords: []string{"max_tokens", "structured_output"},
		},
		{
			name: "max_tokens as int below floor is flagged",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "llm_call:Write a haiku about the merge conflict.",
				Metadata: map[string]any{
					"max_tokens": 10,
				},
			},
			wantFlag:  true,
			wantWords: []string{"max_tokens", "llm_call"},
		},
		{
			name: "at the floor is not flagged",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "llm_call:Reply with a single word.",
				Metadata: map[string]any{
					"max_tokens": float64(16),
				},
			},
			wantFlag: false,
		},
		{
			name: "comfortably above the floor is not flagged",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "llm_call:Write a detailed report on the incident.",
				Metadata: map[string]any{
					"max_tokens": float64(2048),
				},
			},
			wantFlag: false,
		},
		{
			name: "tool_call chain kind is not subject to the floor",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "tool_call:run_linter",
				Metadata: map[string]any{
					"max_tokens": float64(1),
				},
			},
			wantFlag: false,
		},
		{
			name: "agent chain kind is not subject to the floor",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "agent:Investigate the failing build.",
				Metadata: map[string]any{
					"max_tokens": float64(1),
				},
			},
			wantFlag: false,
		},
		{
			name: "no max_tokens metadata at all is not flagged",
			node: SerializableNode{
				Type: "ChainAction",
				Name: "llm_call:Summarize the meeting notes.",
			},
			wantFlag: false,
		},
		{
			name: "non-ChainAction node with tiny max_tokens is not flagged",
			node: SerializableNode{
				Type: "Action",
				Name: "llm_call:not actually a chain node",
				Metadata: map[string]any{
					"max_tokens": float64(1),
				},
			},
			wantFlag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := tt.node.Validate()

			var flagged string
			for _, e := range errs {
				if strings.Contains(e, "max_tokens") {
					flagged = e
					break
				}
			}

			if tt.wantFlag && flagged == "" {
				t.Fatalf("Validate() = %v, want an error flagging implausibly small max_tokens", errs)
			}
			if !tt.wantFlag && flagged != "" {
				t.Fatalf("Validate() = %v, want no max_tokens error", errs)
			}
			for _, word := range tt.wantWords {
				if !strings.Contains(flagged, word) {
					t.Errorf("max_tokens error %q missing expected substring %q", flagged, word)
				}
			}
		})
	}
}
