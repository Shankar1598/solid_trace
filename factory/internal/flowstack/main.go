// Command flowstack is the factory part of the Seam 5 flow stack: the
// real Dispatcher and the real wrapper against the real SolidTrace (the
// dev Console and EventStore), with the Seam 3 fakes for the Runner, the
// Harness, the Forge and the Tracker only. A Playwright flow
// (console/e2e/flows/runs.spec.ts) builds it, starts it, and stops it.
//
// Its environment:
//
//	FLOWSTACK_CONSOLE_URL        Console (the OAuth issuer, child tokens, MCP)
//	FLOWSTACK_INGEST_URL         EventStore's ingest (run events, uploads)
//	DISPATCHER_SOLIDTRACE_TOKEN  the Dispatcher's API token at Console
//
// When it is ready, it prints one JSON line on stdout: the Dispatcher's
// URL, the URL of its control API, and the fake Forge's token. It stops
// when its stdin closes or at SIGTERM, and then stops each wrapper.
//
// The control API is for the flow only, and is not the Dispatcher's:
//
//	POST /repos {"name": "workspace/name"}  a repository on the fake Forge
//	POST /agent {"lines": [...]}            the harness script of the next Execution
//	GET  /executions                        the Executions that the Runner started
//	GET  /child-tokens                      each child token that Console made
//
// The program is also the wrapper, the harness and the agent's solidtrace
// CLI, by its program name (factorytest.MainIfAsked).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/factorytest"
)

func main() {
	factorytest.MainIfAsked()
	os.Exit(run())
}

func run() int {
	console, ingest := os.Getenv("FLOWSTACK_CONSOLE_URL"), os.Getenv("FLOWSTACK_INGEST_URL")
	token := os.Getenv("DISPATCHER_SOLIDTRACE_TOKEN")
	if console == "" || ingest == "" || token == "" {
		fmt.Fprintln(os.Stderr, "flowstack: set FLOWSTACK_CONSOLE_URL, FLOWSTACK_INGEST_URL and DISPATCHER_SOLIDTRACE_TOKEN")
		return 2
	}
	t := newProcessTB()
	defer t.cleanup()

	children := &childTokens{next: http.DefaultTransport}
	env := factorytest.New(t, factorytest.RealClock, factorytest.AtSolidTrace(console, ingest, token),
		factorytest.WithHTTP(&http.Client{Timeout: 30 * time.Second, Transport: children}), factorytest.Executions)

	control := httptest.NewServer(controlAPI(env, children))
	t.Cleanup(control.Close)

	ready, _ := json.Marshal(map[string]string{
		"dispatcher_url": env.URL(), "control_url": control.URL, "forge_token": env.Forge.Token,
	})
	fmt.Println(string(ready))

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	stdinClosed := make(chan struct{})
	go func() {
		io.Copy(io.Discard, os.Stdin)
		close(stdinClosed)
	}()
	select {
	case <-stop:
	case <-stdinClosed:
	}
	fmt.Fprintf(os.Stderr, "flowstack: stopping; the Dispatcher's log:\n%s", env.Logs())
	return 0
}

func controlAPI(env *factorytest.Env, children *childTokens) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /repos", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name string }
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Name == "" {
			http.Error(w, `give {"name": "workspace/name"}`, http.StatusBadRequest)
			return
		}
		env.Forge.AddRepo(body.Name, nil)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("POST /agent", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Lines []string }
		if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Lines) == 0 {
			http.Error(w, `give {"lines": [...]}`, http.StatusBadRequest)
			return
		}
		env.Runner.Agent(body.Lines...)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /executions", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"executions": env.Runner.Dispatched()})
	})
	mux.HandleFunc("GET /child-tokens", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{"child_tokens": children.list()})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

// childToken is one child token that Console made for the Dispatcher.
type childToken struct {
	ID              any      `json:"id"`
	Secret          string   `json:"secret"`
	Scopes          []string `json:"scopes"`
	AgentSessionIDs []string `json:"agent_session_ids"`
	ExpiresAt       string   `json:"expires_at"`
}

// childTokens is the Dispatcher's transport to SolidTrace. It keeps each
// child token that Console makes (POST /api/child_tokens), so that the
// flow can use an Execution's token as the wrapper does. The wrapper
// keeps it in memory only; this is the flow's one way to see it.
type childTokens struct {
	next http.RoundTripper

	mu     sync.Mutex
	tokens []childToken
}

func (c *childTokens) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodPost || req.URL.Path != "/api/child_tokens" {
		return c.next.RoundTrip(req)
	}
	var sent []byte
	if req.Body != nil {
		var err error
		if sent, err = io.ReadAll(req.Body); err != nil {
			return nil, err
		}
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(sent))
	}
	resp, err := c.next.RoundTrip(req)
	if err != nil || resp.StatusCode != http.StatusCreated {
		return resp, err
	}
	answer, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(answer))
	if err != nil {
		return resp, nil
	}
	var request struct {
		Scopes          []string `json:"scopes"`
		AgentSessionIDs []string `json:"agent_session_ids"`
		ExpiresAt       string   `json:"expires_at"`
	}
	var made struct {
		APIToken struct {
			ID any `json:"id"`
		} `json:"api_token"`
		Secret string `json:"secret"`
	}
	json.Unmarshal(sent, &request)
	json.Unmarshal(answer, &made)
	c.mu.Lock()
	c.tokens = append(c.tokens, childToken{ID: made.APIToken.ID, Secret: made.Secret, Scopes: request.Scopes,
		AgentSessionIDs: request.AgentSessionIDs, ExpiresAt: request.ExpiresAt})
	c.mu.Unlock()
	return resp, nil
}

func (c *childTokens) list() []childToken {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]childToken{}, c.tokens...)
}

// processTB is the testing.TB that factorytest needs, for a process that
// is not a test: a failure prints its message and stops the process, and
// the cleanups run at the end, last first. Only the methods that
// factorytest and its fakes call are here; another method panics.
type processTB struct {
	// TB is nil: it gives the type testing.TB's private method.
	testing.TB
	mu       sync.Mutex
	cleanups []func()
}

func newProcessTB() *processTB { return &processTB{} }

func (p *processTB) Helper() {}

func (p *processTB) Cleanup(f func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanups = append(p.cleanups, f)
}

func (p *processTB) TempDir() string {
	dir, err := os.MkdirTemp("", "flowstack-")
	if err != nil {
		p.Fatal(err)
	}
	p.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func (p *processTB) Logf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }

func (p *processTB) Errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "flowstack: "+format+"\n", args...)
}

func (p *processTB) Fatal(args ...any) { p.Fatalf("%s", fmt.Sprint(args...)) }

func (p *processTB) Fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "flowstack: "+format+"\n", args...)
	p.cleanup()
	os.Exit(1)
}

func (p *processTB) Skipf(format string, args ...any) { p.Fatalf(format, args...) }

// cleanup runs the cleanups one time, last first.
func (p *processTB) cleanup() {
	p.mu.Lock()
	cleanups := p.cleanups
	p.cleanups = nil
	p.mu.Unlock()
	for i := len(cleanups) - 1; i >= 0; i-- {
		cleanups[i]()
	}
}
