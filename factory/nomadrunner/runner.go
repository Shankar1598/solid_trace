// Package nomadrunner is the Runner of the MVP: Nomad. Each machine kind
// has a parameterized batch job, in the node pool of the kind's name. An
// Execution is one dispatch of that job, with the Run id and the first
// Execution's id in the meta. The job reserves all the memory of the
// machine, so one Run works on each machine. Nomad never restarts or
// reschedules the task.
package nomadrunner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/nomad/api"
	"github.com/solidtrace/factory/dispatcher"
)

// Config configures the Runner.
type Config struct {
	// Address is the Nomad server's HTTP API, and Token the Dispatcher's
	// ACL token. Its policy needs, in the namespace, the capabilities
	// submit-job, dispatch-job, read-job and list-jobs, and node read.
	Address, Token string
	// Namespace of the jobs ("" is "default").
	Namespace string
	// JobPrefix names the parameterized jobs: <JobPrefix>-<kind> (default
	// "factory-execution").
	JobPrefix string
	// Kinds are the machine kinds; DefaultKind names the one for an
	// Execution with no kind ("" is the first).
	Kinds       []dispatcher.MachineKind
	DefaultKind string
	// Command and Args are the task's program on the agent machine: the
	// wrapper.
	Command string
	Args    []string
	// WrapperURL is the Dispatcher's private listener as the agent machine
	// reaches it.
	WrapperURL string
	// IdentityTTL is the life of a workload identity JWT; Nomad renews it
	// in the file before it ends (default 1 hour).
	IdentityTTL time.Duration
	// Log gets the Runner's warnings (default: none).
	Log *slog.Logger
}

// FromConfig is the Runner's configuration from the operator
// configuration.
func FromConfig(c dispatcher.Config, log *slog.Logger) Config {
	return Config{
		Address:     c.NomadAddress,
		Token:       c.NomadToken,
		Namespace:   c.NomadNamespace,
		Kinds:       c.MachineKinds,
		DefaultKind: c.DefaultMachineKind,
		Command:     c.WrapperCommand,
		WrapperURL:  c.WrapperURL,
		Log:         log,
	}
}

// The names in the job template that the wrapper and the Runner share.
const (
	// GroupName and TaskName are the job's one group and one task.
	GroupName = "execution"
	TaskName  = "wrapper"
	// IdentityName is the task's workload identity for the Dispatcher.
	// Nomad writes its JWT to <secrets>/nomad_<IdentityName>.jwt.
	IdentityName = "dispatcher"
	// MetaRun and MetaExecution are the dispatch meta.
	MetaRun       = "run"
	MetaExecution = "execution"
	// KillTimeout is the time from SIGTERM to SIGKILL when the task
	// stops: the wrapper's end procedure and preemption handler need it.
	// Each client's max_kill_timeout must be at least this.
	KillTimeout = 120 * time.Second
)

var jobPrefix = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// Runner starts Executions as dispatches of Nomad parameterized jobs.
type Runner struct {
	cfg    Config
	client *api.Client
	log    *slog.Logger
	stop   context.CancelFunc
	done   sync.WaitGroup

	mu         sync.Mutex
	changed    *sync.Cond
	events     []dispatcher.RunnerEvent
	registered map[string]bool
	// executions maps each dispatched job (a workload) to its Execution.
	executions map[string]string
	// tracked is what the Runner knows of each Execution's allocation.
	tracked map[string]*tracked
	// workloads maps an allocation to its dispatched job.
	workloads map[string]string
}

type tracked struct {
	state string
	node  string
	ended bool
}

// New makes the Runner and starts its watch of Nomad's allocations and
// nodes. Close stops it.
func New(cfg Config) (*Runner, error) {
	if cfg.JobPrefix == "" {
		cfg.JobPrefix = "factory-execution"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	if cfg.IdentityTTL == 0 {
		cfg.IdentityTTL = time.Hour
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	switch {
	case !jobPrefix.MatchString(cfg.JobPrefix):
		return nil, fmt.Errorf("nomadrunner: the job prefix %q is not lower-case letters, digits and -", cfg.JobPrefix)
	case len(cfg.Kinds) == 0:
		return nil, errors.New("nomadrunner: no machine kind")
	case cfg.Command == "":
		return nil, errors.New("nomadrunner: no wrapper command")
	}
	apiConfig := api.DefaultConfig()
	apiConfig.Address = cfg.Address
	apiConfig.SecretID = cfg.Token
	apiConfig.Namespace = cfg.Namespace
	client, err := api.NewClient(apiConfig)
	if err != nil {
		return nil, fmt.Errorf("nomadrunner: %w", err)
	}
	r := &Runner{cfg: cfg, client: client, log: cfg.Log,
		registered: map[string]bool{}, executions: map[string]string{},
		tracked: map[string]*tracked{}, workloads: map[string]string{}}
	if _, ok := r.kind(cfg.DefaultKind); !ok {
		return nil, fmt.Errorf("nomadrunner: the default kind %q is not a machine kind", cfg.DefaultKind)
	}
	r.changed = sync.NewCond(&r.mu)
	ctx, stop := context.WithCancel(context.Background())
	r.stop = stop
	r.done.Add(2)
	go func() { defer r.done.Done(); r.watchAllocations(ctx) }()
	go func() { defer r.done.Done(); r.watchNodes(ctx) }()
	return r, nil
}

// Close stops the watch.
func (r *Runner) Close() {
	r.stop()
	r.mu.Lock()
	r.changed.Broadcast()
	r.mu.Unlock()
	r.done.Wait()
}

// JWKSURL is Nomad's key set for workload identities.
func (r *Runner) JWKSURL() string {
	return strings.TrimRight(r.cfg.Address, "/") + "/.well-known/jwks.json"
}

// JobID is the parameterized job of a machine kind.
func (r *Runner) JobID(kind string) string { return r.cfg.JobPrefix + "-" + kind }

func (r *Runner) kind(name string) (dispatcher.MachineKind, bool) {
	if name == "" {
		name = r.cfg.DefaultKind
	}
	for _, k := range r.cfg.Kinds {
		if name == "" || k.Name == name {
			return k, true
		}
	}
	return dispatcher.MachineKind{}, false
}

// ours tells if a job is a dispatch of one of the Runner's jobs.
func (r *Runner) ours(jobID string) bool {
	parent, _, ok := strings.Cut(jobID, "/dispatch-")
	if !ok {
		return false
	}
	for _, k := range r.cfg.Kinds {
		if parent == r.JobID(k.Name) {
			return true
		}
	}
	return false
}

// Dispatch dispatches the job of the Execution's machine kind, and gives
// the dispatched job as the workload.
func (r *Runner) Dispatch(ctx context.Context, e dispatcher.RunnerExecution) (string, error) {
	kind, ok := r.kind(e.MachineKind)
	if !ok {
		names := make([]string, len(r.cfg.Kinds))
		for i, k := range r.cfg.Kinds {
			names[i] = k.Name
		}
		return "", fmt.Errorf("the machine kind %q is not one of the operator's kinds (%s)", e.MachineKind, strings.Join(names, ", "))
	}
	if err := r.register(ctx, kind); err != nil {
		return "", err
	}
	resp, _, err := r.client.Jobs().DispatchOpts(&api.DispatchOptions{
		JobID: r.JobID(kind.Name),
		Meta:  map[string]string{MetaRun: e.Run, MetaExecution: e.Execution},
	}, (&api.WriteOptions{}).WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("dispatch the Nomad job %s: %w", r.JobID(kind.Name), err)
	}
	r.mu.Lock()
	r.executions[resp.DispatchedJobID] = e.Execution
	r.mu.Unlock()
	return resp.DispatchedJobID, nil
}

// register registers the kind's job, one time for each Runner, so that a
// change of the template or of the kind applies after a restart.
func (r *Runner) register(ctx context.Context, kind dispatcher.MachineKind) error {
	r.mu.Lock()
	done := r.registered[kind.Name]
	r.mu.Unlock()
	if done {
		return nil
	}
	if _, _, err := r.client.Jobs().Register(r.job(kind), (&api.WriteOptions{}).WithContext(ctx)); err != nil {
		return fmt.Errorf("register the Nomad job %s: %w", r.JobID(kind.Name), err)
	}
	r.mu.Lock()
	r.registered[kind.Name] = true
	r.mu.Unlock()
	return nil
}

// job is the job template of a machine kind.
func (r *Runner) job(kind dispatcher.MachineKind) *api.Job {
	id := r.JobID(kind.Name)
	job := api.NewBatchJob(id, id, "", api.JobDefaultPriority)
	job.Namespace = &r.cfg.Namespace
	job.NodePool = &kind.Name
	job.Datacenters = []string{"*"}
	job.ParameterizedJob = &api.ParameterizedJobConfig{
		Payload:      "forbidden",
		MetaRequired: []string{MetaRun, MetaExecution},
	}
	zero, no, mode := 0, false, "fail"
	killTimeout := KillTimeout
	task := api.NewTask(TaskName, "raw_exec")
	task.Config = map[string]any{"command": r.cfg.Command, "args": r.cfg.Args}
	task.Env = map[string]string{
		"WRAPPER_DISPATCHER_URL": r.cfg.WrapperURL,
		"WRAPPER_EXECUTION_ID":   "${NOMAD_META_" + MetaExecution + "}",
		"WRAPPER_IDENTITY_FILE":  "${NOMAD_SECRETS_DIR}/nomad_" + IdentityName + ".jwt",
	}
	task.KillTimeout = &killTimeout
	task.KillSignal = "SIGTERM"
	task.Resources = &api.Resources{MemoryMB: &kind.MemoryMB}
	task.Identities = []*api.WorkloadIdentity{{
		Name:     IdentityName,
		Audience: []string{dispatcher.IdentityAudience},
		File:     true,
		TTL:      r.cfg.IdentityTTL,
	}}
	group := api.NewTaskGroup(GroupName, 1)
	group.RestartPolicy = &api.RestartPolicy{Attempts: &zero, Mode: &mode}
	group.ReschedulePolicy = &api.ReschedulePolicy{Attempts: &zero, Unlimited: &no}
	// A lost machine's allocation is not replaced: the Dispatcher
	// restores the Run in a new Execution.
	group.Disconnect = &api.DisconnectStrategy{Replace: &no}
	group.AddTask(task)
	job.AddTaskGroup(group)
	return job
}

// Workload gives the dispatched job of the allocation that holds the JWT.
// The subject does not tell it: each dispatch of a job has the same
// subject. The allocation id claim does, and Nomad says which job the
// allocation is of.
func (r *Runner) Workload(ctx context.Context, identity dispatcher.Identity) (string, error) {
	alloc := identity.StringClaim("nomad_allocation_id")
	if alloc == "" || identity.StringClaim("nomad_namespace") != r.cfg.Namespace {
		return "", dispatcher.ErrUnknownWorkload
	}
	r.mu.Lock()
	job, ok := r.workloads[alloc]
	r.mu.Unlock()
	if ok {
		return job, nil
	}
	a, _, err := r.client.Allocations().Info(alloc, (&api.QueryOptions{}).WithContext(ctx))
	if notFound(err) {
		return "", dispatcher.ErrUnknownWorkload
	}
	if err != nil {
		return "", fmt.Errorf("read the Nomad allocation %s: %w", alloc, err)
	}
	if a.Namespace != r.cfg.Namespace || !r.ours(a.JobID) {
		return "", dispatcher.ErrUnknownWorkload
	}
	r.mu.Lock()
	r.workloads[alloc] = a.JobID
	r.mu.Unlock()
	return a.JobID, nil
}

func notFound(err error) bool {
	var unexpected api.UnexpectedResponseError
	return errors.As(err, &unexpected) && unexpected.StatusCode() == http.StatusNotFound
}

// Stop stops the dispatched job. Nomad sends the wrapper SIGTERM, and
// kills the task after KillTimeout. The end comes as an event.
func (r *Runner) Stop(ctx context.Context, workload string) error {
	if !r.ours(workload) {
		return fmt.Errorf("nomadrunner: %q is not a workload of the Runner", workload)
	}
	if _, _, err := r.client.Jobs().Deregister(workload, false, (&api.WriteOptions{}).WithContext(ctx)); err != nil {
		return fmt.Errorf("stop the Nomad job %s: %w", workload, err)
	}
	return nil
}

// Events gives the events from number from on, and waits until there is
// at least one or ctx ends.
func (r *Runner) Events(ctx context.Context, from int) ([]dispatcher.RunnerEvent, error) {
	stop := context.AfterFunc(ctx, func() {
		r.mu.Lock()
		r.changed.Broadcast()
		r.mu.Unlock()
	})
	defer stop()
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.events) <= from {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r.changed.Wait()
	}
	return append([]dispatcher.RunnerEvent{}, r.events[from:]...), nil
}
