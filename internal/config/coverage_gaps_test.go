package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ─── hasExplicitField ────────────────────────────────────────────────────────

func TestHasExplicitField_NilConfig(t *testing.T) {
	// hasExplicitField re-marshals the Config. nil *Config marshals as "null".
	result := hasExplicitField(nil, "gardener_enabled")
	if result {
		t.Error("hasExplicitField(nil, ...) should return false")
	}
}

func TestHasExplicitField_FieldPresent(t *testing.T) {
	c := &Config{GardenerEnabled: true}
	if !hasExplicitField(c, "gardener_enabled") {
		t.Error("hasExplicitField should detect gardener_enabled=true")
	}
}

func TestHasExplicitField_FieldAbsent(t *testing.T) {
	c := &Config{}
	if hasExplicitField(c, "nonexistent_field") {
		t.Error("hasExplicitField should return false for nonexistent field")
	}
}

// ─── envBool ─────────────────────────────────────────────────────────────────

func TestEnvBool_DefaultValue(t *testing.T) {
	os.Unsetenv("_TEST_ENVBOOL_DEFAULT")
	defer os.Unsetenv("_TEST_ENVBOOL_DEFAULT")

	if got := envBool("_TEST_ENVBOOL_DEFAULT", true); got != true {
		t.Errorf("envBool(unset) with default=true = %v, want true", got)
	}
	if got := envBool("_TEST_ENVBOOL_DEFAULT", false); got != false {
		t.Errorf("envBool(unset) with default=false = %v, want false", got)
	}
}

func TestEnvBool_UnknownValue(t *testing.T) {
	os.Setenv("_TEST_ENVBOOL_UNKNOWN", "banana")
	defer os.Unsetenv("_TEST_ENVBOOL_UNKNOWN")

	if got := envBool("_TEST_ENVBOOL_UNKNOWN", true); got != true {
		t.Errorf("envBool('banana') with default=true = %v, want true", got)
	}
	if got := envBool("_TEST_ENVBOOL_UNKNOWN", false); got != false {
		t.Errorf("envBool('banana') with default=false = %v, want false", got)
	}
}

func TestEnvBool_MixedCase(t *testing.T) {
	os.Setenv("_TEST_ENVBOOL_MIXED", "True")
	defer os.Unsetenv("_TEST_ENVBOOL_MIXED")

	if got := envBool("_TEST_ENVBOOL_MIXED", false); got != true {
		t.Errorf("envBool('True') = %v, want true", got)
	}
}

// ─── NewConfigWatcher ───────────────────────────────────────────────────────

func TestNewConfigWatcher_MinIntervalClamp(t *testing.T) {
	w := NewConfigWatcher("/tmp/test-clamp.json", 5*time.Millisecond)
	if w.interval != 10*time.Millisecond {
		t.Errorf("interval clamped to 10ms, got %v", w.interval)
	}
	w.Stop()
}

// ─── SaveFile ────────────────────────────────────────────────────────────────

func TestSaveFile_RoundTrip(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test-config.json")
	c := newDefaultConfig()
	c.DashboardPort = 9999
	c.GardenerEnabled = true

	if err := c.SaveFile(tmpFile); err != nil {
		t.Fatalf("SaveFile failed: %v", err)
	}

	loaded, err := LoadFile(tmpFile)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}
	if loaded.DashboardPort != 9999 {
		t.Errorf("DashboardPort = %d, want 9999", loaded.DashboardPort)
	}
	if !loaded.GardenerEnabled {
		t.Error("GardenerEnabled should be true from saved file")
	}
}

// ─── Validate ────────────────────────────────────────────────────────────────

func TestValidate_NegativeRateLimit(t *testing.T) {
	c := newDefaultConfig()
	c.RateLimitRPS = -1
	if err := c.Validate(); err == nil {
		t.Error("expected validation error for negative RateLimitRPS")
	}
}

func TestValidate_TLSCertWithoutKey(t *testing.T) {
	c := newDefaultConfig()
	c.TLSCert = "/tmp/cert.pem"
	c.TLSKey = ""
	if err := c.Validate(); err == nil {
		t.Error("expected validation error for TLS cert without key")
	}
}

func TestValidate_TLSKeyWithoutCert(t *testing.T) {
	c := newDefaultConfig()
	c.TLSCert = ""
	c.TLSKey = "/tmp/key.pem"
	if err := c.Validate(); err == nil {
		t.Error("expected validation error for TLS key without cert")
	}
}

func TestValidate_NegativeBurst(t *testing.T) {
	c := newDefaultConfig()
	c.RateLimitBurst = -5
	if err := c.Validate(); err == nil {
		t.Error("expected validation error for negative RateLimitBurst")
	}
}

func TestValidate_NegativeLLMTimeout(t *testing.T) {
	c := newDefaultConfig()
	c.LLMTimeout = -100
	if err := c.Validate(); err == nil {
		t.Error("expected validation error for negative LLMTimeout")
	}
}

// ─── LoadFileWithDotEnv ─────────────────────────────────────────────────────

func TestLoadFileWithDotEnv_NonexistentConfig(t *testing.T) {
	tmpDir := t.TempDir()
	dotEnvPath := filepath.Join(tmpDir, ".env")
	os.WriteFile(dotEnvPath, []byte("BT_DASHBOARD_PORT=8888\n"), 0644)

	_, err := LoadFileWithDotEnv(filepath.Join(tmpDir, "nonexistent.json"), dotEnvPath)
	if err == nil {
		t.Error("expected error when config file doesn't exist")
	}
}

func TestLoadFileWithDotEnv_NonexistentDotEnv(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")
	config := map[string]any{"dashboard_port": 7777}
	data := marshalConfig(t, config)
	os.WriteFile(configPath, data, 0644)

	c, err := LoadFileWithDotEnv(configPath, filepath.Join(tmpDir, "nonexistent.env"))
	if err != nil {
		t.Errorf("LoadFileWithDotEnv with missing .env should not error: %v", err)
	}
	if c.DashboardPort != 7777 {
		t.Errorf("DashboardPort = %d, want 7777", c.DashboardPort)
	}
}

func marshalConfig(t *testing.T, v map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return data
}

// ─── Load ──────────────────────────────────────────────────────────────────

func TestLoad_WithBTConfigFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("BT_CONFIG_FILE", filepath.Join(tmpDir, "no-such-file.json"))

	_, err := Load()
	if err == nil {
		t.Error("expected error when BT_CONFIG_FILE doesn't exist")
	}
}

// ─── Diff ──────────────────────────────────────────────────────────────────

func TestDiff_EmptyConfigs(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	diffs := a.Diff(b)
	if len(diffs) != 0 {
		t.Errorf("expected empty diff between identical configs, got %d diffs: %v", len(diffs), diffs)
	}
}

func TestDiff_DifferentValues(t *testing.T) {
	a := newDefaultConfig()
	b := newDefaultConfig()
	b.DashboardPort = 1111
	diffs := a.Diff(b)
	if len(diffs) == 0 {
		t.Errorf("expected non-empty diff between different configs")
	}
}
