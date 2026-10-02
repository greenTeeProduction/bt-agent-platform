package reliability

import (
	"errors"
	"sync"
)

var ErrRestartPending = errors.New("restart handoff is pending")

// RestartAdmissionGate owns process-local execution through actual cleanup.
// The zero value admits work. Sealing and admission share the same mutex;
// an accepted or uncertain handoff stays sealed until process exit.
type RestartAdmissionGate struct {
	mu     sync.Mutex
	active int
	sealed bool
}

func (g *RestartAdmissionGate) Acquire() (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sealed {
		return nil, ErrRestartPending
	}
	g.active++
	return sync.OnceFunc(func() { g.mu.Lock(); defer g.mu.Unlock(); g.active-- }), nil
}

func (g *RestartAdmissionGate) Busy() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.sealed || g.active != 0
}

// BeginRestart supplements leases with optional owner diagnostics. The
// diagnostic callback must not acquire this gate. Finish(false) reopens only a
// proven rejection; Finish(true) retains admission exclusion until exit.
func (g *RestartAdmissionGate) BeginRestart(additionalBusy func() bool) (func(bool), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sealed || g.active != 0 || (additionalBusy != nil && additionalBusy()) {
		return nil, false
	}
	g.sealed = true
	var once sync.Once
	return func(keep bool) {
		once.Do(func() {
			if !keep {
				g.mu.Lock()
				defer g.mu.Unlock()
				g.sealed = false
			}
		})
	}, true
}
