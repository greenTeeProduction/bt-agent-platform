package util

import "time"

// Clock abstracts time operations for testability.
// Production code uses RealClock; tests inject FakeClock to control time.
type Clock interface {
	Now() time.Time
	Since(t time.Time) time.Duration
	Sleep(d time.Duration)
}

// RealClock uses the system clock. This is the production default.
type RealClock struct{}

func (RealClock) Now() time.Time                  { return time.Now() }
func (RealClock) Since(t time.Time) time.Duration { return time.Since(t) }
func (RealClock) Sleep(d time.Duration)           { time.Sleep(d) }

// FakeClock is a controllable clock for tests. All methods return/pause based
// on the fake clock's internal state rather than the system clock.
type FakeClock struct {
	NowTime time.Time
}

func (f *FakeClock) Now() time.Time                  { return f.NowTime }
func (f *FakeClock) Since(t time.Time) time.Duration { return f.NowTime.Sub(t) }
func (f *FakeClock) Sleep(d time.Duration)           { f.NowTime = f.NowTime.Add(d) }

// Advance moves the fake clock forward by d.
func (f *FakeClock) Advance(d time.Duration) {
	f.NowTime = f.NowTime.Add(d)
}

// Set sets the fake clock to a specific time.
func (f *FakeClock) Set(t time.Time) {
	f.NowTime = t
}

// NewFakeClock creates a FakeClock initialized to the given time.
// If t is zero, uses time.Now().
func NewFakeClock(t time.Time) *FakeClock {
	if t.IsZero() {
		t = time.Now()
	}
	return &FakeClock{NowTime: t}
}
