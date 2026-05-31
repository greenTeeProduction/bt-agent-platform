package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// Unset any env vars that might interfere
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_LLM_TIMEOUT")
	os.Unsetenv("BT_FEATURE_AUTO_EVOLVE")
	os.Unsetenv("BT_CONFIG_FILE")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if c.DashboardPort != 9800 {
		t.Errorf("expected DashboardPort=9800, got %d", c.DashboardPort)
	}
	if c.LLMTimeout != 300 {
		t.Errorf("expected LLMTimeout=300, got %d", c.LLMTimeout)
	}
	if c.AutoEvolveEnabled {
		t.Error("expected AutoEvolveEnabled=false by default")
	}
	if c.OllamaModel != "qwen3.6:35b-a3b" {
		t.Errorf("expected OllamaModel=qwen3.6:35b-a3b, got %s", c.OllamaModel)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	os.Unsetenv("BT_CONFIG_FILE")
	os.Setenv("BT_DASHBOARD_PORT", "8080")
	os.Setenv("BT_OLLAMA_MODEL", "custom-model")
	os.Setenv("BT_LLM_TIMEOUT", "120")
	os.Setenv("BT_FEATURE_GARDENER", "false")
	defer func() {
		os.Unsetenv("BT_DASHBOARD_PORT")
		os.Unsetenv("BT_OLLAMA_MODEL")
		os.Unsetenv("BT_LLM_TIMEOUT")
		os.Unsetenv("BT_FEATURE_GARDENER")
	}()

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if c.DashboardPort != 8080 {
		t.Errorf("expected DashboardPort=8080, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "custom-model" {
		t.Errorf("expected OllamaModel=custom-model, got %s", c.OllamaModel)
	}
	if c.LLMTimeout != 120 {
		t.Errorf("expected LLMTimeout=120, got %d", c.LLMTimeout)
	}
	if c.GardenerEnabled {
		t.Error("expected GardenerEnabled=false")
	}
}

func TestLoadFile_Basic(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"dashboard_port": 1234,
		"ollama_model": "file-model",
		"llm_timeout": 60,
		"gardener_enabled": false,
		"thinktank_enabled": true
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Clear env vars that could interfere
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")
	os.Unsetenv("BT_LLM_TIMEOUT")
	os.Unsetenv("BT_FEATURE_GARDENER")
	os.Unsetenv("BT_FEATURE_THINKTANK")

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}

	if c.DashboardPort != 1234 {
		t.Errorf("expected DashboardPort=1234, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "file-model" {
		t.Errorf("expected OllamaModel=file-model, got %s", c.OllamaModel)
	}
	if c.LLMTimeout != 60 {
		t.Errorf("expected LLMTimeout=60, got %d", c.LLMTimeout)
	}
	if c.GardenerEnabled {
		t.Error("expected GardenerEnabled=false from file")
	}
	if !c.ThinktankEnabled {
		t.Error("expected ThinktankEnabled=true from file")
	}
	// Unset fields should use defaults
	if c.SchedulerCheckInterval != 60 {
		t.Errorf("expected default SchedulerCheckInterval=60, got %d", c.SchedulerCheckInterval)
	}
}

func TestLoadFile_EnvOverridesFile(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"dashboard_port": 1234,
		"ollama_model": "file-model"
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Env should override file
	os.Setenv("BT_DASHBOARD_PORT", "9999")
	defer os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}

	if c.DashboardPort != 9999 {
		t.Errorf("expected env override DashboardPort=9999, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "file-model" {
		t.Errorf("expected OllamaModel=file-model (no env override), got %s", c.OllamaModel)
	}
}

func TestLoadFile_MissingFile(t *testing.T) {
	_, err := LoadFile("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadFile_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cf, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadFile(cf)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoad_BTConfigFile(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"dashboard_port": 5555,
		"ollama_model": "from-file"
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_CONFIG_FILE", cf)
	defer os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() with BT_CONFIG_FILE failed: %v", err)
	}

	if c.DashboardPort != 5555 {
		t.Errorf("expected DashboardPort=5555 from file via BT_CONFIG_FILE, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "from-file" {
		t.Errorf("expected OllamaModel=from-file via BT_CONFIG_FILE, got %s", c.OllamaModel)
	}
	if c.ConfigFile != cf {
		t.Errorf("expected ConfigFile=%s, got %s", cf, c.ConfigFile)
	}
}

func TestLoad_BTConfigFile_EnvOverrides(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"dashboard_port": 5555,
		"ollama_model": "from-file"
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_CONFIG_FILE", cf)
	os.Setenv("BT_DASHBOARD_PORT", "7777") // env overrides file
	defer func() {
		os.Unsetenv("BT_CONFIG_FILE")
		os.Unsetenv("BT_DASHBOARD_PORT")
	}()
	os.Unsetenv("BT_OLLAMA_MODEL")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// Env overrides file
	if c.DashboardPort != 7777 {
		t.Errorf("expected env override DashboardPort=7777, got %d", c.DashboardPort)
	}
	// File value used when no env override
	if c.OllamaModel != "from-file" {
		t.Errorf("expected OllamaModel=from-file (no env override), got %s", c.OllamaModel)
	}
}

func TestSaveFile_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	cf1 := filepath.Join(dir, "config1.json")
	cf2 := filepath.Join(dir, "config2.json")

	// Create a config with non-default values
	c1 := newDefaultConfig()
	c1.DashboardPort = 4321
	c1.OllamaModel = "roundtrip-model"
	c1.GardenerEnabled = false
	c1.ThinktankEnabled = true
	c1.MaxBodySize = 5242880

	if err := c1.SaveFile(cf1); err != nil {
		t.Fatalf("SaveFile() failed: %v", err)
	}

	// Clear env vars, load the saved file
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")
	os.Unsetenv("BT_FEATURE_GARDENER")
	os.Unsetenv("BT_FEATURE_THINKTANK")
	os.Unsetenv("BT_MAX_BODY_SIZE")

	c2, err := LoadFile(cf1)
	if err != nil {
		t.Fatalf("LoadFile() roundtrip failed: %v", err)
	}

	if c2.DashboardPort != 4321 {
		t.Errorf("roundtrip: expected DashboardPort=4321, got %d", c2.DashboardPort)
	}
	if c2.OllamaModel != "roundtrip-model" {
		t.Errorf("roundtrip: expected OllamaModel=roundtrip-model, got %s", c2.OllamaModel)
	}
	if c2.GardenerEnabled {
		t.Error("roundtrip: expected GardenerEnabled=false")
	}
	if !c2.ThinktankEnabled {
		t.Error("roundtrip: expected ThinktankEnabled=true")
	}
	if c2.MaxBodySize != 5242880 {
		t.Errorf("roundtrip: expected MaxBodySize=5242880, got %d", c2.MaxBodySize)
	}

	// Save again and verify consistency
	if err := c2.SaveFile(cf2); err != nil {
		t.Fatalf("second SaveFile() failed: %v", err)
	}

	c3, err := LoadFile(cf2)
	if err != nil {
		t.Fatalf("second LoadFile() failed: %v", err)
	}

	if c3.DashboardPort != c2.DashboardPort {
		t.Error("second roundtrip: ports don't match")
	}
}

func TestLoadFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	if err := os.WriteFile(cf, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile(empty) failed: %v", err)
	}

	// Empty file should produce defaults
	if c.DashboardPort != 9800 {
		t.Errorf("expected default DashboardPort=9800, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "qwen3.6:35b-a3b" {
		t.Errorf("expected default OllamaModel, got %s", c.OllamaModel)
	}
}

func TestValidate_Valid(t *testing.T) {
	c, _ := Load()
	if err := c.Validate(); err != nil {
		t.Errorf("expected valid config, got: %v", err)
	}
}

func TestValidate_Invalid(t *testing.T) {
	c, _ := Load()
	c.DashboardPort = 0   // invalid
	c.LLMTimeout = 0      // invalid
	c.GardenerMaxNodes = 0 // invalid

	err := c.Validate()
	if err == nil {
		t.Error("expected validation errors")
	}
}

func TestFeatureFlags(t *testing.T) {
	c, _ := Load()
	flags := c.FeatureFlags()

	expected := []string{"gardener", "scheduler", "auto_evolve", "kanban", "thinktank", "startup_sim"}
	for _, key := range expected {
		if _, ok := flags[key]; !ok {
			t.Errorf("missing feature flag: %s", key)
		}
	}
}

func TestEnvBool(t *testing.T) {
	tests := []struct {
		val      string
		expected bool
	}{
		{"1", true}, {"true", true}, {"yes", true}, {"on", true},
		{"0", false}, {"false", false}, {"no", false}, {"off", false},
		{"TRUE", true}, {"FALSE", false},
	}

	for _, tt := range tests {
		os.Setenv("_TEST_BOOL", tt.val)
		got := envBool("_TEST_BOOL", !tt.expected) // use opposite default
		os.Unsetenv("_TEST_BOOL")
		if got != tt.expected {
			t.Errorf("envBool(%q) = %v, want %v", tt.val, got, tt.expected)
		}
	}
}

func TestTLS_Defaults(t *testing.T) {
	os.Unsetenv("BT_TLS_CERT")
	os.Unsetenv("BT_TLS_KEY")

	c, _ := Load()
	if c.TLSEnabled() {
		t.Error("expected TLSEnabled()=false by default")
	}
	if err := c.Validate(); err != nil {
		t.Errorf("valid config (no TLS) should pass validation, got: %v", err)
	}
}

func TestTLS_Enabled(t *testing.T) {
	os.Setenv("BT_TLS_CERT", "/etc/certs/server.crt")
	os.Setenv("BT_TLS_KEY", "/etc/certs/server.key")
	defer os.Unsetenv("BT_TLS_CERT")
	defer os.Unsetenv("BT_TLS_KEY")

	c, _ := Load()
	if !c.TLSEnabled() {
		t.Error("expected TLSEnabled()=true when both cert and key set")
	}
	if c.TLSCert != "/etc/certs/server.crt" {
		t.Errorf("expected TLSCert='/etc/certs/server.crt', got %s", c.TLSCert)
	}
	if c.TLSKey != "/etc/certs/server.key" {
		t.Errorf("expected TLSKey='/etc/certs/server.key', got %s", c.TLSKey)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("valid TLS config should pass validation, got: %v", err)
	}
}

func TestTLS_MissingKey(t *testing.T) {
	os.Setenv("BT_TLS_CERT", "/etc/certs/server.crt")
	os.Unsetenv("BT_TLS_KEY")
	defer os.Unsetenv("BT_TLS_CERT")

	c, _ := Load()
	if c.TLSEnabled() {
		t.Error("expected TLSEnabled()=false when only cert set")
	}
	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when cert set but key missing")
	}
}

func TestTLS_MissingCert(t *testing.T) {
	os.Unsetenv("BT_TLS_CERT")
	os.Setenv("BT_TLS_KEY", "/etc/certs/server.key")
	defer os.Unsetenv("BT_TLS_KEY")

	c, _ := Load()
	if c.TLSEnabled() {
		t.Error("expected TLSEnabled()=false when only key set")
	}
	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when key set but cert missing")
	}
}

func TestTLS_FileConfig(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"tls_cert": "/etc/certs/file.crt",
		"tls_key": "/etc/certs/file.key"
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}
	if !c.TLSEnabled() {
		t.Error("expected TLS enabled from config file")
	}
	if c.TLSCert != "/etc/certs/file.crt" {
		t.Errorf("expected TLSCert from file, got %s", c.TLSCert)
	}
	if c.TLSKey != "/etc/certs/file.key" {
		t.Errorf("expected TLSKey from file, got %s", c.TLSKey)
	}
}

func TestLoadFile_BooleanExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	cf := filepath.Join(dir, "config.json")
	content := `{
		"gardener_enabled": false,
		"scheduler_enabled": false
	}`
	if err := os.WriteFile(cf, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Unsetenv("BT_FEATURE_GARDENER")
	os.Unsetenv("BT_FEATURE_SCHEDULER")

	c, err := LoadFile(cf)
	if err != nil {
		t.Fatalf("LoadFile() failed: %v", err)
	}

	if c.GardenerEnabled {
		t.Error("expected GardenerEnabled=false when explicitly set to false in file")
	}
	if c.SchedulerEnabled {
		t.Error("expected SchedulerEnabled=false when explicitly set to false in file")
	}
}

// ─── .env Loading Tests ────────────────────────────────────────────────────

func TestLoadDotEnv_Basic(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_DASHBOARD_PORT=7777\nBT_OLLAMA_MODEL=env-model\nBT_LLM_TIMEOUT=120\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_DASHBOARD_PORT"] != "7777" {
		t.Errorf("expected BT_DASHBOARD_PORT=7777, got %q", kv["BT_DASHBOARD_PORT"])
	}
	if kv["BT_OLLAMA_MODEL"] != "env-model" {
		t.Errorf("expected BT_OLLAMA_MODEL=env-model, got %q", kv["BT_OLLAMA_MODEL"])
	}
	if kv["BT_LLM_TIMEOUT"] != "120" {
		t.Errorf("expected BT_LLM_TIMEOUT=120, got %q", kv["BT_LLM_TIMEOUT"])
	}
}

func TestLoadDotEnv_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "# This is a comment\nBT_API_KEY=secret123\n\n# Another comment\nBT_FEATURE_GARDENER=false\n\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if len(kv) != 2 {
		t.Errorf("expected 2 entries, got %d: %v", len(kv), kv)
	}
	if kv["BT_API_KEY"] != "secret123" {
		t.Errorf("expected BT_API_KEY=secret123, got %q", kv["BT_API_KEY"])
	}
	if kv["BT_FEATURE_GARDENER"] != "false" {
		t.Errorf("expected BT_FEATURE_GARDENER=false, got %q", kv["BT_FEATURE_GARDENER"])
	}
}

func TestLoadDotEnv_QuotedValues(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_API_KEY=\"my secret key\"\nBT_OLLAMA_MODEL='quoted-model'\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_API_KEY"] != "my secret key" {
		t.Errorf("expected BT_API_KEY=my secret key, got %q", kv["BT_API_KEY"])
	}
	if kv["BT_OLLAMA_MODEL"] != "quoted-model" {
		t.Errorf("expected BT_OLLAMA_MODEL=quoted-model, got %q", kv["BT_OLLAMA_MODEL"])
	}
}

func TestLoadDotEnv_ExportPrefix(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "export BT_DASHBOARD_PORT=9999\nexport BT_FEATURE_GARDENER=true\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_DASHBOARD_PORT"] != "9999" {
		t.Errorf("expected BT_DASHBOARD_PORT=9999, got %q", kv["BT_DASHBOARD_PORT"])
	}
	if kv["BT_FEATURE_GARDENER"] != "true" {
		t.Errorf("expected BT_FEATURE_GARDENER=true, got %q", kv["BT_FEATURE_GARDENER"])
	}
}

func TestLoadDotEnv_InlineComments(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_API_KEY=secret123  # API key for dashboard\nBT_LLM_TIMEOUT=120 # 2 minutes\n# pure comment line\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_API_KEY"] != "secret123" {
		t.Errorf("expected BT_API_KEY=secret123, got %q", kv["BT_API_KEY"])
	}
	if kv["BT_LLM_TIMEOUT"] != "120" {
		t.Errorf("expected BT_LLM_TIMEOUT=120, got %q", kv["BT_LLM_TIMEOUT"])
	}
}

func TestLoadDotEnv_MissingFile(t *testing.T) {
	_, err := LoadDotEnv("/nonexistent/.env")
	if err == nil {
		t.Error("expected error for missing .env file")
	}
}

func TestLoadDotEnv_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	if err := os.WriteFile(envFile, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}
	if len(kv) != 0 {
		t.Errorf("expected 0 entries from empty file, got %d", len(kv))
	}
}

func TestLoad_DotEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_DASHBOARD_PORT=6666\nBT_OLLAMA_MODEL=dotenv-model\nBT_FEATURE_GARDENER=false\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Set BT_DOTENV_FILE and clear env vars
	os.Setenv("BT_DOTENV_FILE", envFile)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DASHBOARD_PORT")
	os.Unsetenv("BT_OLLAMA_MODEL")
	os.Unsetenv("BT_FEATURE_GARDENER")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() with .env failed: %v", err)
	}

	if c.DashboardPort != 6666 {
		t.Errorf("expected DashboardPort=6666 from .env, got %d", c.DashboardPort)
	}
	if c.OllamaModel != "dotenv-model" {
		t.Errorf("expected OllamaModel=dotenv-model from .env, got %s", c.OllamaModel)
	}
	if c.GardenerEnabled {
		t.Error("expected GardenerEnabled=false from .env")
	}
	// Unset fields should use defaults
	if c.LLMTimeout != 300 {
		t.Errorf("expected default LLMTimeout=300, got %d", c.LLMTimeout)
	}
}

func TestLoad_DotEnvOverriddenByEnvVar(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_DASHBOARD_PORT=6666\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// .env says 6666 but env var says 8888 — env var must win
	os.Setenv("BT_DOTENV_FILE", envFile)
	os.Setenv("BT_DASHBOARD_PORT", "8888")
	defer func() {
		os.Unsetenv("BT_DOTENV_FILE")
		os.Unsetenv("BT_DASHBOARD_PORT")
	}()
	os.Unsetenv("BT_CONFIG_FILE")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if c.DashboardPort != 8888 {
		t.Errorf("expected env var DashboardPort=8888 (overriding .env=6666), got %d", c.DashboardPort)
	}
}

func TestLoad_DotEnvRespectsEnvVarPrecedence(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_API_KEY=dotenv-key\nBT_OLLAMA_MODEL=dotenv-model\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Only set BT_API_KEY env var, not BT_OLLAMA_MODEL
	os.Setenv("BT_DOTENV_FILE", envFile)
	os.Setenv("BT_API_KEY", "env-var-key")
	defer func() {
		os.Unsetenv("BT_DOTENV_FILE")
		os.Unsetenv("BT_API_KEY")
	}()
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_OLLAMA_MODEL")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// BT_API_KEY should come from env var (overrides .env)
	if c.APIKey != "env-var-key" {
		t.Errorf("expected APIKey=env-var-key (env var overrides .env), got %s", c.APIKey)
	}
	// BT_OLLAMA_MODEL should come from .env (no env var set)
	if c.OllamaModel != "dotenv-model" {
		t.Errorf("expected OllamaModel=dotenv-model (from .env, no env override), got %s", c.OllamaModel)
	}
}

func TestLoad_DotEnvAndConfigFile(t *testing.T) {
	// Priority: defaults → config.json → .env → env vars
	dir := t.TempDir()

	// Create config.json
	cfgFile := filepath.Join(dir, "config.json")
	cfgContent := `{"dashboard_port": 1111, "ollama_model": "cfg-model"}`
	if err := os.WriteFile(cfgFile, []byte(cfgContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create .env
	envFile := filepath.Join(dir, ".env")
	envContent := "BT_DASHBOARD_PORT=2222\n" // .env overrides config.json
	if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_CONFIG_FILE", cfgFile)
	os.Setenv("BT_DOTENV_FILE", envFile)
	defer func() {
		os.Unsetenv("BT_CONFIG_FILE")
		os.Unsetenv("BT_DOTENV_FILE")
	}()

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// .env overrides config.json
	if c.DashboardPort != 2222 {
		t.Errorf("expected DashboardPort=2222 (.env overrides config.json=1111), got %d", c.DashboardPort)
	}
	// Model comes from config.json (not in .env)
	if c.OllamaModel != "cfg-model" {
		t.Errorf("expected OllamaModel=cfg-model (from config.json), got %s", c.OllamaModel)
	}
}

func TestLoad_DotEnvDeepSeekKey(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "DEEPSEEK_API_KEY=sk-dotenv-key\n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	os.Setenv("BT_DOTENV_FILE", envFile)
	defer os.Unsetenv("BT_DOTENV_FILE")
	os.Unsetenv("BT_CONFIG_FILE")
	os.Unsetenv("BT_DEEPSEEK_KEY")
	os.Unsetenv("DEEPSEEK_API_KEY")

	c, err := Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	// DEEPSEEK_API_KEY in .env should map to DeepSeekKey
	if c.DeepSeekKey != "sk-dotenv-key" {
		t.Errorf("expected DeepSeekKey=sk-dotenv-key from .env DEEPSEEK_API_KEY, got %q", c.DeepSeekKey)
	}
}

func TestLoadDotEnv_SpacesAroundEquals(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	content := "BT_API_KEY = padded-key\nBT_OLLAMA_MODEL=  trimmed-model  \n"
	if err := os.WriteFile(envFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	kv, err := LoadDotEnv(envFile)
	if err != nil {
		t.Fatalf("LoadDotEnv() failed: %v", err)
	}

	if kv["BT_API_KEY"] != "padded-key" {
		t.Errorf("expected BT_API_KEY=padded-key, got %q", kv["BT_API_KEY"])
	}
	if kv["BT_OLLAMA_MODEL"] != "trimmed-model" {
		t.Errorf("expected BT_OLLAMA_MODEL=trimmed-model, got %q", kv["BT_OLLAMA_MODEL"])
	}
}

func TestConfig_Sanitized_RedactsSecrets(t *testing.T) {
	c := &Config{
		DashboardPort: 9800,
		APIKey:        "secret-api-key-12345",
		DeepSeekKey:   "sk-deepseek-secret",
		TLSCert:       "/path/to/cert.pem",
		TLSKey:        "/path/to/key.pem",
		OllamaModel:   "qwen3.6:35b-a3b",
		LLMTimeout:    300,
	}
	s := c.Sanitized()

	if s.APIKey != "[REDACTED]" {
		t.Errorf("expected APIKey to be redacted, got %q", s.APIKey)
	}
	if s.DeepSeekKey != "[REDACTED]" {
		t.Errorf("expected DeepSeekKey to be redacted, got %q", s.DeepSeekKey)
	}
	if s.TLSCert != "[REDACTED]" {
		t.Errorf("expected TLSCert to be redacted, got %q", s.TLSCert)
	}
	if s.TLSKey != "[REDACTED]" {
		t.Errorf("expected TLSKey to be redacted, got %q", s.TLSKey)
	}

	// Non-secret fields should be preserved
	if s.DashboardPort != 9800 {
		t.Errorf("expected DashboardPort=9800, got %d", s.DashboardPort)
	}
	if s.OllamaModel != "qwen3.6:35b-a3b" {
		t.Errorf("expected OllamaModel=qwen3.6:35b-a3b, got %q", s.OllamaModel)
	}
	if s.LLMTimeout != 300 {
		t.Errorf("expected LLMTimeout=300, got %d", s.LLMTimeout)
	}

	// Original should be unchanged
	if c.APIKey != "secret-api-key-12345" {
		t.Error("original config was mutated")
	}
}

func TestConfig_Sanitized_EmptySecrets(t *testing.T) {
	c := &Config{
		DashboardPort: 9800,
		OllamaModel:   "qwen3.6:35b-a3b",
	}
	s := c.Sanitized()

	if s.APIKey != "" {
		t.Errorf("expected empty APIKey to stay empty, got %q", s.APIKey)
	}
	if s.DeepSeekKey != "" {
		t.Errorf("expected empty DeepSeekKey to stay empty, got %q", s.DeepSeekKey)
	}
	if s.TLSCert != "" {
		t.Errorf("expected empty TLSCert to stay empty, got %q", s.TLSCert)
	}
}

// ─── Validation Edge Case Tests ─────────────────────────────────────────────

func TestValidate_OllamaProvider_NoHost(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaHost = "" // empty host should fail

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when OllamaHost is empty with ollama provider")
	}
}

func TestValidate_DeepSeekProvider_NoKey(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "" // empty key should fail

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when DeepSeekKey is empty with deepseek provider")
	}
}

func TestValidate_DeepSeekProvider_WithKey(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-valid-deepseek-key"
	c.DeepSeekHost = "https://api.deepseek.com/v1"

	err := c.Validate()
	if err != nil {
		t.Errorf("expected valid deepseek config, got: %v", err)
	}
}

func TestValidate_GardenerCycleInterval_Max(t *testing.T) {
	c := newDefaultConfig()
	c.GardenerCycleInterval = 86400 // exactly at max (1 day)

	err := c.Validate()
	if err != nil {
		t.Errorf("expected GardenerCycleInterval=86400 to be valid, got: %v", err)
	}
}

func TestValidate_GardenerCycleInterval_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.GardenerCycleInterval = 86401 // just above max

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when GardenerCycleInterval > 86400")
	}
}

func TestValidate_SchedulerCheckInterval_Min(t *testing.T) {
	c := newDefaultConfig()
	c.SchedulerCheckInterval = 10 // exactly at min

	err := c.Validate()
	if err != nil {
		t.Errorf("expected SchedulerCheckInterval=10 to be valid, got: %v", err)
	}
}

func TestValidate_SchedulerCheckInterval_Max(t *testing.T) {
	c := newDefaultConfig()
	c.SchedulerCheckInterval = 3600 // exactly at max

	err := c.Validate()
	if err != nil {
		t.Errorf("expected SchedulerCheckInterval=3600 to be valid, got: %v", err)
	}
}

func TestValidate_SchedulerCheckInterval_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.SchedulerCheckInterval = 3601 // just above max

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when SchedulerCheckInterval > 3600")
	}
}

func TestValidate_AllEdgeCases(t *testing.T) {
	c := newDefaultConfig()
	c.DashboardPort = 0         // invalid
	c.LLMTimeout = 0            // invalid
	c.GardenerMaxNodes = 0      // invalid
	c.MaxBodySize = 0           // invalid
	c.GardenerCycleInterval = 5 // invalid (< 10)
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = ""          // invalid (no key for deepseek)
	c.RateLimitBurst = -1       // invalid (negative)

	err := c.Validate()
	if err == nil {
		t.Error("expected multiple validation errors")
	}
	verrs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	// We expect at least 6 errors
	if len(verrs) < 6 {
		t.Errorf("expected at least 6 validation errors, got %d", len(verrs))
	}
}

func TestValidate_LLMProvider_Invalid(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "openai" // not ollama or deepseek

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error for invalid LLMProvider")
	}
}

func TestValidate_RateLimitRPS_Negative(t *testing.T) {
	c := newDefaultConfig()
	c.RateLimitRPS = -1.5

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error for negative RateLimitRPS")
	}
}

func TestValidate_GardenerMutationsPer_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.GardenerMutationsPer = 11

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when GardenerMutationsPer > 10")
	}
}

func TestValidate_GardenerMutationsPer_Negative(t *testing.T) {
	c := newDefaultConfig()
	c.GardenerMutationsPer = -1

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when GardenerMutationsPer < 0")
	}
}

func TestValidate_MaxBodySize_TooSmall(t *testing.T) {
	c := newDefaultConfig()
	c.MaxBodySize = 512 // below min of 1024

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when MaxBodySize < 1024")
	}
}

func TestValidate_MaxBodySize_TooLarge(t *testing.T) {
	c := newDefaultConfig()
	c.MaxBodySize = 200 * 1024 * 1024 // above max of 100MB

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when MaxBodySize > 100MB")
	}
}

func TestValidate_BoundaryValues_AllValid(t *testing.T) {
	c := newDefaultConfig()
	c.DashboardPort = 1          // min
	c.LLMTimeout = 1             // min
	c.RateLimitRPS = 0           // min
	c.RateLimitBurst = 0         // min
	c.GardenerCycleInterval = 10 // min
	c.GardenerMutationsPer = 0   // min
	c.GardenerMaxNodes = 1       // min
	c.SchedulerCheckInterval = 10 // min
	c.MaxBodySize = 1024         // min

	err := c.Validate()
	if err != nil {
		t.Errorf("expected boundary minimums to be valid, got: %v", err)
	}

	c2 := newDefaultConfig()
	c2.DashboardPort = 65535              // max
	c2.LLMTimeout = 3600                  // max
	c2.GardenerCycleInterval = 86400      // max
	c2.GardenerMutationsPer = 10          // max
	c2.GardenerMaxNodes = 100             // max
	c2.SchedulerCheckInterval = 3600      // max
	c2.MaxBodySize = 100 * 1024 * 1024    // max

	err = c2.Validate()
	if err != nil {
		t.Errorf("expected boundary maximums to be valid, got: %v", err)
	}
}

func TestValidate_LLMTimeout_Max(t *testing.T) {
	c := newDefaultConfig()
	c.LLMTimeout = 3600 // exactly at max

	err := c.Validate()
	if err != nil {
		t.Errorf("expected LLMTimeout=3600 to be valid, got: %v", err)
	}
}

func TestValidate_LLMTimeout_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.LLMTimeout = 3601 // just above max

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when LLMTimeout > 3600")
	}
}

func TestValidate_DashboardPort_Max(t *testing.T) {
	c := newDefaultConfig()
	c.DashboardPort = 65535 // exactly at max

	err := c.Validate()
	if err != nil {
		t.Errorf("expected DashboardPort=65535 to be valid, got: %v", err)
	}
}

func TestValidate_DashboardPort_TooHigh(t *testing.T) {
	c := newDefaultConfig()
	c.DashboardPort = 65536 // just above max

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when DashboardPort > 65535")
	}
}

func TestValidate_OllamaProvider_ModelEmpty(t *testing.T) {
	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaModel = ""

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when OllamaModel is empty with ollama provider")
	}
}

func TestValidate_TLS_CertOnly(t *testing.T) {
	c := newDefaultConfig()
	c.TLSCert = "/path/to/cert.pem"
	c.TLSKey = ""

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when TLS cert set but key missing")
	}
}

func TestValidate_TLS_KeyOnly(t *testing.T) {
	c := newDefaultConfig()
	c.TLSCert = ""
	c.TLSKey = "/path/to/key.pem"

	err := c.Validate()
	if err == nil {
		t.Error("expected validation error when TLS key set but cert missing")
	}
}

// ─── CheckRuntime Tests ────────────────────────────────────────────────────

func TestCheckRuntime_AllOk(t *testing.T) {
	// Use a real directory that exists for persistence paths.
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.ReflectionsDir = tmp
	c.AgentDefsDir = tmp
	c.HistoryDir = tmp
	c.LogDir = tmp
	// No TLS, no config file, no Ollama reachability — these are skipped.
	// Set deepseek provider to skip ollama check.
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true, got Ok=false with %d issues: %+v", len(report.Issues), report.Issues)
	}
}

func TestCheckRuntime_TLSCertNotFound(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.TLSCert = filepath.Join(tmp, "nonexistent.pem")
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when TLS cert file doesn't exist")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "TLSCert" && iss.Severity == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TLSCert error, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_TLSKeyNotFound(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.TLSKey = filepath.Join(tmp, "nonexistent.pem")
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when TLS key file doesn't exist")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "TLSKey" && iss.Severity == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected TLSKey error, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_TLSFilesExist(t *testing.T) {
	tmp := t.TempDir()
	certPath := filepath.Join(tmp, "cert.pem")
	keyPath := filepath.Join(tmp, "key.pem")
	os.WriteFile(certPath, []byte("fake-cert"), 0644)
	os.WriteFile(keyPath, []byte("fake-key"), 0644)

	c := newDefaultConfig()
	c.TLSCert = certPath
	c.TLSKey = keyPath
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true when TLS files exist, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_DirExists(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.ReflectionsDir = tmp
	c.AgentDefsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for valid directories, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_DirIsFile(t *testing.T) {
	tmp := t.TempDir()
	filePath := filepath.Join(tmp, "notadir.txt")
	os.WriteFile(filePath, []byte("data"), 0644)

	c := newDefaultConfig()
	c.ReflectionsDir = filePath

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when dir path is a file")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "ReflectionsDir" && iss.Severity == "error" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ReflectionsDir error, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_DirParentMissing(t *testing.T) {
	c := newDefaultConfig()
	c.ReflectionsDir = "/nonexistent/parent/subdir"

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when parent directory doesn't exist")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "ReflectionsDir" && iss.Severity == "warning" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ReflectionsDir warning for missing parent, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_ConfigFileNotFound(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.ConfigFile = filepath.Join(tmp, "nonexistent.json")
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when config file doesn't exist")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "ConfigFile" && iss.Severity == "warning" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ConfigFile warning, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_OllamaUnreachable(t *testing.T) {
	tmp := t.TempDir()
	// Override ollamaChecker to simulate unreachable host.
	oldChecker := ollamaChecker
	ollamaChecker = func(host string) bool { return false }
	defer func() { ollamaChecker = oldChecker }()

	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaHost = "http://localhost:11434"
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false when Ollama is unreachable")
	}
	found := false
	for _, iss := range report.Issues {
		if iss.Component == "OllamaHost" && iss.Severity == "warning" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected OllamaHost warning, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_OllamaReachable(t *testing.T) {
	tmp := t.TempDir()
	oldChecker := ollamaChecker
	ollamaChecker = func(host string) bool { return true }
	defer func() { ollamaChecker = oldChecker }()

	c := newDefaultConfig()
	c.LLMProvider = "ollama"
	c.OllamaHost = "http://localhost:11434"
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true when Ollama is reachable, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_DeepSeekNoOllamaCheck(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for deepseek provider (no Ollama check), got: %+v", report.Issues)
	}
	// Verify no OllamaHost issues.
	for _, iss := range report.Issues {
		if iss.Component == "OllamaHost" {
			t.Error("expected no OllamaHost check for deepseek provider")
		}
	}
}

func TestCheckRuntime_AllEmptyPaths(t *testing.T) {
	c := newDefaultConfig()
	// All persistence dirs empty, no TLS, no config file.
	c.ReflectionsDir = ""
	c.AgentDefsDir = ""
	c.HistoryDir = ""
	c.LogDir = ""
	c.LLMProvider = "deepseek"
	c.DeepSeekKey = "sk-test"

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for all-empty paths, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_MultipleIssues(t *testing.T) {
	tmp := t.TempDir()
	c := newDefaultConfig()
	c.TLSCert = filepath.Join(tmp, "missing.pem")
	c.TLSKey = filepath.Join(tmp, "also-missing.pem")
	c.ReflectionsDir = filepath.Join(tmp, "notadir.txt")
	os.WriteFile(c.ReflectionsDir, []byte("data"), 0644)
	c.AgentDefsDir = "/nonexistent/parent/subdir"
	c.ConfigFile = filepath.Join(tmp, "nosuch.json")

	report := c.CheckRuntime()
	if report.Ok {
		t.Error("expected Ok=false with multiple issues")
	}
	if len(report.Issues) < 4 {
		t.Errorf("expected at least 4 issues, got %d: %+v", len(report.Issues), report.Issues)
	}
}

func TestCheckRuntime_CreatedDir_Valid(t *testing.T) {
	// A newly-created temp dir should be valid.
	tmp := t.TempDir()
	subdir := filepath.Join(tmp, "subdir")
	os.Mkdir(subdir, 0755)

	c := newDefaultConfig()
	c.ReflectionsDir = subdir

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true for newly created directory, got: %+v", report.Issues)
	}
}

func TestCheckRuntime_TLSBothFilesOk(t *testing.T) {
	tmp := t.TempDir()
	certPath := filepath.Join(tmp, "cert.pem")
	keyPath := filepath.Join(tmp, "key.pem")
	os.WriteFile(certPath, []byte("cert"), 0644)
	os.WriteFile(keyPath, []byte("key"), 0644)

	c := newDefaultConfig()
	c.TLSCert = certPath
	c.TLSKey = keyPath
	c.ReflectionsDir = tmp

	report := c.CheckRuntime()
	if !report.Ok {
		t.Errorf("expected Ok=true with both TLS files present, got: %+v", report.Issues)
	}
}
