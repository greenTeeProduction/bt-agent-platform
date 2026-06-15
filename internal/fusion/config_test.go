package fusion

import "testing"

func TestConfig_DefaultsQualityPreset(t *testing.T) {
	cfg := DefaultConfig().Normalize()
	if !cfg.Enabled {
		t.Fatal("default config should be enabled")
	}
	if cfg.MaxToolCalls != 8 {
		t.Fatalf("MaxToolCalls=%d, want 8", cfg.MaxToolCalls)
	}
	if len(cfg.AnalysisModels) != 3 {
		t.Fatalf("analysis model count=%d, want 3", len(cfg.AnalysisModels))
	}
	if cfg.JudgeModel != QualityPreset[0] {
		t.Fatalf("judge=%q, want %q", cfg.JudgeModel, QualityPreset[0])
	}
}

func TestConfig_ValidatesPanelSize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AnalysisModels = []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected >8 panel models to fail validation")
	}

	cfg.AnalysisModels = []string{"one"}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("single panel model should validate: %v", err)
	}
}

func TestConfig_ValidatesMaxToolCalls(t *testing.T) {
	for _, n := range []int{0, 17, -1} {
		cfg := DefaultConfig()
		cfg.MaxToolCalls = n
		if err := cfg.Validate(); err == nil {
			t.Fatalf("MaxToolCalls=%d should fail", n)
		}
	}
	cfg := DefaultConfig()
	cfg.MaxToolCalls = 16
	if err := cfg.Validate(); err != nil {
		t.Fatalf("MaxToolCalls=16 should validate: %v", err)
	}
}

func TestConfig_Disabled(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("disabled config should still validate: %v", err)
	}
}
