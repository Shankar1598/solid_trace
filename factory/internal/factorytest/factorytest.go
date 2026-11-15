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
	"github.com/solidtrace/factory/internal/recorder"
)

// DispatcherToken is the Dispatcher's own API token at the recorder.
const DispatcherToken = "stk_dispatcher"

// Clock is a clock that moves only when a test moves it.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// Now is the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
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

	current atomic.Pointer[dispatcher.Dispatcher]
	server  *httptest.Server
	people  int
	logs    lockedBuffer
	// projects counts the Projects that Project made, for their ids.
	projects int64
	admin    *Person
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
	if e.admin == nil {
		e.admin = e.PersonIn("factorytest-admin", Organization, "admin")
	}
	settings := map[string]string{"repository": "bitbucket.org/example/" + slug}
	if e.Runner != nil {
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
	e.Config = dispatcher.Config{
		SolidTraceURL:   e.SolidTrace.URL(),
		SolidTraceToken: DispatcherToken,
		Database:        filepath.Join(t.TempDir(), "dispatcher.sqlite3"),
		EncryptionKey:   EncryptionKey,
		BitbucketAPIURL: e.Forge.APIURL(),
		BitbucketGitURL: e.Forge.GitURL(),
	}
	e.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.current.Load().Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(e.server.Close)
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
		Log: slog.New(slog.NewTextHandler(&e.logs, nil)), EventRetry: eventRetry}
	if e.Runner != nil {
		options.Runner = e.Runner
	}
	d, err := dispatcher.Open(e.Config, options)
	if err != nil {
		e.t.Fatalf("start the Dispatcher: %v", err)
	}
	e.current.Store(d)
}

// URL is the Dispatcher's private listener.
func (e *Env) URL() string { return e.server.URL }

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
