package nomadrunner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/solidtrace/factory/dispatcher"
)

// waitTime is the longest wait of one blocking query.
const waitTime = 5 * time.Minute

// retryAfter is the pause after a failed query.
const retryAfter = 2 * time.Second

// watchAllocations follows the allocations of the Runner's jobs with
// blocking queries, and records each change of an Execution's state.
func (r *Runner) watchAllocations(ctx context.Context) {
	r.watch(ctx, "allocations", func(index uint64) (uint64, error) {
		q := &api.QueryOptions{Filter: r.allocationFilter(), WaitIndex: index, WaitTime: waitTime}
		allocs, meta, err := r.client.Allocations().List(q.WithContext(ctx))
		if err != nil {
			return 0, err
		}
		for _, a := range allocs {
			if err := r.observe(ctx, a); err != nil {
				return 0, err
			}
		}
		return meta.LastIndex, nil
	})
}

// watchNodes follows Nomad's nodes. A node that is down lost its
// allocations: each Execution on it ends as lost.
func (r *Runner) watchNodes(ctx context.Context) {
	r.watch(ctx, "nodes", func(index uint64) (uint64, error) {
		q := &api.QueryOptions{WaitIndex: index, WaitTime: waitTime}
		nodes, meta, err := r.client.Nodes().List(q.WithContext(ctx))
		if err != nil {
			return 0, err
		}
		for _, n := range nodes {
			if n.Status == api.NodeStatusDown {
				r.nodeDown(n.ID)
			}
		}
		return meta.LastIndex, nil
	})
}

// watch runs query in a loop with Nomad's index, until ctx ends. After an
// error, it waits and asks again from the same index.
func (r *Runner) watch(ctx context.Context, what string, query func(index uint64) (uint64, error)) {
	var index uint64
	for ctx.Err() == nil {
		last, err := query(index)
		if err != nil {
			if ctx.Err() == nil {
				r.log.Warn("could not read Nomad's "+what, "error", err)
			}
			select {
			case <-ctx.Done():
			case <-time.After(retryAfter):
			}
			continue
		}
		// Nomad's index can go back (a new leader); then start again.
		if last < index {
			last = 0
		}
		index = last
	}
}

func (r *Runner) allocationFilter() string {
	jobs := make([]string, len(r.cfg.Kinds))
	for i, k := range r.cfg.Kinds {
		jobs[i] = r.JobID(k.Name)
	}
	return fmt.Sprintf(`JobID matches "^(%s)/dispatch-"`, strings.Join(jobs, "|"))
}

// observe records the allocation's state for its Execution, when it
// changed.
func (r *Runner) observe(ctx context.Context, a *api.AllocationListStub) error {
	if a.Namespace != r.cfg.Namespace || !r.ours(a.JobID) {
		return nil
	}
	execution, err := r.execution(ctx, a.JobID)
	if err != nil || execution == "" {
		return err
	}
	ev := dispatcher.RunnerEvent{Execution: execution}
	switch a.ClientStatus {
	case api.AllocClientStatusPending:
		ev.State = dispatcher.RunnerPending
	case api.AllocClientStatusRunning:
		ev.State = dispatcher.RunnerRunning
	case api.AllocClientStatusComplete:
		ev.State, ev.Ended, ev.ExitCode = dispatcher.RunnerComplete, true, exitCode(a)
	case api.AllocClientStatusFailed:
		ev.State, ev.Ended, ev.ExitCode = dispatcher.RunnerFailed, true, exitCode(a)
	case api.AllocClientStatusLost:
		ev.State, ev.Ended, ev.Lost = dispatcher.RunnerLost, true, true
	default:
		// "unknown": the client is disconnected; its node's state tells.
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workloads[a.ID] = a.JobID
	t := r.tracked[execution]
	if t == nil {
		t = &tracked{}
		r.tracked[execution] = t
	}
	t.node = a.NodeID
	if t.ended || t.state == ev.State {
		return nil
	}
	t.state, t.ended = ev.State, ev.Ended
	r.record(ev)
	return nil
}

// execution gives the Execution of a dispatched job: from Dispatch, or,
// for a job that another Runner dispatched (before a restart), from the
// job's meta.
func (r *Runner) execution(ctx context.Context, jobID string) (string, error) {
	r.mu.Lock()
	execution, ok := r.executions[jobID]
	r.mu.Unlock()
	if ok {
		return execution, nil
	}
	job, _, err := r.client.Jobs().Info(jobID, (&api.QueryOptions{}).WithContext(ctx))
	if notFound(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("read the Nomad job %s: %w", jobID, err)
	}
	execution = job.Meta[MetaExecution]
	r.mu.Lock()
	r.executions[jobID] = execution
	r.mu.Unlock()
	return execution, nil
}

// nodeDown ends each Execution on the node as lost.
func (r *Runner) nodeDown(node string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for execution, t := range r.tracked {
		if t.ended || t.node != node {
			continue
		}
		t.state, t.ended = dispatcher.RunnerLost, true
		r.record(dispatcher.RunnerEvent{Execution: execution, State: dispatcher.RunnerLost, Ended: true, Lost: true})
	}
}

// record keeps an event. The caller holds r.mu.
func (r *Runner) record(ev dispatcher.RunnerEvent) {
	r.events = append(r.events, ev)
	r.changed.Broadcast()
}

// exitCode is the wrapper's exit code from its task's last Terminated
// event, or -1 when it has none (the task did not start).
func exitCode(a *api.AllocationListStub) int {
	state := a.TaskStates[TaskName]
	if state == nil {
		return -1
	}
	for i := len(state.Events) - 1; i >= 0; i-- {
		if state.Events[i].Type == api.TaskTerminated {
			return state.Events[i].ExitCode
		}
	}
	return -1
}
