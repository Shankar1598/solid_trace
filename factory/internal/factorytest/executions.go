package factorytest

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/fs"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/cli/command"
	"github.com/solidtrace/factory/dispatcher"
	"github.com/solidtrace/factory/internal/fakedocker"
	"github.com/solidtrace/factory/internal/fakeharness"
	"github.com/solidtrace/factory/internal/fakerunner"
	"github.com/solidtrace/factory/internal/killboundary"
	"github.com/solidtrace/factory/wrapper"
)

// The program names of the test binary. Executions starts the test binary
// under these names (symbolic links): as the wrapper, as the harness
// (Claude Code), and as the solidtrace CLI that the agent runs.
const (
	wrapperName    = "factory-wrapper"
	harnessName    = "claude"
	solidtraceName = "solidtrace"
	dockerName     = "docker"
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
	case dockerName:
		os.Exit(fakedocker.Main(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
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
//
// The wrapper starts each harness in its kill boundary, a child cgroup.
// So the tests need a cgroup v2 that they can write to: root, or a
// delegated cgroup (such as a systemd-run --user --scope with
// Delegate=yes). Where there is none, the test skips with the reason, or
// fails when killboundary.EnvRequired is set.
func Executions(e *Env) {
	e.t.Helper()
	if err := killboundary.Check(); err != nil {
		if os.Getenv(killboundary.EnvRequired) != "" {
			e.t.Fatalf("the wrapper's kill boundary is not available, and %s is set: %v", killboundary.EnvRequired, err)
		}
		e.t.Skipf("the wrapper's kill boundary is not available here, so this test skips (run it as root or in a delegated cgroup v2): %v", err)
	}
	runUser, err := testRunUser()
	if err != nil {
		if os.Getenv(killboundary.EnvRequired) != "" {
			e.t.Fatalf("%v, and %s is set", err, killboundary.EnvRequired)
		}
		e.t.Skipf("%v, so this test skips", err)
	}
	r, err := newRunner(e, runUser)
	if err != nil {
		e.t.Fatalf("start the fake Runner: %v", err)
	}
	e.Runner = r
	// The wrappers' limits read the environment's clock.
	e.Clock.mu.Lock()
	e.Clock.machines, e.Clock.beforeMove = r.clockFile(), r.renewIdentities
	writeClock(e.Clock.machines, e.Clock.now)
	e.Clock.mu.Unlock()
}

// WithRunner gives the Dispatcher runner in place of the fake Runner,
// such as the Nomad Runner on the local factory stack. As with
// Executions, Project gives each Project a repository on the fake Forge
// and a connection.
func WithRunner(runner dispatcher.Runner) Option {
	return func(e *Env) { e.runner = runner }
}

// EnvRunUser names the run user of the wrappers when the tests run as root,
// as the wrapper runs on an agent machine (the local factory stack runs
// them so, with the run user agent). As another user, the tests run the
// wrappers with that user as the run user, and no user changes.
const EnvRunUser = "FACTORYTEST_RUN_USER"

// testRunUser is the run user of the wrappers.
func testRunUser() (*user.User, error) {
	if os.Geteuid() != 0 {
		return user.Current()
	}
	name := os.Getenv(EnvRunUser)
	if name == "" {
		return nil, fmt.Errorf("the tests run as root and %s names no run user", EnvRunUser)
	}
	return user.Lookup(name)
}

// Runner is the fake Runner as the Dispatcher sees it: it starts each
// Execution's wrapper with the fakerunner, and signs each wrapper's
// workload identity JWT with its own key.
type Runner struct {
	env *Env
	// Fake is the fakerunner, to lose a machine or read a wrapper's output.
	Fake *fakerunner.Runner
	// Docker is the fake Docker of the agent and of the wrapper's end.
	Docker *fakedocker.Docker
	dir    string
	bin    string
	key    *rsa.PrivateKey
	jwks   *httptest.Server

	mu         sync.Mutex
	scripts    []string
	executions []string
	// kinds are the machine kinds that the Dispatcher asked for, by
	// Execution.
	kinds    map[string]string
	harness  string
	runUser  *user.User
	broken   bool
	noDocker bool
}

// BreakWrapper makes the next Execution's wrapper start with no Dispatcher
// URL setting, so that it exits with code 2 before its end call.
func (r *Runner) BreakWrapper() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.broken = true
}

// HoldClock stops the agent machines' clock at its time: the wrappers'
// limits do not pass when the environment's clock moves, as when a
// wrapper's timers fail. The Dispatcher's clock still moves.
func (r *Runner) HoldClock() {
	r.env.Clock.mu.Lock()
	defer r.env.Clock.mu.Unlock()
	r.env.Clock.held = true
}

// clockFile is the file of the agent machines' clock.
func (r *Runner) clockFile() string { return filepath.Join(r.dir, "clock") }

// renewIdentities renews the workload identity JWT of each Execution
// before the environment's clock moves to next: each new JWT is valid
// from now until 1 hour after next, so no call of a wrapper finds its JWT
// not valid while the clock moves.
func (r *Runner) renewIdentities(next time.Time) {
	for _, execution := range r.Dispatched() {
		claims := r.Claims(Workload(execution))
		claims["exp"] = next.Add(time.Hour).Unix()
		r.writeIdentity(execution, r.Sign(claims))
	}
}

// Wrapper is the wrapper program.
func (r *Runner) Wrapper() string { return filepath.Join(r.bin, wrapperName) }

// NoDocker gives the next Executions' wrappers no docker program, as on a
// machine with no Docker.
func (r *Runner) NoDocker() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.noDocker = true
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

func newRunner(e *Env, runUser *user.User) (*Runner, error) {
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
	r := &Runner{env: e, dir: dir, bin: filepath.Join(dir, "bin"), runUser: runUser}
	if err := os.MkdirAll(r.bin, 0o755); err != nil {
		return nil, err
	}
	// As root, the run user reaches the programs, the scripts and the
	// Executions' directories, and cannot change them.
	if err := os.Chmod(dir, 0o711); err != nil {
		return nil, err
	}
	for _, name := range []string{wrapperName, harnessName, solidtraceName, dockerName} {
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
	if r.Docker, err = fakedocker.New(filepath.Join(dir, "docker")); err != nil {
		return nil, err
	}
	if err := r.forRunUser(filepath.Join(dir, "docker"), filepath.Join(dir, "agent-config")); err != nil {
		return nil, err
	}
	// After the fakerunner's Close (cleanups run last first): a lost
	// machine's wrapper leaves its child cgroup, which a real lost machine
	// takes with it.
	e.t.Cleanup(r.removeCgroups)
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
	if err == nil {
		// The harness runs as the run user.
		err = os.Chmod(f.Name(), 0o644)
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
	if r.kinds == nil {
		r.kinds = map[string]string{}
	}
	r.kinds[x.Execution] = x.MachineKind
	program := r.harness
	broken := r.broken
	r.broken = false
	docker := filepath.Join(r.bin, dockerName)
	if r.noDocker {
		docker = filepath.Join(r.dir, "no-docker", dockerName)
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
		wrapper.EnvRunUser + "=" + r.runUser.Username,
		// Never the dev box's real Docker: the end kills each container.
		wrapper.EnvDocker + "=" + docker,
		fakedocker.EnvDir + "=" + r.Docker.Dir(),
		// Each Execution is on its own machine: its own workspace and run
		// user's home. The caches are of the Runner, as on a VM that
		// keeps them across Executions.
		wrapper.EnvWorkspace + "=" + r.Workspace(x.Execution),
		wrapper.EnvHome + "=" + r.Home(x.Execution),
		wrapper.EnvCache + "=" + r.Cache(),
		harness,
		"PATH=" + r.bin + string(os.PathListSeparator) + os.Getenv("PATH"),
		// The agent's solidtrace has no sign-in of a person.
		"SOLIDTRACE_CONFIG_DIR=" + filepath.Join(r.dir, "agent-config"),
		wrapper.EnvUploadEvery + "=" + UploadEvery.String(),
		wrapper.EnvClockFile + "=" + r.clockFile(),
		// The wrapper gives the harness its own Claude Code directories.
		"CLAUDE_CONFIG_DIR=", "CLAUDE_CODE_PROJECT_DIR_NAME=",
	}
	if r.env.Proxy != nil {
		env = append(env, "HTTP_PROXY="+r.env.Proxy.URL(), "NO_PROXY=", "no_proxy=")
	}
	if broken {
		env = env[1:]
	}
	return workload, r.Fake.Dispatch(ctx, fakerunner.Execution{ID: x.Execution, Env: env})
}

// RunUser is the run user of the wrappers (see EnvRunUser).
func (r *Runner) RunUser() *user.User { return r.runUser }

// forRunUser gives each directory under each of dirs (made when it is not
// there) to the run user, when the tests run as root: the agent's fake
// Docker and solidtrace CLI write there.
func (r *Runner) forRunUser(dirs ...string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	uid, _ := strconv.Atoi(r.runUser.Uid)
	gid, _ := strconv.Atoi(r.runUser.Gid)
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return err
			}
			return os.Chown(path, uid, gid)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// RenewIdentity writes a new workload identity JWT for the Execution, at
// the environment's clock, to the file that its wrapper reads, as the
// Runner does before the old JWT expires.
func (r *Runner) RenewIdentity(execution string) {
	r.env.t.Helper()
	r.writeIdentity(execution, r.Identity(Workload(execution)))
}

func (r *Runner) writeIdentity(execution, jwt string) {
	r.env.t.Helper()
	path := filepath.Join(r.dir, "identity-"+execution+".jwt")
	if err := os.WriteFile(path+".new", []byte(jwt), 0o600); err != nil {
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
		out[i] = dispatcher.RunnerEvent{Execution: ev.Execution, State: string(ev.State), Ended: ev.State.Ended(),
			Lost: ev.State == fakerunner.Lost, ExitCode: ev.ExitCode}
	}
	return out, err
}

// Workload gives the workload of a checked JWT: its subject, which is
// Workload of the Execution, for a workload that the Runner started.
func (r *Runner) Workload(_ context.Context, identity dispatcher.Identity) (string, error) {
	execution, ok := strings.CutPrefix(identity.Subject, Workload(""))
	if !ok {
		return "", dispatcher.ErrUnknownWorkload
	}
	if _, known := r.Fake.State(execution); !known {
		return "", dispatcher.ErrUnknownWorkload
	}
	return identity.Subject, nil
}

// Stop stops the Execution's wrapper, as Nomad stops a task.
func (r *Runner) Stop(ctx context.Context, workload string) error {
	execution, ok := strings.CutPrefix(workload, Workload(""))
	if !ok {
		return fmt.Errorf("factorytest: no workload %q", workload)
	}
	return r.Fake.Stop(ctx, execution)
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

// MachineKind is the machine kind that the Dispatcher asked for when it
// dispatched the Execution ("" is the operator's default kind).
func (r *Runner) MachineKind(execution string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.kinds[execution]
}

// Workspace is the folder of an Execution's checkouts (WRAPPER_WORKSPACE).
func (r *Runner) Workspace(execution string) string {
	return filepath.Join(r.dir, "machines", execution, "workspace")
}

// Home is the run user's home of an Execution (WRAPPER_HOME).
func (r *Runner) Home(execution string) string {
	return filepath.Join(r.dir, "machines", execution, "home")
}

// Cache is the caches folder of each Execution (WRAPPER_CACHE).
func (r *Runner) Cache() string { return filepath.Join(r.dir, "cache") }

// UploadEvery is how often each wrapper uploads during an Execution.
const UploadEvery = 100 * time.Millisecond

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

// removeCgroups kills and removes the child cgroup that an Execution's
// wrapper left.
func (r *Runner) removeCgroups() {
	for _, execution := range r.Dispatched() {
		b, err := killboundary.Reopen(wrapper.CgroupName(execution))
		if err != nil {
			continue
		}
		b.Kill()
		if err := b.Remove(5 * time.Second); err != nil {
			r.env.t.Errorf("factorytest: %v", err)
		}
	}
}
