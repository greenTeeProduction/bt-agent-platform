package security

import (
	"sync"
	"testing"
	"time"
)

func TestKeyRing_GenerateAndValidate(t *testing.T) {
	kr := NewKeyRing()

	// Generate a never-expiring key
	key, err := kr.GenerateKey("test-key", 0)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	if len(key) < 3 || key[:3] != "sk-" {
		t.Errorf("expected key prefix 'sk-', got %q", key)
	}
	if kr.Count() != 1 {
		t.Errorf("expected 1 key, got %d", kr.Count())
	}

	// Valid key should pass
	if !kr.Validate(key) {
		t.Error("expected freshly-generated key to validate")
	}

	// Invalid key should fail
	if kr.Validate("sk-bogus-key-that-doesnt-exist") {
		t.Error("expected bogus key to fail validation")
	}

	// Empty key should fail
	if kr.Validate("") {
		t.Error("expected empty key to fail validation")
	}
}

func TestKeyRing_Expiry(t *testing.T) {
	kr := NewKeyRing()

	// Generate a key that expires in 1 second
	key, err := kr.GenerateKey("expiring-key", 1*time.Second)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Should validate immediately
	if !kr.Validate(key) {
		t.Error("expected key to validate before expiry")
	}

	// Wait for expiry
	time.Sleep(1100 * time.Millisecond)

	// Should fail after expiry
	if kr.Validate(key) {
		t.Error("expected expired key to fail validation")
	}
}

func TestKeyRing_Revoke(t *testing.T) {
	kr := NewKeyRing()

	key1, _ := kr.GenerateKey("key-1", 0)
	key2, _ := kr.GenerateKey("key-2", 0)

	if kr.Count() != 2 {
		t.Errorf("expected 2 keys, got %d", kr.Count())
	}

	// Revoke key1 by value
	if err := kr.RevokeKeyByValue(key1); err != nil {
		t.Errorf("RevokeKeyByValue failed: %v", err)
	}

	if kr.Count() != 1 {
		t.Errorf("expected 1 key after revoke, got %d", kr.Count())
	}

	// key1 should no longer validate
	if kr.Validate(key1) {
		t.Error("expected revoked key to fail validation")
	}

	// key2 should still validate
	if !kr.Validate(key2) {
		t.Error("expected non-revoked key to validate")
	}

	// Revoke by hash
	hash2 := KeyHash(key2)
	if err := kr.RevokeKey(hash2); err != nil {
		t.Errorf("RevokeKey failed: %v", err)
	}

	if kr.Count() != 0 {
		t.Errorf("expected 0 keys after revoking both, got %d", kr.Count())
	}

	// Revoking non-existent key should error
	if err := kr.RevokeKey("nonexistent"); err == nil {
		t.Error("expected error revoking non-existent key")
	}
}

func TestKeyRing_Rotate(t *testing.T) {
	kr := NewKeyRing()

	// Create an old key
	oldKey, _ := kr.GenerateKey("rotating-key", 0)
	if kr.Count() != 1 {
		t.Fatalf("expected 1 key, got %d", kr.Count())
	}

	// Rotate: generate new key, old key gets 1 second grace period
	newKey, err := kr.RotateKey(oldKey, "rotating-key", 100*time.Millisecond)
	if err != nil {
		t.Fatalf("RotateKey failed: %v", err)
	}

	// Both keys should exist (2 total)
	if kr.Count() != 2 {
		t.Errorf("expected 2 keys after rotation, got %d", kr.Count())
	}

	// Both keys should validate during grace period
	if !kr.Validate(oldKey) {
		t.Error("expected old key to validate during grace period")
	}
	if !kr.Validate(newKey) {
		t.Error("expected new key to validate")
	}

	// Wait for grace period to expire
	time.Sleep(150 * time.Millisecond)

	// Old key should fail after grace period
	if kr.Validate(oldKey) {
		t.Error("expected old key to fail after grace period")
	}

	// New key should still validate
	if !kr.Validate(newKey) {
		t.Error("expected new key to validate after grace period")
	}

	// CleanupExpired should remove the old key
	removed := kr.CleanupExpired()
	if removed != 1 {
		t.Errorf("expected 1 expired key removed, got %d", removed)
	}
	if kr.Count() != 1 {
		t.Errorf("expected 1 key after cleanup, got %d", kr.Count())
	}
}

func TestKeyRing_RotateEmptyOldKey(t *testing.T) {
	kr := NewKeyRing()

	// Rotate with empty oldKey — just generates a new key, no expiry on old
	newKey, err := kr.RotateKey("", "solo-key", 1*time.Hour)
	if err != nil {
		t.Fatalf("RotateKey with empty old failed: %v", err)
	}

	if kr.Count() != 1 {
		t.Errorf("expected 1 key, got %d", kr.Count())
	}
	if !kr.Validate(newKey) {
		t.Error("expected new key to validate")
	}
}

func TestKeyRing_ListKeys(t *testing.T) {
	kr := NewKeyRing()

	_, _ = kr.GenerateKey("label-a", 0)
	_, _ = kr.GenerateKey("label-b", 1*time.Hour)

	keys := kr.ListKeys()
	if len(keys) != 2 {
		t.Errorf("expected 2 listed keys, got %d", len(keys))
	}

	labels := make(map[string]bool)
	for _, k := range keys {
		labels[k.Label] = true
		if k.CreatedAt.IsZero() {
			t.Errorf("expected non-zero CreatedAt for key %q", k.Label)
		}
		if k.UseCount != 0 {
			t.Errorf("expected UseCount=0 before validation, got %d", k.UseCount)
		}
	}
	if !labels["label-a"] || !labels["label-b"] {
		t.Error("expected both labels in key list")
	}
}

func TestKeyRing_UseCount(t *testing.T) {
	kr := NewKeyRing()

	key, _ := kr.GenerateKey("counted-key", 0)

	// Validate 3 times
	for i := 0; i < 3; i++ {
		if !kr.Validate(key) {
			t.Fatalf("validation %d failed", i)
		}
	}

	keys := kr.ListKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].UseCount != 3 {
		t.Errorf("expected UseCount=3, got %d", keys[0].UseCount)
	}
	if keys[0].LastUsed.IsZero() {
		t.Error("expected non-zero LastUsed")
	}
}

func TestKeyRing_AddKey(t *testing.T) {
	kr := NewKeyRing()

	hash := kr.AddKey("sk-preexisting-key-from-env", "imported-key", 1*time.Hour)
	if hash == "" {
		t.Error("expected non-empty hash from AddKey")
	}
	if kr.Count() != 1 {
		t.Errorf("expected 1 key, got %d", kr.Count())
	}
	if !kr.Validate("sk-preexisting-key-from-env") {
		t.Error("expected AddKey'd key to validate")
	}
	if kr.Validate("wrong-key") {
		t.Error("expected wrong key to fail")
	}
}

func TestKeyRing_ConcurrentAccess(t *testing.T) {
	kr := NewKeyRing()

	// Generate some keys
	keys := make([]string, 5)
	for i := 0; i < 5; i++ {
		k, err := kr.GenerateKey("concurrent", 0)
		if err != nil {
			t.Fatalf("GenerateKey %d failed: %v", i, err)
		}
		keys[i] = k
	}

	// Concurrent validation
	done := make(chan bool, 20)
	for i := 0; i < 10; i++ {
		go func() {
			for _, k := range keys {
				kr.Validate(k)
				kr.Validate("bogus")
			}
			done <- true
		}()
	}

	// Concurrent listing
	go func() {
		for i := 0; i < 10; i++ {
			kr.ListKeys()
			kr.Count()
			kr.CleanupExpired()
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 11; i++ {
		<-done
	}

	if kr.Count() != 5 {
		t.Errorf("expected 5 keys after concurrent access, got %d", kr.Count())
	}
}

func TestKeyRing_CleanupExpired_None(t *testing.T) {
	kr := NewKeyRing()

	// Generate non-expiring keys
	_, _ = kr.GenerateKey("permanent-1", 0)
	_, _ = kr.GenerateKey("permanent-2", 0)

	removed := kr.CleanupExpired()
	if removed != 0 {
		t.Errorf("expected 0 removed, got %d", removed)
	}
	if kr.Count() != 2 {
		t.Errorf("expected 2 keys, got %d", kr.Count())
	}
}

func TestKeyRing_Empty(t *testing.T) {
	kr := NewKeyRing()

	if kr.Count() != 0 {
		t.Errorf("expected 0 keys, got %d", kr.Count())
	}
	if kr.Validate("anything") {
		t.Error("expected empty keyring to reject all keys")
	}

	keys := kr.ListKeys()
	if len(keys) != 0 {
		t.Errorf("expected empty list, got %d keys", len(keys))
	}

	removed := kr.CleanupExpired()
	if removed != 0 {
		t.Errorf("expected 0 removed from empty ring, got %d", removed)
	}
}

func TestKeyHash_Deterministic(t *testing.T) {
	h1 := KeyHash("sk-test-key")
	h2 := KeyHash("sk-test-key")
	h3 := KeyHash("***")

	if h1 != h2 {
		t.Errorf("expected same hash for same key, got %q != %q", h1, h2)
	}
	if h1 == h3 {
		t.Error("expected different hash for different key")
	}
	if len(h1) != 64 { // SHA-256 hex = 64 chars
		t.Errorf("expected 64-char hex hash, got %d chars", len(h1))
	}
}

func TestExpiringKeys_EmptyRing(t *testing.T) {
	kr := NewKeyRing()
	hashes := kr.ExpiringKeys(1 * time.Hour)
	if len(hashes) != 0 {
		t.Errorf("expected 0 expiring keys on empty ring, got %d", len(hashes))
	}
}

func TestExpiringKeys_OnlyPermanent(t *testing.T) {
	kr := NewKeyRing()
	_, _ = kr.GenerateKey("perm-1", 0)
	_, _ = kr.GenerateKey("perm-2", 0)

	hashes := kr.ExpiringKeys(1 * time.Hour)
	if len(hashes) != 0 {
		t.Errorf("expected 0 expiring keys for permanent keys, got %d", len(hashes))
	}
}

func TestExpiringKeys_WithinWindow(t *testing.T) {
	kr := NewKeyRing()
	_, _ = kr.GenerateKey("expiring-soon", 100*time.Millisecond)

	hashes := kr.ExpiringKeys(1 * time.Hour)
	if len(hashes) != 1 {
		t.Errorf("expected 1 expiring key, got %d", len(hashes))
	}
}

func TestExpiringKeys_OutsideWindow(t *testing.T) {
	kr := NewKeyRing()
	_, _ = kr.GenerateKey("expiring-later", 2*time.Hour)

	hashes := kr.ExpiringKeys(1 * time.Hour)
	if len(hashes) != 0 {
		t.Errorf("expected 0 expiring keys outside window, got %d", len(hashes))
	}
}

func TestExpireKey_NotFound(t *testing.T) {
	kr := NewKeyRing()
	err := kr.ExpireKey("nonexistent-hash", 1*time.Hour)
	if err == nil {
		t.Error("expected error expiring non-existent key")
	}
}

func TestExpireKey_Success(t *testing.T) {
	kr := NewKeyRing()
	key, _ := kr.GenerateKey("expire-me", 0)
	hash := KeyHash(key)

	// Key should validate before expiry
	if !kr.Validate(key) {
		t.Fatal("expected key to validate before expire")
	}

	// Set expiry 50ms from now
	err := kr.ExpireKey(hash, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("ExpireKey failed: %v", err)
	}

	// Should still validate during grace period
	if !kr.Validate(key) {
		t.Error("expected key to validate during grace period")
	}

	// Wait for expiry
	time.Sleep(100 * time.Millisecond)

	if kr.Validate(key) {
		t.Error("expected key to fail after expiry")
	}
}

func TestKeyRotationScheduler_RotateNow(t *testing.T) {
	kr := NewKeyRing()

	// Create a key that expires very soon
	oldKey, err := kr.GenerateKey("auto-rotate-label", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	initialCount := kr.Count()
	if initialCount != 1 {
		t.Fatalf("expected 1 key, got %d", initialCount)
	}

	// Wait for it to be past its original expiry
	time.Sleep(60 * time.Millisecond)

	krs := NewKeyRotationScheduler(kr, 1*time.Hour, 1*time.Second, "auto-rotated", nil)
	rotated := krs.RotateNow()
	if rotated != 1 {
		t.Errorf("expected 1 key rotated, got %d", rotated)
	}

	// Old key should STILL validate — rotation grants a 1s grace period
	if !kr.Validate(oldKey) {
		t.Error("expected old key to validate during rotation grace period")
	}

	// We should have 2 keys (old with grace period + new)
	if kr.Count() != 2 {
		t.Errorf("expected 2 keys after rotation (old with grace + new), got %d", kr.Count())
	}

	// After grace period, old key should fail
	time.Sleep(1100 * time.Millisecond)
	if kr.Validate(oldKey) {
		t.Error("expected old key to be expired after grace period")
	}
}

func TestKeyRotationScheduler_NoExpiringKeys(t *testing.T) {
	kr := NewKeyRing()
	_, _ = kr.GenerateKey("perm-key", 0) // never expires

	krs := NewKeyRotationScheduler(kr, 1*time.Hour, 1*time.Hour, "rotated", nil)
	rotated := krs.RotateNow()
	if rotated != 0 {
		t.Errorf("expected 0 rotations when no keys expiring, got %d", rotated)
	}
	if kr.Count() != 1 {
		t.Errorf("expected 1 key unchanged, got %d", kr.Count())
	}
}

func TestKeyRotationScheduler_StartStop(t *testing.T) {
	kr := NewKeyRing()

	krs := NewKeyRotationScheduler(kr, 1*time.Hour, 1*time.Hour, "auto", nil)
	krs.Start()
	krs.Stop() // should not hang or panic

	if kr.Count() != 0 {
		t.Errorf("expected 0 keys, got %d", kr.Count())
	}
}

func TestKeyRotationScheduler_Callback(t *testing.T) {
	kr := NewKeyRing()

	oldKey, err := kr.GenerateKey("callback-key", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	time.Sleep(60 * time.Millisecond)

	var (
		cbHash    string
		cbNewKey  string
		cbInvoked bool
	)

	onRotate := func(hash, newKey string) {
		cbHash = hash
		cbNewKey = newKey
		cbInvoked = true
	}

	krs := NewKeyRotationScheduler(kr, 1*time.Hour, 1*time.Second, "callback-rotated", onRotate)
	rotated := krs.RotateNow()
	if rotated != 1 {
		t.Fatalf("expected 1 rotation, got %d", rotated)
	}

	if !cbInvoked {
		t.Error("expected rotation callback to be invoked")
	}
	if cbHash == "" {
		t.Error("expected non-empty callback hash")
	}
	if cbNewKey == "" {
		t.Error("expected non-empty callback new key")
	}
	if oldKey == cbNewKey {
		t.Error("expected new key to differ from old key")
	}
}

func TestKeyRotationScheduler_BackgroundLoop(t *testing.T) {
	kr := NewKeyRing()

	// Create key that expires in 150ms
	_, err := kr.GenerateKey("bg-key", 150*time.Millisecond)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	var rotations int
	var mu sync.Mutex
	onRotate := func(_, _ string) {
		mu.Lock()
		rotations++
		mu.Unlock()
	}

	// Check every 50ms, rotate keys expiring within 200ms
	krs := NewKeyRotationScheduler(kr, 50*time.Millisecond, 200*time.Millisecond, "bg-rotated", onRotate)
	krs.Start()

	// Wait for the background loop to detect and rotate
	time.Sleep(200 * time.Millisecond)

	krs.Stop()

	mu.Lock()
	r := rotations
	mu.Unlock()

	if r < 1 {
		t.Errorf("expected at least 1 background rotation, got %d", r)
	}
}

func TestKeyRotationScheduler_EmptyRingRotateNow(t *testing.T) {
	kr := NewKeyRing()
	krs := NewKeyRotationScheduler(kr, 1*time.Hour, 1*time.Hour, "empty", nil)
	rotated := krs.RotateNow()
	if rotated != 0 {
		t.Errorf("expected 0 rotations on empty ring, got %d", rotated)
	}
}

func TestKeyRotationScheduler_MultipleExpiring(t *testing.T) {
	kr := NewKeyRing()

	// Create 3 keys that expire soon
	for i := 0; i < 3; i++ {
		_, _ = kr.GenerateKey("multi-key", 50*time.Millisecond)
	}

	time.Sleep(60 * time.Millisecond)

	krs := NewKeyRotationScheduler(kr, 1*time.Hour, 1*time.Second, "multi-rotated", nil)
	rotated := krs.RotateNow()
	if rotated != 3 {
		t.Errorf("expected 3 rotations, got %d", rotated)
	}
}

func TestKeyRotationScheduler_StartStopSafe(_ *testing.T) {
	kr := NewKeyRing()
	krs := NewKeyRotationScheduler(kr, 1*time.Hour, 1*time.Hour, "safe", nil)
	krs.Start()
	krs.Stop() // should not hang or panic
	// Second Stop is NOT safe (closes already-closed channel — use once)
}
