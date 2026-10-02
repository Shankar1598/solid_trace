// Package clock is where EventStore's Log modules read the current time, so a
// test can decide what "now" is. Log ingest takes receipt times from it; the
// hot window, the hourly export, the daily set, Retention and the disk limit
// are to read it too.
//
// UUIDv7 ids can't follow it: google/uuid reads the real clock.
package clock

import (
	"sync"
	"time"
)

// Clock tells the current time.
type Clock interface {
	Now() time.Time
}

// System is the real clock.
type System struct{}

func (System) Now() time.Time { return time.Now() }

// Manual is a clock that moves only when it is told to, for tests. It is safe
// for concurrent use.
type Manual struct {
	mu  sync.Mutex
	now time.Time
}

// NewManual returns a Manual clock that reads now.
func NewManual(now time.Time) *Manual {
	return &Manual{now: now}
}

func (c *Manual) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to now, forwards or back.
func (c *Manual) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// Advance moves the clock forward by d.
func (c *Manual) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}
