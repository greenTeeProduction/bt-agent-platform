package knowledge

import (
	"math"
	"testing"
	"time"
)

// =============================================================================
// CosineSimilarity (pure math, no Ollama needed)
// =============================================================================

func TestCosineSimilarity_Identical(t *testing.T) {
	a := Embedding{1.0, 2.0, 3.0}
	b := Embedding{1.0, 2.0, 3.0}
	sim := CosineSimilarity(a, b)
	if math.Abs(sim-1.0) > 0.0001 {
		t.Errorf("identical vectors should have similarity 1.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := Embedding{1.0, 0.0}
	b := Embedding{-1.0, 0.0}
	sim := CosineSimilarity(a, b)
	if math.Abs(sim+1.0) > 0.0001 {
		t.Errorf("opposite vectors should have similarity -1.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := Embedding{1.0, 0.0}
	b := Embedding{0.0, 1.0}
	sim := CosineSimilarity(a, b)
	if math.Abs(sim-0.0) > 0.0001 {
		t.Errorf("orthogonal vectors should have similarity 0.0, got %.4f", sim)
	}
}

func TestCosineSimilarity_Partial(t *testing.T) {
	a := Embedding{1.0, 2.0, 3.0}
	b := Embedding{4.0, 5.0, 6.0}
	sim := CosineSimilarity(a, b)
	// (4+10+18) / (sqrt(14) * sqrt(77)) = 32 / (3.742 * 8.775) = 32 / 32.831 ≈ 0.9746
	expected := 32.0 / (math.Sqrt(14.0) * math.Sqrt(77.0))
	if math.Abs(sim-expected) > 0.0001 {
		t.Errorf("expected similarity ~%.4f, got %.4f", expected, sim)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	sim := CosineSimilarity(Embedding{}, Embedding{1.0, 2.0})
	if sim != 0.0 {
		t.Errorf("empty vector should return 0.0, got %.2f", sim)
	}
}

func TestCosineSimilarity_Nil(t *testing.T) {
	sim := CosineSimilarity(nil, Embedding{1.0})
	if sim != 0.0 {
		t.Errorf("nil vector should return 0.0, got %.2f", sim)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := Embedding{1.0, 2.0}
	b := Embedding{1.0, 2.0, 3.0}
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("different lengths should return 0.0, got %.2f", sim)
	}
}

func TestCosineSimilarity_ZeroNorm(t *testing.T) {
	a := Embedding{0.0, 0.0}
	b := Embedding{1.0, 0.0}
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("zero-norm vector should return 0.0, got %.2f", sim)
	}
}

func TestCosineSimilarity_BothZero(t *testing.T) {
	a := Embedding{0.0, 0.0}
	b := Embedding{0.0, 0.0}
	sim := CosineSimilarity(a, b)
	if sim != 0.0 {
		t.Errorf("both zero-norm should return 0.0, got %.2f", sim)
	}
}

// =============================================================================
// BuildIndex (per-tree goroutine panic recovery)
// =============================================================================

// TestBuildIndex_PanicRecovered verifies that a panic in a per-tree embedding
// goroutine (e.g. from a nil tree blowing up during capability-text assembly,
// or from a panic inside the embedding client itself) is turned into an error
// result on the goroutine's result channel instead of crashing the process or
// deadlocking the synchronous receive loop that waits for one result per tree.
func TestBuildIndex_PanicRecovered(t *testing.T) {
	kg := NewKnowledgeGraph()
	// A nil TreeMeta panics as soon as BuildIndex's goroutine dereferences it
	// (t.Name, at the start of capability-text assembly) — simulating any panic
	// raised while building the embedding for this tree.
	kg.Trees["panicky"] = nil

	done := make(chan error, 1)
	go func() {
		done <- kg.BuildIndex()
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected BuildIndex to return an error when a per-tree goroutine panics, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("BuildIndex hung after a per-tree goroutine panic instead of returning an error")
	}
}
