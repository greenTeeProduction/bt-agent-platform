package security

import (
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

	kr.GenerateKey("label-a", 0)
	kr.GenerateKey("label-b", 1*time.Hour)

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
	kr.GenerateKey("permanent-1", 0)
	kr.GenerateKey("permanent-2", 0)

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
	h3 := KeyHash("sk-different-key")

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
