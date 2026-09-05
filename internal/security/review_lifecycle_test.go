package security

import (
	"sync"
	"testing"
	"time"
)

func TestReviewKeyExpiryConcurrent(t *testing.T) {
	kr := NewKeyRing()
	hash := kr.AddKey("test", "test", time.Hour)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 10000 {
				kr.Validate("test")
			}
		})
		wg.Go(func() {
			for range 10000 {
				if err := kr.ExpireKey(hash, time.Hour); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}
func TestReviewRotationOnceFiniteNoRevival(t *testing.T) {
	kr := NewKeyRing()
	hash := kr.AddKey("old", "old", time.Minute)
	original := kr.ListKeys()[0].ExpiresAt
	kr.AddKey("expired", "expired", time.Nanosecond)
	time.Sleep(time.Millisecond)
	s := NewKeyRotationScheduler(kr, time.Second, 2*time.Minute, "replacement", nil)
	if got := s.RotateNow(); got != 1 {
		t.Errorf("rotated %d, want only live old key", got)
	}
	if got := s.RotateNow(); got != 0 {
		t.Errorf("repeat rotated %d", got)
	}
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	if kr.keys[hash].ExpiresAt.After(original) {
		t.Error("old deadline extended")
	}
	for _, k := range kr.keys {
		if k.Label == "replacement" && k.ExpiresAt.IsZero() {
			t.Error("immortal replacement")
		}
	}
}
func TestReviewManualRotationCannotRepeatOrRevive(t *testing.T) {
	kr := NewKeyRing()
	old, err := kr.GenerateKey("old", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kr.RotateKey(old, "replacement", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := kr.RotateKey(old, "repeat", time.Hour); err == nil {
		t.Error("rotated the same old key twice")
	}
	expired, err := kr.GenerateKey("expired", time.Nanosecond)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Millisecond)
	if _, err := kr.RotateKey(expired, "revived", time.Hour); err == nil {
		t.Error("rotated an expired key")
	}
}
