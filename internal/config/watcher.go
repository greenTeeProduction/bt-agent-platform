// Package config provides environment-based configuration for the Go BT framework.
//
// ConfigWatcher enables hot-reload of configuration from a JSON file.
// When the file changes (modification time), the watcher reloads the config
// and notifies registered callbacks. This enables runtime configuration
// changes without process restart — useful for feature flags, rate limits,
// and LLM model switching in long-running daemons (gardener, dashboard).
package config

import (
	"log"
	"os"
	"sync"
	"time"
)

// ChangeCallback is called when the config file changes and the new
// configuration has been successfully loaded and validated.
// The callback receives the new Config; any validation errors during
// reload are logged but the callback is not invoked (previous config
// is preserved).
type ChangeCallback func(*Config)

// ConfigWatcher polls a JSON config file for changes and reloads
// configuration on modification. Designed for long-running daemons
// (bt-gardener, bt-dashboard) that benefit from runtime config updates
// without process restart.
//
// Usage:
//
//	watcher := NewConfigWatcher("/path/to/config.json", 30*time.Second)
//	watcher.OnChange(func(cfg *Config) {
//	    log.Printf("config reloaded: gardener_enabled=%v", cfg.GardenerEnabled)
//	})
//	watcher.Start()
//	defer watcher.Stop()
type ConfigWatcher struct {
	mu             sync.Mutex
	path           string
	dotenvPath     string // optional .env file to watch for hot-reload
	interval       time.Duration
	cbs            []ChangeCallback
	stopCh         chan struct{}
	running        bool
	lastMod        time.Time
	lastSize       int64     // file size for detecting changes within same timestamp second
	lastDotEnvMod  time.Time // last .env file modification time
	lastDotEnvSize int64     // last .env file size
}

// NewConfigWatcher creates a watcher for the given config file.
// interval is the polling frequency. It must be >= 10ms; values below
// 10ms are clamped to 10ms to avoid busy-looping. For production daemons,
// 30s is a reasonable default; for tests, use shorter intervals.
func NewConfigWatcher(path string, interval time.Duration) *ConfigWatcher {
	minInterval := 10 * time.Millisecond
	if interval < minInterval {
		interval = minInterval
	}
	return &ConfigWatcher{
		path:     path,
		interval: interval,
		stopCh:   make(chan struct{}),
	}
}

// WithDotEnv enables .env file hot-reload alongside the JSON config file.
// When the .env file changes on disk, the watcher reloads the full config
// chain: defaults → config.json → .env → env vars → callbacks.
//
// This enables runtime updates to feature flags, LLM settings, and other
// configuration without process restart — useful for long-running daemons
// that need to pick up .env changes from configuration management tools
// or CI/CD pipelines.
//
// Usage:
//
//	w := NewConfigWatcher("/etc/bt/config.json", 30*time.Second).
//	    WithDotEnv("/etc/bt/.env")
func (w *ConfigWatcher) WithDotEnv(path string) *ConfigWatcher {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.dotenvPath = path
	return w
}

// OnChange registers a callback invoked when the config file changes.
// Callbacks run synchronously in the watcher goroutine, so they should
// be fast and non-blocking.
func (w *ConfigWatcher) OnChange(cb ChangeCallback) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.cbs = append(w.cbs, cb)
}

// Start begins polling the config file for changes in a background goroutine.
// If the file doesn't exist at startup, the watcher logs a warning and
// retries on each poll interval until the file appears.
func (w *ConfigWatcher) Start() {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return
	}
	w.running = true
	// Record initial mod time and size so we don't trigger on the first poll.
	if fi, err := os.Stat(w.path); err == nil {
		w.lastMod = fi.ModTime()
		w.lastSize = fi.Size()
	}
	// Record .env initial state if configured.
	if w.dotenvPath != "" {
		if fi, err := os.Stat(w.dotenvPath); err == nil {
			w.lastDotEnvMod = fi.ModTime()
			w.lastDotEnvSize = fi.Size()
		}
	}
	w.mu.Unlock()

	go w.loop()
}

// Stop stops the background polling goroutine. Safe to call multiple times.
func (w *ConfigWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.running {
		return
	}
	w.running = false
	close(w.stopCh)
}

// IsRunning returns true if the watcher is actively polling.
func (w *ConfigWatcher) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// LastMod returns the last recorded modification time of the config file.
// Returns zero time if never polled.
func (w *ConfigWatcher) LastMod() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastMod
}

// LastSize returns the last recorded file size of the config file.
func (w *ConfigWatcher) LastSize() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastSize
}

func (w *ConfigWatcher) loop() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.checkAndReload()
		}
	}
}

func (w *ConfigWatcher) checkAndReload() {
	// Check JSON config file.
	configChanged := false
	modTime := time.Time{}
	fileSize := int64(0)
	dotEnvChanged := false

	fi, err := os.Stat(w.path)
	if err == nil {
		modTime = fi.ModTime()
		fileSize = fi.Size()

		w.mu.Lock()
		prevMod := w.lastMod
		prevSize := w.lastSize
		w.mu.Unlock()

		// Reload if mod time advanced OR file size changed (handles
		// filesystems with second-granularity timestamps where rapid
		// writes produce identical mod times).
		if modTime.After(prevMod) || fileSize != prevSize {
			configChanged = true
		}
	} else if !os.IsNotExist(err) {
		log.Printf("[config-watcher] stat %s: %v", w.path, err)
	}

	// Check .env file if configured.
	if w.dotenvPath != "" {
		if dotFi, err := os.Stat(w.dotenvPath); err == nil {
			dotMod := dotFi.ModTime()
			dotSize := dotFi.Size()

			w.mu.Lock()
			prevDotMod := w.lastDotEnvMod
			prevDotSize := w.lastDotEnvSize
			w.mu.Unlock()

			if dotMod.After(prevDotMod) || dotSize != prevDotSize {
				dotEnvChanged = true
			}
		}
	}

	if !configChanged && !dotEnvChanged {
		return
	}

	// Reload: use LoadFileWithDotEnv if .env file is configured,
	// otherwise use the standard LoadFile (backward compatible).
	var cfg *Config
	var reloadErr error

	if w.dotenvPath != "" {
		cfg, reloadErr = LoadFileWithDotEnv(w.path, w.dotenvPath)
	} else {
		cfg, reloadErr = LoadFile(w.path)
	}

	if reloadErr != nil {
		log.Printf("[config-watcher] reload %s failed: %v (keeping previous config)", w.path, reloadErr)
		return
	}

	w.mu.Lock()
	w.lastMod = modTime
	w.lastSize = fileSize
	// Update .env tracking if it changed or if we just did a full reload.
	if configChanged || dotEnvChanged {
		if w.dotenvPath != "" {
			if dotFi, err := os.Stat(w.dotenvPath); err == nil {
				w.lastDotEnvMod = dotFi.ModTime()
				w.lastDotEnvSize = dotFi.Size()
			}
		}
	}
	cbs := make([]ChangeCallback, len(w.cbs))
	copy(cbs, w.cbs)
	w.mu.Unlock()

	log.Printf("[config-watcher] reloaded %s (config_changed=%v dotenv_changed=%v, %d callbacks)",
		w.path, configChanged, dotEnvChanged, len(cbs))

	for _, cb := range cbs {
		cb(cfg)
	}
}
