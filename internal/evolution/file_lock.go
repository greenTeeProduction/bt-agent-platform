package evolution

import (
	"fmt"
	"os"
	"sync"
	"syscall"
)

// acquireExperienceLock takes an exclusive advisory flock on the sidecar
// `<persistPath>.lock`, blocking until the lock is available. flock attaches
// to the open file description, so two separate opens of the same sidecar
// exclude each other even within one process — the same shape as the
// daemon/gardener cross-process case. The lock is advisory and relies on
// Linux flock semantics (the platform target). The returned release func is
// safe to call more than once.
func acquireExperienceLock(persistPath string) (func(), error) {
	lockPath := persistPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("open experience lock %s: %w", lockPath, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, fmt.Errorf("flock experience lock %s: %w", lockPath, err)
	}
	var once sync.Once
	release := func() {
		once.Do(func() {
			syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			f.Close()
		})
	}
	return release, nil
}
