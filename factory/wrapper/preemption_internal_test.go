package wrapper

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// fakeClock is a clock that moves only when the test advances it.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters []fakeWaiter
	// changes counts each new and each stopped waiter.
	changes int
}

type fakeWaiter struct {
	at time.Time
	ch chan time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) After(d time.Duration) <-chan time.Time {
	ch, _ := c.Timer(d)
	return ch
}

func (c *fakeClock) Timer(d time.Duration) (<-chan time.Time, func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan time.Time, 1)
	if d <= 0 {
		ch <- c.now
		return ch, func() {}
	}
	c.waiters = append(c.waiters, fakeWaiter{at: c.now.Add(d), ch: ch})
	c.changes++
	return ch, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.changes++
		c.waiters = slices.DeleteFunc(c.waiters, func(w fakeWaiter) bool { return w.ch == ch })
	}
}

// advanceToNext moves the clock to the earliest waiter and fires each
// waiter that is due. It is false when no one waits, or when the waiters
// changed since the last call: the handler's goroutines are not all
// waiting yet.
func (c *fakeClock) advanceToNext(seen *int) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.waiters) == 0 || c.changes != *seen {
		*seen = c.changes
		return false
	}
	next := c.waiters[0].at
	for _, w := range c.waiters {
		if w.at.Before(next) {
			next = w.at
		}
	}
	c.now = next
	kept := c.waiters[:0]
	for _, w := range c.waiters {
		if w.at.After(c.now) {
			kept = append(kept, w)
			continue
		}
		w.ch <- c.now
	}
	c.waiters = kept
	c.changes++
	*seen = c.changes
	return true
}

// stepLog keeps the steps that the handler started, with the clock's
// time since the notice, and when each step's context ended.
type stepLog struct {
	mu      sync.Mutex
	clock   *fakeClock
	notice  time.Time
	started []string
	ended   map[string]time.Duration
}

func (l *stepLog) start(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.started = append(l.started, name)
}

// hang is a step that never returns by itself: it waits until its
// context ends and records the time.
func (l *stepLog) hang(ctx context.Context, name string) {
	l.start(name)
	<-ctx.Done()
	l.mu.Lock()
	l.ended[name] = l.clock.Now().Sub(l.notice)
	l.mu.Unlock()
}

// waitEnded waits until n steps recorded the end of their context: a
// step goes on after the handler moved on.
func (l *stepLog) waitEnded(t *testing.T, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		l.mu.Lock()
		got := len(l.ended)
		l.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d steps ended, want %d", got, n)
		}
	}
}

// drive runs the handler and moves the fake clock on each time that the
// handler waits, until the handler returns.
func drive(t *testing.T, clock *fakeClock, handle func() dispatcherapi.Preemption) dispatcherapi.Preemption {
	t.Helper()
	done := make(chan dispatcherapi.Preemption, 1)
	go func() { done <- handle() }()
	seen := -1
	for deadline := time.Now().Add(10 * time.Second); ; {
		select {
		case p := <-done:
			return p
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the handler did not return in 10 s of real time")
		}
		// Each goroutine of the handler gets time to start its wait
		// before the clock moves.
		time.Sleep(10 * time.Millisecond)
		clock.advanceToNext(&seen)
	}
}

func TestEachStepOfThePreemptionHandlerStopsAtItsLimitAndTheHandlerEndsAtTheHardLimit(t *testing.T) {
	clock := newFakeClock()
	notice := clock.Now()
	log := &stepLog{clock: clock, notice: notice, ended: map[string]time.Duration{}}
	var reported dispatcherapi.Preemption
	steps := preemptSteps{
		mark: func(reason string) { log.start("mark " + reason) },
		tell: func(ctx context.Context, p dispatcherapi.Preempting) { log.hang(ctx, "tell") },
		halt: func(ctx context.Context) { log.hang(ctx, "halt") },
		save: func(ctx context.Context, reason string) (savedWork, error) {
			log.hang(ctx, "save")
			return savedWork{Commit: "late"}, nil
		},
		lastUpload: func(ctx context.Context) (*uint64, error) {
			log.hang(ctx, "upload")
			return nil, ctx.Err()
		},
		lastCheckpoint: func() time.Time { return time.Time{} },
		end: func(ctx context.Context, p dispatcherapi.Preemption) {
			log.mu.Lock()
			reported = p
			log.mu.Unlock()
			log.hang(ctx, "end")
		},
	}

	p := drive(t, clock, func() dispatcherapi.Preemption {
		return handlePreemption(clock, dispatcherapi.EndPreempted, notice, steps)
	})

	if got := clock.Now().Sub(notice); got != hardLimit {
		t.Errorf("the handler ended %s after the notice, want at the hard limit %s", got, hardLimit)
	}
	log.waitEnded(t, 5)
	log.mu.Lock()
	defer log.mu.Unlock()
	want := map[string]time.Duration{"tell": 2 * time.Second, "halt": 10 * time.Second,
		"upload": 20 * time.Second, "save": 70 * time.Second, "end": 100 * time.Second}
	for name, at := range want {
		if log.ended[name] != at {
			t.Errorf("the %s step stopped %s after the notice, want %s", name, log.ended[name], at)
		}
	}
	if log.started[0] != "mark preempted" || !slices.Contains(log.started, "tell") ||
		slices.Index(log.started, "halt") > slices.Index(log.started, "save") ||
		slices.Index(log.started, "save") > slices.Index(log.started, "end") {
		t.Errorf("the steps started in the order %v", log.started)
	}
	if p.WIPCommit != "" || p.TranscriptOffset != nil {
		t.Errorf("a step that passed its limit gave what it saved: %+v", p)
	}
	if reported.SecondsToSaved != 70 || reported.SecondsToEnd != 70 || p.Reason != dispatcherapi.EndPreempted {
		t.Errorf("the report: %+v", reported)
	}
	problems := strings.Join(reported.Problems, "\n")
	for _, step := range []string{"halt", "save", "upload"} {
		if !strings.Contains(problems, step) {
			t.Errorf("the problems do not name the %s step: %v", step, reported.Problems)
		}
	}
}

func TestThePreemptionHandlerReportsWhatWasSavedAndTheAgeOfTheLastCheckpoint(t *testing.T) {
	clock := newFakeClock()
	notice := clock.Now()
	var reported dispatcherapi.Preemption
	offset := uint64(4096)
	steps := preemptSteps{
		mark: func(string) {},
		tell: func(context.Context, dispatcherapi.Preempting) {},
		halt: func(context.Context) { <-clock.After(4 * time.Second) },
		save: func(ctx context.Context, reason string) (savedWork, error) {
			<-clock.After(12 * time.Second)
			return savedWork{Commit: "0123abcd", Skipped: []string{"build/big.bin"}}, nil
		},
		lastUpload:     func(context.Context) (*uint64, error) { return &offset, nil },
		lastCheckpoint: func() time.Time { return notice.Add(-3 * time.Minute) },
		end:            func(_ context.Context, p dispatcherapi.Preemption) { reported = p },
	}

	drive(t, clock, func() dispatcherapi.Preemption {
		return handlePreemption(clock, dispatcherapi.EndMachineStopped, notice, steps)
	})

	if reported.Reason != dispatcherapi.EndMachineStopped || reported.WIPCommit != "0123abcd" ||
		!slices.Equal(reported.Skipped, []string{"build/big.bin"}) || reported.TranscriptOffset == nil ||
		*reported.TranscriptOffset != 4096 || reported.SecondsToSaved != 16 || len(reported.Problems) != 0 {
		t.Errorf("the report: %+v", reported)
	}
	if reported.CheckpointAgeSeconds == nil || *reported.CheckpointAgeSeconds != 180 {
		t.Errorf("the checkpoint age: %v, want 180 s", reported.CheckpointAgeSeconds)
	}
	if !reported.NoticeAt.Equal(notice) {
		t.Errorf("notice at %s, want %s", reported.NoticeAt, notice)
	}
}

func TestAStepThatFailsIsAProblemAndTheNextStepsStillRun(t *testing.T) {
	clock := newFakeClock()
	var reported *dispatcherapi.Preemption
	steps := preemptSteps{
		mark: func(string) {},
		tell: func(context.Context, dispatcherapi.Preempting) {},
		halt: func(context.Context) {},
		save: func(context.Context, string) (savedWork, error) {
			return savedWork{}, errors.New("the forge refused the push")
		},
		lastUpload:     func(context.Context) (*uint64, error) { return nil, errors.New("SolidTrace is down") },
		lastCheckpoint: func() time.Time { return time.Time{} },
		end:            func(_ context.Context, p dispatcherapi.Preemption) { reported = &p },
	}

	drive(t, clock, func() dispatcherapi.Preemption {
		return handlePreemption(clock, dispatcherapi.EndPreempted, clock.Now(), steps)
	})

	if reported == nil {
		t.Fatal("the handler made no report")
	}
	problems := strings.Join(reported.Problems, "\n")
	if !strings.Contains(problems, "the forge refused the push") || !strings.Contains(problems, "SolidTrace is down") {
		t.Errorf("the problems: %v", reported.Problems)
	}
	if reported.CheckpointAgeSeconds != nil {
		t.Errorf("a checkpoint age with no checkpoint: %v", *reported.CheckpointAgeSeconds)
	}
}

// A notice that the wrapper takes late (the read of the notice after a
// SIGTERM) leaves less time: the hard limit counts from the notice.
func TestTheHardLimitCountsFromTheNoticeNotFromTheStartOfTheHandler(t *testing.T) {
	clock := newFakeClock()
	notice := clock.Now().Add(-95 * time.Second)
	endStopped := make(chan time.Duration, 1)
	steps := preemptSteps{
		mark: func(string) {},
		tell: func(context.Context, dispatcherapi.Preempting) {},
		halt: func(context.Context) {},
		save: func(context.Context, string) (savedWork, error) { return savedWork{}, nil },
		lastUpload: func(context.Context) (*uint64, error) {
			return nil, nil
		},
		lastCheckpoint: func() time.Time { return time.Time{} },
		end: func(ctx context.Context, _ dispatcherapi.Preemption) {
			<-ctx.Done()
			endStopped <- clock.Now().Sub(notice)
		},
	}

	drive(t, clock, func() dispatcherapi.Preemption {
		return handlePreemption(clock, dispatcherapi.EndPreempted, notice, steps)
	})

	if got, end := clock.Now().Sub(notice), <-endStopped; got != hardLimit || end != hardLimit {
		t.Errorf("the handler ended %s after the notice (the end step at %s), want %s", got, end, hardLimit)
	}
}
