package wrapper

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The preemption handler (spec V3). When the Machine notice says that the
// machine will be taken back, or a SIGTERM or a SIGINT comes with no stop
// command, the wrapper does these steps in order, each with a time limit:
//
//  0. The read of the last checkpoint's time (markLimit).
//  1. The "preempting" marker (markLimit): no more messages on stdin, the
//     agent's socket refuses new calls and stops the calls that run, and
//     one call tells the Dispatcher (preemptingWait, not awaited).
//  2. The halt (haltLimit): the interrupt request, haltInterruptWait for the
//     result, then the end procedure with haltEndWait.
//  3. The save of the work (saveLimit) and the last upload of the
//     transcript and evidence/ (lastUploadLimit), together.
//  4. The end call, which reports what was saved, best effort.
//
// The handler ends at hardLimit after the notice at the latest, also when
// a step does not return: it goes on at each limit.
const (
	markLimit         = 1 * time.Second
	preemptingWait    = 2 * time.Second
	haltLimit         = 10 * time.Second
	haltInterruptWait = 3 * time.Second
	haltEndWait       = 1 * time.Second
	saveLimit         = 60 * time.Second
	lastUploadLimit   = 10 * time.Second
	hardLimit         = 100 * time.Second
)

// clock is the time of the preemption handler. Tests give a clock that
// they move.
type clock interface {
	Now() time.Time
	// Timer fires after d; stop stops it.
	Timer(d time.Duration) (c <-chan time.Time, stop func())
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func (realClock) Timer(d time.Duration) (<-chan time.Time, func()) {
	t := time.NewTimer(d)
	return t.C, func() { t.Stop() }
}

// savedWork is what the save of the work saved.
type savedWork struct {
	// Commit is the work-in-progress commit ("" when none was saved).
	Commit string
	// Skipped are the files that the save left out.
	Skipped []string
}

// preemptSteps are the steps of the preemption handler. Each step gets a
// context that ends at its time limit.
type preemptSteps struct {
	mark           func(reason string)
	tell           func(ctx context.Context, p dispatcherapi.Preempting)
	halt           func(ctx context.Context)
	save           func(ctx context.Context, reason string) (savedWork, error)
	lastUpload     func(ctx context.Context) (transcriptOffset *uint64, err error)
	lastCheckpoint func() time.Time
	end            func(ctx context.Context, p dispatcherapi.Preemption)
}

// within runs step with a context that ends after limit. It gives the
// step's value and true when the step returned in the limit; else it
// ends the context and returns at the limit, and the step's value is lost.
func within[T any](c clock, limit time.Duration, step func(ctx context.Context) T) (T, bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan T, 1)
	go func() { done <- step(ctx) }()
	timer, stop := c.Timer(limit)
	defer stop()
	select {
	case v := <-done:
		return v, true
	case <-timer:
		var zero T
		return zero, false
	}
}

// handlePreemption runs the steps in order, and gives what the end step
// reported.
func handlePreemption(c clock, reason string, noticeAt time.Time, s preemptSteps) dispatcherapi.Preemption {
	p := dispatcherapi.Preemption{Reason: reason, NoticeAt: noticeAt.UTC()}
	hard := noticeAt.Add(hardLimit)
	// limit is a step's limit, never past the hard limit.
	limit := func(d time.Duration) time.Duration { return min(d, hard.Sub(c.Now())) }
	var mu sync.Mutex
	problem := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		p.Problems = append(p.Problems, fmt.Sprintf(format, args...))
	}

	if last, ok := within(c, limit(markLimit), func(context.Context) time.Time { return s.lastCheckpoint() }); !ok {
		problem("the read of the last checkpoint did not end in %s", markLimit)
	} else if !last.IsZero() {
		age := noticeAt.Sub(last).Seconds()
		p.CheckpointAgeSeconds = &age
	}
	if _, ok := within(c, limit(markLimit), func(context.Context) struct{} {
		s.mark(reason)
		return struct{}{}
	}); !ok {
		problem("the preempting marker did not end in %s", markLimit)
	}
	tell := dispatcherapi.Preempting{Reason: reason, NoticeAt: p.NoticeAt}
	go within(c, limit(preemptingWait), func(ctx context.Context) struct{} {
		s.tell(ctx, tell)
		return struct{}{}
	})

	if _, ok := within(c, limit(haltLimit), func(ctx context.Context) struct{} {
		s.halt(ctx)
		return struct{}{}
	}); !ok {
		problem("the halt did not end in %s", haltLimit)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		type result struct {
			saved savedWork
			err   error
		}
		r, ok := within(c, limit(saveLimit), func(ctx context.Context) result {
			saved, err := s.save(ctx, reason)
			return result{saved, err}
		})
		switch {
		case !ok:
			problem("the save of the work did not end in %s", saveLimit)
		case r.err != nil:
			problem("the save of the work: %v", r.err)
		default:
			mu.Lock()
			p.WIPCommit, p.Skipped = r.saved.Commit, r.saved.Skipped
			mu.Unlock()
		}
	}()
	go func() {
		defer wg.Done()
		type result struct {
			offset *uint64
			err    error
		}
		r, ok := within(c, limit(lastUploadLimit), func(ctx context.Context) result {
			offset, err := s.lastUpload(ctx)
			return result{offset, err}
		})
		// An error with an offset is a problem of the other files: the
		// offset of the transcript still counts.
		switch {
		case !ok:
			problem("the last upload did not end in %s", lastUploadLimit)
			return
		case r.err != nil:
			problem("the last upload: %v", r.err)
		}
		mu.Lock()
		p.TranscriptOffset = r.offset
		mu.Unlock()
	}()
	wg.Wait()
	p.SecondsToSaved = c.Now().Sub(noticeAt).Seconds()

	p.SecondsToEnd = c.Now().Sub(noticeAt).Seconds()
	report := p
	within(c, limit(hardLimit), func(ctx context.Context) struct{} {
		s.end(ctx, report)
		return struct{}{}
	})
	return p
}
