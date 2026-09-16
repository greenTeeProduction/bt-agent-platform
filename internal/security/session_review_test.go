package security

import (
	"sync"
	"testing"
	"time"
)

func TestSessionStore_ValidationReturnsSnapshot(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{})
	t.Cleanup(ss.Stop)
	token, err := ss.CreateSession("original-user")
	if err != nil {
		t.Fatal(err)
	}
	session, ok := ss.ValidateSession(token)
	if !ok {
		t.Fatal("fresh session was rejected")
	}
	session.UserID = "changed-user"
	session.ExpiresAt = time.Time{}
	current, ok := ss.ValidateSession(token)
	if !ok || current.UserID != "original-user" {
		t.Fatal("modifying a returned session changed the stored session")
	}

	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 100 {
				snapshot, valid := ss.ValidateSession(token)
				if !valid {
					t.Error("session unexpectedly expired")
					return
				}
				ss.RefreshSession(token)
				if snapshot.LastUsed.After(snapshot.ExpiresAt) {
					t.Error("invalid session timestamps")
				}
			}
		})
	}
	wg.Wait()
}

func TestSessionStore_CreateReclaimsExpiredCapacity(t *testing.T) {
	ss := NewSessionStore(SessionStoreConfig{MaxSessions: 2, CleanupInterval: time.Hour})
	t.Cleanup(ss.Stop)
	live, err := ss.CreateSession("live")
	if err != nil {
		t.Fatal(err)
	}
	expired, err := ss.CreateSession("expired")
	if err != nil {
		t.Fatal(err)
	}
	ss.mu.Lock()
	ss.sessions[sha256Hex(expired)].ExpiresAt = time.Now().Add(-time.Minute)
	ss.mu.Unlock()

	if _, err := ss.CreateSession("new-user"); err != nil {
		t.Fatalf("expired session blocked a new login: %v", err)
	}
	if ss.Count() != 2 {
		t.Fatalf("expected two active sessions, got %d", ss.Count())
	}
	if _, ok := ss.ValidateSession(live); !ok {
		t.Fatal("reclaiming expired capacity evicted a live session")
	}
	if _, ok := ss.ValidateSession(expired); ok {
		t.Fatal("expired session was revived")
	}
	if _, err := ss.CreateSession("overflow"); err == nil {
		t.Fatal("live session limit was bypassed")
	}
}
