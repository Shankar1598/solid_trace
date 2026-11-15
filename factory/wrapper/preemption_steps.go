package wrapper

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// The preemption handler's steps on an Execution (preemption.go has their
// order and limits).

// maxSkipped is the most names of skipped files that the report keeps.
const maxSkipped = 200

// Owners of the end of an Execution (preemption.owner): the first to take
// it does the end call.
const (
	ownerNone int32 = iota
	// ownerEnd is the usual end, after the harness and the end procedure.
	ownerEnd
	// ownerPreemption is the preemption handler, from its trigger on.
	ownerPreemption
)

// errMarked is the refusal of what the preempting marker stops.
var errMarked = errors.New("the machine is being taken back")

// errNotSent is the preemption handler's end call before it is sent.
var errNotSent = errors.New("the end call was not sent")

// workSaver saves the work: a work-in-progress commit of the checkout.
type workSaver interface {
	save(ctx context.Context, reason string) (savedWork, error)
	// lastCheckpoint is the time of the last checkpoint (zero: none).
	lastCheckpoint() time.Time
}

// noSaver is the saver of a wrapper with no work-in-progress save.
type noSaver struct{}

func (noSaver) save(context.Context, string) (savedWork, error) {
	return savedWork{}, errors.New("the wrapper has no work-in-progress save, so the work in the checkout is not saved")
}

func (noSaver) lastCheckpoint() time.Time { return time.Time{} }

// preemption is the preemption handler's part of an Execution.
type preemption struct {
	clock clock
	saver workSaver
	// owner is the owner of the end (ownerNone until one takes it).
	owner atomic.Int32
	// marked ends at the preempting marker (and when the wrapper's own
	// context ends): the setup, the agent's socket calls and the uploads
	// of the usual end stop with it. isMarked tells the marker only.
	marked     context.Context
	mark       context.CancelFunc
	markedFlag atomic.Bool
	// preempted closes when the handler returned; preemptEnd is the end
	// call's error.
	preempted  chan struct{}
	preemptMu  sync.Mutex
	preemptEnd error
}

func (e *execution) isMarked() bool { return e.markedFlag.Load() }

// watch starts the preemption handler at the Machine notice, or at a
// SIGTERM or a SIGINT. A signal after a stop command starts no handler:
// it is the Runner's stop, not the machine's, and the stop command ends
// the Execution.
func (e *execution) watch(ctx context.Context, notice machineNotice, signals <-chan os.Signal) {
	noticed := make(chan time.Time, 1)
	go func() {
		if notice.Wait(ctx) == nil {
			noticed <- e.clock.Now()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-noticed:
			e.trigger(dispatcherapi.EndPreempted, at)
		case sig := <-signals:
			if e.stopping.Load() {
				fmt.Fprintf(e.log, "wrapper: %s after a stop command: the stop command ends the Execution\n", sig)
				continue
			}
			at := e.clock.Now()
			go func() {
				// The reason is preempted when the notice says so.
				read, cancel := context.WithTimeout(ctx, time.Second)
				defer cancel()
				reason := dispatcherapi.EndMachineStopped
				if preempted, err := notice.Preempted(read); err != nil {
					fmt.Fprintf(e.log, "wrapper: %s: read the Machine notice: %v\n", sig, err)
				} else if preempted {
					reason = dispatcherapi.EndPreempted
				}
				e.trigger(reason, at)
			}()
		}
	}
}

// trigger starts the preemption handler, once, unless the usual end call
// started.
func (e *execution) trigger(reason string, at time.Time) {
	if !e.owner.CompareAndSwap(ownerNone, ownerPreemption) {
		return
	}
	fmt.Fprintf(e.log, "wrapper: %s: the preemption handler starts\n", reason)
	go func() {
		defer close(e.preempted)
		handlePreemption(e.clock, reason, at, preemptSteps{
			mark:           e.setMarker,
			tell:           e.tellPreempting,
			halt:           func(ctx context.Context) { e.halt(ctx, reason) },
			save:           e.save,
			lastUpload:     e.uploads.last,
			lastCheckpoint: e.saver.lastCheckpoint,
			end:            e.endPreempted,
		})
		// The secrets go in the time that is left. The workspace and the
		// run user's home stay: a save or a last upload that did not end
		// leaves its work only there. The next Execution's setup deletes
		// them, and a machine that is taken back takes its disk too.
		if left := at.Add(hardLimit).Sub(e.clock.Now()); left > 0 {
			within(e.clock, left, func(context.Context) struct{} {
				if err := removeAll(e.cfg.secrets()); err != nil {
					fmt.Fprintln(e.log, "wrapper: delete the secrets after the preemption:", err)
				}
				return struct{}{}
			})
		}
	}()
}

// save is step 3's save of the work: the work-in-progress save, then the
// push of each commit of the Run's branch that is not on the remote (as
// the usual end does).
func (e *execution) save(ctx context.Context, reason string) (savedWork, error) {
	saved, err := e.saver.save(ctx, reason)
	if failed := e.push(e.environ); failed != "" {
		err = errors.Join(err, errors.New(failed))
	}
	return saved, err
}

// setMarker is step 1's marker: no more messages on stdin, and the
// agent's socket calls, the setup and the usual end's uploads stop.
// It takes no lock, so that a write or a report that blocks does not
// hold it.
func (e *execution) setMarker(string) {
	e.markedFlag.Store(true)
	e.mark()
}

func (e *execution) tellPreempting(ctx context.Context, p dispatcherapi.Preempting) {
	a, err := e.client.call(ctx, "preempting", p)
	if err == nil {
		err = a.err("preempting")
	}
	if err != nil {
		fmt.Fprintln(e.log, "wrapper: tell the Dispatcher that the machine goes:", err)
	}
}

// halt is step 2: the interrupt request, interruptWait for the result,
// then the end procedure with haltEndWait.
func (e *execution) halt(ctx context.Context, reason string) {
	// The harness starts under mu, and does not start after the marker.
	e.mu.Lock()
	started := e.stdin != nil
	e.mu.Unlock()
	if started {
		// The interrupt waits at most writeWait (its write deadline),
		// and the close of stdin stops it too. It does not hold the wait
		// for the result.
		go func() {
			if err := e.interrupt(); err != nil {
				fmt.Fprintln(e.log, "wrapper: the halt's interrupt request:", err)
			}
		}()
		timer, stop := e.clock.Timer(interruptWait)
		select {
		case <-e.results:
		case <-e.followed:
		case <-timer:
		case <-ctx.Done():
		}
		stop()
		e.beginEndWithin(reason, "", false, haltEndWait)
		select {
		case <-e.followed:
		case <-ctx.Done():
			return
		}
	}
	e.endProcedure(ctx)
}

// endPreempted is step 4: the end call, with what was saved.
func (e *execution) endPreempted(ctx context.Context, p dispatcherapi.Preemption) {
	if len(p.Skipped) > maxSkipped {
		p.Problems = append(p.Problems, fmt.Sprintf("the save left out %d more files", len(p.Skipped)-maxSkipped))
		p.Skipped = p.Skipped[:maxSkipped]
	}
	end := dispatcherapi.ExecutionEnd{Reason: p.Reason, Preemption: &p}
	select {
	case <-e.followed:
		end.ExitCode, end.TaskDeliveredAt = e.followEnd.ExitCode, e.followEnd.TaskDeliveredAt
	default:
	}
	e.mu.Lock()
	if e.populatedAtKill {
		end.BackgroundTasksKilled = e.backgroundTasks
	}
	e.mu.Unlock()
	err := e.client.end(ctx, end)
	if err != nil {
		fmt.Fprintln(e.log, "wrapper: the end call after the preemption:", err)
	}
	e.preemptMu.Lock()
	e.preemptEnd = err
	e.preemptMu.Unlock()
}

// preemptionCode is the wrapper's exit code after the preemption handler:
// 0 when the Dispatcher took the end call and the end procedure emptied
// and removed the Execution's cgroup.
func (e *execution) preemptionCode() int {
	e.preemptMu.Lock()
	err := e.preemptEnd
	e.preemptMu.Unlock()
	select {
	case <-e.finished:
	default:
		return 1
	}
	if err != nil || e.left != nil {
		return 1
	}
	return 0
}
