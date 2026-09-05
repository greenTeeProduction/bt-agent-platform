package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nico/go-bt-evolve/internal/llm"
)

// Compile-time check requested verbatim by the LLMInterface doc comment in
// mock_llm.go: production asserts only the engine-local LLMInterface subset, so
// the full llm.LLM satisfaction is pinned here. Safe from an import cycle:
// internal/llm does not import internal/engine, and internal/engine/tree.go
// already imports internal/llm.
var _ llm.LLM = (*MockLLM)(nil)

// errGenerateBoom is the sentinel used to pin GenerateErr propagation.
var errGenerateBoom = errors.New("mock generate failed")

// TestNewMockLLMDefaults pins the five field values NewMockLLM sets, verbatim.
func TestNewMockLLMDefaults(t *testing.T) {
	m := NewMockLLM()

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"ComplexityResp", m.ComplexityResp, "low"},
		{"PlanResp", m.PlanResp, "1. Execute the task\n2. Verify results\n3. Report outcome"},
		{"WentWellResp", m.WentWellResp, "Task completed successfully"},
		{"ToImproveResp", m.ToImproveResp, "Add more error handling"},
		{"GenerateResp", m.GenerateResp, "Mock response with sufficient length for quality validation checks"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}

	if m.GenerateErr != nil {
		t.Errorf("GenerateErr = %v, want nil", m.GenerateErr)
	}
}

// TestMockLLMGeneratePrecedence pins Generate's three-way precedence:
// GenerateErr short-circuits, then a non-empty GenerateResp, then the default.
func TestMockLLMGeneratePrecedence(t *testing.T) {
	tests := []struct {
		name    string
		mock    MockLLM
		wantOut string
		wantErr error
	}{
		{
			name:    "error short-circuits even when GenerateResp is set",
			mock:    MockLLM{GenerateResp: "custom response", GenerateErr: errGenerateBoom},
			wantOut: "",
			wantErr: errGenerateBoom,
		},
		{
			name:    "error only",
			mock:    MockLLM{GenerateErr: errGenerateBoom},
			wantOut: "",
			wantErr: errGenerateBoom,
		},
		{
			name:    "non-empty GenerateResp wins",
			mock:    MockLLM{GenerateResp: "custom response"},
			wantOut: "custom response",
			wantErr: nil,
		},
		{
			name:    "empty GenerateResp falls back to default",
			mock:    MockLLM{GenerateResp: ""},
			wantOut: defaultGenerateResp,
			wantErr: nil,
		},
		{
			name:    "zero value returns default",
			mock:    MockLLM{},
			wantOut: defaultGenerateResp,
			wantErr: nil,
		},
		{
			name:    "NewMockLLM returns its configured GenerateResp",
			mock:    *NewMockLLM(),
			wantOut: defaultGenerateResp,
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.mock
			got, err := m.Generate("any prompt")
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("Generate() err = %v, want %v", err, tt.wantErr)
			}
			if got != tt.wantOut {
				t.Errorf("Generate() = %q, want %q", got, tt.wantOut)
			}
		})
	}
}

// TestMockLLMGenerateIgnoresPrompt pins that the prompt argument is ignored.
func TestMockLLMGenerateIgnoresPrompt(t *testing.T) {
	m := NewMockLLM()
	for _, prompt := range []string{"", "short", "a much longer prompt with\nnewlines and punctuation!"} {
		got, err := m.Generate(prompt)
		if err != nil {
			t.Fatalf("Generate(%q) unexpected err: %v", prompt, err)
		}
		if got != defaultGenerateResp {
			t.Errorf("Generate(%q) = %q, want %q", prompt, got, defaultGenerateResp)
		}
	}
}

// TestMockLLMGenerateCtxAndTimeoutDelegate pins that GenerateCtx and
// GenerateWithTimeout delegate to Generate, ignoring the context (including an
// already-cancelled one) and the timeout (including zero and negative values).
func TestMockLLMGenerateCtxAndTimeoutDelegate(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	receivers := []struct {
		name string
		mock MockLLM
	}{
		{"configured", *NewMockLLM()},
		{"custom response", MockLLM{GenerateResp: "custom response"}},
		{"zero value", MockLLM{}},
		{"error", MockLLM{GenerateResp: "custom response", GenerateErr: errGenerateBoom}},
	}

	contexts := []struct {
		name string
		ctx  context.Context
	}{
		{"background", context.Background()},
		{"todo", context.TODO()},
		{"cancelled", cancelled},
	}

	timeouts := []struct {
		name string
		d    time.Duration
	}{
		{"positive", time.Minute},
		{"zero", 0},
		{"negative", -time.Second},
	}

	for _, r := range receivers {
		t.Run(r.name, func(t *testing.T) {
			m := r.mock
			wantOut, wantErr := m.Generate("prompt")

			for _, c := range contexts {
				t.Run("GenerateCtx/"+c.name, func(t *testing.T) {
					got, err := m.GenerateCtx(c.ctx, "prompt")
					if !errors.Is(err, wantErr) {
						t.Errorf("GenerateCtx() err = %v, want %v", err, wantErr)
					}
					if got != wantOut {
						t.Errorf("GenerateCtx() = %q, want %q", got, wantOut)
					}
				})
			}

			for _, to := range timeouts {
				t.Run("GenerateWithTimeout/"+to.name, func(t *testing.T) {
					got, err := m.GenerateWithTimeout("prompt", to.d)
					if !errors.Is(err, wantErr) {
						t.Errorf("GenerateWithTimeout() err = %v, want %v", err, wantErr)
					}
					if got != wantOut {
						t.Errorf("GenerateWithTimeout() = %q, want %q", got, wantOut)
					}
				})
			}
		})
	}
}

// TestMockLLMGenerateWithTimeoutDoesNotBlock pins that the timeout is inert:
// even a zero or negative duration returns immediately rather than blocking.
func TestMockLLMGenerateWithTimeoutDoesNotBlock(t *testing.T) {
	m := NewMockLLM()

	for _, d := range []time.Duration{0, -time.Second, time.Hour} {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = m.GenerateWithTimeout("prompt", d)
		}()

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatalf("GenerateWithTimeout(prompt, %v) blocked", d)
		}
	}
}

// TestMockLLMPassthroughAccessors pins AnalyzeComplexity, GeneratePlan and
// Reflect as verbatim field passthroughs that ignore their arguments, plus the
// zero-value receiver path where all three return "".
func TestMockLLMPassthroughAccessors(t *testing.T) {
	configured := *NewMockLLM()

	tests := []struct {
		name          string
		mock          MockLLM
		task          string
		outcome       string
		plan          string
		complexity    string
		wantComplex   string
		wantPlan      string
		wantWentWell  string
		wantToImprove string
	}{
		{
			name:          "configured with populated args",
			mock:          configured,
			task:          "build the thing",
			outcome:       "succeeded",
			plan:          "1. do it",
			complexity:    "high",
			wantComplex:   "low",
			wantPlan:      "1. Execute the task\n2. Verify results\n3. Report outcome",
			wantWentWell:  "Task completed successfully",
			wantToImprove: "Add more error handling",
		},
		{
			name:          "configured with empty args",
			mock:          configured,
			wantComplex:   "low",
			wantPlan:      "1. Execute the task\n2. Verify results\n3. Report outcome",
			wantWentWell:  "Task completed successfully",
			wantToImprove: "Add more error handling",
		},
		{
			name:          "zero value returns empty strings",
			mock:          MockLLM{},
			task:          "build the thing",
			outcome:       "succeeded",
			plan:          "1. do it",
			complexity:    "high",
			wantComplex:   "",
			wantPlan:      "",
			wantWentWell:  "",
			wantToImprove: "",
		},
		{
			name:          "custom fields returned verbatim",
			mock:          MockLLM{ComplexityResp: "medium", PlanResp: "step one", WentWellResp: "went well", ToImproveResp: "to improve"},
			task:          "anything",
			wantComplex:   "medium",
			wantPlan:      "step one",
			wantWentWell:  "went well",
			wantToImprove: "to improve",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.mock

			if got := m.AnalyzeComplexity(tt.task); got != tt.wantComplex {
				t.Errorf("AnalyzeComplexity() = %q, want %q", got, tt.wantComplex)
			}
			if got := m.GeneratePlan(tt.task, tt.complexity); got != tt.wantPlan {
				t.Errorf("GeneratePlan() = %q, want %q", got, tt.wantPlan)
			}

			// Reflect returns (WentWellResp, ToImproveResp) in that order.
			wentWell, toImprove := m.Reflect(tt.task, tt.outcome, tt.plan)
			if wentWell != tt.wantWentWell {
				t.Errorf("Reflect() wentWell = %q, want %q", wentWell, tt.wantWentWell)
			}
			if toImprove != tt.wantToImprove {
				t.Errorf("Reflect() toImprove = %q, want %q", toImprove, tt.wantToImprove)
			}
		})
	}
}

// minQualityLen is the threshold validateOutputQuality enforces, and the number
// NewMockLLM's doc comment claims for its responses.
const minQualityLen = 40

// TestMockLLMResponseLengths pins the length invariant NewMockLLM's doc comment
// claims, scoped to where it actually holds.
//
// The >= 40 guarantee is real for the Generate* family, all three members of
// which return defaultGenerateResp — that is what keeps mock-driven results
// above validateOutputQuality's minimum length. It was never true of
// ComplexityResp, WentWellResp or ToImproveResp: those are short fixture
// strings that are returned directly to callers and never flow through
// validateOutputQuality. The doc comment overstated its scope; it has been
// narrowed to match. The values themselves are unchanged and their current
// lengths are pinned exactly below, so any future edit to them is a deliberate
// choice rather than a silent drift.
func TestMockLLMResponseLengths(t *testing.T) {
	m := NewMockLLM()

	generated, err := m.Generate("prompt")
	if err != nil {
		t.Fatalf("Generate() unexpected err: %v", err)
	}
	generatedCtx, err := m.GenerateCtx(context.Background(), "prompt")
	if err != nil {
		t.Fatalf("GenerateCtx() unexpected err: %v", err)
	}
	generatedTimeout, err := m.GenerateWithTimeout("prompt", time.Minute)
	if err != nil {
		t.Fatalf("GenerateWithTimeout() unexpected err: %v", err)
	}

	t.Run("Generate family clears minQualityLen", func(t *testing.T) {
		tests := []struct {
			name  string
			value string
		}{
			{"Generate", generated},
			{"GenerateCtx", generatedCtx},
			{"GenerateWithTimeout", generatedTimeout},
			{"defaultGenerateResp", defaultGenerateResp},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if len(tt.value) < minQualityLen {
					t.Errorf("len(%s) = %d, want >= %d", tt.name, len(tt.value), minQualityLen)
				}
			})
		}
	})

	t.Run("fixture lengths", func(t *testing.T) {
		// Actual current lengths, not aspirational ones. ComplexityResp,
		// WentWellResp and ToImproveResp sit below minQualityLen; PlanResp
		// happens to clear it. None of them are subject to the check.
		tests := []struct {
			name    string
			value   string
			wantLen int
		}{
			{"ComplexityResp", m.ComplexityResp, 3},
			{"PlanResp", m.PlanResp, 55},
			{"WentWellResp", m.WentWellResp, 27},
			{"ToImproveResp", m.ToImproveResp, 23},
			{"defaultGenerateResp", defaultGenerateResp, 66},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if len(tt.value) != tt.wantLen {
					t.Errorf("len(%s) = %d, want %d", tt.name, len(tt.value), tt.wantLen)
				}
			})
		}
	})
}
