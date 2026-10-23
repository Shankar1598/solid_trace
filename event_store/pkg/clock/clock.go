// Package clock is where EventStore's Log modules read the current time, so a
// test can decide what "now" is. Log ingest takes receipt times from it, and
// Log processing, Retention and the disk limit read it too.
//
// UUIDv7 ids can't follow it: google/uuid reads the real clock.
package clock

import (
	"sync"
	"time"
)

// Clock tells the current time, and when a wait by it ends.
type Clock interface {
	Now() time.Time
	// After sends the time on the channel it returns once d has passed.
	After(d time.Duration) <-chan time.Time
}

// System is the real clock.
type System struct{}

func (System) Now() time.Time { return time.Now() }

func (System) After(d time.Duration) <-chan time.Time { return time.After(d) }

// Manual is a clock that moves only when it is told to, for tests. It is safe
// for concurrent use.
type Manual struct {
	mu  sync.Mutex
	now time.Time
	// timers are the waits from After not yet ended, and waitBegan is
	// signalled whenever one begins.
	timers    []manualTimer
	waitBegan *sync.Cond
}

// manualTimer is a wait from After, which ends once the clock reaches at.
type manualTimer struct {
	at time.Time
	c  chan time.Time
}

// NewManual returns a Manual clock that reads now.
func NewManual(now time.Time) *Manual {
	c := &Manual{now: now}
	c.waitBegan = sync.NewCond(&c.mu)
	return c
}

func (c *Manual) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// After's channel gets the clock's time once Set or Advance moves it d past
// now.
func (c *Manual) After(d time.Duration) <-chan time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch
	}
	c.timers = append(c.timers, manualTimer{at: c.now.Add(d), c: ch})
	c.waitBegan.Broadcast()
	return ch
}

// BlockUntilTimers waits until at least n waits from After are under way, so
// that a test moves the clock only once the code it tests is waiting.
func (c *Manual) BlockUntilTimers(n int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(c.timers) < n {
		c.waitBegan.Wait()
	}
}

// Set moves the clock to now, forwards or back.
func (c *Manual) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
	c.fire()
}

// Advance moves the clock forward by d.
func (c *Manual) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	c.fire()
}

// fire ends the waits the clock has reached. c.mu must be held.
func (c *Manual) fire() {
	waiting := c.timers[:0]
	for _, timer := range c.timers {
		if timer.at.After(c.now) {
			waiting = append(waiting, timer)
			continue
		}
		timer.c <- c.now
	}
	c.timers = waiting
}
