package factorytest

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/command"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/fakeharness"
	"github.com/solidtrace/factory/internal/fakerunner"
	"github.com/solidtrace/factory/wrapper"
)

// The program names of the test binary. Executions starts the test binary
// under these names (symbolic links): as the wrapper, as the harness
// (Claude Code), and as the solidtrace CLI that the agent runs.
const (
	wrapperName    = "factory-wrapper"
	harnessName    = "claude"
	solidtraceName = "solidtrace"
)

// MainIfAsked runs the test binary as the wrapper, the harness or the
// solidtrace CLI when its program name asks for it, and exits. Call it
// first in the TestMain of a package that uses Executions.
func MainIfAsked() {
	switch filepath.Base(os.Args[0]) {
	case wrapperName:
		os.Exit(wrapper.Main(context.Background(), os.Getenv, os.Environ(), os.Stderr))
	case harnessName:
		os.Exit(fakeharness.Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
	case solidtraceName:
		os.Exit(command.Run(context.Background(), os.Args[1:], command.Env{
			Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
			ConfigDir:   filepath.Join(os.TempDir(), "solidtrace-agent-config"),
			HTTP:        &http.Client{Timeout: 30 * time.Second},
			Now:         time.Now,
			Version:     "0.0.0-test",
			AgentSocket: os.Getenv(agentsocket.Env),
		}))
	}
}

// Option changes the environment that New makes.
type Option func(*Env)

// Executions gives the Dispatcher the fake Runner (each Execution is a
// local wrapper process, with the scripted harness), a fake Runner
// identity (a JWKS and the JWTs it signs), and the fake Forge, which holds
// each Project's repository. The test binary's TestMain must call
// MainIfAsked.
func Executions(e *Env) {
	e.t.Helper()
	r, err := newRunner(e)
	if err != nil {
		e.t.Fatalf("start the fake Runner: %v", err)
	}
	e.Runner = r
	// The operator command needs the database that the Dispatcher makes
	// at its first start.
	e.afterStart = append(e.afterStart, func() { e.AddAccount(DefaultAccount, DefaultAccountToken) })
}

// DefaultAccount is the subscription account that the Executions option
// adds to the pool, with the token DefaultAccountToken. A test that needs
// other accounts adds them, or removes this one.
const (
	DefaultAccount      = "pool-1"
	DefaultAccountToken = "sk-ant-oat01-factorytest-pool-1"
)

// Runner is the fake Runner as the Dispatcher sees it: it starts each
// Execution's wrapper with the fakerunner, and signs each wrapper's
// workload identity JWT with its own key.
type Runner struct {
	env *Env
	// Fake is the fakerunner, to lose a machine or read a wrapper's output.
	Fake *fakerunner.Runner
	dir  string
	bin  string
	key  *rsa.PrivateKey
	jwks *httptest.Server

	mu         sync.Mutex
	scripts    []string
	executions []string
	harness    string
	pathFirst  string
	before     func(execution string)
}

// BeforeDispatch makes the Runner call f with the id of the next
// Execution that it starts, before it starts the Execution's wrapper: the
// Execution is dispatched and has no task call yet.
func (r *Runner) BeforeDispatch(f func(execution string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.before = f
}

// PathFirst puts dir first in the PATH of the next Executions' wrappers,
// before the test binary's programs: a test puts a program there that
// records how the wrapper runs it, such as git.
func (r *Runner) PathFirst(dir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pathFirst = dir
}

// Harness is the scripted harness program (Claude Code's place).
func (r *Runner) Harness() string { return filepath.Join(r.bin, harnessName) }

// UseHarness makes program the harness of the next Executions ("" is the
// scripted harness again). A test uses it for a harness that changes the
// command line before it starts Harness.
func (r *Runner) UseHarness(program string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.harness = program
}

// keyID is the kid of the fake Runner's key.
const keyID = "fake-runner-key"

func newRunner(e *Env) (*Runner, error) {
	// A short directory: a Unix socket path has at most 107 bytes.
	dir, err := os.MkdirTemp("", "factorytest-")
	if err != nil {
		return nil, err
	}
	e.t.Cleanup(func() { os.RemoveAll(dir) })
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	r := &Runner{env: e, dir: dir, bin: filepath.Join(dir, "bin")}
	if err := os.MkdirAll(r.bin, 0o755); err != nil {
		return nil, err
	}
	for _, name := range []string{wrapperName, harnessName, solidtraceName} {
		if err := os.Symlink(self, filepath.Join(r.bin, name)); err != nil {
			return nil, err
		}
	}
	if r.key, err = rsa.GenerateKey(rand.Reader, 2048); err != nil {
		return nil, err
	}
	r.jwks = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"keys": []any{map[string]string{
			"kty": "RSA", "kid": keyID, "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(r.key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(r.key.E)).Bytes()),
		}}})
	}))
	e.t.Cleanup(r.jwks.Close)
	r.Fake, err = fakerunner.New(fakerunner.Options{
		Command:     []string{filepath.Join(r.bin, wrapperName)},
		Dir:         filepath.Join(dir, "executions"),
		KillTimeout: 3 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	e.t.Cleanup(r.Fake.Close)
	return r, nil
}

// Agent queues the script of the harness of the next Execution that the
// Runner starts: lines of a fakeharness stream. An Execution with no
// queued script plays the recorded stream turn-with-tool-call.
func (r *Runner) Agent(lines ...string) {
	r.env.t.Helper()
	f, err := os.CreateTemp(r.dir, "agent-*.jsonl")
	if err == nil {
		_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
		f.Close()
	}
	if err != nil {
		r.env.t.Fatal(err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scripts = append(r.scripts, f.Name())
}

// Workload is the subject of the fake JWT of an Execution.
func Workload(execution string) string { return "fake-runner:" + execution }

// Dispatch starts the Execution's wrapper with a workload identity JWT.
func (r *Runner) Dispatch(ctx context.Context, x dispatcher.RunnerExecution) (string, error) {
	r.mu.Lock()
	before := r.before
	r.before = nil
	r.mu.Unlock()
	if before != nil {
		before(x.Execution)
	}
	workload := Workload(x.Execution)
	identity := filepath.Join(r.dir, "identity-"+x.Execution+".jwt")
	if err := os.WriteFile(identity, []byte(r.Identity(workload)), 0o600); err != nil {
		return "", err
	}
	harness := "FAKE_HARNESS_STREAM=turn-with-tool-call"
	r.mu.Lock()
	if len(r.scripts) > 0 {
		harness = "FAKE_HARNESS_SCRIPT=" + r.scripts[0]
		r.scripts = r.scripts[1:]
	}
	r.executions = append(r.executions, x.Execution)
	program := r.harness
	path := r.bin + string(os.PathListSeparator) + os.Getenv("PATH")
	if r.pathFirst != "" {
		path = r.pathFirst + string(os.PathListSeparator) + path
	}
	r.mu.Unlock()
	if program == "" {
		program = r.Harness()
	}
	env := []string{
		wrapper.EnvDispatcherURL + "=" + r.env.URL(),
		wrapper.EnvExecution + "=" + x.Execution,
		wrapper.EnvIdentityFile + "=" + identity,
		wrapper.EnvHarness + "=" + program,
		harness,
		"PATH=" + path,
		// The agent's solidtrace has no sign-in of a person.
		"SOLIDTRACE_CONFIG_DIR=" + filepath.Join(r.dir, "agent-config"),
	}
	return workload, r.Fake.Dispatch(ctx, fakerunner.Execution{ID: x.Execution, Env: env})
}

// RenewIdentity writes a new workload identity JWT for the Execution, at
// the environment's clock, to the file that its wrapper reads, as the
// Runner does before the old JWT expires.
func (r *Runner) RenewIdentity(execution string) {
	r.env.t.Helper()
	path := filepath.Join(r.dir, "identity-"+execution+".jwt")
	if err := os.WriteFile(path+".new", []byte(r.Identity(Workload(execution))), 0o600); err != nil {
		r.env.t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		r.env.t.Fatal(err)
	}
}

// Events gives the fakerunner's events as the Dispatcher reads them.
func (r *Runner) Events(ctx context.Context, from int) ([]dispatcher.RunnerEvent, error) {
	evs, err := r.Fake.Events(ctx, from)
	out := make([]dispatcher.RunnerEvent, len(evs))
	for i, ev := range evs {
		out[i] = dispatcher.RunnerEvent{Execution: ev.Execution, Ended: ev.State.Ended(),
			Lost: ev.State == fakerunner.Lost, ExitCode: ev.ExitCode}
	}
	return out, err
}

// JWKSURL is the fake Runner's key set.
func (r *Runner) JWKSURL() string { return r.jwks.URL }

// Dispatched are the ids of the Executions that the Runner started, in
// order.
func (r *Runner) Dispatched() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.executions...)
}

// Socket is the path of the agent's socket of an Execution.
func (r *Runner) Socket(execution string) string {
	return filepath.Join(r.dir, "executions", execution, "agent.sock")
}

// Claims are the claims of a valid workload identity JWT for workload,
// at the environment's clock.
func (r *Runner) Claims(workload string) map[string]any {
	now := r.env.Clock.Now()
	return map[string]any{"sub": workload, "aud": []string{dispatcher.IdentityAudience},
		"iat": now.Unix(), "nbf": now.Unix(), "exp": now.Add(time.Hour).Unix(),
		"nomad_job_id": "factory-execution/dispatch-" + workload}
}

// Identity is a valid workload identity JWT for workload.
func (r *Runner) Identity(workload string) string { return r.Sign(r.Claims(workload)) }

// Sign signs claims with the fake Runner's key.
func (r *Runner) Sign(claims map[string]any) string { return sign(r.key, "RS256", claims) }

// SignWithAlg signs claims with the fake Runner's key (RS256), with alg in
// the JWT's header: the signature is correct, and only the alg is wrong.
func (r *Runner) SignWithAlg(alg string, claims map[string]any) string {
	return sign(r.key, alg, claims)
}

// Forged signs claims with a key that is not the fake Runner's, with the
// Runner's key id.
func (r *Runner) Forged(claims map[string]any) string {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		r.env.t.Fatal(err)
	}
	return sign(key, "RS256", claims)
}

func sign(key *rsa.PrivateKey, alg string, claims map[string]any) string {
	header, _ := json.Marshal(map[string]string{"alg": alg, "typ": "JWT", "kid": keyID})
	body, _ := json.Marshal(claims)
	signed := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(body)
	digest := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		panic(err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ForgeConnection is the name of the forge connection that Project names
// when the environment has Executions.
const ForgeConnection = "forge"
