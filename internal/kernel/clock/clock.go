package clock

import "time"

// Clock abstracts "now". Domain and application code take a Clock rather than calling
// time.Now(), so behaviour is deterministic under test (the no-time.Now architecture
// rule enforces this).
type Clock interface {
	Now() time.Time
}

type systemClock struct{}

// System returns the real clock.
func System() Clock { return systemClock{} }

func (systemClock) Now() time.Time { return time.Now() }

// Fixed is a deterministic clock for tests. It is safe to advance between assertions.
type Fixed struct {
	Current time.Time
}

// NewFixed returns a Fixed clock pinned to t.
func NewFixed(t time.Time) *Fixed { return &Fixed{Current: t} }

// Now returns the pinned instant.
func (f *Fixed) Now() time.Time { return f.Current }

// Advance moves the clock forward by d.
func (f *Fixed) Advance(d time.Duration) { f.Current = f.Current.Add(d) }
