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
	mu       sync.Mutex
	path     string
	interval time.Duration
	cbs      []ChangeCallback
	stopCh   chan struct{}
	running  bool
	lastMod  time.Time
	lastSize int64 // file size for detecting changes within same timestamp second
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
	fi, err := os.Stat(w.path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("[config-watcher] stat %s: %v", w.path, err)
		}
		return
	}

	modTime := fi.ModTime()
	fileSize := fi.Size()

	w.mu.Lock()
	prevMod := w.lastMod
	prevSize := w.lastSize
	w.mu.Unlock()

	// Reload if mod time advanced OR file size changed (handles
	// filesystems with second-granularity timestamps where rapid
	// writes produce identical mod times).
	if !modTime.After(prevMod) && fileSize == prevSize {
		return
	}

	cfg, err := LoadFile(w.path)
	if err != nil {
		log.Printf("[config-watcher] reload %s failed: %v (keeping previous config)", w.path, err)
		return
	}

	w.mu.Lock()
	w.lastMod = modTime
	w.lastSize = fileSize
	cbs := make([]ChangeCallback, len(w.cbs))
	copy(cbs, w.cbs)
	w.mu.Unlock()

	log.Printf("[config-watcher] reloaded %s (%d fields, %d callbacks)",
		w.path, 26, len(cbs))

	for _, cb := range cbs {
		cb(cfg)
	}
}
