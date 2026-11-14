// Package dispatcher is the Dispatcher: it holds the queue of Runs in
// SQLite and serves the private API that the solidtrace CLI, Console and
// the wrappers call. It reaches SolidTrace only through public APIs.
package dispatcher

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
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
	if options.Version == "" {
		options.Version = "dev"
	}
	s, err := openStore(config.Database)
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
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST "+dispatcherapi.Path+"/runs", d.person(d.startRun))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/runs", d.person(d.listRuns))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/runs/{id}", d.person(d.getRun))
	mux.HandleFunc("GET "+dispatcherapi.Path+"/status", d.status)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, http.StatusNotFound, "no such path: "+r.Method+" "+r.URL.Path)
	})
	d.handler = d.refuseOldCLIs(mux)
	return d, nil
}

// Handler serves the private listener.
func (d *Dispatcher) Handler() http.Handler { return d.handler }

// Close closes the database.
func (d *Dispatcher) Close() error { return d.store.close() }

// listLimit is the most Runs that a list gives.
const listLimit = 100

// maxBody is the largest request body that the API reads.
const maxBody = 1 << 20

type personHandler func(w http.ResponseWriter, r *http.Request, p person)

// person checks the request's token and gives the handler its person.
// The token needs the factory scope; each member of its one Organization
// can use the handler.
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
// private listener only, and it gives no person's data.
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
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&start); err != nil {
		writeProblem(w, http.StatusBadRequest, "the body must be a JSON object with project and prompt")
		return
	}
	start.Project = strings.TrimSpace(start.Project)
	if start.Project == "" || strings.TrimSpace(start.Prompt) == "" {
		writeProblem(w, http.StatusBadRequest, "a Run needs a project and a prompt")
		return
	}
	run := storedRun{
		Run: dispatcherapi.Run{
			ID:           newRunID(),
			State:        dispatcherapi.StateQueued,
			Project:      start.Project,
			Prompt:       start.Prompt,
			Organization: p.organization.slug,
			StartedBy:    dispatcherapi.Person{ID: p.id, Name: p.name},
			CreatedAt:    d.now().UTC().Truncate(time.Microsecond),
		},
		organizationID: p.organization.id,
	}
	if err := d.store.insertRun(run); err != nil {
		d.serverError(w, "start a Run", err)
		return
	}
	writeJSON(w, http.StatusCreated, d.withURL(run.Run, p))
}

func (d *Dispatcher) getRun(w http.ResponseWriter, r *http.Request, p person) {
	run, err := d.store.run(p.organization.id, r.PathValue("id"))
	if errors.Is(err, errNotFound) {
		writeProblem(w, http.StatusNotFound, "no Run "+r.PathValue("id")+" in this Organization")
		return
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
