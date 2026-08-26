package evolution

import (
	"math"
	"testing"

	"github.com/nico/go-bt-evolve/internal/util"
)

func TestNewCMAESOptimizer(t *testing.T) {
	cma := NewCMAESOptimizer()
	if cma.PopulationSize != 10 {
		t.Errorf("expected PopulationSize=10, got %d", cma.PopulationSize)
	}
	if cma.InitialSigma != 0.3 {
		t.Errorf("expected InitialSigma=0.3, got %f", cma.InitialSigma)
	}
	if cma.MaxGenerations != 20 {
		t.Errorf("expected MaxGenerations=20, got %d", cma.MaxGenerations)
	}
}

func TestCMAESOptimizer_SimpleSphere(t *testing.T) {
	cma := NewCMAESOptimizer()
	cma.PopulationSize = 8
	cma.MaxGenerations = 15
	cma.TargetFitness = 99.0

	params := []TunableParam{
		{Name: "x", Lower: -5, Upper: 5, InitValue: 3.0},
		{Name: "y", Lower: -5, Upper: 5, InitValue: -2.0},
	}

	// Sphere function: minimize distance from (0,0)
	// Convert to maximization: 100 - distance
	result := cma.Optimize(params, func(sol []float64) float64 {
		x := -5 + sol[0]*10 // denormalize: sol is 0-1
		y := -5 + sol[1]*10
		dist := math.Sqrt(x*x + y*y)
		return 100.0 - dist
	})

	if len(result) != 2 {
		t.Fatalf("expected 2 params, got %d", len(result))
	}

	// Should have converged near (0, 0) = fitness near 100
	if result[0] < -2 || result[0] > 2 {
		t.Errorf("expected x near 0, got %f", result[0])
	}
	if result[1] < -2 || result[1] > 2 {
		t.Errorf("expected y near 0, got %f", result[1])
	}

	t.Logf("CMA-ES result: x=%.4f, y=%.4f (bestFit=%.2f)", result[0], result[1], cma.BestFit)
}

func TestCMAESOptimizer_Convergence(t *testing.T) {
	cma := NewCMAESOptimizer()
	cma.MaxGenerations = 8
	cma.PopulationSize = 6

	params := []TunableParam{
		{Name: "p", Lower: 0, Upper: 10, InitValue: 5.0},
	}

	// Simple parabola: maximize -(p-3)^2
	result := cma.Optimize(params, func(sol []float64) float64 {
		p := sol[0] * 10
		return -(p-3)*(p-3) + 100
	})

	if len(result) != 1 {
		t.Fatalf("expected 1 param, got %d", len(result))
	}

	// Should find p ≈ 3
	if result[0] < 1 || result[0] > 5 {
		t.Errorf("expected p near 3, got %f", result[0])
	}

	t.Logf("CMA-ES parabola: p=%.4f (bestFit=%.2f)", result[0], cma.BestFit)
}

func TestExtractParameters(t *testing.T) {
	tree := &SerializableNode{
		Name: "root",
		Type: "Selector",
		Metadata: map[string]any{
			"timeout_ms": float64(5000),
			"threshold":  0.7,
		},
		MaxRetries: 3,
		Children: []SerializableNode{
			{
				Name: "child1",
				Type: "Action",
				Metadata: map[string]any{
					"timeout_ms": float64(10000),
				},
			},
		},
	}

	params := ExtractParameters(tree)
	if len(params) < 3 {
		t.Fatalf("expected at least 3 params, got %d", len(params))
	}

	foundTimeout := false
	foundThreshold := false
	foundRetries := false
	for _, p := range params {
		if p.MetaKey == "timeout_ms" {
			foundTimeout = true
		}
		if p.MetaKey == "threshold" {
			foundThreshold = true
		}
		if p.MetaKey == "max_retries" {
			foundRetries = true
		}
	}
	if !foundTimeout {
		t.Error("expected timeout_ms param")
	}
	if !foundThreshold {
		t.Error("expected threshold param")
	}
	if !foundRetries {
		t.Error("expected max_retries param")
	}
}

func TestExtractParameters_StructFields(t *testing.T) {
	// Nodes with TimeoutMs/MaxRetries set as struct fields (the form
	// ApplyParameters writes back) but nil Metadata must still be tunable.
	tree := &SerializableNode{
		Name:       "root",
		Type:       "Selector",
		TimeoutMs:  5000,
		MaxRetries: 3,
		Children: []SerializableNode{
			{
				Name:      "child1",
				Type:      "Action",
				TimeoutMs: 8000,
			},
		},
	}

	params := ExtractParameters(tree)

	find := func(nodePath, metaKey string) *TunableParam {
		for i := range params {
			if params[i].NodePath == nodePath && params[i].MetaKey == metaKey {
				return &params[i]
			}
		}
		return nil
	}

	rootTimeout := find("", "timeout_ms")
	if rootTimeout == nil {
		t.Fatalf("expected timeout_ms param for root (TimeoutMs field, nil Metadata); got params: %+v", params)
	}
	if rootTimeout.InitValue != 5000 {
		t.Errorf("expected root timeout_ms InitValue=5000 (seeded from field), got %v", rootTimeout.InitValue)
	}

	rootRetries := find("", "max_retries")
	if rootRetries == nil {
		t.Fatalf("expected max_retries param for root (MaxRetries field, nil Metadata); got params: %+v", params)
	}
	if rootRetries.InitValue != 3 {
		t.Errorf("expected root max_retries InitValue=3, got %v", rootRetries.InitValue)
	}

	childTimeout := find("children.0", "timeout_ms")
	if childTimeout == nil {
		t.Fatalf("expected timeout_ms param for child at children.0; got params: %+v", params)
	}
	if childTimeout.InitValue != 8000 {
		t.Errorf("expected child timeout_ms InitValue=8000 (seeded from field), got %v", childTimeout.InitValue)
	}
}

func TestExtractParameters_TimeoutDedupe(t *testing.T) {
	// A node carrying both the Metadata key and the struct field must emit
	// exactly one timeout_ms param, not two.
	tree := &SerializableNode{
		Name:      "root",
		Type:      "Action",
		TimeoutMs: 5000,
		Metadata: map[string]any{
			"timeout_ms": float64(5000),
		},
	}

	params := ExtractParameters(tree)

	count := 0
	for _, p := range params {
		if p.MetaKey == "timeout_ms" && p.NodePath == "" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 timeout_ms param for root, got %d (params: %+v)", count, params)
	}
}

func TestApplyParameters(t *testing.T) {
	tree := &SerializableNode{
		Name: "root",
		Type: "Selector",
		Metadata: map[string]any{
			"timeout_ms": float64(5000),
			"threshold":  0.5,
		},
		MaxRetries: 3,
	}

	params := []TunableParam{
		{Name: "root.timeout_ms", Lower: 100, Upper: 60000, InitValue: 5000, MetaKey: "timeout_ms"},
		{Name: "root.threshold", Lower: 0, Upper: 1, InitValue: 0.5, MetaKey: "threshold"},
	}

	solution := []float64{15000, 0.8}
	ApplyParameters(tree, params, solution)

	if tree.TimeoutMs != 15000 {
		t.Errorf("expected TimeoutMs=15000, got %d", tree.TimeoutMs)
	}
	thresh, ok := tree.Metadata["threshold"].(float64)
	if !ok || thresh != 0.8 {
		t.Errorf("expected threshold=0.8, got %v (type %T)", tree.Metadata["threshold"], tree.Metadata["threshold"])
	}
}

func TestTuneTreeParameters(t *testing.T) {
	t.Run("nil tree returns ok=false", func(t *testing.T) {
		tuned, params, _, ok := TuneTreeParameters(nil, 6, 5, func(*SerializableNode) float64 { return 0 })
		if ok {
			t.Error("expected ok=false for nil tree")
		}
		if tuned != nil {
			t.Errorf("expected nil tuned tree, got %+v", tuned)
		}
		if len(params) != 0 {
			t.Errorf("expected no params, got %+v", params)
		}
	})

	t.Run("no tunable params returns ok=false", func(t *testing.T) {
		tree := &SerializableNode{Name: "leaf", Type: "Action"}
		called := false
		tuned, params, _, ok := TuneTreeParameters(tree, 6, 5, func(*SerializableNode) float64 {
			called = true
			return 0
		})
		if ok {
			t.Error("expected ok=false for tree without tunable params")
		}
		if tuned != nil {
			t.Errorf("expected nil tuned tree, got %+v", tuned)
		}
		if len(params) != 0 {
			t.Errorf("expected no params, got %+v", params)
		}
		if called {
			t.Error("fitnessFn must not be called when there is nothing to tune")
		}
	})

	t.Run("tunes timeout without mutating input", func(t *testing.T) {
		tree := &SerializableNode{
			Name:      "root",
			Type:      "Action",
			TimeoutMs: 5000,
		}

		// Deterministic structural fitness: reward proximity to a 30s timeout.
		fitnessFn := func(n *SerializableNode) float64 {
			return 100.0 - math.Abs(float64(n.TimeoutMs)-30000)/1000.0
		}

		tuned, params, bestFitness, ok := TuneTreeParameters(tree, 6, 5, fitnessFn)
		if !ok {
			t.Fatal("expected ok=true for tree with a tunable timeout")
		}
		if tuned == nil {
			t.Fatal("expected non-nil tuned tree")
		}
		if tuned == tree {
			t.Fatal("tuned tree must be a clone, not the input tree")
		}
		if len(params) != 1 {
			t.Fatalf("expected 1 extracted param, got %d: %+v", len(params), params)
		}
		if params[0].MetaKey != "timeout_ms" {
			t.Errorf("expected timeout_ms param, got %+v", params[0])
		}

		// Input tree must be untouched.
		if tree.TimeoutMs != 5000 {
			t.Errorf("input tree mutated: TimeoutMs=%d, want 5000", tree.TimeoutMs)
		}
		if len(tree.Children) != 0 || tree.Metadata != nil {
			t.Errorf("input tree structure mutated: %+v", tree)
		}

		// Tuned value must lie within the extracted param bounds.
		got := float64(tuned.TimeoutMs)
		if got < params[0].Lower || got > params[0].Upper {
			t.Errorf("tuned TimeoutMs=%v outside bounds [%v, %v]", got, params[0].Lower, params[0].Upper)
		}

		// bestFitness must describe the returned tuned tree.
		if want := fitnessFn(tuned); math.Abs(bestFitness-want) > 1e-9 {
			t.Errorf("bestFitness=%v, but fitnessFn(tuned)=%v", bestFitness, want)
		}
	})
}

func TestSampleStdNormal(t *testing.T) {
	n := 1000
	samples := sampleStdNormal(n)
	if len(samples) != n {
		t.Fatalf("expected %d samples, got %d", n, len(samples))
	}

	// Check approximate mean ≈ 0
	mean := 0.0
	for _, v := range samples {
		mean += v
	}
	mean /= float64(n)
	if math.Abs(mean) > 0.2 {
		t.Errorf("expected mean near 0, got %f", mean)
	}

	// Check approximate stddev ≈ 1
	variance := 0.0
	for _, v := range samples {
		diff := v - mean
		variance += diff * diff
	}
	variance /= float64(n)
	stddev := math.Sqrt(variance)
	if math.Abs(stddev-1.0) > 0.2 {
		t.Errorf("expected stddev near 1, got %f", stddev)
	}
}

func TestCholesky(t *testing.T) {
	C := [][]float64{
		{2.0, 0.5},
		{0.5, 1.0},
	}
	L := cholesky(C)
	if L == nil {
		t.Fatal("cholesky returned nil")
	}

	// Verify L * L^T = C
	for i := range 2 {
		for j := range 2 {
			sum := 0.0
			for k := range 2 {
				sum += L[i][k] * L[j][k]
			}
			if math.Abs(sum-C[i][j]) > 1e-10 {
				t.Errorf("L*L^T[%d][%d] = %f, expected %f", i, j, sum, C[i][j])
			}
		}
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{42, "42"},
		{100, "100"},
		{-5, "-5"},
	}
	for _, tt := range tests {
		got := util.Itoa(tt.input)
		if got != tt.want {
			t.Errorf("itoa(%d) = %s, want %s", tt.input, got, tt.want)
		}
	}
}
