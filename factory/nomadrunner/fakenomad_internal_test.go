package nomadrunner

// The Runner's tests against a fake Nomad HTTP API. They try the cases
// that the stack's one Nomad cannot easily make: another namespace, a
// failed stop, a task that did not start, an index that goes back.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/solidtrace/factory/dispatcher"
)

// fakeNomad serves the part of Nomad's HTTP API that the Runner uses.
// The allocation and node lists are blocking queries, each on its own
// index, as in Nomad.
type fakeNomad struct {
	mu      sync.Mutex
	index   map[string]uint64
	changed chan struct{}
	allocs  []*api.AllocationListStub
	nodes   []*api.NodeListStub
	// jobs are the dispatched jobs, and allocations each allocation that
	// GET /v1/allocation/<id> gives.
	jobs        map[string]*api.Job
	allocations map[string]*api.Allocation
	// stops are the jobs of each DELETE /v1/job/<id>; failStops is the
	// number of the next stops that fail with 500.
	stops     []string
	failStops int
	// allocationReads counts the GET /v1/allocation/<id> calls.
	allocationReads int
}

func newFakeNomad(t *testing.T) (*fakeNomad, *Runner) {
	t.Helper()
	f := &fakeNomad{index: map[string]uint64{"/v1/allocations": 1, "/v1/nodes": 1}, changed: make(chan struct{}), allocs: []*api.AllocationListStub{}, nodes: []*api.NodeListStub{}, jobs: map[string]*api.Job{}, allocations: map[string]*api.Allocation{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	r, err := New(Config{Address: srv.URL, Kinds: []dispatcher.MachineKind{{Name: "default", MemoryMB: 64}}, Command: "/bin/true"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return f, r
}

// update changes the fake's state. A change of a list (its path) ends
// the blocking queries of that list.
func (f *fakeNomad) update(list string, change func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change()
	if list != "" {
		f.index[list]++
	}
	close(f.changed)
	f.changed = make(chan struct{})
}

// job adds a dispatched job of the Runner for an Execution.
func (f *fakeNomad) job(execution string) string {
	id := "factory-execution-default/dispatch-1-" + execution
	f.update("", func() { f.jobs[id] = &api.Job{ID: &id, Meta: map[string]string{MetaExecution: execution}} })
	return id
}

func (f *fakeNomad) stopped() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.stops...)
}

func (f *fakeNomad) serve(w http.ResponseWriter, req *http.Request) {
	f.mu.Lock()
	h := w.Header()
	h.Set("X-Nomad-LastContact", "0")
	h.Set("X-Nomad-KnownLeader", "true")
	path := req.URL.Path
	var body any
	switch {
	case req.Method == http.MethodGet && (path == "/v1/allocations" || path == "/v1/nodes"):
		wait, _ := strconv.ParseUint(req.URL.Query().Get("index"), 10, 64)
		for wait >= f.index[path] {
			changed := f.changed
			f.mu.Unlock()
			select {
			case <-changed:
			case <-req.Context().Done():
				return
			}
			f.mu.Lock()
		}
		body = f.allocs
		if path == "/v1/nodes" {
			body = f.nodes
		}
	case strings.HasPrefix(path, "/v1/job/") && req.Method == http.MethodDelete:
		f.stops = append(f.stops, strings.TrimPrefix(path, "/v1/job/"))
		if f.failStops > 0 {
			f.failStops--
			f.mu.Unlock()
			http.Error(w, "no leader", http.StatusInternalServerError)
			return
		}
		body = api.JobDeregisterResponse{}
	case strings.HasPrefix(path, "/v1/job/"):
		body = f.jobs[strings.TrimPrefix(path, "/v1/job/")]
	case strings.HasPrefix(path, "/v1/allocation/"):
		f.allocationReads++
		body = f.allocations[strings.TrimPrefix(path, "/v1/allocation/")]
	}
	h.Set("X-Nomad-Index", strconv.FormatUint(max(f.index[path], 1), 10))
	f.mu.Unlock()
	if b, _ := json.Marshal(body); body == nil || string(b) == "null" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(body)
}

// nextEvent gives the Runner's event number from, or fails after 5 s.
func nextEvent(t *testing.T, r *Runner, from int) dispatcher.RunnerEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	events, err := r.Events(ctx, from)
	if err != nil {
		t.Fatalf("no event %d: %v", from, err)
	}
	return events[0]
}

// eventually fails when ok is not true in 5 s.
func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal(what)
		}
	}
}

func identity(alloc, namespace string) dispatcher.Identity {
	claims := map[string]json.RawMessage{}
	for name, v := range map[string]string{"nomad_allocation_id": alloc, "nomad_namespace": namespace} {
		claims[name], _ = json.Marshal(v)
	}
	return dispatcher.Identity{Subject: "global:default:factory-execution-default:execution:wrapper:dispatcher", Claims: claims}
}

func TestWorkloadKnowsOnlyTheAllocationsOfTheRunnersJobsInItsNamespace(t *testing.T) {
	f, r := newFakeNomad(t)
	ours := "factory-execution-default/dispatch-1-abc"
	f.update("", func() {
		f.allocations["a-ours"] = &api.Allocation{ID: "a-ours", Namespace: "default", JobID: ours}
		f.allocations["a-other-job"] = &api.Allocation{ID: "a-other-job", Namespace: "default", JobID: "test-other-abc"}
		f.allocations["a-other-dispatch"] = &api.Allocation{ID: "a-other-dispatch", Namespace: "default", JobID: "other-default/dispatch-1-abc"}
		f.allocations["a-other-namespace"] = &api.Allocation{ID: "a-other-namespace", Namespace: "other", JobID: ours}
	})
	for _, alloc := range []string{"a-other-job", "a-other-dispatch", "a-other-namespace", "a-missing"} {
		if job, err := r.Workload(t.Context(), identity(alloc, "default")); !errors.Is(err, dispatcher.ErrUnknownWorkload) {
			t.Errorf("the JWT of allocation %s gives %q, %v; want ErrUnknownWorkload", alloc, job, err)
		}
	}
	// A JWT with no allocation, or of another namespace, needs no call.
	reads := f.allocationReads
	for _, id := range []dispatcher.Identity{identity("", "default"), identity("a-ours", "other"), identity("a-ours", "")} {
		if job, err := r.Workload(t.Context(), id); !errors.Is(err, dispatcher.ErrUnknownWorkload) {
			t.Errorf("the JWT with claims %s gives %q, %v; want ErrUnknownWorkload", id.Claims, job, err)
		}
	}
	if f.allocationReads != reads {
		t.Errorf("the Runner read %d allocations for JWTs that it refuses by their claims", f.allocationReads-reads)
	}

	if job, err := r.Workload(t.Context(), identity("a-ours", "default")); err != nil || job != ours {
		t.Fatalf("the JWT of the Runner's allocation gives %q, %v", job, err)
	}
	// The Runner keeps the allocations of its own jobs, and no other.
	r.mu.Lock()
	cached := len(r.workloads)
	r.mu.Unlock()
	if cached != 1 {
		t.Errorf("the Runner keeps %d allocations, want only its own", cached)
	}
	// From the Runner's memory, the namespace claim is still checked.
	if job, err := r.Workload(t.Context(), identity("a-ours", "other")); !errors.Is(err, dispatcher.ErrUnknownWorkload) {
		t.Errorf("a JWT of another namespace with a known allocation gives %q, %v", job, err)
	}
}

func TestADownNodeEndsItsExecutionsAsLostAndStopsTheirJobs(t *testing.T) {
	f, r := newFakeNomad(t)
	job := f.job("exec-1")
	f.update("/v1/allocations", func() {
		f.allocs = []*api.AllocationListStub{{ID: "a-1", Namespace: "default", JobID: job, NodeID: "n-1", ClientStatus: api.AllocClientStatusRunning}}
	})
	if ev := nextEvent(t, r, 0); ev.State != dispatcher.RunnerRunning {
		t.Fatalf("the first event is %+v, want running", ev)
	}
	// The node is down, and its allocation is "unknown": only the node
	// tells that the Execution is lost.
	f.update("/v1/nodes", func() { f.nodes = []*api.NodeListStub{{ID: "n-1", Status: api.NodeStatusDown}} })
	f.update("/v1/allocations", func() { f.allocs[0].ClientStatus = api.AllocClientStatusUnknown })
	if ev := nextEvent(t, r, 1); ev.State != dispatcher.RunnerLost || !ev.Lost || !ev.Ended {
		t.Fatalf("the event after the node went down is %+v, want lost", ev)
	}
	eventually(t, "the Runner did not stop the job of the lost Execution", func() bool { return len(f.stopped()) == 1 })
	if got := f.stopped()[0]; got != job {
		t.Errorf("the Runner stopped %q, want %q", got, job)
	}
}

func TestTheRunnerStopsALostExecutionsJobAgainWhenTheFirstStopFailed(t *testing.T) {
	f, r := newFakeNomad(t)
	job := f.job("exec-1")
	alloc := func(status string) func() {
		return func() {
			f.allocs = []*api.AllocationListStub{{ID: "a-1", Namespace: "default", JobID: job, NodeID: "n-1", ClientStatus: status}}
		}
	}
	f.update("", func() { f.failStops = 1 })
	f.update("/v1/allocations", alloc(api.AllocClientStatusRunning))
	nextEvent(t, r, 0)
	f.update("/v1/allocations", alloc(api.AllocClientStatusLost))
	if ev := nextEvent(t, r, 1); !ev.Lost {
		t.Fatalf("the event is %+v, want lost", ev)
	}
	eventually(t, "the Runner did not stop the job of the lost Execution", func() bool { return len(f.stopped()) == 1 })
	// The first stop failed. The machine comes back, and its task runs.
	f.update("/v1/allocations", alloc(api.AllocClientStatusRunning))
	eventually(t, "the Runner did not stop the job again when the lost allocation ran", func() bool { return len(f.stopped()) == 2 })
	if got := f.stopped(); got[1] != job {
		t.Errorf("the Runner stopped %v, want %s again", got, job)
	}
	// The Execution ended: no new event.
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	if events, err := r.Events(ctx, 2); err == nil {
		t.Errorf("an event after the end: %+v", events)
	}
}

func TestAnEndBeforeTheTaskStartedHasExitCodeMinusOne(t *testing.T) {
	f, r := newFakeNomad(t)
	ends := map[string]*api.AllocationListStub{
		// The task did not start: no state of the task.
		"exec-no-state": {ClientStatus: api.AllocClientStatusFailed},
		// The driver could not start it: a state with no Terminated event.
		"exec-no-start": {ClientStatus: api.AllocClientStatusFailed, TaskStates: map[string]*api.TaskState{
			TaskName: {Events: []*api.TaskEvent{{Type: api.TaskReceived}, {Type: api.TaskDriverFailure}}}}},
		"exec-exit-3": {ClientStatus: api.AllocClientStatusFailed, TaskStates: map[string]*api.TaskState{
			TaskName: {Events: []*api.TaskEvent{{Type: api.TaskStarted}, {Type: api.TaskTerminated, ExitCode: 3}, {Type: api.TaskNotRestarting}}}}},
	}
	want := map[string]int{"exec-no-state": -1, "exec-no-start": -1, "exec-exit-3": 3}
	var allocs []*api.AllocationListStub
	for execution, a := range ends {
		a.ID, a.Namespace, a.JobID, a.NodeID = "a-"+execution, "default", f.job(execution), "n-1"
		allocs = append(allocs, a)
	}
	f.update("/v1/allocations", func() { f.allocs = allocs })
	for i := range len(ends) {
		ev := nextEvent(t, r, i)
		if ev.State != dispatcher.RunnerFailed || !ev.Ended || ev.ExitCode != want[ev.Execution] {
			t.Errorf("the end of %s is %+v, want failed with exit code %d", ev.Execution, ev, want[ev.Execution])
		}
	}
}

func TestTheWatchStartsAgainFromZeroWhenNomadsIndexGoesBack(t *testing.T) {
	r := &Runner{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// A new leader gives index 3 after 10.
	answers := []uint64{10, 3, 4}
	var asked []uint64
	r.watch(ctx, "test", func(index uint64) (uint64, error) {
		asked = append(asked, index)
		if len(asked) > len(answers) {
			cancel()
			return 0, ctx.Err()
		}
		return answers[len(asked)-1], nil
	})
	if got, want := asked, []uint64{0, 10, 0, 4}; !slices.Equal(got, want) {
		t.Errorf("the watch asked from the indexes %v, want %v", got, want)
	}
}

// The wrapper's directory is under the image recipe's directory of the
// Executions, not the task's directory in Nomad's data_dir, which the run
// user cannot reach (the wrapper refuses it).
func TestTheJobGivesTheWrapperADirectoryOutsideNomadsDataDir(t *testing.T) {
	_, r := newFakeNomad(t)
	env := r.job(r.cfg.Kinds[0]).TaskGroups[0].Tasks[0].Env
	if got, want := env["WRAPPER_DIR"], DefaultExecutionsDir+"/${NOMAD_META_"+MetaExecution+"}"; got != want {
		t.Errorf("WRAPPER_DIR is %q, want %q", got, want)
	}
}
