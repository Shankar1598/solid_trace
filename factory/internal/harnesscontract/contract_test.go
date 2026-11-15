package harnesscontract_test

// The Harness contract checks (Seam 4). They run the real Claude Code with
// a cheap model, under the login of the person who runs them, and check
// what the wrapper depends on. Each check that passes saves its stream in
// factory/internal/fakeharness/streams, where the scripted Harness of
// Seam 3 reads it.
//
// They are skipped in the usual suites. Run them when the pinned Claude
// Code version (fakeharness.Version) changes, from factory/:
//
//	FACTORY_HARNESS_CONTRACT=1 go test -count=1 -v -timeout 15m ./internal/harnesscontract/
//
// FACTORY_CLAUDE names the program (default "claude" on the PATH), and
// FACTORY_HARNESS_MODEL the model (default "haiku"). A run costs a few US
// cents; TestMain fails it when the total is more than maxCost.

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/agentsocket"
	"github.com/solidtrace/factory/internal/fakeharness"
	"github.com/solidtrace/factory/internal/harnesscontract"
	"github.com/solidtrace/factory/wrapper"
)

const maxCost = 0.05 // US dollars for one run of all the checks

var (
	costMu    sync.Mutex
	totalCost float64
	// severalLines is true when a check saw a reply of several lines: one
	// message id on more than one assistant line of a .jsonl.
	severalLines bool
)

func TestMain(m *testing.M) {
	code := m.Run()
	if madeGlobal != "" {
		removeMadeGlobal()
	}
	if os.Getenv("FACTORY_HARNESS_CONTRACT") == "1" {
		fmt.Printf("harness contract checks: the total cost is %.4f US dollars\n", totalCost)
		if totalCost > maxCost {
			fmt.Printf("FAIL: the cost is more than %.2f US dollars\n", maxCost)
			code = 1
		}
		// The parse counts a reply of several lines one time. A run of all
		// the checks must see one such reply, or that rule is not checked.
		if f := flag.Lookup("test.run"); f != nil && f.Value.String() == "" && !severalLines {
			fmt.Println("FAIL: no check saw a reply of several lines in a .jsonl")
			code = 1
		}
	}
	os.Exit(code)
}

var (
	globalOnce sync.Once
	madeGlobal string // the global settings file that this run made, if any
	// oldBackups holds the backups of the global settings file before the run.
	oldBackups map[string]bool
)

// removeMadeGlobal removes the global settings file that this run made,
// and the backups of it that Claude Code wrote (in <config>/backups). A
// backup is of that file when it is new and has the same firstStartTime.
// The other backups stay: a Claude Code session beside the run can write a
// backup of the person's own settings in the same directory.
func removeMadeGlobal() {
	first := firstStartTime(madeGlobal)
	os.Remove(madeGlobal)
	if first == "" {
		return
	}
	for _, b := range globalBackups(filepath.Dir(madeGlobal)) {
		if !oldBackups[b] && firstStartTime(b) == first {
			os.Remove(b)
		}
	}
}

func firstStartTime(file string) string {
	b, _ := os.ReadFile(file)
	var v struct {
		FirstStartTime string `json:"firstStartTime"`
	}
	json.Unmarshal(b, &v)
	return v.FirstStartTime
}

func globalBackups(config string) []string {
	var out []string
	for _, pattern := range []string{
		filepath.Join(config, ".claude.json.backup*"),
		filepath.Join(config, "backups", ".claude.json.backup*"),
	} {
		matches, _ := filepath.Glob(pattern)
		out = append(out, matches...)
	}
	return out
}

var versionOnce sync.Once
var versionErr error

// contract skips the test unless the run asks for the contract checks, and
// gives a fresh place for one check: a git checkout with the repository's
// own .claude settings, and a hooks directory outside it.
func contract(t *testing.T) *place {
	t.Helper()
	if os.Getenv("FACTORY_HARNESS_CONTRACT") != "1" {
		t.Skip("a Harness contract check: it runs the real Claude Code; set FACTORY_HARNESS_CONTRACT=1 to run it")
	}
	program := os.Getenv("FACTORY_CLAUDE")
	if program == "" {
		program = "claude"
	}
	versionOnce.Do(func() {
		out, err := exec.Command(program, "--version").Output()
		if err != nil {
			versionErr = fmt.Errorf("%s --version: %v", program, err)
			return
		}
		if v := strings.Fields(string(out)); len(v) == 0 || v[0] != fakeharness.Version {
			versionErr = fmt.Errorf("%s is version %q; the pinned version is %s. To move the pin, change fakeharness.Version first, then run the checks again", program, strings.TrimSpace(string(out)), fakeharness.Version)
		}
	})
	if versionErr != nil {
		t.Fatal(versionErr)
	}
	model := os.Getenv("FACTORY_HARNESS_MODEL")
	if model == "" {
		model = "haiku"
	}
	home, _ := os.UserHomeDir()
	config := os.Getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		config = filepath.Join(home, ".claude")
	}
	// With CLAUDE_CONFIG_DIR set, Claude Code keeps its global settings in
	// <config>/.claude.json. Remove that file at the end if this run made it.
	globalOnce.Do(func() {
		file := filepath.Join(config, ".claude.json")
		if _, err := os.Stat(file); err != nil {
			madeGlobal = file
			oldBackups = map[string]bool{}
			for _, b := range globalBackups(config) {
				oldBackups[b] = true
			}
		}
	})
	root := t.TempDir()
	p := &place{t: t, program: program, model: model, home: home, config: config,
		origin:   filepath.Join(root, "origin.git"),
		checkout: filepath.Join(root, "workspace", "repo"),
		hooks:    filepath.Join(root, "hooks"),
		project:  "factory-contract-" + newUUID()[:8],
	}
	must(t, os.MkdirAll(p.hooks, 0o700))
	p.writeHooks()
	p.makeOrigin()
	p.clone()
	t.Cleanup(p.cleanup)
	return p
}

type place struct {
	t                       *testing.T
	program, model          string
	home, config            string
	origin, checkout, hooks string
	project                 string // CLAUDE_CODE_PROJECT_DIR_NAME
	sessions                []string
}

// transcriptDir is where Claude Code keeps this place's Agent sessions.
func (p *place) transcriptDir() string { return filepath.Join(p.config, "projects", p.project) }

func (p *place) cleanup() {
	os.RemoveAll(p.transcriptDir())
	for _, s := range p.sessions {
		for _, d := range []string{"session-env", "file-history", "tasks"} {
			os.RemoveAll(filepath.Join(p.config, d, s))
		}
		matches, _ := filepath.Glob(filepath.Join(p.config, "todos", s+"*"))
		for _, m := range matches {
			os.Remove(m)
		}
	}
}

// writeHooks writes the wrapper's Stop hook and a reduced report command.
// The hook refuses the end of a turn until the report command ran in that
// turn, and gives up after two refusals, as the wrapper's hook does.
func (p *place) writeHooks() {
	stop := `#!/bin/bash
H="$(dirname "$0")"; IN=$(cat)
printf '%s\n' "$IN" | tr -d '\n' >> "$H/stop-hook.log"; echo >> "$H/stop-hook.log"
if [ -f "$H/marker" ]; then rm -f "$H/marker" "$H/count"; exit 0; fi
n=$(( $(cat "$H/count" 2>/dev/null || echo 0) + 1 )); echo $n > "$H/count"
if [ $n -gt 2 ]; then rm -f "$H/count"; exit 0; fi
printf '{"decision":"block","reason":"Before you stop, run this with Bash: %s/report done \\"<one-line summary of this turn>\\""}\n' "$H"
`
	report := `#!/bin/bash
echo "$*" >> "$(dirname "$0")/reports.log"
touch "$(dirname "$0")/marker"
echo "Report recorded."
`
	must(p.t, os.WriteFile(filepath.Join(p.hooks, "stop-hook.sh"), []byte(stop), 0o700))
	must(p.t, os.WriteFile(filepath.Join(p.hooks, "report"), []byte(report), 0o700))
	p.writeSettings("settings.json", map[string]any{"skipDangerousModePermissionPrompt": true,
		"hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{map[string]any{
			"type": "command", "command": filepath.Join(p.hooks, "stop-hook.sh")}}}}}})
	p.writeSettings("no-hook.json", map[string]any{"skipDangerousModePermissionPrompt": true})
}

func (p *place) writeSettings(name string, v any) {
	b, _ := json.Marshal(v)
	must(p.t, os.WriteFile(filepath.Join(p.hooks, name), b, 0o600))
}

// makeOrigin makes the repository's remote, with a README and the
// repository's own .claude settings: a Stop hook that writes a line to a
// file outside the checkout.
func (p *place) makeOrigin() {
	seed := filepath.Join(filepath.Dir(p.origin), "seed")
	must(p.t, os.MkdirAll(filepath.Join(seed, ".claude"), 0o700))
	must(p.t, os.WriteFile(filepath.Join(seed, "README.md"), []byte("# A test repository\n"), 0o600))
	b, _ := json.Marshal(map[string]any{"hooks": map[string]any{"Stop": []any{map[string]any{"hooks": []any{
		map[string]any{"type": "command", "command": "echo repo-hook >> " + filepath.Join(p.hooks, "repo-hook.log")}}}}}})
	must(p.t, os.WriteFile(filepath.Join(seed, ".claude", "settings.json"), b, 0o600))
	p.git(seed, "init", "-q", "-b", "main")
	p.git(seed, "add", ".")
	p.git(seed, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-q", "-m", "First commit")
	p.git("", "clone", "-q", "--bare", seed, p.origin)
}

func (p *place) clone() {
	must(p.t, os.MkdirAll(filepath.Dir(p.checkout), 0o700))
	p.git("", "clone", "-q", p.origin, p.checkout)
}

func (p *place) git(dir string, args ...string) {
	p.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		p.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// paths gives the replacements for this place's local paths in a saved
// stream. The report command reads as the factory's report command.
func (p *place) paths() map[string]string {
	return map[string]string{
		filepath.Join(p.hooks, "report"): "solidtrace report",
		p.hooks:                          "/workspace/hooks",
		p.checkout:                       "/workspace/repo",
		p.config:                         "/workspace/config",
		p.home:                           "/workspace/home",
		filepath.Dir(p.origin):           "/workspace",
		// Claude Code writes a background task's output under the
		// temporary directory, in a folder named after the checkout.
		filepath.Join(os.TempDir(), fmt.Sprintf("claude-%d", os.Getuid()), fakeharness.ProjectDirName(p.checkout)): "/tmp/claude-1000/-workspace-repo",
	}
}

func (p *place) hookLog(name string) []string {
	b, _ := os.ReadFile(filepath.Join(p.hooks, name))
	return strings.Fields(strings.TrimSpace(string(b)))
}

// proc is one Claude Code process, driven as the wrapper drives it.
type proc struct {
	t       *testing.T
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	start   time.Time
	stderr  bytes.Buffer
	ch      chan harnesscontract.Event
	mu      sync.Mutex
	events  []harnesscontract.Event
	sent    map[string]bool
	ints    map[string]bool
	closed  bool
	exit    int
	session string
	place   *place
}

type opts struct {
	session, resume string
	stopHook        bool
	// env is added last to the environment of Claude Code.
	env []string
}

func (p *place) run(o opts) *proc {
	p.t.Helper()
	settings := "no-hook.json"
	if o.stopHook {
		settings = "settings.json"
	}
	args := []string{"--model", p.model, "--permission-mode", "bypassPermissions", "--strict-mcp-config",
		"--setting-sources", "project,local", "--disallowedTools", "AskUserQuestion",
		"--settings", filepath.Join(p.hooks, settings),
		"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages"}
	session := o.session
	if o.resume != "" {
		args = append(args, "--resume", o.resume)
		session = o.resume
	} else {
		args = append(args, "--session-id", o.session)
	}
	p.sessions = append(p.sessions, session)
	c := &proc{t: p.t, place: p, session: session, ch: make(chan harnesscontract.Event, 1000),
		sent: map[string]bool{}, ints: map[string]bool{}}
	c.cmd = exec.Command(p.program, args...)
	c.cmd.Dir = p.checkout
	// Claude Code 2.1.325 reads CLAUDE_CODE_PROJECT_DIR_NAME only when
	// CLAUDE_CONFIG_DIR is set, so the check sets both, as the wrapper does.
	c.cmd.Env = append(append(childEnv(), "CLAUDE_CONFIG_DIR="+p.config, "CLAUDE_CODE_PROJECT_DIR_NAME="+p.project), o.env...)
	c.cmd.Stderr = &c.stderr
	c.stdin, _ = c.cmd.StdinPipe()
	stdout, _ := c.cmd.StdoutPipe()
	c.start = time.Now()
	must(p.t, c.cmd.Start())
	p.t.Cleanup(func() {
		if c.cmd.ProcessState == nil {
			c.cmd.Process.Kill()
			c.cmd.Wait()
		}
	})
	// The cost is added at the end of the test, also after a Fatal or a
	// kill, from the last total_cost_usd that the process gave.
	p.t.Cleanup(c.addCost)
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 64<<20)
		for sc.Scan() {
			var e map[string]any
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				p.t.Errorf("a stdout line is not JSON: %q", sc.Text())
				continue
			}
			ev := harnesscontract.Event{At: time.Since(c.start), Data: e}
			c.mu.Lock()
			c.events = append(c.events, ev)
			c.mu.Unlock()
			c.ch <- ev
		}
		close(c.ch)
	}()
	return c
}

// childEnv is this process's environment with no variable of a Claude Code
// session around it, so that the check runs as a fresh headless process.
// It keeps the login and the provider: CLAUDE_CODE_OAUTH_TOKEN (the
// `claude setup-token` login that an Execution uses) and CLAUDE_CODE_USE_*.
func childEnv() []string { return sessionFree(os.Environ()) }

func sessionFree(environ []string) []string {
	var env []string
	for _, kv := range environ {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "CLAUDE_CODE_OAUTH_TOKEN", strings.HasPrefix(k, "CLAUDE_CODE_USE_"):
		case k == "CLAUDECODE", k == "CLAUDE_PID", k == "CLAUDE_EFFORT", k == "CLAUDE_CONFIG_DIR",
			strings.HasPrefix(k, "CLAUDE_CODE_"):
			continue
		}
		env = append(env, kv)
	}
	return env
}

func (c *proc) send(v map[string]any) {
	c.t.Helper()
	b, _ := json.Marshal(v)
	if _, err := c.stdin.Write(append(b, '\n')); err != nil {
		c.t.Fatal(err)
	}
}

// message writes a user message with this uuid, and gives the time.
func (c *proc) message(uuid, text string) time.Duration {
	c.t.Helper()
	c.sent[uuid] = true
	c.send(map[string]any{"type": "user", "uuid": uuid, "session_id": "", "parent_tool_use_id": nil,
		"message": map[string]any{"role": "user", "content": text}})
	return time.Since(c.start)
}

func (c *proc) interrupt(id string) time.Duration {
	c.t.Helper()
	c.ints[id] = true
	c.send(map[string]any{"type": "control_request", "request_id": id, "request": map[string]any{"subtype": "interrupt"}})
	return time.Since(c.start)
}

// until reads events up to the first that matches.
func (c *proc) until(what string, match func(map[string]any) bool, limit time.Duration) harnesscontract.Event {
	c.t.Helper()
	deadline := time.After(limit)
	for {
		select {
		case ev, ok := <-c.ch:
			if !ok {
				c.t.Fatalf("stdout closed before %s; events %v; stderr %q", what, c.all(), c.stderr.String())
			}
			if match(ev.Data) {
				return ev
			}
		case <-deadline:
			c.t.Fatalf("no %s in %v; events %v", what, limit, c.all())
		}
	}
}

// quiet reads events until none comes for d.
func (c *proc) quiet(d time.Duration) {
	for {
		select {
		case _, ok := <-c.ch:
			if !ok {
				return
			}
		case <-time.After(d):
			return
		}
	}
}

// end closes stdin and gives the exit code.
func (c *proc) end() int {
	c.t.Helper()
	c.closed = true
	c.stdin.Close()
	done := make(chan struct{})
	go func() {
		for range c.ch {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		c.t.Fatalf("Claude Code did not exit in 60 s after stdin closed; events %v", c.all())
	}
	err := c.cmd.Wait()
	if x, ok := err.(*exec.ExitError); ok {
		c.exit = x.ExitCode()
	} else if err != nil {
		c.t.Fatal(err)
	}
	return c.exit
}

// processCost is the last total_cost_usd in the events: Claude Code gives
// it for the whole process up to that result.
func processCost(events []harnesscontract.Event) float64 {
	var cost float64
	for _, ev := range events {
		if ev.Data["type"] == "result" {
			if v, ok := ev.Data["total_cost_usd"].(float64); ok {
				cost = v
			}
		}
	}
	return cost
}

func (c *proc) all() []harnesscontract.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]harnesscontract.Event(nil), c.events...)
}

// addCost adds the cost of the process: the total_cost_usd of its last
// result, which Claude Code gives for the whole process.
func (c *proc) addCost() {
	cost := processCost(c.all())
	c.t.Logf("this process cost %.4f US dollars", cost)
	costMu.Lock()
	totalCost += cost
	costMu.Unlock()
}

// save keeps the process's stream for the scripted Harness when the check
// passes.
func (c *proc) save(name, notes string) {
	c.t.Cleanup(func() {
		if c.t.Failed() {
			c.t.Logf("the check failed: the stream %s is not saved", name)
			return
		}
		rec := harnesscontract.Recording{Name: name, Notes: notes, Version: fakeharness.Version, Model: c.place.model,
			Paths: c.place.paths(), Sent: c.sent, Interrupts: c.ints, Events: c.all(),
			StdinClosed: c.closed, ExitCode: c.exit}
		script, err := harnesscontract.Script(rec)
		if err != nil {
			c.t.Errorf("stream %s: %v", name, err)
			return
		}
		file := filepath.Join("..", "fakeharness", "streams", name+".jsonl")
		if err := os.WriteFile(file, script, 0o644); err != nil {
			c.t.Errorf("stream %s: %v", name, err)
			return
		}
		c.t.Logf("saved the stream %s", file)
	})
}

// transcript reads the Agent session's .jsonl.
func (c *proc) transcript() []map[string]any {
	c.t.Helper()
	file := filepath.Join(c.place.transcriptDir(), c.session+".jsonl")
	b, err := os.ReadFile(file)
	if err != nil {
		c.t.Fatalf("no transcript where CLAUDE_CODE_PROJECT_DIR_NAME puts it: %v", err)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var o map[string]any
		if err := json.Unmarshal([]byte(l), &o); err != nil {
			c.t.Fatalf("a transcript line is not JSON: %q", l)
		}
		lines = append(lines, o)
	}
	return lines
}

func is(typ, subtype string) func(map[string]any) bool {
	return func(e map[string]any) bool { return e["type"] == typ && (subtype == "" || e["subtype"] == subtype) }
}

func echoOf(uuid string) func(map[string]any) bool {
	return func(e map[string]any) bool { return e["type"] == "user" && e["uuid"] == uuid && e["isReplay"] == true }
}

// toolUse gives the first tool_use block of an assistant event, or nil.
func toolUse(e map[string]any) map[string]any {
	if e["type"] != "assistant" {
		return nil
	}
	msg, _ := e["message"].(map[string]any)
	content, _ := msg["content"].([]any)
	for _, b := range content {
		if b, _ := b.(map[string]any); b["type"] == "tool_use" {
			return b
		}
	}
	return nil
}

func hasToolUse(e map[string]any) bool { return toolUse(e) != nil }

func toolResultFor(id string) func(map[string]any) bool {
	return func(e map[string]any) bool {
		msg, _ := e["message"].(map[string]any)
		content, _ := msg["content"].([]any)
		for _, b := range content {
			if b, _ := b.(map[string]any); b["type"] == "tool_result" && b["tool_use_id"] == id {
				return true
			}
		}
		return false
	}
}

func index(events []harnesscontract.Event, match func(map[string]any) bool) int {
	for i, ev := range events {
		if match(ev.Data) {
			return i
		}
	}
	return -1
}

func count(events []harnesscontract.Event, match func(map[string]any) bool) int {
	n := 0
	for _, ev := range events {
		if match(ev.Data) {
			n++
		}
	}
	return n
}

func text(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// --- The checks ---

// The init event of a headless Execution in bypass mode: the permission
// mode, and the tools without AskUserQuestion (--disallowedTools).
// The first message of an Execution: its replay echo and its user line in
// the .jsonl carry the wrapper's uuid. The assistant lines of one reply
// share one message id.
func TestContractTurnWithToolCall(t *testing.T) {
	p := contract(t)
	c := p.run(opts{session: newUUID()})
	c.save("turn-with-tool-call", "One message. The agent runs one Bash tool call (git status) and replies; one result. No Stop hook.")
	first := newUUID()
	// The sentence before the tool call asks for a reply of several lines:
	// a text block and a tool_use block with one message id.
	c.message(first, "First write one short sentence that says what you will run. Then use Bash to run `git status --short --branch` in this directory. Then reply in one sentence with the branch name.")
	init := c.until("the init event", is("system", "init"), 90*time.Second)
	c.until("the echo of the first message", echoOf(first), 90*time.Second)
	result := c.until("the result", is("result", ""), 120*time.Second)
	if code := c.end(); code != 0 {
		t.Errorf("exit %d after stdin closed; want 0; stderr %q", code, c.stderr.String())
	}

	if init.Data["permissionMode"] != "bypassPermissions" {
		t.Errorf("permissionMode %v; want bypassPermissions", init.Data["permissionMode"])
	}
	tools := text(init.Data["tools"])
	if strings.Contains(tools, `"AskUserQuestion"`) {
		t.Errorf("AskUserQuestion is in the tools of a headless Execution: %s", tools)
	}
	if !strings.Contains(tools, `"Bash"`) {
		t.Errorf("no Bash in the tools: %s", tools)
	}
	if result.Data["subtype"] != "success" || !strings.Contains(text(result.Data["result"]), "main") {
		t.Errorf("result %s; want a success that names the branch main", text(result.Data))
	}
	if count(c.all(), hasToolUse) == 0 {
		t.Errorf("no tool call; events %v", c.all())
	}

	lines := c.transcript()
	firstLine := -1
	for i, l := range lines {
		if l["type"] == "user" && l["uuid"] == first {
			firstLine = i
		}
	}
	if firstLine < 0 {
		t.Errorf("no user line with the wrapper's uuid %s in the .jsonl: %s", first, typesOfLines(lines))
	}

	c.checkReplies(lines)
}

// checkReplies checks the lines of each reply in the .jsonl: each assistant
// event and each assistant line has a message id, the stream and the
// .jsonl have the same ids, and the lines of one id have the same usage,
// so that a parse that counts each message id one time (on its last line)
// counts the reply one time.
func (c *proc) checkReplies(lines []map[string]any) {
	c.t.Helper()
	problems, several := replyProblems(c.all(), lines)
	for _, p := range problems {
		c.t.Error(p)
	}
	if several {
		costMu.Lock()
		severalLines = true
		costMu.Unlock()
	}
}

// replyProblems gives what is wrong with the replies of a stream and its
// .jsonl, and whether a reply has several lines in the .jsonl.
func replyProblems(events []harnesscontract.Event, lines []map[string]any) (problems []string, several bool) {
	streamIDs := map[string]int{}
	for _, ev := range events {
		if ev.Data["type"] == "assistant" {
			msg, _ := ev.Data["message"].(map[string]any)
			streamIDs[text(msg["id"])]++
		}
	}
	fileIDs := map[string][]string{}
	for _, l := range lines {
		if l["type"] == "assistant" {
			msg, _ := l["message"].(map[string]any)
			id := text(msg["id"])
			fileIDs[id] = append(fileIDs[id], text(msg["usage"]))
		}
	}
	if len(streamIDs) == 0 || streamIDs["null"] > 0 || len(fileIDs["null"]) > 0 {
		problems = append(problems, fmt.Sprintf("an assistant event or line has no message id: stream %v, .jsonl %v", streamIDs, fileIDs))
	}
	for id := range streamIDs {
		if _, ok := fileIDs[id]; !ok {
			problems = append(problems, fmt.Sprintf("the reply %s of the stream is not in the .jsonl", id))
		}
	}
	for id, usages := range fileIDs {
		if len(usages) > 1 {
			several = true
		}
		for _, u := range usages[1:] {
			if u != usages[0] {
				problems = append(problems, fmt.Sprintf("the lines of the reply %s have different usage, so the last line does not give the tokens of the reply: %v", id, usages))
				break
			}
		}
	}
	return problems, several
}

func typesOfLines(lines []map[string]any) string {
	var out []string
	for _, l := range lines {
		s, _ := l["type"].(string)
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// The Stop hook in stream-json: in each turn it refuses one time, the
// agent runs the report command that its reason names, and the next stop
// comes with stop_hook_active and is allowed. Its feedback is a user event
// with isSynthetic, not a replay. A hook from the repository's .claude
// settings runs beside it. A message between turns starts a new turn in
// the same process, and its user line in the .jsonl has the wrapper's uuid.
func TestContractStopHook(t *testing.T) {
	p := contract(t)
	c := p.run(opts{session: newUUID(), stopHook: true})
	c.save("stop-hook-refuses-once", "Two turns, the second from a message between turns. In each, the Stop hook refuses the first stop, the agent runs the report command, and the turn ends with a result. The repository's own Stop hook runs too.")
	first, second := newUUID(), newUUID()
	c.message(first, "Reply with the word APPLE.")
	c.until("the echo of the first message", echoOf(first), 90*time.Second)
	c.until("the first result", is("result", ""), 120*time.Second)
	c.message(second, "Reply with the word BANANA.")
	c.until("the echo of the second message", echoOf(second), 60*time.Second)
	c.until("the second result", is("result", ""), 120*time.Second)
	if code := c.end(); code != 0 {
		t.Errorf("exit %d after stdin closed; want 0; stderr %q", code, c.stderr.String())
	}

	events := c.all()
	synthetic := func(e map[string]any) bool {
		return e["type"] == "user" && e["isSynthetic"] == true && strings.Contains(text(e["message"]), "report done")
	}
	if n := count(events, synthetic); n != 2 {
		t.Errorf("%d Stop hook feedback events; want 2 (one refusal in each turn); events %v", n, events)
	}
	for _, ev := range events {
		if synthetic(ev.Data) && ev.Data["isReplay"] == true {
			t.Errorf("a Stop hook feedback event is a replay: %s", text(ev.Data))
		}
	}
	if n := count(events, is("result", "")); n != 2 {
		t.Errorf("%d results; want 2", n)
	}
	if reports := p.hookLog("reports.log"); len(reports) == 0 {
		t.Errorf("the agent did not run the report command")
	}
	hookLog, _ := os.ReadFile(filepath.Join(p.hooks, "stop-hook.log"))
	if n := strings.Count(string(hookLog), `"stop_hook_active":true`); n < 2 {
		t.Errorf("%d stops with stop_hook_active; want 2; the hook got:\n%s", n, hookLog)
	}
	if n := len(p.hookLog("repo-hook.log")); n < 2 {
		t.Errorf("the repository's own Stop hook ran %d times; want one time at each stop", n)
	}

	lines := c.transcript()
	c.checkReplies(lines)
	var turnStart, meta int
	for _, l := range lines {
		if l["type"] == "user" && l["uuid"] == second {
			turnStart++
		}
		if l["isMeta"] == true && strings.Contains(text(l["message"]), "report done") {
			meta++
		}
	}
	if turnStart != 1 {
		t.Errorf("%d user lines with the uuid of the message between turns; want 1: %s", turnStart, typesOfLines(lines))
	}
	if meta == 0 {
		t.Errorf("no Stop hook feedback line with isMeta in the .jsonl")
	}
}

// A message during a tool call: Claude Code delivers it when the tool call
// ends, inside the same turn. Its replay echo comes at delivery, not at
// the write. In the .jsonl it is the three mid-turn lines with the
// wrapper's uuid, not a user line.
func TestContractMessageDuringToolCall(t *testing.T) {
	p := contract(t)
	c := p.run(opts{session: newUUID(), stopHook: true})
	c.save("message-during-tool-call", "A second message comes during a 12-second tool call. Claude Code delivers it when the tool call ends: its replay echo comes then, not at the write. Then the Stop hook refuses one time, the agent reports, and the turn ends with one result for the two messages.")
	first, second := newUUID(), newUUID()
	c.message(first, "Use Bash to run exactly `sleep 12 && echo done-sleeping` in the foreground. Then reply with the word APPLE, and with each other word that I ask for.")
	call := c.until("the tool call", hasToolUse, 120*time.Second)
	time.Sleep(2 * time.Second)
	wrote := c.message(second, "Also reply with the word BANANA.")
	echo := c.until("the echo of the second message", echoOf(second), 60*time.Second)
	c.until("the result", is("result", ""), 120*time.Second)
	c.quiet(3 * time.Second)
	if code := c.end(); code != 0 {
		t.Errorf("exit %d after stdin closed; want 0; stderr %q", code, c.stderr.String())
	}

	events := c.all()
	id, _ := toolUse(call.Data)["id"].(string)
	resultAt := index(events, toolResultFor(id))
	echoAt := index(events, echoOf(second))
	if resultAt < 0 || echoAt < resultAt {
		t.Errorf("the echo (event %d) does not come after the tool result (event %d)", echoAt, resultAt)
	}
	if gap := echo.At - wrote; gap < 5*time.Second {
		t.Errorf("the echo came %v after the write; want it at delivery, when the 12-second tool call ends", gap)
	}
	if n := count(events, is("result", "")); n != 1 {
		t.Errorf("%d results; want one for the two messages", n)
	}
	// The echo of the second message holds the word BANANA. So look for it
	// only in the replies after the echo and in the result.
	if !answered(events, echoAt, "BANANA") {
		t.Errorf("the agent did not answer the second message in the same turn")
	}

	lines := c.transcript()
	c.checkReplies(lines)
	var enqueue, attachment, remove, userLine bool
	for _, l := range lines {
		switch {
		case l["type"] == "queue-operation" && l["operation"] == "enqueue":
			enqueue = true
		case l["type"] == "attachment":
			a, _ := l["attachment"].(map[string]any)
			if a["type"] == "queued_command" && a["source_uuid"] == second {
				attachment = true
			}
		case l["type"] == "queue-operation" && l["operation"] == "remove":
			if l["reason"] == "absorbed_mid_turn" && l["commandUuid"] == second {
				remove = true
			}
		case l["type"] == "user" && l["uuid"] == second:
			userLine = true
		}
	}
	if !enqueue || !attachment || !remove {
		t.Errorf("the mid-turn lines: enqueue %v, queued_command with source_uuid %v, remove absorbed_mid_turn with commandUuid %v; want all", enqueue, attachment, remove)
	}
	if userLine {
		t.Errorf("a message during a tool call has a plain user line with the wrapper's uuid; the parse expects the mid-turn lines")
	}
}

// The interrupt control request during a foreground tool call: a
// control_response at once, a result with error_during_execution, no Stop
// hook, the tool's child process stopped, and exit code 1 at stdin close.
func TestContractInterrupt(t *testing.T) {
	p := contract(t)
	c := p.run(opts{session: newUUID(), stopHook: true})
	c.save("interrupt", "The interrupt control request during a foreground tool call. Claude Code answers the control request at once, then gives a result with the subtype error_during_execution. The Stop hook does not run. When stdin closes, Claude Code exits with code 1, not 0.")
	mark := "contract" + newUUID()[:8]
	c.message(newUUID(), fmt.Sprintf("Use Bash to run exactly this command, in the foreground: python3 -c \"import time; [time.sleep(1) for _ in range(40)]; print('finished')\" %s . Then reply with the word APPLE.", mark))
	call := c.until("the tool call", hasToolUse, 120*time.Second)
	if !strings.Contains(text(toolUse(call.Data)["input"]), mark) {
		t.Fatalf("the agent did not run the command as asked: %s", text(toolUse(call.Data)))
	}
	time.Sleep(4 * time.Second)
	sent := c.interrupt("interrupt-1")
	resp := c.until("the control response", is("control_response", ""), 10*time.Second)
	result := c.until("the result", is("result", ""), 10*time.Second)
	time.Sleep(2 * time.Second)
	left, _ := exec.Command("pgrep", "-f", mark).Output()
	code := c.end()

	r, _ := resp.Data["response"].(map[string]any)
	if r["subtype"] != "success" || r["request_id"] != "interrupt-1" {
		t.Errorf("control response %s; want success for interrupt-1", text(resp.Data))
	}
	if result.Data["subtype"] != "error_during_execution" {
		t.Errorf("result subtype %v; want error_during_execution", result.Data["subtype"])
	}
	if gap := result.At - sent; gap > 3*time.Second {
		t.Errorf("the result came %v after the interrupt; want at once", gap)
	}
	if log := p.hookLog("stop-hook.log"); len(log) > 0 {
		t.Errorf("the Stop hook ran after the interrupt: %v", log)
	}
	if len(bytes.TrimSpace(left)) > 0 {
		t.Errorf("the tool's child process still runs after the interrupt: pids %s", left)
	}
	if code != 1 {
		t.Errorf("exit %d after stdin closed; want 1", code)
	}
}

// A background task: the turn ends with a result while the task still
// runs, and Claude Code gives the task events on the stream. When the task
// ends, Claude Code starts a new turn by itself, with no message, and that
// turn ends with a result of its own.
func TestContractBackgroundTask(t *testing.T) {
	p := contract(t)
	c := p.run(opts{session: newUUID()})
	c.save("background-task", "The agent starts a 20-second command as a background task. The turn ends with a result while the task still runs. When the task ends, Claude Code gives the task events and starts a new turn by itself, with no message: init, an assistant reply and a second result.")
	c.message(newUUID(), "Use Bash with run_in_background set to true to run exactly `sleep 20 && echo finished`. Do not wait for it and do not check it. Then reply with the word STARTED.")
	result := c.until("the result", is("result", ""), 120*time.Second)
	c.quiet(30 * time.Second)
	code := c.end()

	events := c.all()
	resultAt := index(events, func(e map[string]any) bool { return e["type"] == "result" })
	started := index(events, is("system", "task_started"))
	if started < 0 || started > resultAt {
		t.Errorf("no task_started before the result; events %v", events)
	}
	var background bool
	for _, ev := range events[:resultAt] {
		if tu := toolUse(ev.Data); tu != nil && strings.Contains(text(tu["input"]), `"run_in_background":true`) {
			background = true
		}
	}
	if !background {
		t.Errorf("the agent did not start a background task")
	}
	if result.Data["subtype"] != "success" {
		t.Errorf("result %s; want success", text(result.Data))
	}
	ended := index(events, is("system", "task_notification"))
	if ended < resultAt {
		t.Errorf("no task_notification after the result; events %v", events)
	} else if n := count(events[ended:], is("result", "")); n != 1 {
		t.Errorf("%d results after the task ended; want 1, from the turn that Claude Code starts by itself", n)
	}
	// The wrapper's Stop hook counts the running background tasks from the
	// tasks list of background_tasks_changed.
	if started >= 0 && ended >= 0 {
		task, _ := events[started].Data["task_id"].(string)
		if !runningTasks(events[:resultAt], task, 1) {
			t.Errorf("no background_tasks_changed with the one running task %s before the result; events %v", task, events[:resultAt])
		}
		if !runningTasks(events[ended:], "", 0) {
			t.Errorf("no background_tasks_changed with no running task after task_notification; events %v", events[ended:])
		}
	}
	t.Logf("events: %v; exit %d", events, code)
}

// runningTasks is true when a background_tasks_changed event in the events
// gives n running tasks, the task among them when n > 0.
func runningTasks(events []harnesscontract.Event, task string, n int) bool {
	for _, ev := range events {
		if !is("system", "background_tasks_changed")(ev.Data) {
			continue
		}
		tasks, ok := ev.Data["tasks"].([]any)
		if !ok || len(tasks) != n {
			continue
		}
		if n == 0 {
			return true
		}
		for _, x := range tasks {
			if x, _ := x.(map[string]any); x["task_id"] == task {
				return true
			}
		}
	}
	return false
}

// A resume from a restored directory, and a resume with no tool-results.
// Execution 1 learns a code word. Then each trace of it under the config
// directory goes away, the checkout is cloned again at the same path, and
// only the .jsonl and the <session id>/ directory come back. Execution 2
// resumes it, knows the code word, and makes a large tool output that
// goes to tool-results/. That folder is deleted, and Execution 3 resumes
// again. The first message of each Execution has the wrapper's uuid in
// the .jsonl.
func TestContractResume(t *testing.T) {
	p := contract(t)
	session := newUUID()
	word := fmt.Sprintf("ZEBRA-%d", time.Now().UnixNano()%9000+1000)

	c1 := p.run(opts{session: session})
	c1.message(newUUID(), "Remember this code word: "+word+". Reply with the word OK only.")
	c1.until("the first result", is("result", ""), 120*time.Second)
	if code := c1.end(); code != 0 {
		t.Fatalf("Execution 1: exit %d; stderr %q", code, c1.stderr.String())
	}

	// Move the Agent session away, as the end of an Execution on a VM does.
	aside := t.TempDir()
	jsonl := filepath.Join(p.transcriptDir(), session+".jsonl")
	dir := filepath.Join(p.transcriptDir(), session)
	must(t, os.Rename(p.transcriptDir(), filepath.Join(aside, "project")))
	for _, d := range []string{"session-env", "file-history", "tasks"} {
		os.RemoveAll(filepath.Join(p.config, d, session))
	}
	must(t, os.RemoveAll(p.checkout))
	p.clone()
	// Restore only the .jsonl and the <session id>/ directory, as a download
	// from SolidTrace does.
	must(t, os.MkdirAll(p.transcriptDir(), 0o700))
	must(t, os.Rename(filepath.Join(aside, "project", session+".jsonl"), jsonl))
	if _, err := os.Stat(filepath.Join(aside, "project", session)); err == nil {
		must(t, os.Rename(filepath.Join(aside, "project", session), dir))
	}

	c2 := p.run(opts{resume: session})
	m2 := newUUID()
	c2.message(m2, "First, what was the code word? Then use Bash to run exactly `seq 1 20000` and say only its last number.")
	r2 := c2.until("the result of Execution 2", is("result", ""), 120*time.Second)
	if code := c2.end(); code != 0 {
		t.Fatalf("Execution 2: exit %d; stderr %q", code, c2.stderr.String())
	}
	if r2.Data["session_id"] != session {
		t.Errorf("Execution 2 has the session id %v; want %s", r2.Data["session_id"], session)
	}
	if !strings.Contains(text(c2.all()), word) {
		t.Errorf("Execution 2 does not know the code word %s from the restored directory: %s", word, text(r2.Data["result"]))
	}
	lines2 := c2.transcript()
	c2.checkReplies(lines2)
	if !hasUserLine(lines2, m2) {
		t.Errorf("no user line with the uuid of the first message of Execution 2")
	}
	results, _ := os.ReadDir(filepath.Join(dir, "tool-results"))
	if len(results) == 0 {
		t.Errorf("the large tool output did not go to %s/tool-results", session)
	}
	must(t, os.RemoveAll(filepath.Join(dir, "tool-results")))

	c3 := p.run(opts{resume: session})
	m3 := newUUID()
	c3.message(m3, "What was the code word? Reply with it only.")
	r3 := c3.until("the result of Execution 3", is("result", ""), 120*time.Second)
	if code := c3.end(); code != 0 {
		t.Fatalf("Execution 3, with no tool-results: exit %d; stderr %q", code, c3.stderr.String())
	}
	if r3.Data["subtype"] != "success" || !strings.Contains(text(r3.Data["result"]), word) {
		t.Errorf("Execution 3 result %s; want a success with the code word %s", text(r3.Data), word)
	}
	lines3 := c3.transcript()
	c3.checkReplies(lines3)
	if !hasUserLine(lines3, m3) {
		t.Errorf("no user line with the uuid of the first message of Execution 3")
	}
}

// answered is true when an assistant reply after the event at, or the
// result after it, holds the word.
func answered(events []harnesscontract.Event, at int, word string) bool {
	if at < 0 {
		return false
	}
	for _, ev := range events[at+1:] {
		switch ev.Data["type"] {
		case "assistant":
			msg, _ := ev.Data["message"].(map[string]any)
			if strings.Contains(text(msg["content"]), word) {
				return true
			}
		case "result":
			if strings.Contains(text(ev.Data["result"]), word) {
				return true
			}
		}
	}
	return false
}

func hasUserLine(lines []map[string]any, uuid string) bool {
	for _, l := range lines {
		if l["type"] == "user" && l["uuid"] == uuid {
			return true
		}
	}
	return false
}

// The subprocess environment scrub, which the wrapper turns off
// (wrapper.EnvSubprocessScrub=0). With it on, Claude Code forces the
// permission mode to default, so a headless Execution cannot run Bash: no
// check and no report. With it off, as the wrapper starts the harness, a
// Bash call has the agent's socket and its evidence/ folder: it writes to
// evidence/ and sends a report through the socket. When a new version no
// longer forces the default mode, decide the scrub again.
func TestContractSubprocessEnvScrub(t *testing.T) {
	t.Run("on", func(t *testing.T) {
		p := contract(t)
		c := p.run(opts{session: newUUID(), env: []string{wrapper.EnvSubprocessScrub + "=1"}})
		c.message(newUUID(), "Reply with the word OK.")
		init := c.until("the init event", is("system", "init"), 90*time.Second)
		c.until("the result", is("result", ""), 120*time.Second)
		c.end()
		if init.Data["permissionMode"] != "default" {
			t.Errorf("with the scrub on, permissionMode %v; Claude Code 2.1.325 forces default. "+
				"The scrub changed: decide again if the wrapper turns it off (wrapper.EnvSubprocessScrub)", init.Data["permissionMode"])
		}
	})

	t.Run("off, as the wrapper starts the harness", func(t *testing.T) {
		p := contract(t)
		sockets, err := os.MkdirTemp("", "scrub-")
		must(t, err)
		t.Cleanup(func() { os.RemoveAll(sockets) })
		socket := filepath.Join(sockets, "agent.sock")
		listener, err := net.Listen("unix", socket)
		must(t, err)
		reports := make(chan string, 4)
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			reports <- r.Method + " " + r.URL.Path + " " + string(body)
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"ok":true}`)
		})}
		go server.Serve(listener)
		t.Cleanup(func() { server.Close() })
		evidence := filepath.Join(p.transcriptDir(), "evidence")
		must(t, os.MkdirAll(evidence, 0o755))

		c := p.run(opts{session: newUUID(), env: []string{
			agentsocket.Env + "=" + socket, agentsocket.EvidenceEnv + "=" + evidence,
			// The machine's environment turns the scrub on; the wrapper's
			// value comes last.
			wrapper.EnvSubprocessScrub + "=1", wrapper.EnvSubprocessScrub + "=0"}})
		c.message(newUUID(), "Use Bash to run this one command exactly as written, then reply with the word DONE: "+
			"env >\"$"+agentsocket.EvidenceEnv+"/env.txt\" && curl -sS --unix-socket \"$"+agentsocket.Env+"\" "+
			"-H 'Content-Type: application/json' -d '{\"outcome\":\"done\",\"summary\":\"scrub\"}' "+
			"http://agent"+agentsocket.ReportPath)
		init := c.until("the init event", is("system", "init"), 90*time.Second)
		c.until("the result", is("result", ""), 120*time.Second)
		if code := c.end(); code != 0 {
			t.Errorf("exit %d after stdin closed; want 0; stderr %q", code, c.stderr.String())
		}

		if init.Data["permissionMode"] != "bypassPermissions" {
			t.Errorf("permissionMode %v; want bypassPermissions", init.Data["permissionMode"])
		}
		select {
		case got := <-reports:
			if !strings.HasPrefix(got, "POST "+agentsocket.ReportPath+" ") || !strings.Contains(got, `"scrub"`) {
				t.Errorf("the socket got %q", got)
			}
		default:
			t.Errorf("no report reached the agent's socket; events %v", c.all())
		}
		env, err := os.ReadFile(filepath.Join(evidence, "env.txt"))
		if err != nil {
			t.Fatalf("the Bash call wrote nothing in evidence/: %v", err)
		}
		for _, want := range []string{agentsocket.Env + "=" + socket, agentsocket.EvidenceEnv + "=" + evidence} {
			if !strings.Contains("\n"+string(env), "\n"+want+"\n") {
				t.Errorf("the Bash call has no %s", want)
			}
		}
	})
}
