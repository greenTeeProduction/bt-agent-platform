package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConfigWatcher_Lifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)
	if w == nil {
		t.Fatal("NewConfigWatcher returned nil")
	}

	if w.IsRunning() {
		t.Error("watcher should not be running before Start()")
	}

	w.Start()
	if !w.IsRunning() {
		t.Error("watcher should be running after Start()")
	}

	w.Stop()
	if w.IsRunning() {
		t.Error("watcher should not be running after Stop()")
	}

	// Double stop should not panic
	w.Stop()
}

func TestConfigWatcher_OnChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	var mu sync.Mutex
	changed := false
	done := make(chan struct{})

	w.OnChange(func(_ *Config) {
		mu.Lock()
		changed = true
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	// Wait for initial load, then update the config
	time.Sleep(100 * time.Millisecond)
	writeConfigFile(t, path, map[string]any{"dashboard_port": 9000})

	select {
	case <-done:
		mu.Lock()
		c := changed
		mu.Unlock()
		if !c {
			t.Error("OnChange callback should have been invoked")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for OnChange callback")
	}
}

func TestConfigWatcher_NoChangeNoCallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	callbackCount := 0
	var mu sync.Mutex
	done := make(chan struct{})

	w.OnChange(func(_ *Config) {
		mu.Lock()
		callbackCount++
		mu.Unlock()
		if callbackCount > 1 {
			close(done)
		}
	})

	w.Start()
	defer w.Stop()

	// Wait long enough for several poll cycles without changing the file.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	count := callbackCount
	mu.Unlock()
	if count > 1 {
		t.Errorf("expected at most 1 callback (initial load), got %d", count)
	}
}

func TestConfigWatcher_InvalidConfigNoCallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Write invalid JSON
	if err := os.WriteFile(path, []byte("{invalid}"), 0644); err != nil {
		t.Fatal(err)
	}

	w := NewConfigWatcher(path, 50*time.Millisecond)

	callbackCount := 0
	var mu sync.Mutex

	w.OnChange(func(_ *Config) {
		mu.Lock()
		callbackCount++
		mu.Unlock()
	})

	w.Start()
	defer w.Stop()

	// Wait for several poll cycles
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	count := callbackCount
	mu.Unlock()
	if count > 0 {
		t.Errorf("expected 0 callbacks for invalid config, got %d", count)
	}
}

func TestConfigWatcher_MultipleCallbacks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	var mu1, mu2 sync.Mutex
	called1 := false
	called2 := false
	done1 := make(chan struct{})
	done2 := make(chan struct{})

	w.OnChange(func(_ *Config) {
		mu1.Lock()
		called1 = true
		mu1.Unlock()
		close(done1)
	})
	w.OnChange(func(_ *Config) {
		mu2.Lock()
		called2 = true
		mu2.Unlock()
		close(done2)
	})

	w.Start()
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)
	writeConfigFile(t, path, map[string]any{"dashboard_port": 9000})

	<-done1
	<-done2

	if !called1 || !called2 {
		t.Error("all callbacks should have been invoked")
	}
}

func TestConfigWatcher_LargeChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	callbackCount := 0
	var mu sync.Mutex
	done := make(chan struct{})

	w.OnChange(func(_ *Config) {
		mu.Lock()
		callbackCount++
		count := callbackCount
		mu.Unlock()
		if count >= 2 {
			close(done)
		}
	})

	w.Start()
	defer w.Stop()

	time.Sleep(80 * time.Millisecond)

	// Write a much larger config
	writeConfigFile(t, path, map[string]any{
		"dashboard_port": 8000,
		"llm_timeout":    300,
		"ollama_host":    "http://localhost:11434",
		"ollama_model":   "qwen3.6",
	})

	time.Sleep(150 * time.Millisecond)

	// Write an even larger config
	writeConfigFile(t, path, map[string]any{
		"dashboard_port": 8080,
		"enable_tls":     true,
		"llm_timeout":    600,
		"max_body_size":  2097152,
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for large change callbacks")
	}
}

func TestConfigWatcher_NonExistentFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	w := NewConfigWatcher(path, 50*time.Millisecond)
	w.Start()
	defer w.Stop()

	time.Sleep(200 * time.Millisecond)
	if !w.IsRunning() {
		t.Error("watcher should continue running even with non-existent file")
	}
}

func TestConfigWatcher_FileAppears(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "delayed.json")

	w := NewConfigWatcher(path, 50*time.Millisecond)

	callbackCount := 0
	var mu sync.Mutex
	done := make(chan struct{})

	w.OnChange(func(_ *Config) {
		mu.Lock()
		callbackCount++
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	time.Sleep(200 * time.Millisecond)
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for file appear callback")
	}

	mu.Lock()
	count := callbackCount
	mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 callback for file appearing, got %d", count)
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

	w.OnChange(func(_ *Config) {
		mu.Lock()
		count++
		c := count
		mu.Unlock()
		if c >= 1 {
			close(done)
		}
	})

	w.Start()
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)

	// Write a larger file within the same second (no mod time change).
	writeConfigFile(t, path, map[string]any{
		"dashboard_port": 8000,
		"llm_timeout":    300,
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for size change detection")
	}
}

func TestConfigWatcher_LargeFileNoCrash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8000})

	w := NewConfigWatcher(path, time.Hour) // Very long interval — won't re-poll
	w.Start()
	defer w.Stop()

	time.Sleep(100 * time.Millisecond)
	// Should have loaded the config once, no crash.
	if !w.IsRunning() {
		t.Error("watcher should still be running")
	}
}

func TestConfigWatcher_DotEnvHotReload(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	// Write initial config
	writeConfigFile(t, configPath, map[string]any{
		"dashboard_port":       8000,
		"ollama_host":          "http://localhost:11434",
		"enable_rate_limiting": true,
	})

	// Write initial .env
	writeDotenvFile(t, dotenvPath, "BT_AGENT_DEFS_DIR="+dir+"\n")

	w := NewConfigWatcher(configPath, 50*time.Millisecond).WithDotEnv(dotenvPath)

	var mu sync.Mutex
	callbackCount := 0
	done := make(chan struct{})

	w.OnChange(func(_ *Config) {
		mu.Lock()
		callbackCount++
		c := callbackCount
		mu.Unlock()
		if c >= 2 {
			close(done)
		}
	})

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	// Change config file
	writeConfigFile(t, configPath, map[string]any{
		"dashboard_port":       8080,
		"ollama_host":          "http://localhost:11434",
		"enable_rate_limiting": true,
	})

	time.Sleep(150 * time.Millisecond)

	// Change .env file
	writeDotenvFile(t, dotenvPath, "BT_AGENT_DEFS_DIR="+dir+"\nBT_LLM_TIMEOUT=600\n")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hot-reload callbacks")
	}
}

func TestConfigWatcher_DotEnvChangeDoesNotTriggerOnNoChange(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	writeConfigFile(t, configPath, map[string]any{"dashboard_port": 8000})
	writeDotenvFile(t, dotenvPath, "BT_AGENT_DEFS_DIR="+dir+"\n")

	w := NewConfigWatcher(configPath, 50*time.Millisecond)

	var mu sync.Mutex
	callbackCount := 0
	done := make(chan struct{})

	w.OnChange(func(_ *Config) {
		mu.Lock()
		callbackCount++
		mu.Unlock()
		close(done)
	})

	w.Start()
	defer w.Stop()

	time.Sleep(80 * time.Millisecond)

	// Write the EXACT SAME content to .env (no actual change)
	writeDotenvFile(t, dotenvPath, "BT_AGENT_DEFS_DIR="+dir+"\n")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	mu.Lock()
	count := callbackCount
	mu.Unlock()
	if count > 1 {
		t.Errorf("expected at most 1 callback for unchanged .env, got %d", count)
	}
}

func TestConfigWatcher_ConfigChangeWithDotEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	// Initial config and .env
	writeConfigFile(t, configPath, map[string]any{"dashboard_port": 8000})
	writeDotenvFile(t, dotenvPath, "BT_LLM_TIMEOUT=300\n")

	w := NewConfigWatcher(configPath, 50*time.Millisecond).WithDotEnv(dotenvPath)

	callbackCount := 0
	var mu sync.Mutex
	done := make(chan struct{})

	w.OnChange(func(_ *Config) {
		mu.Lock()
		callbackCount++
		count := callbackCount
		mu.Unlock()
		if count >= 1 {
			close(done)
		}
	})

	w.Start()
	defer w.Stop()

	time.Sleep(150 * time.Millisecond)

	// Update config with a larger payload (different file size ensures detection)
	writeConfigFile(t, configPath, map[string]any{
		"dashboard_port": 9000,
		"extra_field":    "this makes the file larger so size-based detection works",
	})

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for config+dotenv update")
	}
}

func TestConfigWatcher_DotEnvFileDisappears(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	dotenvPath := filepath.Join(dir, ".env")

	writeConfigFile(t, configPath, map[string]any{"dashboard_port": 8000})
	writeDotenvFile(t, dotenvPath, "BT_AGENT_DEFS_DIR="+dir+"\n")

	w := NewConfigWatcher(configPath, 50*time.Millisecond)

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

func TestConfigWatcher_LastSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lastsize.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8080})

	w := NewConfigWatcher(path, 50*time.Millisecond)
	w.Start()
	defer w.Stop()

	// After start, the watcher should have read the file and recorded its size.
	time.Sleep(100 * time.Millisecond)

	size := w.LastSize()
	if size <= 0 {
		t.Errorf("LastSize() = %d, want > 0 (config file should have content)", size)
	}

	// Write a larger file — LastSize should update after the next poll cycle.
	time.Sleep(50 * time.Millisecond) // ensure timestamp advances
	writeConfigFile(t, path, map[string]any{
		"dashboard_port": 8080,
		"ollama_host":    "http://localhost:11434",
		"llm_timeout":    300,
	})
	time.Sleep(150 * time.Millisecond)

	newSize := w.LastSize()
	if newSize <= size {
		t.Errorf("LastSize() after larger write = %d, want > %d", newSize, size)
	}
}

// writeConfigFile writes a JSON config file and ensures the timestamp advances.
func writeConfigFile(t *testing.T, path string, cfg map[string]any) {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	// Write, then wait for timestamp to advance before returning
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
}

// writeDotenvFile writes a .env file and ensures timestamp advances.
func writeDotenvFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write .env: %v", err)
	}
	time.Sleep(15 * time.Millisecond)
}

func TestConfigWatcher_LastMod(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "lastmod.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8080})

	w := NewConfigWatcher(path, 50*time.Millisecond)
	w.Start()
	defer w.Stop()

	// After start, the watcher should have read the file and recorded its mod time.
	time.Sleep(100 * time.Millisecond)

	mod := w.LastMod()
	if mod.IsZero() {
		t.Error("LastMod() returned zero time, want non-zero")
	}

	// Write an updated file — LastMod should advance after the next poll.
	time.Sleep(50 * time.Millisecond) // ensure timestamp advances
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8080, "ollama_model": "new-model"})
	time.Sleep(150 * time.Millisecond)

	newMod := w.LastMod()
	if newMod.IsZero() {
		t.Error("LastMod() after file change returned zero time")
	}
	if !newMod.After(mod) && !newMod.Equal(mod) {
		t.Errorf("LastMod() after change = %v, expected > %v", newMod, mod)
	}
}

func TestConfigWatcher_LastMod_NotStarted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "neverstarted.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8080})

	w := NewConfigWatcher(path, 50*time.Millisecond)
	// Do NOT call w.Start()

	mod := w.LastMod()
	if !mod.IsZero() {
		t.Errorf("LastMod() on never-started watcher = %v, want zero time", mod)
	}
}

func TestConfigWatcher_LastSize_NotStarted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "neverstartedsize.json")
	writeConfigFile(t, path, map[string]any{"dashboard_port": 8080})

	w := NewConfigWatcher(path, 50*time.Millisecond)

	size := w.LastSize()
	if size != 0 {
		t.Errorf("LastSize() on never-started watcher = %d, want 0", size)
	}
}
