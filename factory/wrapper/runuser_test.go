package wrapper_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/wrapper"
)

// needsRoot skips a case that needs root: the wrapper changes to the run
// user only as root. The local factory stack's smoke runs these cases as
// root under raw_exec, with the run user agent.
func needsRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("this case needs root: the wrapper changes to the run user only as root " +
			"(factory/local/smoke runs it as root under Nomad's raw_exec on the agent machine)")
	}
}

// evidence is a file that the agent wrote in its evidence/ folder, as
// SolidTrace got it.
func evidence(t *testing.T, env *factorytest.Env, name string) string {
	t.Helper()
	session := firstToken(t, env).AgentSessionIDs[0]
	b := env.SolidTrace.Stored(session, "evidence/"+name)
	if b == nil {
		t.Fatalf("SolidTrace has no evidence/%s", name)
	}
	return string(b)
}

// runnerDir is the fake Runner's directory: the identity files are there.
func runnerDir(env *factorytest.Env) string { return filepath.Dir(filepath.Dir(env.Runner.Wrapper())) }

func TestTheHarnessAndItsToolsRunAsTheRunUserInItsGroups(t *testing.T) {
	needsRoot(t)
	env, ana := newEnv(t)
	// The tool (the shell) and its parent, the harness.
	env.Runner.Agent(takeTask, initLine(t),
		shell(`{ id -u; id -G; stat -c %u /proc/$PPID; stat -c %u .; stat -c %u "$SOLIDTRACE_EVIDENCE_DIR"; } `+
			`>"$SOLIDTRACE_EVIDENCE_DIR/ids.new" && mv "$SOLIDTRACE_EVIDENCE_DIR/ids.new" "$SOLIDTRACE_EVIDENCE_DIR/ids"`),
		reply, reportStep("--outcome", "done", "--summary", "Ran as the run user"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)

	// The report went through the agent's socket as the run user.
	if run.Summary != "Ran as the run user" {
		t.Errorf("summary %q, reason %q", run.Summary, run.Reason)
	}
	u := env.Runner.RunUser()
	if u.Uid == "0" {
		t.Fatalf("the run user %s is root", u.Username)
	}
	lines := strings.Split(strings.TrimSpace(evidence(t, env, "ids")), "\n")
	if len(lines) != 5 {
		t.Fatalf("ids: %q", lines)
	}
	for i, what := range map[int]string{0: "the tool", 2: "the harness", 3: "the checkout", 4: "the evidence/ folder"} {
		if lines[i] != u.Uid {
			t.Errorf("%s has the user id %s, want the run user's %s", what, lines[i], u.Uid)
		}
	}
	want, err := u.GroupIds()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Fields(lines[1])
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, slices.Compact(want)) {
		t.Errorf("the tool has the groups %v, want the run user's groups %v", got, want)
	}
}

func TestTheRunUserCannotReadTheWrappersSecretsOrChangeItsFiles(t *testing.T) {
	needsRoot(t)
	// The Runner's machine gives the wrapper a credential in its
	// environment (Nomad, with a workload identity that has env = true).
	t.Setenv("NOMAD_TOKEN", "decoy-runner-token")
	env, ana := newEnv(t)
	// The agent looks at the wrapper (the harness's parent), its identity
	// file and its directory. Each try that works is a leak.
	probe := `dir=$(dirname "$SOLIDTRACE_AGENT_SOCKET"); s="$dir/secrets"
w=$(awk '{ print $4 }' /proc/$PPID/stat)
identity="` + runnerDir(env) + `/identity-$(basename "$dir").jwt"
leak() { if "$@" >/dev/null 2>&1; then echo "LEAK: $*"; fi; }
{
  leak cat "$identity"
  leak cat /proc/$w/environ
  leak head -c 1 /proc/$w/mem
  leak ls "$dir/uploads"
  leak touch "$dir/new-file"
  leak mv "$s" "$dir/moved"
  leak rm -f "$SOLIDTRACE_AGENT_SOCKET"
  leak sh -c "echo x >>'$s/forge-token'"
  leak chmod 777 "$s/git-credential"
  leak touch "$s/new-file"
  "$s/git-credential" get | grep -q '^password=.' && echo "the credential helper gives the forge token"
  tr '\0' '\n' </proc/$PPID/environ >"$SOLIDTRACE_EVIDENCE_DIR/harness-env"
  env >"$SOLIDTRACE_EVIDENCE_DIR/tool-env"
} >"$SOLIDTRACE_EVIDENCE_DIR/probe.new" 2>&1
mv "$SOLIDTRACE_EVIDENCE_DIR/probe.new" "$SOLIDTRACE_EVIDENCE_DIR/probe"`
	env.Runner.Agent(takeTask, initLine(t), shell(probe), reply,
		reportStep("--outcome", "done", "--summary", "Probed"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	execution := waitFor(t, env, ana, id, dispatcherapi.StateDone).Executions[0].ID

	if _, err := os.Stat(filepath.Join(runnerDir(env), "identity-"+execution+".jwt")); err != nil {
		t.Fatalf("the probe looked for the identity file at another path: %v", err)
	}
	got := evidence(t, env, "probe")
	if strings.Contains(got, "LEAK") || !strings.Contains(got, "the credential helper gives the forge token") {
		t.Errorf("the run user's probe:\n%s", got)
	}
	jwt, err := os.ReadFile(filepath.Join(runnerDir(env), "identity-"+execution+".jwt"))
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{
		"the Execution's child token":             firstToken(t, env).Secret,
		"the workload identity JWT":               strings.TrimSpace(string(jwt)),
		"the forge token":                         env.Forge.Token,
		"the Runner's token in the wrapper's env": "decoy-runner-token",
	}
	for _, file := range []string{"harness-env", "tool-env"} {
		environ := evidence(t, env, file)
		for what, secret := range secrets {
			if secret != "" && strings.Contains(environ, secret) {
				t.Errorf("%s has %s", file, what)
			}
		}
		if !strings.Contains("\n"+environ, "\n"+wrapper.EnvSubprocessScrub+"=0\n") {
			t.Errorf("%s has no %s=0", file, wrapper.EnvSubprocessScrub)
		}
	}
}

// The wrapper refuses to start when it cannot keep the run user out of
// what root holds: as root with root as the run user, or with an
// Execution's directory that the run user can change.
func TestTheWrapperAsRootRefusesARunUserThatIsRootOrCanChangeItsDirectory(t *testing.T) {
	needsRoot(t)
	env, _ := newEnv(t)
	u := env.Runner.RunUser()
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	for _, c := range []struct {
		name, runUser string
		dir           func(t *testing.T, dir string)
		want          string
	}{
		{"root", "root", nil, "is root"},
		{"its directory", u.Username, func(t *testing.T, dir string) { os.Chown(dir, uid, gid) }, "can change"},
		{"a parent", u.Username, func(t *testing.T, dir string) { os.Chmod(filepath.Dir(dir), 0o777) }, "can change"},
		{"no way to its socket", u.Username, func(t *testing.T, dir string) { os.Chmod(dir, 0o700) }, "cannot reach"},
	} {
		t.Run(c.name, func(t *testing.T) {
			parent, err := os.MkdirTemp("", "runuser-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.RemoveAll(parent) })
			dir := filepath.Join(parent, "execution")
			if err := os.Mkdir(dir, 0o711); err != nil {
				t.Fatal(err)
			}
			os.Chmod(parent, 0o711)
			if c.dir != nil {
				c.dir(t, dir)
			}
			code, stderr := runWrapper(t, env, dir, c.runUser)
			if code != 2 || !strings.Contains(stderr, c.want) {
				t.Errorf("exit %d, stderr %q; want 2 and %q", code, stderr, c.want)
			}
			// The setup makes secrets/ first.
			if _, err := os.Stat(filepath.Join(dir, "secrets")); !os.IsNotExist(err) {
				t.Error("the wrapper went on to the setup")
			}
		})
	}
}

// A wrapper that is not root starts its processes as its own user: it
// refuses another run user.
func TestTheWrapperRefusesARunUserThatItCannotChangeTo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can change to each user")
	}
	env, _ := newEnv(t)
	for _, runUser := range []string{"root", "no-such-run-user"} {
		code, stderr := runWrapper(t, env, t.TempDir(), runUser)
		if code != 2 || !strings.Contains(stderr, "run user "+runUser) {
			t.Errorf("the run user %s: exit %d, stderr %q; want 2 and the reason", runUser, code, stderr)
		}
	}
}

// runWrapper runs a wrapper for an Execution that the Dispatcher does not
// have, in dir, with extra in its environment, and gives its exit code and
// stderr.
func runWrapper(t *testing.T, env *factorytest.Env, dir, runUser string, extra ...string) (int, string) {
	t.Helper()
	identity := filepath.Join(t.TempDir(), "identity.jwt")
	if err := os.WriteFile(identity, []byte(env.Runner.Identity(factorytest.Workload("ex_runuser"))), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(env.Runner.Wrapper())
	cmd.Dir = dir
	cmd.Env = []string{wrapper.EnvDispatcherURL + "=" + env.URL(), wrapper.EnvExecution + "=ex_runuser",
		wrapper.EnvIdentityFile + "=" + identity, wrapper.EnvDocker + "=/no/docker", wrapper.EnvRunUser + "=" + runUser,
		"PATH=" + os.Getenv("PATH")}
	cmd.Env = append(cmd.Env, extra...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Run()
	return cmd.ProcessState.ExitCode(), stderr.String()
}

// The Runner names a directory for the Execution that is not there (the
// Nomad job: under the image recipe's directory of the Executions). The
// wrapper makes it, which the run user reaches and cannot change, and goes
// on to the task call.
func TestTheWrapperMakesItsDirectoryWhenItIsNotThere(t *testing.T) {
	env, _ := newEnv(t)
	parent, err := os.MkdirTemp("", "executions-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(parent) })
	if err := os.Chmod(parent, 0o711); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(parent, "ex_runuser")
	code, stderr := runWrapper(t, env, t.TempDir(), env.Runner.RunUser().Username, wrapper.EnvDir+"="+dir)
	// The Dispatcher has no such Execution: the task call fails (1).
	if code != 1 || !strings.Contains(stderr, "task") {
		t.Errorf("exit %d, stderr %q; want 1 at the task call", code, stderr)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("the wrapper did not make its directory: %v", err)
	}
	if mode := info.Mode(); !mode.IsDir() || mode.Perm() != 0o711 {
		t.Errorf("the wrapper's directory has the mode %v, want 0711", mode)
	}
}

func TestALinkThatTheRunUserPutsInPlaceOfTheProjectDirectoryMovesNoUpload(t *testing.T) {
	env, ana := newEnv(t)
	// The agent moves its project directory away and puts a link to
	// another directory in its place. Then it waits until that directory
	// has the files of its Agent session.
	outside, err := os.MkdirTemp("", "outside-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(outside) })
	env.Runner.Agent(takeTask, initLine(t),
		shell(`printf 'PNG' >"$SOLIDTRACE_EVIDENCE_DIR/cart.png" && p=$(dirname "$SOLIDTRACE_EVIDENCE_DIR") && `+
			`mv "$p" "$p.moved" && ln -s '`+outside+`' "$p" && while [ ! -e "$p.ready" ]; do sleep 0.02; done`),
		reply, reportStep("--outcome", "done", "--summary", "Fixed"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	execution := waitFor(t, env, ana, id, dispatcherapi.StateRunning).Executions[0].ID
	session := firstToken(t, env).AgentSessionIDs[0]
	// A directory of root's that the run user cannot read, with a
	// transcript and an evidence/ file of that Agent session, and the
	// transcript of an Agent session that only it has.
	const onlyOutside = "0badc0de-0000-4000-8000-000000000001"
	for name, content := range map[string]string{session + ".jsonl": `{"secret":"root's transcript"}` + "\n",
		onlyOutside + ".jsonl": `{"secret":"root's other transcript"}` + "\n",
		"evidence/secret.txt":  "a secret of root's"} {
		path := filepath.Join(outside, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The project directory is <home>/.claude/projects/<name>.
	var project string
	waitUntil(t, "the agent put the link in place", func() bool {
		dirs, _ := filepath.Glob(filepath.Join(env.Runner.Home(execution), ".claude", "projects", "*"))
		for _, dir := range dirs {
			if target, err := os.Readlink(dir); err == nil && target == outside {
				project = dir
				return true
			}
		}
		return false
	})
	if err := os.WriteFile(project+".ready", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	for _, up := range env.SolidTrace.Uploads() {
		if bytes.Contains(up.Bytes, []byte("root's")) || strings.Contains(up.Path, "secret") {
			t.Errorf("an upload came from the link's target: %s", up.Path)
		}
	}
	// The listing of Agent sessions reads the directory that the wrapper
	// made too: the wrapper asks for no Agent session of the link's target.
	for _, tok := range env.SolidTrace.ChildTokens() {
		if slices.Contains(tok.AgentSessionIDs, onlyOutside) {
			t.Errorf("the wrapper added the Agent session %s of the link's target to the Run", onlyOutside)
		}
	}
	// The uploads read the project directory that the wrapper made.
	if got := evidence(t, env, "cart.png"); got != "PNG" {
		t.Errorf("evidence/cart.png: %q", got)
	}
	if len(env.SolidTrace.Stored(session, session+".jsonl")) == 0 {
		t.Error("no transcript")
	}
}

// The wrapper's git runs as the run user in the Execution's kill
// boundary: a hook from the machine's git configuration starts in the
// child cgroup, so the end kills each process that it leaves (the end's
// own cases check the kill). The project directory is there before the
// first process of the run user starts: root makes nothing after it.
func TestTheWrappersGitRunsInTheKillBoundaryAfterTheProjectDirectoryIsMade(t *testing.T) {
	out, err := os.MkdirTemp("", "git-hook-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(out) })
	// The run user writes here and reads the hook (as root too).
	if err := os.Chmod(out, 0o777); err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(out, "hooks")
	if err := os.Mkdir(hooks, 0o755); err != nil {
		t.Fatal(err)
	}
	hook := "#!/bin/sh\nout='" + out + "'\n" +
		`cat /proc/self/cgroup >"$out/cgroup"` + "\n" +
		`ls -d "$HOME"/.claude/projects/*/evidence >"$out/project" 2>&1` + "\n"
	if err := os.WriteFile(filepath.Join(hooks, "post-checkout"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(out, "gitconfig")
	if err := os.WriteFile(config, []byte("[core]\n\thooksPath = "+hooks+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The wrapper gives git the run user's own global configuration.
	t.Setenv("GIT_CONFIG_SYSTEM", config)
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), reply, reportStep("--outcome", "done", "--summary", "Fixed"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	execution := onlyExecution(t, waitFor(t, env, ana, id, dispatcherapi.StateDone)).ID
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := env.Runner.Fake.Wait(ctx, execution); err != nil {
		t.Fatal(err)
	}

	cgroup, err := os.ReadFile(filepath.Join(out, "cgroup"))
	if err != nil {
		t.Fatalf("the clone ran no hook: %v", err)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(cgroup)), "/"+wrapper.CgroupName(execution)) {
		t.Errorf("the hook of the wrapper's git ran in the cgroup %q, not in the Execution's child cgroup", strings.TrimSpace(string(cgroup)))
	}
	if project, _ := os.ReadFile(filepath.Join(out, "project")); !strings.HasSuffix(strings.TrimSpace(string(project)), "/evidence") {
		t.Errorf("the project directory was not there when the run user's first process started: %q", project)
	}
}

// The Stop hook runs as the run user, as the harness does: it reads the
// harness's settings and changes the run state file of the Execution's
// directory (root's), so it refuses a stop with no report.
func TestTheRunUsersStopHookRefusesAStopWithNoReport(t *testing.T) {
	needsRoot(t)
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), reply, stopStep([]string{reply}), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	if got := kindsOf(t, feedback(t, env)); strings.Join(got, ", ") != "no report, no report" {
		t.Errorf("refusals %q, want two for no report", got)
	}
	if e := onlyExecution(t, run); e.EndReason != dispatcherapi.EndNoReport {
		t.Errorf("end reason %q", e.EndReason)
	}
}
