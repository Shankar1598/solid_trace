package dispatcher

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
)

// Runner is the interface that starts Executions on machines (Nomad in
// the MVP; the fake Runner in Seam 3).
type Runner interface {
	// Dispatch starts the wrapper of an Execution. It gives the workload:
	// the name of the one workload on the Runner that runs this
	// Execution's wrapper (on Nomad, the dispatched job). The Dispatcher
	// gives the Execution's task only to a JWT that Workload maps to it.
	Dispatch(ctx context.Context, e RunnerExecution) (workload string, err error)
	// Workload gives the workload that holds a workload identity JWT
	// that the Dispatcher checked against the JWKS. It gives
	// ErrUnknownWorkload when the JWT is of no workload of the Runner.
	Workload(ctx context.Context, identity Identity) (string, error)
	// Events gives the Runner's events from number from on, and waits
	// until there is at least one or ctx ends.
	Events(ctx context.Context, from int) ([]RunnerEvent, error)
	// Stop stops the workload that Dispatch gave. The wrapper gets
	// SIGTERM, and each of its processes is killed after the Runner's
	// kill timeout. The end comes as an event.
	Stop(ctx context.Context, workload string) error
	// JWKSURL is where the Runner gives the keys that sign its workload
	// identity JWTs.
	JWKSURL() string
}

// ErrUnknownWorkload is the error of Runner.Workload for a JWT of no
// workload of the Runner.
var ErrUnknownWorkload = errors.New("the JWT is of no workload of the Runner")

// RunnerExecution is what the Runner needs to start an Execution.
type RunnerExecution struct {
	Run       string
	Execution string
	// MachineKind is the kind of machine that the factory file names; ""
	// is the operator's default kind.
	MachineKind string
}

// The states of an Execution on the Runner (RunnerEvent.State).
const (
	RunnerPending  = "pending"
	RunnerRunning  = "running"
	RunnerComplete = "complete" // the wrapper exited with code 0
	RunnerFailed   = "failed"   // the wrapper exited with another code, or did not start
	RunnerLost     = "lost"     // the machine went with no notice
)

// RunnerEvent is one change of an Execution on the Runner.
type RunnerEvent struct {
	Execution string
	// State is one of the Runner* states.
	State string
	// Ended is true when the wrapper is no longer there.
	Ended bool
	// Lost is true when the machine went with no notice.
	Lost bool
	// ExitCode is the wrapper's exit code, when it ended.
	ExitCode int
}

// dispatchEvery is how often the dispatch loop looks for queued Runs when
// nothing wakes it: a Run that waits for a free account gets one at most
// this long after an operator adds an account or a limit's reset time.
const dispatchEvery = 10 * time.Second

// dispatchLoop starts an Execution for each queued Run. A start wakes it.
func (d *Dispatcher) dispatchLoop(ctx context.Context) {
	for {
		d.dispatchQueued(ctx)
		select {
		case <-ctx.Done():
			return
		case <-d.wake:
		case <-time.After(d.dispatchEvery):
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
	// A Run that waits on an account limit continues when an account is
	// free again (at a reset time, or an account that the operator added).
	if _, err := d.store.continueWaitingRuns(d.now()); err != nil {
		d.log.Error("could not continue the Runs that wait on an account limit", "error", err)
	}
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
	file, err := d.dispatchFactoryFile(ctx, runID, id)
	if err != nil {
		d.log.Warn("the factory file fails a Run", "run", runID, "execution", id, "error", err)
		d.endByDispatcher(id, err.Error(), nil)
		return
	}
	workload, err := d.runner.Dispatch(ctx, RunnerExecution{Run: runID, Execution: id, MachineKind: file.MachineKind})
	if err == nil && workload == "" {
		err = fmt.Errorf("the Runner gave no workload")
	}
	if err != nil {
		d.log.Error("the Runner could not start an Execution", "run", runID, "execution", id, "error", err)
		d.endByDispatcher(id, "the Runner could not start the Execution: "+err.Error(), nil)
		return
	}
	if err := d.store.setWorkload(id, workload); err != nil {
		d.log.Error("could not keep an Execution's workload", "execution", id, "error", err)
	}
}

// dispatchFactoryFile reads the Run's factory file from the default
// branch, keeps it for the Execution, and adds its extra hosts to the
// Squid allowlist before the Execution starts.
func (d *Dispatcher) dispatchFactoryFile(ctx context.Context, runID, executionID string) (factoryFile, error) {
	run, err := d.store.runCheckout(runID)
	if err != nil {
		return factoryFile{}, fmt.Errorf("the Dispatcher could not read the Run: %w", err)
	}
	connection := ""
	settings, err := d.store.settings(run.organizationID, run.projectID)
	switch {
	case err == nil:
		connection = settings.Connection
		if run.repository == "" {
			run.repository = settings.Repository
		}
	case !errors.Is(err, errNotFound):
		return factoryFile{}, fmt.Errorf("the Dispatcher could not read the Factory settings: %w", err)
	}
	f, err := d.repositoryForge(run.organizationID, connection)
	if err != nil {
		return factoryFile{}, err
	}
	file, err := d.readFactoryFile(ctx, f, run.repository, run.folder)
	if err != nil {
		return factoryFile{}, err
	}
	encoded, err := encodeFactoryFile(file)
	if err == nil {
		err = d.store.setFactoryFile(executionID, encoded)
	}
	if err == nil {
		err = d.store.setRepositoryHosts(run.repository, run.folder, file.AllowedHosts, d.now())
	}
	if err != nil {
		return factoryFile{}, fmt.Errorf("the Dispatcher could not keep the factory file: %w", err)
	}
	// With no Squid reload, an Execution that needs an extra host fails
	// at its first call to it with a reason that does not name the
	// allowlist. Fail it here with the cause. The next refresh tries the
	// reload again.
	if err := d.refreshAllowlist(); err != nil && !d.allowlist.has(file.AllowedHosts) {
		return factoryFile{}, fmt.Errorf("the Dispatcher could not add the allowed hosts of the factory file "+
			"to the Squid allowlist: %w", err)
	}
	return file, nil
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
				d.log.Info("an Execution changed state on the Runner", "execution", ev.Execution, "state", ev.State)
				continue
			}
			// The Runner names the workload's first Execution. A take-over
			// Execution in the same workload ends with it.
			ids, err := d.store.executionsOfWorkload(ev.Execution)
			if err != nil {
				d.log.Error("could not read the Executions of a workload", "execution", ev.Execution, "error", err)
				ids = []string{ev.Execution}
			}
			for _, id := range ids {
				d.endOnTheRunner(id, ev)
			}
		}
	}
}

// endOnTheRunner ends an Execution whose workload ended on the Runner
// before its end call.
func (d *Dispatcher) endOnTheRunner(id string, ev RunnerEvent) {
	_, stopped := d.runnerStopped.LoadAndDelete(id)
	if ev.Lost {
		d.endByDispatcher(id, "the machine was lost", nil)
		return
	}
	code := ev.ExitCode
	if stopped {
		d.endByDispatcher(id, endStoppedByRunner, &code)
		return
	}
	d.endByDispatcher(id, fmt.Sprintf("the wrapper exited with code %d before the end of the Execution", code), &code)
}

// endByDispatcher ends an Execution that has not ended, and fails its
// Run with reason. exitCode is the wrapper's exit code, or nil when there
// is none (a lost machine, or a wrapper that did not start). An Execution
// that ended already does not change.
func (d *Dispatcher) endByDispatcher(id, reason string, exitCode *int) {
	ended, err := d.store.failExecution(id, reason, exitCode, d.now())
	if err != nil {
		d.log.Error("could not end an Execution", "execution", id, "error", err)
		return
	}
	if ended {
		d.warn("an Execution ended with no end call", "execution.id", id, "execution.reason", reason)
		d.revokeTokens(id)
		d.wakeDispatch()
	}
}

// takeOverOutcome is the Run's state at the end of a take-over Execution:
// the outcome of a report made during the take-over, or with none,
// waiting on a person. A stop wins, as for a headless Execution.
func takeOverOutcome(report *dispatcherapi.Report, end dispatcherapi.ExecutionEnd) (state, reason, question, summary string) {
	if end.Reason == dispatcherapi.EndStopped || report != nil {
		return outcome(report, end)
	}
	reason = dispatcherapi.ReasonTakeOverNoReport
	if end.Reason != dispatcherapi.EndSessionEnded && end.Reason != "" {
		reason += " (" + end.Reason
		if end.Error != "" {
			reason += ": " + end.Error
		}
		reason += ")"
	}
	return dispatcherapi.StateWaitingOnPerson, reason, "", ""
}

// outcome is the Run's state from the Execution's last valid report, or,
// with none, failed with the wrapper's reason. It never reads the exit
// code.
func outcome(report *dispatcherapi.Report, end dispatcherapi.ExecutionEnd) (state, reason, question, summary string) {
	// A stop is a person's choice, and wins over a report. The stop call
	// set the Run's state already; this is the same state.
	if end.Reason == dispatcherapi.EndStopped {
		return dispatcherapi.StateStopped, dispatcherapi.EndStopped, "", ""
	}
	// An interrupt with no message is a person's choice too: the Run
	// waits on that person, also after a report. The report's summary
	// stays.
	if end.Reason == dispatcherapi.EndInterrupted {
		if report != nil {
			summary = report.Summary
		}
		return dispatcherapi.StateWaitingOnPerson, dispatcherapi.EndInterrupted, "", summary
	}
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
	// At the usage limit, the end decides the Run's account (see
	// store.atUsageLimit). A report before it set the outcome above.
	if end.Reason == dispatcherapi.EndUsageLimit {
		return dispatcherapi.StateWaitingOnAccountLimit, dispatcherapi.EndUsageLimit, "", ""
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
