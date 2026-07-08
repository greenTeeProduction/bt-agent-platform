package evolution

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestAcquireExperienceLockMutualExclusion proves the sidecar flock excludes a
// second acquisition until release. Because flock attaches to the open file
// description, two separate opens of the same sidecar exclude each other even
// within one process — the same shape as the daemon/gardener cross-process case.
func TestAcquireExperienceLockMutualExclusion(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "experience.json")

	release1, err := acquireExperienceLock(persistPath)
	if err != nil {
		t.Fatalf("first acquireExperienceLock: %v", err)
	}

	lockPath := persistPath + ".lock"
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("sidecar lock file %s not created: %v", lockPath, err)
	}

	acquired := make(chan struct{})
	go func() {
		release2, err := acquireExperienceLock(persistPath)
		if err != nil {
			t.Errorf("second acquireExperienceLock: %v", err)
			close(acquired)
			return
		}
		close(acquired)
		release2()
	}()

	// Let the goroutine reach the blocking flock, then confirm it has NOT
	// acquired while the first lock is still held.
	time.Sleep(100 * time.Millisecond)
	select {
	case <-acquired:
		t.Fatal("second acquisition succeeded while first lock was still held")
	default:
	}

	release1()

	select {
	case <-acquired:
	case <-time.After(5 * time.Second):
		t.Fatal("second acquisition did not complete after release of the first lock")
	}
}

// TestAcquireExperienceLockReleaseIdempotent verifies that calling release
// more than once neither panics nor errors out the process.
func TestAcquireExperienceLockReleaseIdempotent(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "experience.json")

	release, err := acquireExperienceLock(persistPath)
	if err != nil {
		t.Fatalf("acquireExperienceLock: %v", err)
	}

	release()
	release() // second call must be a safe no-op
}

// TestAcquireExperienceLockMissingDir verifies that an unopenable sidecar path
// (parent directory does not exist) surfaces an error instead of a nil release.
func TestAcquireExperienceLockMissingDir(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "no-such-dir", "experience.json")

	release, err := acquireExperienceLock(persistPath)
	if err == nil {
		if release != nil {
			release()
		}
		t.Fatal("expected error for missing parent directory, got nil")
	}
}
