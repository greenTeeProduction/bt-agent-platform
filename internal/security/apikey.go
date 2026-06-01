// Package security provides production security primitives for the BT platform.
//
// It implements a layered security stack with:
//
//   - API key management (KeyRing with SHA-256 hashing, generation, validation,
//     rotation with grace periods, TTL-based expiry, revocation)
//   - Rate limiting (token bucket per client, configurable rate/burst)
//   - Input sanitization (null byte removal, ANSI escape stripping, size limits)
//   - IP filtering (allowlist/blocklist with CIDR support)
//   - CSRF protection (double-submit cookie pattern, crypto/rand tokens)
//   - Security headers (CSP, HSTS, X-Frame-Options, X-Content-Type-Options)
//   - Structured audit logging (per-context event deduplication)
//   - Request ID middleware (crypto/rand correlation IDs for distributed tracing)
//
// All components are concurrency-safe and configurable via the Config type.
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

// APIKeyInfo is the public-facing representation of an API key.
// The raw key value is never exposed after creation — only SHA-256 hashes are stored.
type APIKeyInfo struct {
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"` // zero time = never expires
	LastUsed  time.Time `json:"last_used,omitempty"`  // last successful validation
	UseCount  int64     `json:"use_count"`            // number of successful validations
}

// apiKey is the internal representation including the hash.
type apiKey struct {
	Hash      string    `json:"hash"` // SHA-256 hex of the raw key
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	LastUsed  time.Time `json:"last_used,omitempty"`
	UseCount  int64     `json:"use_count"`
}

// KeyRing manages a set of API keys. Safe for concurrent use.
// Keys are stored as SHA-256 hashes — raw key values are never persisted.
type KeyRing struct {
	mu   sync.RWMutex
	keys map[string]*apiKey // hash -> key entry
}

// NewKeyRing creates an empty key ring.
func NewKeyRing() *KeyRing {
	return &KeyRing{
		keys: make(map[string]*apiKey),
	}
}

// GenerateKey creates a new API key and adds it to the ring.
// label is a human-readable identifier (e.g., "dashboard-readonly", "mcp-agent").
// ttl is the key's lifetime. If zero, the key never expires.
//
// Returns the raw key value. This is the ONLY time the raw key is exposed —
// store it immediately; only the SHA-256 hash is retained.
func (kr *KeyRing) GenerateKey(label string, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("api key generation failed: %w", err)
	}
	keyStr := "sk-" + hex.EncodeToString(raw)

	hash := sha256Hex(keyStr)

	kr.mu.Lock()
	defer kr.mu.Unlock()

	entry := &apiKey{
		Hash:      hash,
		Label:     label,
		CreatedAt: time.Now(),
	}
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
	}
	kr.keys[hash] = entry

	return keyStr, nil
}

// AddKey adds a pre-existing key string to the ring.
// label is a human-readable identifier. ttl is the key's lifetime (zero = never expires).
// Returns the SHA-256 hash for future reference (e.g., for revocation).
func (kr *KeyRing) AddKey(keyStr, label string, ttl time.Duration) string {
	hash := sha256Hex(keyStr)

	kr.mu.Lock()
	defer kr.mu.Unlock()

	entry := &apiKey{
		Hash:      hash,
		Label:     label,
		CreatedAt: time.Now(),
	}
	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
	}
	kr.keys[hash] = entry
	return hash
}

// Validate checks if a raw key string is valid (exists and not expired).
// On successful validation, LastUsed and UseCount are updated.
// Returns true if the key is valid and allowed.
func (kr *KeyRing) Validate(keyStr string) bool {
	hash := sha256Hex(keyStr)

	kr.mu.RLock()
	entry, ok := kr.keys[hash]
	kr.mu.RUnlock()

	if !ok {
		return false
	}

	// Check expiry
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		return false
	}

	// Update usage stats (non-blocking write)
	kr.mu.Lock()
	if e, ok := kr.keys[hash]; ok {
		e.LastUsed = time.Now()
		e.UseCount++
	}
	kr.mu.Unlock()

	return true
}

// RevokeKey removes a key by its hash. Returns an error if the hash is not found.
func (kr *KeyRing) RevokeKey(hash string) error {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	if _, ok := kr.keys[hash]; !ok {
		return fmt.Errorf("key hash %q not found", hash)
	}
	delete(kr.keys, hash)
	return nil
}

// RevokeKeyByValue removes a key by its raw value string.
// Convenience wrapper around RevokeKey.
func (kr *KeyRing) RevokeKeyByValue(keyStr string) error {
	return kr.RevokeKey(sha256Hex(keyStr))
}

// RotateKey generates a new key and optionally marks the old key for expiry
// after a grace period. The old key continues to work until gracePeriod elapses.
//
// oldKeyStr is the raw key to rotate out (empty = no old key to expire).
// label is the label for the new key.
// gracePeriod is how long the old key remains valid after rotation.
//
// Returns the new raw key value and its hash.
func (kr *KeyRing) RotateKey(oldKeyStr, label string, gracePeriod time.Duration) (newKey string, _ error) {
	// Generate the new key first
	newKey, err := kr.GenerateKey(label, 0) // new key gets its own TTL later
	if err != nil {
		return "", err
	}

	// Expire the old key after grace period
	if oldKeyStr != "" {
		oldHash := sha256Hex(oldKeyStr)

		kr.mu.Lock()
		if entry, ok := kr.keys[oldHash]; ok {
			entry.ExpiresAt = time.Now().Add(gracePeriod)
		}
		kr.mu.Unlock()
	}

	return newKey, nil
}

// ListKeys returns public information about all keys (labels, timestamps, usage).
// Raw key values and hashes are never exposed.
func (kr *KeyRing) ListKeys() []APIKeyInfo {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	result := make([]APIKeyInfo, 0, len(kr.keys))
	for _, entry := range kr.keys {
		result = append(result, APIKeyInfo{
			Label:     entry.Label,
			CreatedAt: entry.CreatedAt,
			ExpiresAt: entry.ExpiresAt,
			LastUsed:  entry.LastUsed,
			UseCount:  entry.UseCount,
		})
	}
	return result
}

// CleanupExpired removes all expired keys.
// Returns the number of keys removed.
func (kr *KeyRing) CleanupExpired() int {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	now := time.Now()
	removed := 0
	for hash, entry := range kr.keys {
		if !entry.ExpiresAt.IsZero() && now.After(entry.ExpiresAt) {
			delete(kr.keys, hash)
			removed++
		}
	}
	return removed
}

// Count returns the number of keys in the ring (active + expired).
func (kr *KeyRing) Count() int {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return len(kr.keys)
}

// KeyHash returns the SHA-256 hash of a raw key string.
// Useful for referencing keys by hash.
func KeyHash(keyStr string) string {
	return sha256Hex(keyStr)
}

// ExpireKey sets the expiry time for a key identified by its hash.
// gracePeriod is how long from now the key remains valid.
// Returns an error if the hash is not found.
func (kr *KeyRing) ExpireKey(hash string, gracePeriod time.Duration) error {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	entry, ok := kr.keys[hash]
	if !ok {
		return fmt.Errorf("key hash %q not found", hash)
	}
	entry.ExpiresAt = time.Now().Add(gracePeriod)
	return nil
}

// ExpiringKeys returns the hashes of keys that will expire within the given window.
// Keys that never expire (ExpiresAt zero) are not included.
func (kr *KeyRing) ExpiringKeys(window time.Duration) []string {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	threshold := time.Now().Add(window)
	var hashes []string
	for hash, entry := range kr.keys {
		if !entry.ExpiresAt.IsZero() && entry.ExpiresAt.Before(threshold) {
			hashes = append(hashes, hash)
		}
	}
	return hashes
}

// KeyRotationScheduler periodically checks a KeyRing for keys approaching expiry
// and auto-rotates them, creating new keys before old ones expire.
type KeyRotationScheduler struct {
	keyRing      *KeyRing
	interval     time.Duration
	rotateWindow time.Duration
	label        string
	onRotate     func(hash, newKey string)
	stopCh       chan struct{}
	done         chan struct{}
}

// NewKeyRotationScheduler creates a new rotation scheduler.
// interval is how often the scheduler checks for expiring keys.
// rotateWindow is the threshold: keys expiring within this window are rotated.
// label is used as the label for auto-rotated replacement keys.
// onRotate is an optional callback invoked after each rotation with the old hash and new key.
func NewKeyRotationScheduler(kr *KeyRing, interval, rotateWindow time.Duration, label string, onRotate func(hash, newKey string)) *KeyRotationScheduler {
	return &KeyRotationScheduler{
		keyRing:      kr,
		interval:     interval,
		rotateWindow: rotateWindow,
		label:        label,
		onRotate:     onRotate,
		stopCh:       make(chan struct{}),
		done:         make(chan struct{}),
	}
}

// Start begins the rotation loop in a background goroutine.
func (krs *KeyRotationScheduler) Start() {
	go krs.loop()
}

// Stop gracefully shuts down the rotation scheduler.
func (krs *KeyRotationScheduler) Stop() {
	close(krs.stopCh)
	<-krs.done
}

// RotateNow performs an immediate rotation pass — rotates all keys
// that expire within the configured rotateWindow. Returns the number of keys rotated.
func (krs *KeyRotationScheduler) RotateNow() int {
	return krs.rotate()
}

func (krs *KeyRotationScheduler) loop() {
	defer close(krs.done)

	ticker := time.NewTicker(krs.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			krs.rotate()
		case <-krs.stopCh:
			return
		}
	}
}

func (krs *KeyRotationScheduler) rotate() int {
	hashes := krs.keyRing.ExpiringKeys(krs.rotateWindow)
	rotated := 0
	for _, oldHash := range hashes {
		newKey, err := krs.keyRing.GenerateKey(krs.label, 0)
		if err != nil {
			continue
		}
		_ = krs.keyRing.ExpireKey(oldHash, krs.rotateWindow)
		if krs.onRotate != nil {
			krs.onRotate(oldHash, newKey)
		}
		rotated++
	}
	return rotated
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
