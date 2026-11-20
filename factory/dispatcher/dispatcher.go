// Package dispatcher is the Dispatcher: it holds the queue of Runs in
// SQLite and serves the private API that the solidtrace CLI, Console and
// the wrappers call. It reaches SolidTrace only through public APIs.
package dispatcher

import (
	"cmp"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/linear"
)

// Options are the parts of the machine that tests replace.
type Options struct {
	// Now is the clock (time.Now when nil).
	Now func() time.Time
	// HTTP calls SolidTrace (a client with a 30 s timeout when nil).
	HTTP *http.Client
	// Log gets the Dispatcher's own log lines (slog.Default when nil).
	Log *slog.Logger
	// Version is the Dispatcher's release, such as v0.19.0 ("" or dev for
	// a build from a checkout). A release refuses a solidtrace CLI that
	// is older than SupportedMinorVersions minors.
	Version string
	// Runner starts the Executions of queued Runs. With none, a Run stays
	// queued (a Dispatcher with no Runner configured).
	Runner Runner
	// DispatchEvery is how often the dispatch loop looks for queued Runs
	// when nothing wakes it (dispatchEvery when 0).
	DispatchEvery time.Duration
	// EventRetry is the longest wait of the event sender between two
	// tries to send the outbox to SolidTrace (maxEventRetry when 0).
	EventRetry time.Duration
	// WebhookKinds are the kinds of connection that send webhooks, more
	// than the Dispatcher's own (Seam 3 adds its fake source).
	WebhookKinds []WebhookKind
	// Trackers are the kinds of tracker connection, more than the
	// Dispatcher's own (Seam 3 adds its fake tracker).
	Trackers []Tracker
	// ReconcileTick is how often the timer looks for a due backstop (1 s
	// when 0).
	ReconcileTick time.Duration
	// ScheduleEvery is the wait between two passes of the schedule loop
	// (scheduleEvery when 0). ScheduleChecked, when set, gets the clock
	// time of each pass after the pass.
	ScheduleEvery   time.Duration
	ScheduleChecked func(at time.Time)
	// BackstopEvery is how often the backstop looks for Executions past
	// their total limit plus 15 minutes (backstopEvery when 0).
	BackstopEvery time.Duration
	// CloseEvery is the wait between two passes of the close loop
	// (closeEvery when 0). CloseChecked, when set, gets the clock time of
	// each pass after the pass.
	CloseEvery   time.Duration
	CloseChecked func(at time.Time)
}

// Dispatcher is one open Dispatcher.
type Dispatcher struct {
	config  Config
	version string
	now     func() time.Time
	log     *slog.Logger
	store   *store
	tokens  *tokenCheck
	handler http.Handler
	// children makes and revokes the Executions' child tokens.
	children *childTokens

	// http and eventRetry are for the event sender.
	http       *http.Client
	eventRetry time.Duration

	scheduleEvery   time.Duration
	scheduleChecked func(time.Time)
	closeEvery      time.Duration
	closeChecked    func(time.Time)

	runner   Runner
	identity *identityCheck
	// allowlist writes Squid's allowlist.
	allowlist *allowlist
	// dispatching holds a channel for each Execution that the Runner is
	// starting; it closes when the Runner gave the workload.
	dispatching sync.Map
	// prLocks holds one pr call of a Run at a time.
	prLocks runLocks
	wake    chan struct{}

	// kinds are the kinds of connection that send webhooks, by name.
	kinds map[string]WebhookKind
	// trackers are the kinds of tracker connection, by name. Each is in
	// kinds too.
	trackers      map[string]Tracker
	webhookLimits *webhookLimits
	// setupLimits counts the setup requests (Notion's verification
	// request), which are not verified, apart from the verified webhooks.
	setupLimits *webhookLimits
	reconciles  *reconcileQueue
	// commandsAdded wakes the waiting commands calls.
	commandsAdded signal
	backstopEvery time.Duration
	// runnerStopped holds the Executions whose workload the backstop
	// stopped through the Runner, until the Runner's end event.
	runnerStopped sync.Map
	// closing closes when Serve or Close starts to stop.
	closing     chan struct{}
	closingOnce sync.Once
	// dispatchEvery is Options.DispatchEvery.
	dispatchEvery time.Duration
	stop          context.CancelFunc
	loops         sync.WaitGroup
}

// Open checks config and opens the database (it makes it at first use).
func Open(config Config, options Options) (*Dispatcher, error) {
	if err := config.Check(); err != nil {
		return nil, err
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.HTTP == nil {
		options.HTTP = &http.Client{Timeout: 30 * time.Second}
	}
	if options.Log == nil {
		options.Log = slog.Default()
	}
	if options.DispatchEvery <= 0 {
		options.DispatchEvery = dispatchEvery
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	key, err := config.encryptionKey()
	if err != nil {
		return nil, err
	}
	list, err := newAllowlist(config)
	if err != nil {
		return nil, err
	}
	s, err := openStore(config.Database, key)
	if err != nil {
		return nil, err
	}
	d := &Dispatcher{
		config:  config,
		version: options.Version,
		now:     options.Now,
		log:     options.Log,
		store:   s,
		tokens: &tokenCheck{url: config.SolidTraceURL, token: config.SolidTraceToken, http: options.HTTP,
			now: options.Now, kept: map[[sha256.Size]byte]keptAnswer{}},
		children:   &childTokens{url: config.SolidTraceURL, token: config.SolidTraceToken, http: options.HTTP},
		http:       options.HTTP,
		eventRetry: cmp.Or(options.EventRetry, maxEventRetry),
		runner:     options.Runner,
		allowlist:  list,
		wake:       make(chan struct{}, 1),

		kinds:           map[string]WebhookKind{},
		trackers:        map[string]Tracker{},
		webhookLimits:   &webhookLimits{windows: map[string]webhookWindow{}},
		setupLimits:     &webhookLimits{windows: map[string]webhookWindow{}},
		reconciles:      newReconcileQueue(),
		scheduleEvery:   cmp.Or(options.ScheduleEvery, scheduleEvery),
		scheduleChecked: options.ScheduleChecked,
		closeEvery:      cmp.Or(options.CloseEvery, closeEvery),
		closeChecked:    options.CloseChecked,
		closing:         make(chan struct{}),
		backstopEvery:   cmp.Or(options.BackstopEvery, backstopEvery),
		dispatchEvery:   options.DispatchEvery,
	}
	// The Bitbucket forge connections send the PR outcome webhooks.
	d.kinds[dispatcherapi.ForgeBitbucket] = bitbucketWebhooks{d}
	for _, kind := range options.WebhookKinds {
		d.kinds[kind.Kind()] = kind
	}
	// The Dispatcher's own trackers.
	trackers := []Tracker{newLinearTracker(d, linear.API{URL: config.LinearAPIURL, HTTP: options.HTTP}), newNotion(config.NotionAPIURL, options.HTTP, options.Now)}
	trackers = append(trackers, options.Trackers...)
	for _, t := range trackers {
		d.trackers[t.Kind()] = t
		d.kinds[t.Kind()] = trackerKind{t, d}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+dispatcherapi.Path+"/runs", d.person(d.startRun))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/runs", d.person(d.listRuns))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/runs/{id}", d.person(d.getRun))
	mux.HandleFunc("POST "+dispatcherapi.Path+"/runs/{id}/stop", d.person(d.stopRun))
	mux.HandleFunc("POST "+dispatcherapi.Path+"/runs/{id}/messages", d.person(d.sendMessage))
	mux.HandleFunc("POST "+dispatcherapi.Path+"/runs/{id}/take-over", d.person(d.takeOverRun))
	mux.HandleFunc("POST "+dispatcherapi.Path+"/runs/{id}/interrupt", d.person(d.interruptRun))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/projects", d.person(d.listSettings))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/projects/{project}/settings", d.person(d.getSettings))
	mux.HandleFunc("PUT "+dispatcherapi.Path+"/projects/{project}/settings", d.person(d.admin(d.saveSettings)))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/connections", d.person(d.listConnections))
	mux.HandleFunc("PUT "+dispatcherapi.Path+"/connections/{name}", d.person(d.admin(d.saveConnection)))
	mux.HandleFunc("PUT "+dispatcherapi.Path+"/connections/{name}/webhook-secret", d.person(d.admin(d.saveWebhookSecret)))
	mux.HandleFunc("PUT "+dispatcherapi.Path+"/connections/{name}/linear", d.person(d.admin(d.installLinear)))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/senders", d.person(d.listSenders))
	mux.HandleFunc("POST "+dispatcherapi.Path+"/senders", d.person(d.admin(d.createSender)))
	mux.HandleFunc("DELETE "+dispatcherapi.Path+"/senders/{id}", d.person(d.admin(d.deleteSender)))
	mux.HandleFunc("PUT "+dispatcherapi.Path+"/connections/{name}/tracker", d.person(d.admin(d.saveTrackerSettings)))
	mux.HandleFunc("POST /webhooks/", d.webhook)
	mux.HandleFunc("GET "+dispatcherapi.Path+"/schedules", d.person(d.listSchedules))
	mux.HandleFunc("POST "+dispatcherapi.Path+"/schedules", d.person(d.createSchedule))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/schedules/{id}", d.person(d.getSchedule))
	mux.HandleFunc("PATCH "+dispatcherapi.Path+"/schedules/{id}", d.person(d.updateSchedule))
	mux.HandleFunc("DELETE "+dispatcherapi.Path+"/schedules/{id}", d.person(d.deleteSchedule))
	mux.HandleFunc("POST "+dispatcherapi.Path+"/schedules/{id}/pause", d.person(d.pauseSchedule))
	mux.HandleFunc("POST "+dispatcherapi.Path+"/schedules/{id}/continue", d.person(d.continueSchedule))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/status", d.status)
	if d.runner != nil {
		d.identity = &identityCheck{jwksURL: d.runner.JWKSURL(), http: options.HTTP, now: options.Now}
		mux.HandleFunc("POST "+dispatcherapi.Path+"/executions/{id}/task", d.wrapper(d.task))
		mux.HandleFunc("POST "+dispatcherapi.Path+"/executions/{id}/report", d.wrapper(d.report))
		mux.HandleFunc("POST "+dispatcherapi.Path+"/executions/{id}/pr", d.wrapper(d.pr))
		mux.HandleFunc("POST "+dispatcherapi.Path+"/executions/{id}/token", d.wrapper(d.token))
		mux.HandleFunc("POST "+dispatcherapi.Path+"/executions/{id}/end", d.wrapper(d.end))
		mux.HandleFunc("POST "+dispatcherapi.Path+"/executions/{id}/commands", d.wrapper(d.commands))
		mux.HandleFunc("POST "+dispatcherapi.Path+"/executions/{id}/message", d.wrapper(d.messageEvent))
		mux.HandleFunc("POST "+dispatcherapi.Path+"/executions/{id}/compare-end", d.wrapper(d.compareEnd))
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "no such path: "+r.Method+" "+r.URL.Path)
	})
	d.handler = d.refuseOldCLIs(mux)
	// The default list or SolidTrace's hosts can have changed.
	d.refreshAllowlist()
	ctx, stop := context.WithCancel(context.Background())
	d.stop = stop
	d.loops.Add(6)
	go func() { defer d.loops.Done(); d.sendLoop(ctx) }()
	go func() { defer d.loops.Done(); d.writeBackLoop(ctx) }()
	go func() { defer d.loops.Done(); d.scheduleLoop(ctx) }()
	go func() { defer d.loops.Done(); d.closeLoop(ctx) }()
	go func() { defer d.loops.Done(); d.reconcileLoop(ctx) }()
	go func() {
		defer d.loops.Done()
		d.backstopLoop(ctx, cmp.Or(options.ReconcileTick, defaultReconcileTick))
	}()
	if d.runner != nil {
		d.loops.Add(3)
		go func() { defer d.loops.Done(); d.dispatchLoop(ctx) }()
		go func() { defer d.loops.Done(); d.watchLoop(ctx) }()
		go func() { defer d.loops.Done(); d.executionBackstopLoop(ctx) }()
	}
	return d, nil
}

// Handler serves the private listener.
func (d *Dispatcher) Handler() http.Handler { return d.handler }

// shutdownWait is the longest time that Serve waits for the open calls
// when it stops.
const shutdownWait = 10 * time.Second

// Serve serves the private listener until ctx ends, then stops. Each
// commands call (a long poll) then gets 503 at once, and Serve waits up
// to shutdownWait for the other calls.
func (d *Dispatcher) Serve(ctx context.Context, listener net.Listener) error {
	server := &http.Server{Handler: d.handler, ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()
	select {
	case err := <-served:
		return err
	case <-ctx.Done():
	}
	d.drain()
	shutdown, cancel := context.WithTimeout(context.Background(), shutdownWait)
	defer cancel()
	return server.Shutdown(shutdown)
}

// drain ends each waiting commands call, once.
func (d *Dispatcher) drain() { d.closingOnce.Do(func() { close(d.closing) }) }

// Close stops the loops and closes the database.
func (d *Dispatcher) Close() error {
	d.drain()
	d.stop()
	d.loops.Wait()
	return d.store.close()
}

// listLimit is the most Runs that a list gives.
const listLimit = 100

// maxBody is the largest request body that the API reads.
const maxBody = 1 << 20

type personHandler func(w http.ResponseWriter, r *http.Request, p person)

// person checks the request's token and gives the handler its person.
// The token needs the factory scope, and its one Organization must be
// the Organization that the Dispatcher serves; each member of that
// Organization can use the handler.
func (d *Dispatcher) person(next personHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeProblem(w, http.StatusUnauthorized, "no token: sign in with 'solidtrace login'")
			return
		}
		p, err := d.tokens.check(r.Context(), strings.TrimSpace(token))
		var refused *refusal
		if errors.As(err, &refused) {
			switch {
			case refused.status == http.StatusUnauthorized:
				w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
			case refused.scope:
				w.Header().Set("WWW-Authenticate", `Bearer error="insufficient_scope", scope="`+FactoryScope+`"`)
			}
			writeProblem(w, refused.status, refused.message)
			return
		}
		if errors.Is(err, errCallerRefused) {
			d.log.Error("SolidTrace refuses the Dispatcher's own token")
			writeProblem(w, http.StatusServiceUnavailable, callerRefused)
			return
		}
		if err == nil {
			var served organization
			served, err = d.tokens.organization(r.Context())
			switch {
			case errors.Is(err, errOwnOrganization):
				d.log.Error(err.Error())
				writeProblem(w, http.StatusServiceUnavailable, err.Error())
				return
			case errors.Is(err, errCallerRefused):
				d.log.Error("SolidTrace refuses the Dispatcher's own token")
				writeProblem(w, http.StatusServiceUnavailable, callerRefused)
				return
			case err == nil && served.id != p.organization.id:
				writeProblem(w, http.StatusForbidden, "this Dispatcher serves the Organization "+served.slug+
					", not "+p.organization.slug+": the Dispatcher's SolidTrace token belongs to "+served.slug)
				return
			}
		}
		if err != nil {
			d.log.Warn("token check failed", "error", err)
			writeProblem(w, http.StatusBadGateway, "the Dispatcher could not check the token at SolidTrace")
			return
		}
		next(w, r, p)
	}
}

// admin lets only an Admin of the token's Organization use next. Each
// Admin action is under person, then admin.
func (d *Dispatcher) admin(next personHandler) personHandler {
	return func(w http.ResponseWriter, r *http.Request, p person) {
		if !p.admin() {
			writeProblem(w, http.StatusForbidden, "only an Admin of the Organization "+p.organization.slug+" can do this")
			return
		}
		next(w, r, p)
	}
}

// status says whether SolidTrace accepts the Dispatcher's own token. It
// needs no person's token: when SolidTrace refuses the Dispatcher's
// token, the Dispatcher cannot check a person's token. It is on the
// private listener only, and it gives no person's data. It keeps its
// answer for ownKeepFor, so it costs at most one introspection in that
// time.
func (d *Dispatcher) status(w http.ResponseWriter, r *http.Request) {
	status := dispatcherapi.Status{
		Version:    d.version,
		SolidTrace: dispatcherapi.SolidTraceStatus{URL: d.config.SolidTraceURL, TokenActive: true},
	}
	code := http.StatusOK
	if err := d.tokens.ownToken(r.Context()); err != nil {
		status.SolidTrace.TokenActive = false
		status.SolidTrace.Error = err.Error()
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, status)
}

func (d *Dispatcher) startRun(w http.ResponseWriter, r *http.Request, p person) {
	var start dispatcherapi.StartRun
	if !decode(w, r, &start, "the body must be a JSON object with project and prompt") {
		return
	}
	start.Project = strings.TrimSpace(start.Project)
	if start.Project == "" || strings.TrimSpace(start.Prompt) == "" {
		writeProblem(w, http.StatusBadRequest, "a Run needs a project and a prompt")
		return
	}
	branch, ok := cleanBranch(start.Branch)
	if !ok {
		writeProblem(w, http.StatusBadRequest, "the branch must be a branch name, such as main or release/2.0, not "+quote(start.Branch))
		return
	}
	settings, ok := d.triggerSettings(w, r, p.starter(), start.Project)
	if !ok {
		return
	}
	if branch == "" {
		branch = settings.BaseBranch
	}
	run := storedRun{
		Run: dispatcherapi.Run{
			ID:           newRunID(),
			State:        dispatcherapi.StateQueued,
			Project:      settings.Project,
			Prompt:       start.Prompt,
			Organization: p.organization.slug,
			StartedBy:    dispatcherapi.Person{ID: p.id, Name: p.name},
			CreatedAt:    d.now().UTC().Truncate(time.Microsecond),
		},
		organizationID: p.organization.id,
		projectID:      settings.projectID,
		repository:     settings.Repository,
		folder:         settings.Folder,
		baseBranch:     branch,
		trigger:        triggerPerson,
	}
	if err := d.store.insertRun(run); err != nil {
		d.serverError(w, "start a Run", err)
		return
	}
	d.wakeDispatch()
	writeJSON(w, http.StatusCreated, d.withURL(run.Run, p))
}

func (d *Dispatcher) getRun(w http.ResponseWriter, r *http.Request, p person) {
	run, err := d.store.run(p.organization.id, r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusNotFound, "no Run "+r.PathValue("id")+" in this Organization")
		return
	}
	if err == nil {
		run.Executions, err = d.store.executionsOf(run.ID)
	}
	if err != nil {
		d.serverError(w, "read a Run", err)
		return
	}
	writeJSON(w, http.StatusOK, d.withURL(run, p))
}

func (d *Dispatcher) listRuns(w http.ResponseWriter, r *http.Request, p person) {
	runs, err := d.store.runs(p.organization.id, listLimit)
	if err != nil {
		d.serverError(w, "list Runs", err)
		return
	}
	for i := range runs {
		runs[i] = d.withURL(runs[i], p)
	}
	writeJSON(w, http.StatusOK, dispatcherapi.Runs{Runs: runs})
}

// withURL sets the Organization and the link to the Run's page in Console
// from the slug in the current token check, not the stored slug: an Admin
// can change the slug, and Console routes are under the current slug. The
// Run is in p's Organization, because the store reads by Organization id.
func (d *Dispatcher) withURL(run dispatcherapi.Run, p person) dispatcherapi.Run {
	run.Organization = p.organization.slug
	run.URL = d.config.SolidTraceURL + "/" + run.Organization + "/runs/" + run.ID
	return run
}

func (d *Dispatcher) serverError(w http.ResponseWriter, what string, err error) {
	d.log.Error("could not "+what, "error", err)
	writeProblem(w, http.StatusInternalServerError, "the Dispatcher could not "+what)
}

// newRunID is a random id of 16 lower-case letters and digits. It is part
// of the branch name agent/<run id>.
func newRunID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return strings.ToLower(base32.StdEncoding.EncodeToString(b))
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, dispatcherapi.Problem{Error: message})
}
