package engine

import (
	"testing"

	btcore "github.com/rvitorper/go-bt/core"
)

// Characterization tests for actions_stockfish.go. These pin the current
// exported behavior of the four registered actions (InitTranspositionTable,
// LoadCachedFitness, StoreInTranspositionTable, hasCachedFitness) so future
// refactors don't silently change semantics.
//
// The actions are registered once via actions_stockfish.go's init(), so
// tests look them up directly in actionRegistry rather than re-registering
// (RegisterAction panics on a duplicate name).

// ─── InitTranspositionTable ─────────────────────────────────────────────────

func TestInitTranspositionTable(t *testing.T) {
	action, ok := actionRegistry["InitTranspositionTable"]
	if !ok {
		t.Fatal("InitTranspositionTable not registered")
	}

	tests := []struct {
		name  string
		setup func() *Blackboard
	}{
		{
			name: "nil chain state gets allocated and populated",
			setup: func() *Blackboard {
				return &Blackboard{}
			},
		},
		{
			name: "existing chain state preserves unrelated keys",
			setup: func() *Blackboard {
				return &Blackboard{ChainState: map[string]any{"unrelated": "keep-me"}}
			},
		},
		{
			name: "existing chain state resets prior tt fields",
			setup: func() *Blackboard {
				return &Blackboard{ChainState: map[string]any{
					"tt_hits":                    5,
					"tt_misses":                  9,
					"best_fitness":               0.87,
					"cycles_without_improvement": 3,
					"killer_moves":               []any{"e2e4"},
					"history_scores":             map[string]any{"e2e4": 1.0},
				}}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bb := tt.setup()
			ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

			status := action(ctx)

			if status != 1 {
				t.Errorf("status = %d, want 1", status)
			}
			if bb.ChainState == nil {
				t.Fatal("ChainState is nil after init")
			}
			if got := bb.ChainState["tt_hits"]; got != 0 {
				t.Errorf("tt_hits = %v, want 0", got)
			}
			if got := bb.ChainState["tt_misses"]; got != 0 {
				t.Errorf("tt_misses = %v, want 0", got)
			}
			if got := bb.ChainState["best_fitness"]; got != 0.0 {
				t.Errorf("best_fitness = %v, want 0.0", got)
			}
			if got := bb.ChainState["cycles_without_improvement"]; got != 0 {
				t.Errorf("cycles_without_improvement = %v, want 0", got)
			}
			km, ok := bb.ChainState["killer_moves"].([]any)
			if !ok || len(km) != 0 {
				t.Errorf("killer_moves = %#v, want empty []any", bb.ChainState["killer_moves"])
			}
			hs, ok := bb.ChainState["history_scores"].(map[string]any)
			if !ok || len(hs) != 0 {
				t.Errorf("history_scores = %#v, want empty map[string]any", bb.ChainState["history_scores"])
			}
			if unrelated, present := bb.ChainState["unrelated"]; present && unrelated != "keep-me" {
				t.Errorf("unrelated key mutated: %v", unrelated)
			}
		})
	}
}

// ─── LoadCachedFitness ───────────────────────────────────────────────────────

func TestLoadCachedFitness(t *testing.T) {
	action, ok := actionRegistry["LoadCachedFitness"]
	if !ok {
		t.Fatal("LoadCachedFitness not registered")
	}

	tests := []struct {
		name           string
		bb             *Blackboard
		wantStatus     int
		wantCachedResl string
	}{
		{
			name:           "nil chain state is a no-op",
			bb:             &Blackboard{},
			wantStatus:     1,
			wantCachedResl: "",
		},
		{
			name:           "cached_fitness present as float64 is formatted",
			bb:             &Blackboard{ChainState: map[string]any{"cached_fitness": 0.756}},
			wantStatus:     1,
			wantCachedResl: "cached_fitness:0.76",
		},
		{
			name:           "cached_fitness absent leaves CachedResult untouched",
			bb:             &Blackboard{ChainState: map[string]any{}, CachedResult: "prior"},
			wantStatus:     1,
			wantCachedResl: "prior",
		},
		{
			name:           "cached_fitness of the wrong type is ignored",
			bb:             &Blackboard{ChainState: map[string]any{"cached_fitness": "high"}},
			wantStatus:     1,
			wantCachedResl: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &btcore.BTContext[Blackboard]{Blackboard: tt.bb}

			status := action(ctx)

			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
			if tt.bb.CachedResult != tt.wantCachedResl {
				t.Errorf("CachedResult = %q, want %q", tt.bb.CachedResult, tt.wantCachedResl)
			}
		})
	}
}

// ─── StoreInTranspositionTable ──────────────────────────────────────────────

func TestStoreInTranspositionTable(t *testing.T) {
	action, ok := actionRegistry["StoreInTranspositionTable"]
	if !ok {
		t.Fatal("StoreInTranspositionTable not registered")
	}

	t.Run("nil chain state is a no-op", func(t *testing.T) {
		bb := &Blackboard{Result: "some result"}
		ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

		status := action(ctx)

		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if bb.ChainState != nil {
			t.Errorf("ChainState = %#v, want nil", bb.ChainState)
		}
	})

	t.Run("increments tt_hits and stores non-empty result", func(t *testing.T) {
		bb := &Blackboard{
			ChainState: map[string]any{"tt_hits": 3},
			Result:     "fitness=0.9",
		}
		ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

		status := action(ctx)

		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if got := bb.ChainState["tt_hits"]; got != 4 {
			t.Errorf("tt_hits = %v, want 4", got)
		}
		if got := bb.ChainState["cached_result"]; got != "fitness=0.9" {
			t.Errorf("cached_result = %v, want %q", got, "fitness=0.9")
		}
	})

	t.Run("empty result does not set cached_result", func(t *testing.T) {
		bb := &Blackboard{
			ChainState: map[string]any{"tt_hits": 0},
			Result:     "",
		}
		ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

		status := action(ctx)

		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if got := bb.ChainState["tt_hits"]; got != 1 {
			t.Errorf("tt_hits = %v, want 1", got)
		}
		if _, present := bb.ChainState["cached_result"]; present {
			t.Errorf("cached_result should not be set when Result is empty, got %v", bb.ChainState["cached_result"])
		}
	})

	// Pins the intended contract: a chain state that exists but has not yet
	// been through InitTranspositionTable (so it lacks "tt_hits") must be
	// handled gracefully — tt_hits should be treated as starting at 0 rather
	// than crashing the tree. Today's implementation does an unchecked
	// `bb.ChainState["tt_hits"].(int)` type assertion, which panics with
	// "interface conversion: interface {} is nil, not int" when the key is
	// absent. This test documents the desired behavior and currently fails
	// (via the recover below) until that assertion is made safe.
	t.Run("missing tt_hits key does not panic", func(t *testing.T) {
		bb := &Blackboard{
			ChainState: map[string]any{},
			Result:     "some result",
		}
		ctx := &btcore.BTContext[Blackboard]{Blackboard: bb}

		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("StoreInTranspositionTable panicked on missing tt_hits key: %v", r)
			}
		}()

		status := action(ctx)

		if status != 1 {
			t.Errorf("status = %d, want 1", status)
		}
		if got := bb.ChainState["tt_hits"]; got != 1 {
			t.Errorf("tt_hits = %v, want 1", got)
		}
	})
}

// ─── hasCachedFitness ────────────────────────────────────────────────────────

func TestHasCachedFitness(t *testing.T) {
	action, ok := actionRegistry["hasCachedFitness"]
	if !ok {
		t.Fatal("hasCachedFitness not registered")
	}

	tests := []struct {
		name       string
		bb         *Blackboard
		wantStatus int
	}{
		{
			name:       "nil chain state returns failure",
			bb:         &Blackboard{},
			wantStatus: -1,
		},
		{
			name:       "cached_fitness key absent returns failure",
			bb:         &Blackboard{ChainState: map[string]any{}},
			wantStatus: -1,
		},
		{
			name:       "cached_fitness key present returns success regardless of type",
			bb:         &Blackboard{ChainState: map[string]any{"cached_fitness": "anything"}},
			wantStatus: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &btcore.BTContext[Blackboard]{Blackboard: tt.bb}

			status := action(ctx)

			if status != tt.wantStatus {
				t.Errorf("status = %d, want %d", status, tt.wantStatus)
			}
		})
	}
}
