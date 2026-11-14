package agentprocessing

import (
	"errors"
	"sync"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

const (
	tickInterval = time.Second

	minBackoff = 100 * time.Millisecond
	maxBackoff = 30 * time.Second
)

// ErrStopped is returned when Stop ends processing.
var ErrStopped = errors.New("agent file processing stopped")

// loop is the worker that Agent file processing and the parse share: it
// runs step for each Project that has work, one at a time, and retries a
// Project whose step fails with backoff while the others carry on.
type loop struct {
	// name is the work, for the log.
	name string
	// step does the next piece of a Project's work. more reports whether
	// more may be waiting.
	step func(projectID uint32) (more bool, err error)

	mu      sync.Mutex
	pending map[uint32]struct{}
	wake    chan struct{}

	// Used only by the worker.
	failing map[uint32]*failure

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{}
	started  bool
}

// failure is when a Project whose step failed is tried again.
type failure struct {
	retryAt time.Time
	backoff time.Duration
}

func newLoop(name string, step func(projectID uint32) (bool, error)) *loop {
	return &loop{
		name:    name,
		step:    step,
		pending: map[uint32]struct{}{},
		wake:    make(chan struct{}, 1),
		failing: map[uint32]*failure{},
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

// Notify marks a Project as having new work and wakes the worker.
func (l *loop) Notify(projectID uint32) {
	l.mu.Lock()
	l.pending[projectID] = struct{}{}
	l.mu.Unlock()
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// Start runs the worker in its own goroutine.
func (l *loop) Start() {
	l.started = true
	go func() {
		defer close(l.done)
		tick := time.NewTicker(tickInterval)
		defer tick.Stop()
		for {
			if err := l.ProcessUntilCaughtUp(); errors.Is(err, ErrStopped) {
				return
			}
			select {
			case <-l.stop:
				return
			case <-l.wake:
			case <-tick.C:
			}
		}
	}()
}

// Stop stops the worker and waits for it to exit. Work not yet done is
// done after a restart, from the cursor.
func (l *loop) Stop() {
	l.stopOnce.Do(func() { close(l.stop) })
	if l.started {
		<-l.done
	}
}

// ProcessUntilCaughtUp does the work until no Project has more. A failing
// Project is retried after its backoff, so it only returns ErrStopped.
func (l *loop) ProcessUntilCaughtUp() error {
	for {
		select {
		case <-l.stop:
			return ErrStopped
		default:
		}
		projectID, ok, retryAt := l.takeDue()
		if ok {
			l.processProject(projectID)
			continue
		}
		if retryAt.IsZero() {
			return nil
		}
		timer := time.NewTimer(time.Until(retryAt))
		select {
		case <-l.stop:
			timer.Stop()
			return ErrStopped
		case <-l.wake:
		case <-timer.C:
		}
		timer.Stop()
	}
}

// takeDue takes a pending Project that isn't waiting to be retried, or
// returns the earliest retry time, zero if none is pending.
func (l *loop) takeDue() (projectID uint32, ok bool, retryAt time.Time) {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	for projectID := range l.pending {
		failed := l.failing[projectID]
		if failed == nil || !failed.retryAt.After(now) {
			delete(l.pending, projectID)
			return projectID, true, time.Time{}
		}
		if retryAt.IsZero() || failed.retryAt.Before(retryAt) {
			retryAt = failed.retryAt
		}
	}
	return 0, false, retryAt
}

// processProject does a Project's next step, or has it retried later.
func (l *loop) processProject(projectID uint32) {
	more, err := l.step(projectID)
	if err != nil {
		failed := l.failing[projectID]
		if failed == nil {
			failed = &failure{backoff: minBackoff}
			l.failing[projectID] = failed
		}
		logger.L.Error(l.name+" failed, retrying", "project_id", projectID, "error", err, "retry_in", failed.backoff)
		failed.retryAt = time.Now().Add(failed.backoff)
		failed.backoff = min(2*failed.backoff, maxBackoff)
		more = true
	} else {
		delete(l.failing, projectID)
	}
	if more {
		l.mu.Lock()
		l.pending[projectID] = struct{}{}
		l.mu.Unlock()
	}
}
