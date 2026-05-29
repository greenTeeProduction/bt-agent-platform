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
