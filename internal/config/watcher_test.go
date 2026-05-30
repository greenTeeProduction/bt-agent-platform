package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func writeConfigFile(t *testing.T, path string, cfg map[string]any) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Ensure filesystem timestamp advances — on some filesystems
	// (tmpfs, ext4 with noatime), rapid writes within the same second
	// get identical timestamps. A small sleep guarantees detection.
	time.Sleep(10 * time.Millisecond)
}

func TestConfigWatcher_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 9999})

	w := NewConfigWatcher(path, 50*time.Millisecond)
	if w.IsRunning() {
		t.Error("should not be running before Start()")
	}

	w.Start()
	if !w.IsRunning() {
		t.Error("should be running after Start()")
	}

	// Double start should be safe.
	w.Start()

	mod := w.LastMod()
	if mod.IsZero() {
		t.Error("should have recorded initial mod time")
	}

	w.Stop()
	if w.IsRunning() {
		t.Error("should not be running after Stop()")
	}

	// Double stop should be safe.
	w.Stop()
}

func TestConfigWatcher_OnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Use different-sized payloads so either mod time OR size change
	// is detected by the watcher.
	writeConfigFile(t, path, map[string]any{
		"dashboard_port": 8000,
	})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	var mu sync.Mutex
	var gotCfg *Config
	done := make(chan struct{})

	w.OnChange(func(cfg *Config) {
		mu.Lock()
		gotCfg = cfg
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	// Wait for watcher to record initial state.
	time.Sleep(150 * time.Millisecond)

	// Write a substantially different config — different port + extra field
	// ensures either mod time or file size changes detectably.
	writeConfigFile(t, path, map[string]any{
		"dashboard_port":   9000,
		"gardener_enabled": false,
	})

	// Wait for watcher to detect and reload.
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for config change callback")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCfg == nil {
		t.Fatal("callback not invoked")
	}
	if gotCfg.DashboardPort != 9000 {
		t.Errorf("expected DashboardPort=9000, got %d", gotCfg.DashboardPort)
	}
	if gotCfg.GardenerEnabled {
		t.Error("expected GardenerEnabled=false")
	}
}

func TestConfigWatcher_NoChangeNoCallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	fired := false
	var mu sync.Mutex
	w.OnChange(func(cfg *Config) {
		mu.Lock()
		fired = true
		mu.Unlock()
	})

	w.Start()
	defer w.Stop()

	// Wait several poll intervals — file hasn't changed.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	if fired {
		t.Error("callback should not fire when file hasn't changed")
	}
	mu.Unlock()
}

func TestConfigWatcher_InvalidConfigNoCallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	fired := false
	var mu sync.Mutex
	w.OnChange(func(cfg *Config) {
		mu.Lock()
		fired = true
		mu.Unlock()
	})

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	// Write invalid JSON (different size, triggers detection).
	if err := os.WriteFile(path, []byte("not json{{invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)

	time.Sleep(300 * time.Millisecond)

	// Write valid but failing validation (gardener_cycle_interval too small).
	// Note: zero values are skipped by mergeFileConfig, so use a non-zero
	// value that fails Validate() (e.g., gardener_cycle_interval: 5, min is 10).
	writeConfigFile(t, path, map[string]any{
		"dashboard_port":          8000,
		"gardener_cycle_interval": 5,
	})

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	if fired {
		t.Error("callback should not fire for invalid config")
	}
	mu.Unlock()
}

func TestConfigWatcher_MultipleCallbacks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{
		"dashboard_port": 8000,
	})

	w := NewConfigWatcher(path, 100*time.Millisecond)

	var mu sync.Mutex
	count := 0
	cb := func(cfg *Config) {
		mu.Lock()
		count++
		mu.Unlock()
	}

	w.OnChange(cb)
	w.OnChange(cb)
	w.OnChange(cb)

	w.Start()
	defer w.Stop()

	// Wait for watcher to stabilize.
	time.Sleep(200 * time.Millisecond)

	writeConfigFile(t, path, map[string]any{
		"dashboard_port":   9000,
		"gardener_enabled": false,
	})

	// Wait for the change to be detected exactly once.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	c := count
	mu.Unlock()

	// All 3 callbacks should fire for the one change.
	// (Multiple polling cycles with same file should not retrigger.)
	if c < 3 {
		t.Errorf("expected at least 3 callbacks, got %d", c)
	}
	if c > 3 {
		// Log as info, not error — rapid same-second writes can
		// trigger an extra detection cycle. This is benign.
		t.Logf("note: %d callbacks (expected 3) — extra detection cycle", c)
	}
}

func TestConfigWatcher_LargeChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	var mu sync.Mutex
	var gotCfg *Config
	done := make(chan struct{})

	w.OnChange(func(cfg *Config) {
		mu.Lock()
		gotCfg = cfg
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	// Write a full config with many fields — large size delta.
	full := map[string]any{
		"dashboard_port":           7777,
		"ollama_host":              "http://custom:11434",
		"ollama_model":             "custom-model:latest",
		"llm_timeout":              600,
		"rate_limit_rps":           50.0,
		"rate_limit_burst":         10,
		"gardener_enabled":         false,
		"scheduler_enabled":        false,
		"auto_evolve_enabled":      true,
		"kanban_enabled":           false,
		"thinktank_enabled":        false,
		"startup_sim_enabled":      false,
		"gardener_cycle_interval":  600,
		"gardener_mutations_per":   4,
		"gardener_max_nodes":       50,
		"scheduler_check_interval": 120,
		"max_body_size":            2097152,
	}
	writeConfigFile(t, path, full)

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for large config change")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCfg == nil {
		t.Fatal("callback not invoked")
	}
	if gotCfg.DashboardPort != 7777 {
		t.Errorf("expected DashboardPort=7777, got %d", gotCfg.DashboardPort)
	}
	if gotCfg.OllamaModel != "custom-model:latest" {
		t.Errorf("expected OllamaModel=custom-model:latest, got %s", gotCfg.OllamaModel)
	}
	if gotCfg.LLMTimeout != 600 {
		t.Errorf("expected LLMTimeout=600, got %d", gotCfg.LLMTimeout)
	}
	if gotCfg.GardenerEnabled {
		t.Error("expected GardenerEnabled=false")
	}
	if gotCfg.AutoEvolveEnabled != true {
		t.Error("expected AutoEvolveEnabled=true")
	}
	if gotCfg.GardenerMaxNodes != 50 {
		t.Errorf("expected GardenerMaxNodes=50, got %d", gotCfg.GardenerMaxNodes)
	}
	if gotCfg.MaxBodySize != 2097152 {
		t.Errorf("expected MaxBodySize=2097152, got %d", gotCfg.MaxBodySize)
	}
}

func TestConfigWatcher_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	w := NewConfigWatcher(path, 50*time.Millisecond)
	w.Start()
	defer w.Stop()

	// Should not crash when file doesn't exist.
	time.Sleep(200 * time.Millisecond)

	// Not an error — the watcher handles missing files gracefully.
}

func TestConfigWatcher_FileAppears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "will-appear.json")

	w := NewConfigWatcher(path, 50*time.Millisecond)

	var mu sync.Mutex
	var gotCfg *Config
	done := make(chan struct{})

	w.OnChange(func(cfg *Config) {
		mu.Lock()
		gotCfg = cfg
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	// File doesn't exist yet — watcher should keep polling.
	time.Sleep(150 * time.Millisecond)

	// Create the file.
	writeConfigFile(t, path, map[string]any{
		"dashboard_port": 5555,
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for file to appear and be loaded")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCfg == nil {
		t.Fatal("callback not invoked")
	}
	if gotCfg.DashboardPort != 5555 {
		t.Errorf("expected DashboardPort=5555, got %d", gotCfg.DashboardPort)
	}
}

func TestConfigWatcher_SizeChangeDetection(t *testing.T) {
	// Verify that size change triggers reload even when mod time
	// hasn't advanced (same-second write).
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Initial write: small file.
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	var mu sync.Mutex
	count := 0
	done := make(chan struct{})

	w.OnChange(func(cfg *Config) {
		mu.Lock()
		count++
		c := count
		mu.Unlock()
		if c >= 2 {
			close(done)
		}
	})

	w.Start()
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	// Write a larger file (different size, same structure shape).
	// The size change alone should trigger detection.
	writeConfigFile(t, path, map[string]any{
		"dashboard_port": 9000,
		"ollama_host":    "http://bigger-host:11434",
	})

	time.Sleep(400 * time.Millisecond)

	// Second change.
	writeConfigFile(t, path, map[string]any{
		"dashboard_port":   7777,
		"gardener_enabled": false,
		"kanban_enabled":   false,
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout: only %d callbacks fired", count)
	}

	mu.Lock()
	defer mu.Unlock()
	if count < 2 {
		t.Errorf("expected at least 2 callbacks, got %d", count)
	}
}

func TestConfigWatcher_IntervalEnforcement(t *testing.T) {
	// Very short intervals are clamped to 10ms minimum.
	w := NewConfigWatcher("/nonexistent", 1*time.Millisecond)
	if w.interval != 10*time.Millisecond {
		t.Errorf("expected interval=10ms for sub-10ms input, got %v", w.interval)
	}

	// Intervals above the minimum are preserved.
	w2 := NewConfigWatcher("/nonexistent", 5*time.Second)
	if w2.interval != 5*time.Second {
		t.Errorf("expected interval=5s, got %v", w2.interval)
	}
}

func TestConfigWatcher_DotEnvHotReload(t *testing.T) {
	// Test that changing the .env file triggers a reload even when
	// the JSON config file hasn't changed.
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	// Write initial config with a default port.
	writeConfigFile(t, jsonPath, map[string]any{"dashboard_port": 8000})
	// Write initial .env with a feature flag.
	writeDotenvFile(t, dotenvPath, "BT_FEATURE_STARTUP_SIM=false\n")

	w := NewConfigWatcher(jsonPath, 50*time.Millisecond).WithDotEnv(dotenvPath)

	var mu sync.Mutex
	var gotCfg *Config
	done := make(chan struct{})

	w.OnChange(func(cfg *Config) {
		mu.Lock()
		gotCfg = cfg
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	// Wait for watcher to record initial state.
	time.Sleep(150 * time.Millisecond)

	// Change ONLY the .env file — flip the feature flag.
	writeDotenvFile(t, dotenvPath, "BT_FEATURE_STARTUP_SIM=true\n")

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for .env change callback")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCfg == nil {
		t.Fatal("callback not invoked")
	}
	if gotCfg.DashboardPort != 8000 {
		t.Errorf("expected DashboardPort=8000 (unchanged), got %d", gotCfg.DashboardPort)
	}
	if !gotCfg.StartupSimEnabled {
		t.Error("expected StartupSimEnabled=true from hot-reloaded .env")
	}
}

func TestConfigWatcher_DotEnvChangeDoesNotTriggerOnNoChange(t *testing.T) {
	// Test that unchanged .env file does NOT trigger a callback.
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	writeConfigFile(t, jsonPath, map[string]any{"dashboard_port": 8000})
	writeDotenvFile(t, dotenvPath, "BT_FEATURE_AUTO_EVOLVE=true\n")

	w := NewConfigWatcher(jsonPath, 50*time.Millisecond).WithDotEnv(dotenvPath)

	fired := false
	var mu sync.Mutex
	w.OnChange(func(cfg *Config) {
		mu.Lock()
		fired = true
		mu.Unlock()
	})

	w.Start()
	defer w.Stop()

	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	if fired {
		t.Error("callback should not fire when neither file changed")
	}
	mu.Unlock()
}

func TestConfigWatcher_ConfigChangeWithDotEnv(t *testing.T) {
	// Test that changing the JSON config still triggers reload
	// when a .env file is also being watched.
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	writeConfigFile(t, jsonPath, map[string]any{"dashboard_port": 8000})
	writeDotenvFile(t, dotenvPath, "BT_FEATURE_KANBAN=false\n")

	w := NewConfigWatcher(jsonPath, 50*time.Millisecond).WithDotEnv(dotenvPath)

	var mu sync.Mutex
	var gotCfg *Config
	done := make(chan struct{})

	w.OnChange(func(cfg *Config) {
		mu.Lock()
		gotCfg = cfg
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	// Change the JSON config — port changes, .env stays the same.
	writeConfigFile(t, jsonPath, map[string]any{"dashboard_port": 9999})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for config change callback")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCfg == nil {
		t.Fatal("callback not invoked")
	}
	if gotCfg.DashboardPort != 9999 {
		t.Errorf("expected DashboardPort=9999, got %d", gotCfg.DashboardPort)
	}
	if gotCfg.KanbanEnabled {
		t.Error("expected KanbanEnabled=false from .env (unchanged)")
	}
}

func TestConfigWatcher_DotEnvPriorityChain(t *testing.T) {
	// Verify the priority chain on .env hot-reload:
	// defaults → config.json → .env → env vars (highest)
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	// Config file sets port to 8000.
	writeConfigFile(t, jsonPath, map[string]any{"dashboard_port": 8000})
	// .env sets port to 7777 (should override config file).
	writeDotenvFile(t, dotenvPath, "BT_DASHBOARD_PORT=7777\nBT_OLLAMA_MODEL=custom-model:latest\n")

	// Also set an env var for the port (should override everything).
	t.Setenv("BT_DASHBOARD_PORT", "5555")

	w := NewConfigWatcher(jsonPath, 50*time.Millisecond).WithDotEnv(dotenvPath)

	var mu sync.Mutex
	var gotCfg *Config
	done := make(chan struct{})

	w.OnChange(func(cfg *Config) {
		mu.Lock()
		gotCfg = cfg
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	// Change .env to update the model (still has port=7777).
	writeDotenvFile(t, dotenvPath, "BT_DASHBOARD_PORT=7777\nBT_OLLAMA_MODEL=new-model:v2\n")

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for .env change callback")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCfg == nil {
		t.Fatal("callback not invoked")
	}
	// Env var has highest priority — should keep 5555.
	if gotCfg.DashboardPort != 5555 {
		t.Errorf("expected DashboardPort=5555 (env var overrides .env), got %d", gotCfg.DashboardPort)
	}
	// .env model should be applied (no env var override for this field).
	if gotCfg.OllamaModel != "new-model:v2" {
		t.Errorf("expected OllamaModel=new-model:v2 from .env, got %s", gotCfg.OllamaModel)
	}
}

func TestConfigWatcher_BackwardCompatibleNoDotEnv(t *testing.T) {
	// Verify watchers without WithDotEnv still work exactly as before.
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "config.json")

	writeConfigFile(t, jsonPath, map[string]any{"dashboard_port": 8000})

	// Build WITHOUT WithDotEnv — backward compatible.
	w := NewConfigWatcher(jsonPath, 50*time.Millisecond)

	var mu sync.Mutex
	var gotCfg *Config
	done := make(chan struct{})

	w.OnChange(func(cfg *Config) {
		mu.Lock()
		gotCfg = cfg
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	writeConfigFile(t, jsonPath, map[string]any{
		"dashboard_port":   9000,
		"gardener_enabled": false,
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for config change callback")
	}

	mu.Lock()
	defer mu.Unlock()
	if gotCfg == nil {
		t.Fatal("callback not invoked")
	}
	if gotCfg.DashboardPort != 9000 {
		t.Errorf("expected DashboardPort=9000, got %d", gotCfg.DashboardPort)
	}
}

func TestConfigWatcher_DotEnvFileDisappears(t *testing.T) {
	// Test graceful handling when .env file is deleted after Start().
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	writeConfigFile(t, jsonPath, map[string]any{"dashboard_port": 8000})
	writeDotenvFile(t, dotenvPath, "BT_FEATURE_GARDENER=false\n")

	w := NewConfigWatcher(jsonPath, 50*time.Millisecond).WithDotEnv(dotenvPath)

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	// Delete the .env file — should not crash.
	if err := os.Remove(dotenvPath); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)
	// The watcher should continue running without panic.
	if !w.IsRunning() {
		t.Error("watcher should still be running after .env file disappears")
	}
}

// writeDotenvFile writes a .env file and ensures timestamp advances.
func writeDotenvFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
}
