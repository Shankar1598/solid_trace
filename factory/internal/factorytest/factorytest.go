// Package factorytest is Seam 3, the factory test environment: the real
// Dispatcher with real SQLite in a temp dir, the SolidTrace recorder, a
// controllable clock, and people who use the real solidtrace CLI. Tests
// drive it from the API and the CLI, and check what a person can see.
package factorytest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/solidtrace/cli/command"
	"github.com/solidtrace/cli/credentials"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/fakeforge"
	"github.com/solidtrace/factory/internal/fakesource"
	"github.com/solidtrace/factory/internal/faketracker"
	"github.com/solidtrace/factory/internal/recorder"
)

// DispatcherToken is the Dispatcher's own API token at the recorder.
const DispatcherToken = "stk_dispatcher"

// Clock is a clock that moves only when a test moves it. It is the
// Dispatcher's clock, the people's, and (with Executions) the clock of
// the wrappers' limits on the agent machines.
type Clock struct {
	mu  sync.Mutex
	now time.Time
	// moving keeps one Advance at a time, so the machines see each time
	// in order.
	moving sync.Mutex
	// machines is the file that the wrappers read their clock from (""
	// with no Executions). held stops it at its time.
	machines string
	held     bool
	// beforeMove runs at each Advance before the clock moves to the new
	// time: the Runner renews the workload identities, as Nomad does
	// before they expire.
	beforeMove func(next time.Time)
}

// Now is the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock by d, on the agent machines too.
func (c *Clock) Advance(d time.Duration) {
	c.moving.Lock()
	defer c.moving.Unlock()
	c.mu.Lock()
	next, before := c.now.Add(d), c.beforeMove
	c.mu.Unlock()
	if before != nil {
		before(next)
	}
	c.mu.Lock()
	c.now = next
	now, machines, held := c.now, c.machines, c.held
	c.mu.Unlock()
	if machines != "" && !held {
		writeClock(machines, now)
	}
}

// writeClock writes now to the machines' clock file in one step.
func writeClock(path string, now time.Time) {
	if err := os.WriteFile(path+".new", []byte(now.Format(time.RFC3339Nano)), 0o600); err != nil {
		panic("factorytest: write the machines' clock: " + err.Error())
	}
	if err := os.Rename(path+".new", path); err != nil {
		panic("factorytest: write the machines' clock: " + err.Error())
	}
}

// Env is one Dispatcher and one SolidTrace.
type Env struct {
	t          testing.TB
	SolidTrace *recorder.Recorder
	// Forge is the fake Bitbucket Cloud of the Dispatcher's
	// configuration. It has no repositories: a test adds them, such as
	// example/<slug> for the settings that Project saves (Project adds it
	// with the Executions option).
	Forge  *fakeforge.Forge
	Clock  *Clock
	Config dispatcher.Config
	// Version is the Dispatcher's version ("" is a build from a
	// checkout). A change applies at the next Restart.
	Version string
	// Runner is the fake Runner, with the Executions option (else nil).
	Runner *Runner
	// Proxy is the agent machines' proxy, with the ThroughProxy option
	// (else nil: the wrappers reach SolidTrace directly).
	Proxy *Proxy
	// Source is the fake webhook source: the API of each connection of
	// kind fakesource.Kind or fakesource.OtherKind.
	Source *fakesource.Source
	// Tracker is the fake tracker: the API of each connection of kind
	// faketracker.Kind.
	Tracker *faketracker.Tracker
	// runner is a Runner that is not the fake (WithRunner).
	runner dispatcher.Runner

	current atomic.Pointer[dispatcher.Dispatcher]
	server  *httptest.Server
	// webhooks is the Dispatcher's webhook listener.
	webhooks *httptest.Server
	people   int
	logs     lockedBuffer
	// projects counts the Projects that Project made, for their ids.
	projects int64
	admin    *Person
	// checked is the clock time of the schedule loop's last pass.
	checked atomic.Pointer[time.Time]
	// closeChecked is the clock time of the close loop's last pass.
	closeChecked atomic.Pointer[time.Time]
	// squid is the folder of the Squid allowlist and its reloads.
	squid string
}

// MachineKinds are the operator's machine kinds in the environment's
// configuration; the first is the default.
var MachineKinds = []dispatcher.MachineKind{{Name: "standard", MemoryMB: 12288}, {Name: "large", MemoryMB: 28672}}

// DefaultAllowlist is the operator's default allowlist in the
// environment's configuration.
const DefaultAllowlist = "# The default list.\napi.anthropic.com\n.bitbucket.org\n\nproxy.golang.org\n"

// Allowlist is the Squid allowlist that the Dispatcher wrote ("" is
// none), with no comment lines.
func (e *Env) Allowlist() string {
	data, _ := os.ReadFile(e.Config.SquidAllowlist)
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

// SquidReloads counts the Dispatcher's runs of the reload command.
func (e *Env) SquidReloads() int {
	data, _ := os.ReadFile(filepath.Join(e.squid, "reloads"))
	return strings.Count(string(data), "reload\n")
}

// scheduleEvery is the wait between two passes of the Dispatcher's
// schedule loop in a test, so that a pass soon follows a move of the
// clock.
const scheduleEvery = 5 * time.Millisecond

// CheckSchedules waits up to 10 s until the Dispatcher's schedule loop
// did a pass at the clock's time now or later. So each fire time that is
// due now has its Run, or its skip, after it.
func (e *Env) CheckSchedules() {
	e.t.Helper()
	now := e.Clock.Now()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if at := e.checked.Load(); at != nil && !at.Before(now) {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("the schedule loop did no pass at %s after 10 s\nDispatcher log:\n%s", now, e.Logs())
		}
		time.Sleep(time.Millisecond)
	}
}

// CheckClosedRuns waits up to 10 s until the Dispatcher's close loop did
// a pass at the clock's time now or later. So each Run that is due to
// close now is closed after it, and when 5 minutes of the clock passed
// since the last PR check, the PRs were checked.
func (e *Env) CheckClosedRuns() {
	e.t.Helper()
	now := e.Clock.Now()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if at := e.closeChecked.Load(); at != nil && !at.Before(now) {
			return
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("the close loop did no pass at %s after 10 s\nDispatcher log:\n%s", now, e.Logs())
		}
		time.Sleep(time.Millisecond)
	}
}

// RestartAfter stops the Dispatcher, moves the clock by d while it is
// down, and starts it again on the same database.
func (e *Env) RestartAfter(d time.Duration) {
	e.t.Helper()
	if err := e.current.Load().Close(); err != nil {
		e.t.Fatalf("stop the Dispatcher: %v", err)
	}
	e.Clock.Advance(d)
	e.start()
}

// EncryptionKey is the Dispatcher's encryption key in each Env.
const EncryptionKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Logs are the Dispatcher's own log lines so far (the warnings among
// them), one text line each.
func (e *Env) Logs() string { return e.logs.String() }

// eventRetry is the longest wait of the Dispatcher's event sender between
// two tries, so a test sees the events soon after EventStore is back.
const eventRetry = 50 * time.Millisecond

// backstopEvery is how often the Dispatcher's backstop looks for
// Executions past their total limit, so that a test sees its stop soon
// after it moves the clock.
const backstopEvery = 20 * time.Millisecond

// RunEvents are the run events of the Run that SolidTrace has, by run.seq,
// each run.seq one time (a reader drops repeats).
func (e *Env) RunEvents(runID string) []map[string]any {
	bySeq := map[float64]map[string]any{}
	var seqs []float64
	for _, ev := range e.SolidTrace.RunEvents() {
		seq, _ := ev["run.seq"].(float64)
		if ev["run.id"] != runID || bySeq[seq] != nil {
			continue
		}
		bySeq[seq] = ev
		seqs = append(seqs, seq)
	}
	slices.Sort(seqs)
	events := make([]map[string]any, len(seqs))
	for i, seq := range seqs {
		events[i] = bySeq[seq]
	}
	return events
}

// ArrivalSeqs are the run.seq values of the Run's events in the order
// that they first arrived at SolidTrace. A repeat that arrives again later
// is not in the list. A sender that sends in order gives 1, 2, ..., n.
func (e *Env) ArrivalSeqs(runID string) []int {
	seen := map[float64]bool{}
	var seqs []int
	for _, ev := range e.SolidTrace.RunEvents() {
		seq, _ := ev["run.seq"].(float64)
		if ev["run.id"] != runID || seen[seq] {
			continue
		}
		seen[seq] = true
		seqs = append(seqs, int(seq))
	}
	return seqs
}

// WaitForRunEvents waits up to 10 s until SolidTrace has at least n run
// events of the Run, and gives them (see RunEvents).
func (e *Env) WaitForRunEvents(runID string, n int) []map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		events := e.RunEvents(runID)
		if len(events) >= n {
			return events
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("SolidTrace has %d run events of %s, not %d, after 10 s: %v\nDispatcher log:\n%s",
				len(events), runID, n, events, e.Logs())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// WaitForWarning waits up to 10 s for a warning Log of the Dispatcher (a
// Log with no run.id) whose message has text, and gives it.
func (e *Env) WaitForWarning(text string) map[string]any {
	e.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		for _, ev := range e.SolidTrace.RunEvents() {
			if message, _ := ev["message"].(string); ev["run.id"] == nil && strings.Contains(message, text) {
				return ev
			}
		}
		if time.Now().After(deadline) {
			e.t.Fatalf("SolidTrace has no warning with %q after 10 s: %v", text, e.SolidTrace.RunEvents())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Project makes a Project with slug in Organization at SolidTrace, and
// saves its Factory settings as an Admin: repository
// bitbucket.org/example/<slug> at the root. With Executions, the fake
// Forge has the repository example/<slug> (a README on main), and the
// settings name the connection ForgeConnection with the Forge's token. It
// returns the Project.
func (e *Env) Project(slug string) recorder.Project {
	e.t.Helper()
	project := e.SolidTraceProject(slug)
	e.Admin()
	settings := map[string]string{"repository": "bitbucket.org/example/" + slug}
	if e.Runner != nil || e.runner != nil {
		e.Forge.AddRepo("example/"+slug, nil)
		a := e.admin.API("PUT", "/api/v1/connections/"+ForgeConnection,
			map[string]string{"kind": "bitbucket", "token": e.Forge.Token})
		if a.Status/100 != 2 {
			e.t.Fatalf("save the forge connection: %d %s", a.Status, a.Body)
		}
		settings["connection"] = ForgeConnection
	}
	a := e.admin.API("PUT", "/api/v1/projects/"+slug+"/settings", settings)
	if a.Status != http.StatusOK {
		e.t.Fatalf("save the Factory settings of %s: %d %s", slug, a.Status, a.Body)
	}
	return project
}

// Admin is the Admin of Organization that saves the Factory settings in
// Project.
func (e *Env) Admin() *Person {
	if e.admin == nil {
		e.admin = e.PersonIn("factorytest-admin", Organization, "admin")
	}
	return e.admin
}

// SolidTraceProject makes a Project with slug in Organization at
// SolidTrace only, with no Factory settings.
func (e *Env) SolidTraceProject(slug string) recorder.Project {
	e.projects++
	project := recorder.Project{ID: 100 + e.projects, Slug: slug, Name: slug}
	e.SolidTrace.SetProject(Organization.Slug, project)
	return project
}

// New starts the environment with the options. The test's cleanup stops
// it.
func New(t testing.TB, options ...Option) *Env {
	t.Helper()
	e := &Env{
		t:          t,
		SolidTrace: recorder.New(DispatcherToken),
		Forge:      fakeforge.New(t),
		Source:     fakesource.New(),
		Tracker:    faketracker.New(),
		Clock:      &Clock{now: time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)},
	}
	t.Cleanup(e.SolidTrace.Close)
	// The Dispatcher's own token: an Admin made it. Revoke it for an Admin
	// who left the Organization.
	e.SolidTrace.Token(DispatcherToken, recorder.Answer{
		Sub:           "1",
		Username:      "dana@example.com",
		Scope:         "run_events:write issues:comment tokens:create mcp:read",
		Organizations: []recorder.Organization{{ID: Organization.ID, Slug: Organization.Slug, Name: Organization.Name, Role: "admin"}},
	})
	squid := t.TempDir()
	if err := os.WriteFile(filepath.Join(squid, "default-allowlist.txt"), []byte(DefaultAllowlist), 0o644); err != nil {
		t.Fatal(err)
	}
	e.squid = squid
	e.Config = dispatcher.Config{
		SolidTraceURL:   e.SolidTrace.URL(),
		SolidTraceToken: DispatcherToken,
		Database:        filepath.Join(t.TempDir(), "dispatcher.sqlite3"),
		EncryptionKey:   EncryptionKey,
		BitbucketAPIURL: e.Forge.APIURL(),
		BitbucketGitURL: e.Forge.GitURL(),
		WebhookURL:      PublicWebhookURL,
		MachineKinds:    MachineKinds,
		// Squid's place: the reload command counts the reloads.
		SquidAllowlist:   filepath.Join(squid, "allowlist.txt"),
		DefaultAllowlist: filepath.Join(squid, "default-allowlist.txt"),
		SquidReload:      "echo reload >> " + filepath.Join(squid, "reloads"),
	}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.current.Load().Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(e.server.Close)
	e.webhooks = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.current.Load().WebhookHandler().ServeHTTP(w, r)
	}))
	t.Cleanup(e.webhooks.Close)
	for _, option := range options {
		option(e)
	}
	e.start()
	t.Cleanup(func() { e.current.Load().Close() })
	return e
}

func (e *Env) start() {
	e.t.Helper()
	options := dispatcher.Options{Now: e.Clock.Now, Version: e.Version,
		Log: slog.New(slog.NewTextHandler(&e.logs, nil)), EventRetry: eventRetry,
		WebhookKinds: []dispatcher.WebhookKind{e.Source.WebhookKind(), e.Source.OtherWebhookKind()}, ReconcileTick: reconcileTick,
		ScheduleEvery: scheduleEvery, ScheduleChecked: func(at time.Time) { e.checked.Store(&at) },
		BackstopEvery: backstopEvery,
		CloseEvery:    scheduleEvery, CloseChecked: func(at time.Time) { e.closeChecked.Store(&at) },
		Trackers: []dispatcher.Tracker{e.Tracker.Tracker()}}
	switch {
	case e.Runner != nil:
		options.Runner = e.Runner
	case e.runner != nil:
		options.Runner = e.runner
	}
	d, err := dispatcher.Open(e.Config, options)
	if err != nil {
		e.t.Fatalf("start the Dispatcher: %v", err)
	}
	e.current.Store(d)
}

// URL is the Dispatcher's private listener.
func (e *Env) URL() string { return e.server.URL }

// WebhookListenerURL is the Dispatcher's webhook listener (the address
// that Caddy sends /webhooks/* to).
func (e *Env) WebhookListenerURL() string { return e.webhooks.URL }

// Restart stops the Dispatcher and starts it again on the same database.
func (e *Env) Restart() {
	e.t.Helper()
	if err := e.current.Load().Close(); err != nil {
		e.t.Fatalf("stop the Dispatcher: %v", err)
	}
	e.start()
}

// Person is a SolidTrace user with a token, on a machine of their own.
type Person struct {
	env    *Env
	Token  string
	Answer recorder.Answer
	// Version is the version of the person's solidtrace CLI.
	Version   string
	configDir string
}

// Organization is the Organization of the people that Member makes.
var Organization = recorder.Organization{ID: 7, Slug: "example", Name: "Example"}

// Member makes a member of Organization with an active factory token,
// signed in with the solidtrace CLI.
func (e *Env) Member(name string) *Person {
	return e.PersonIn(name, Organization, "member")
}

// PersonIn makes a person with role in org.
func (e *Env) PersonIn(name string, org recorder.Organization, role string) *Person {
	e.t.Helper()
	e.people++
	org.Role = role
	p := &Person{
		env:   e,
		Token: "stk_person_" + strings.ToLower(name),
		Answer: recorder.Answer{
			Sub:           "10" + string(rune('0'+e.people)),
			Username:      strings.ToLower(name) + "@example.com",
			Scope:         "sessions:write factory",
			Organizations: []recorder.Organization{org},
		},
		Version:   "0.0.0-test",
		configDir: e.t.TempDir(),
	}
	e.SolidTrace.Token(p.Token, p.Answer)
	err := credentials.Store{Dir: p.configDir}.Save(credentials.Credentials{
		InstanceURL:          e.SolidTrace.URL(),
		IngestURL:            e.SolidTrace.URL(),
		TokenEndpoint:        e.SolidTrace.URL() + "/oauth/token",
		ClientID:             "solidtrace-cli",
		Resource:             e.SolidTrace.URL() + "/factory",
		Scope:                p.Answer.Scope,
		AccessToken:          p.Token,
		AccessTokenExpiresAt: e.Clock.Now().Add(24 * time.Hour),
		RefreshToken:         "str_" + p.Token,
		Organization:         &credentials.Organization{Name: org.Name, Slug: org.Slug},
	})
	if err != nil {
		e.t.Fatalf("sign %s in: %v", name, err)
	}
	return p
}

// Result is what one CLI command printed, and its exit code.
type Result struct {
	Stdout, Stderr string
	Code           int
}

// CLI runs the solidtrace CLI on the person's machine.
func (p *Person) CLI(args ...string) Result {
	return p.RunCLI("", "", args...)
}

// RunCLI runs the solidtrace CLI in the directory dir ("" is the test's
// own) with stdin as its input.
func (p *Person) RunCLI(dir, stdin string, args ...string) Result {
	var stdout, stderr bytes.Buffer
	code := command.Run(context.Background(), args, command.Env{
		Dir:       dir,
		Stdin:     strings.NewReader(stdin),
		Stdout:    &stdout,
		Stderr:    &stderr,
		ConfigDir: p.configDir,
		HTTP:      &http.Client{Timeout: 10 * time.Second},
		Now:       p.env.Clock.Now,
		Sleep:     func(context.Context, time.Duration) error { return nil },
		Version:   p.Version,
	})
	return Result{stdout.String(), stderr.String(), code}
}

// Answer is one answer of the Dispatcher's API.
type Answer struct {
	Status int
	Header http.Header
	Body   []byte
}

// JSON decodes the body into v.
func (a Answer) JSON(t testing.TB, v any) {
	t.Helper()
	if err := json.Unmarshal(a.Body, v); err != nil {
		t.Fatalf("decode %s: %v", a.Body, err)
	}
}

// API sends one request to the private listener with the person's token.
// body nil sends no body.
func (p *Person) API(method, path string, body any) Answer {
	return p.env.API(p.Token, method, path, body)
}

// API sends one request to the private listener with token ("" sends no
// Authorization). A []byte body is sent as it is; another body is sent as
// JSON.
func (e *Env) API(token, method, path string, body any) Answer {
	e.t.Helper()
	return e.APIAs("", token, method, path, body)
}

// APIAs is API with userAgent as the User-Agent ("" sends Go's own).
func (e *Env) APIAs(userAgent, token, method, path string, body any) Answer {
	e.t.Helper()
	var reader io.Reader
	if raw, ok := body.([]byte); ok {
		reader = bytes.NewReader(raw)
	} else if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, e.URL()+path, reader)
	if err != nil {
		e.t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	return Answer{resp.StatusCode, resp.Header, answer}
}
