package dispatcher

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Runner is the interface that starts Executions on machines (Nomad in
// the MVP; the fake Runner in Seam 3).
type Runner interface {
	// Dispatch starts the wrapper of an Execution. It gives the workload:
	// the subject that the wrapper's workload identity JWT has. The
	// Dispatcher gives the Execution's task only to a JWT with that
	// subject.
	Dispatch(ctx context.Context, e RunnerExecution) (workload string, err error)
	// Events gives the Runner's events from number from on, and waits
	// until there is at least one or ctx ends.
	Events(ctx context.Context, from int) ([]RunnerEvent, error)
	// JWKSURL is where the Runner gives the keys that sign its workload
	// identity JWTs.
	JWKSURL() string
}

// RunnerExecution is what the Runner needs to start an Execution.
type RunnerExecution struct {
	Run       string
	Execution string
}

// RunnerEvent is one change of an Execution on the Runner.
type RunnerEvent struct {
	Execution string
	// Ended is true when the wrapper is no longer there.
	Ended bool
	// Lost is true when the machine went with no notice.
	Lost bool
	// ExitCode is the wrapper's exit code, when it ended.
	ExitCode int
}

// Forge is the part of the Forge interface that a clone needs (the
// Bitbucket Cloud Forge, ticket 37, adds the rest).
type Forge interface {
	// Clone gives the HTTPS URL that git clones repository
	// (host/workspace/name) from, and the user name that goes with the
	// connection's token in the credential helper.
	Clone(repository string) (url, username string)
}

// bitbucketForge is the clone part of Bitbucket Cloud: an access token
// goes with the user name x-token-auth.
type bitbucketForge struct{}

func (bitbucketForge) Clone(repository string) (string, string) {
	return "https://" + strings.TrimSuffix(repository, ".git") + ".git", "x-token-auth"
}

// dispatchEvery is how often the dispatch loop looks for queued Runs when
// nothing wakes it.
const dispatchEvery = 10 * time.Second

// dispatchLoop starts an Execution for each queued Run. A start wakes it.
func (d *Dispatcher) dispatchLoop(ctx context.Context) {
	for {
		d.dispatchQueued(ctx)
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case <-time.After(dispatchEvery):
		}
	}
}

func (d *Dispatcher) wakeDispatch() {
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

func (d *Dispatcher) dispatchQueued(ctx context.Context) {
	runs, err := d.store.queuedRuns()
	if err != nil {
		d.log.Error("could not read the queued Runs", "error", err)
		return
	}
	for _, run := range runs {
		if ctx.Err() != nil {
			return
		}
		d.dispatch(ctx, run)
	}
}

// dispatch makes an Execution for a queued Run and gives it to the
// Runner. The Execution's task waits for the workload (see started).
func (d *Dispatcher) dispatch(ctx context.Context, runID string) {
	id := newRunID()
	ready := make(chan struct{})
	d.dispatching.Store(id, ready)
	defer func() {
		close(ready)
		d.dispatching.Delete(id)
	}()
	made, err := d.store.addExecution(runID, id, d.now())
	if err != nil || !made {
		if err != nil {
			d.log.Error("could not add an Execution", "run", runID, "error", err)
		}
		return
	}
	workload, err := d.runner.Dispatch(ctx, RunnerExecution{Run: runID, Execution: id})
	if err == nil && workload == "" {
		err = fmt.Errorf("the Runner gave no workload")
	}
	if err != nil {
		d.log.Error("the Runner could not start an Execution", "run", runID, "execution", id, "error", err)
		d.endByDispatcher(id, "the Runner could not start the Execution: "+err.Error())
		return
	}
	if err := d.store.setWorkload(id, workload); err != nil {
		d.log.Error("could not keep an Execution's workload", "execution", id, "error", err)
	}
}

// watchLoop reads the Runner's events. An Execution whose wrapper is gone
// before the wrapper's end call fails its Run: the outcome never comes
// from a report that no end confirmed.
func (d *Dispatcher) watchLoop(ctx context.Context) {
	for seen := 0; ; {
		events, err := d.runner.Events(ctx, seen)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			d.log.Warn("could not read the Runner's events", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}
		seen += len(events)
		for _, ev := range events {
			if !ev.Ended {
				continue
			}
			reason := fmt.Sprintf("the wrapper exited with code %d before the end of the Execution", ev.ExitCode)
			if ev.Lost {
				reason = "the machine was lost"
			}
			d.endByDispatcher(ev.Execution, reason)
		}
	}
}

// endByDispatcher ends an Execution that has not ended, and fails its
// Run with reason. An Execution that ended already does not change.
func (d *Dispatcher) endByDispatcher(id, reason string) {
	ended, err := d.store.failExecution(id, reason, d.now())
	if err != nil {
		d.log.Error("could not end an Execution", "execution", id, "error", err)
		return
	}
	if ended {
		d.log.Warn("an Execution ended with no end call", "execution", id, "reason", reason)
	}
}

// outcome is the Run's state from the Execution's last valid report, or,
// with none, failed with the wrapper's reason. It never reads the exit
// code.
func outcome(report *dispatcherapi.Report, end dispatcherapi.ExecutionEnd) (state, reason, question, summary string) {
	if report != nil {
		switch report.Outcome {
		case dispatcherapi.OutcomeDone:
			return dispatcherapi.StateDone, "", "", report.Summary
		case dispatcherapi.OutcomeNeedsAPerson:
			return dispatcherapi.StateWaitingOnPerson, "needs a person", report.Question, report.Summary
		case dispatcherapi.OutcomeFailed:
			return dispatcherapi.StateFailed, report.Summary, "", report.Summary
		}
	}
	reason = end.Reason
	if reason == "" || reason == dispatcherapi.EndReported {
		reason = dispatcherapi.EndNoReport
	}
	if end.Error != "" {
		reason += ": " + end.Error
	}
	return dispatcherapi.StateFailed, reason, "", ""
}
