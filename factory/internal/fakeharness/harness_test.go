package fakeharness_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/fakeharness"
)

func TestMain(m *testing.M) {
	fakeharness.MainIfAsked()
	os.Exit(m.Run())
}

// session is one harness process, driven as the wrapper drives Claude Code.
type session struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan map[string]any
	stderr strings.Builder
	config string
	cwd    string
}

var headless = []string{"-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
	"--replay-user-messages", "--permission-mode", "bypassPermissions", "--disallowedTools", "AskUserQuestion"}

func start(t *testing.T, stream, config string, args ...string) *session {
	t.Helper()
	program, env := fakeharness.Command(stream)
	s := &session{t: t, config: config, cwd: t.TempDir(), events: make(chan map[string]any, 100)}
	s.cmd = exec.Command(program, append(append([]string{}, headless...), args...)...)
	s.cmd.Dir = s.cwd
	s.cmd.Env = append(os.Environ(), append(env, "CLAUDE_CONFIG_DIR="+config, "CLAUDE_CODE_PROJECT_DIR_NAME=-workspace-repo")...)
	s.cmd.Stderr = &s.stderr
	s.stdin, _ = s.cmd.StdinPipe()
	stdout, _ := s.cmd.StdoutPipe()
	if err := s.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.cmd.Process.Kill(); s.cmd.Wait() })
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			var e map[string]any
			if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
				t.Errorf("a stdout line is not JSON: %q", sc.Text())
			}
			s.events <- e
		}
		close(s.events)
	}()
	return s
}

func (s *session) send(v map[string]any) {
	s.t.Helper()
	b, _ := json.Marshal(v)
	if _, err := s.stdin.Write(append(b, '\n')); err != nil {
		s.t.Fatal(err)
	}
}

func (s *session) message(uuid, text string) {
	s.send(map[string]any{"type": "user", "uuid": uuid, "message": map[string]any{"role": "user", "content": text}})
}

// until reads events up to the first that match, and gives the events read.
func (s *session) until(match func(map[string]any) bool) []map[string]any {
	s.t.Helper()
	var seen []map[string]any
	deadline := time.After(10 * time.Second)
	for {
		select {
		case e, ok := <-s.events:
			if !ok {
				s.t.Fatalf("stdout closed before the event; read %v; stderr %q", types(seen), s.stderr.String())
			}
			seen = append(seen, e)
			if match(e) {
				return seen
			}
		case <-deadline:
			s.t.Fatalf("no such event in 10 s; read %v", types(seen))
		}
	}
}

// end closes stdin and gives the exit code.
func (s *session) end() int {
	s.t.Helper()
	s.stdin.Close()
	for range s.events {
	}
	err := s.cmd.Wait()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	if err != nil {
		s.t.Fatal(err)
	}
	return 0
}

func (s *session) transcript(sessionID string) []map[string]any {
	s.t.Helper()
	b, err := os.ReadFile(filepath.Join(s.config, "projects", "-workspace-repo", sessionID+".jsonl"))
	if err != nil {
		s.t.Fatal(err)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var o map[string]any
		if err := json.Unmarshal([]byte(l), &o); err != nil {
			s.t.Fatalf("a transcript line is not JSON: %q", l)
		}
		lines = append(lines, o)
	}
	return lines
}

func is(typ, subtype string) func(map[string]any) bool {
	return func(e map[string]any) bool { return e["type"] == typ && (subtype == "" || e["subtype"] == subtype) }
}

func echoOf(uuid string) func(map[string]any) bool {
	return func(e map[string]any) bool { return e["type"] == "user" && e["uuid"] == uuid }
}

func toolUse(e map[string]any) bool {
	if e["type"] != "assistant" {
		return false
	}
	content, _ := e["message"].(map[string]any)["content"].([]any)
	for _, c := range content {
		if c.(map[string]any)["type"] == "tool_use" {
			return true
		}
	}
	return false
}

func types(events []map[string]any) []string {
	var out []string
	for _, e := range events {
		t, _ := e["type"].(string)
		if st, ok := e["subtype"].(string); ok {
			t += "/" + st
		}
		out = append(out, t)
	}
	return out
}

const first = "a1b2c3d4-0000-4000-8000-000000000001"
const second = "a1b2c3d4-0000-4000-8000-000000000002"
const sessionID = "5e551011-0000-4000-8000-00000000c0de"

func TestReplayEchoCarriesTheUUIDOfTheMessage(t *testing.T) {
	s := start(t, "turn-with-tool-call", t.TempDir(), "--session-id", sessionID)
	s.message(first, "Show the status.")
	echo := s.until(echoOf(first))
	e := echo[len(echo)-1]
	if e["isReplay"] != true || e["session_id"] != sessionID {
		t.Fatalf("echo = %v; want isReplay true and the session id", e)
	}
	if got := types(echo); got[0] != "system/init" {
		t.Fatalf("first event %q; want system/init", got[0])
	}
	s.until(is("result", "success"))
	if code := s.end(); code != 0 {
		t.Fatalf("exit code %d after stdin closed; want 0", code)
	}
	lines := s.transcript(sessionID)
	if lines[0]["type"] != "user" || lines[0]["uuid"] != first {
		t.Fatalf("first transcript line %v; want the user line with the wrapper's uuid", lines[0])
	}
	if last := lines[len(lines)-1]; last["type"] != "assistant" || last["sessionId"] != sessionID {
		t.Fatalf("last transcript line %v; want the agent's reply", last)
	}
	checkParentChain(t, lines)
}

// A message after the stream's last turn starts a new turn, as in Claude
// Code: the Harness does not exit while stdin is open.
func TestMessageAfterTheLastTurnGetsADefaultTurn(t *testing.T) {
	s := start(t, "turn-with-tool-call", t.TempDir(), "--session-id", sessionID)
	s.message(first, "Show the status.")
	s.until(is("result", "success"))
	for _, id := range []string{second, "a1b2c3d4-0000-4000-8000-000000000003"} {
		s.message(id, "And now?")
		turn := s.until(is("result", "success"))
		got := types(turn)
		// As in the recorded second turn: the echo, then a new init event.
		if turn[0]["uuid"] != id || got[1] != "system/init" || got[len(got)-2] != "assistant" {
			t.Fatalf("turn for %s = %v; want the echo, init, an assistant reply and a success result", id, got)
		}
		if r := turn[len(turn)-1]; r["session_id"] != sessionID || r["is_error"] != false {
			t.Fatalf("result = %v", r)
		}
	}
	if code := s.end(); code != 0 {
		t.Fatalf("exit code %d after stdin closed; want 0", code)
	}
	lines := s.transcript(sessionID)
	if last := lines[len(lines)-1]; last["type"] != "assistant" {
		t.Fatalf("last transcript line %v; want the default turn's reply", last)
	}
}

func TestNoEchoWithoutReplayUserMessages(t *testing.T) {
	program, env := fakeharness.Command("turn-with-tool-call")
	cmd := exec.Command(program, "-p", "--input-format", "stream-json", "--output-format", "stream-json")
	cmd.Env = append(os.Environ(), append(env, "CLAUDE_CONFIG_DIR="+t.TempDir())...)
	cmd.Dir = t.TempDir()
	cmd.Stdin = strings.NewReader(`{"type":"user","uuid":"` + first + `","message":{"role":"user","content":"hi"}}` + "\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), first) {
		t.Fatalf("stdout has the message's uuid with no --replay-user-messages:\n%s", out)
	}
}

func TestMessageDuringAToolCallIsEchoedWhenTheToolCallEnds(t *testing.T) {
	s := start(t, "message-during-tool-call", t.TempDir(), "--session-id", sessionID)
	s.message(first, "Run the sleep.")
	s.until(toolUse)
	s.message(second, "Also reply with the word BANANA.")
	events := s.until(echoOf(second))
	if events[len(events)-1]["isReplay"] != true {
		t.Fatalf("echo %v; want isReplay true", events[len(events)-1])
	}
	rest := s.until(is("result", ""))
	var synthetic int
	for _, e := range rest {
		if e["isSynthetic"] == true {
			synthetic++
		}
		if e["type"] == "user" && e["uuid"] == second {
			t.Fatalf("a second echo of the message")
		}
	}
	if synthetic != 1 {
		t.Fatalf("%d Stop hook feedback events; want 1", synthetic)
	}
	if code := s.end(); code != 0 {
		t.Fatalf("exit code %d; want 0", code)
	}
	var removed bool
	for _, l := range s.transcript(sessionID) {
		if l["type"] == "user" && l["uuid"] == second {
			t.Fatalf("the mid-turn message is a plain user line in the transcript")
		}
		if l["type"] == "queue-operation" && l["operation"] == "remove" && l["commandUuid"] == second {
			removed = l["reason"] == "absorbed_mid_turn"
		}
	}
	if !removed {
		t.Fatalf("no queue-operation remove line with the wrapper's uuid")
	}
}

func TestInterruptGivesAnErrorResultAndExitCodeOne(t *testing.T) {
	s := start(t, "interrupt", t.TempDir())
	s.message(first, "Run the loop.")
	s.until(toolUse)
	s.send(map[string]any{"type": "control_request", "request_id": "int-1", "request": map[string]any{"subtype": "interrupt"}})
	resp := s.until(is("control_response", ""))
	r := resp[len(resp)-1]["response"].(map[string]any)
	if r["subtype"] != "success" || r["request_id"] != "int-1" {
		t.Fatalf("control response %v; want success for int-1", r)
	}
	s.until(is("result", "error_during_execution"))
	if code := s.end(); code != 1 {
		t.Fatalf("exit code %d; want 1", code)
	}
}

func TestBackgroundTaskRunsAfterTheResult(t *testing.T) {
	s := start(t, "background-task", t.TempDir())
	s.message(first, "Wait 300 seconds.")
	before := s.until(is("result", ""))
	if got := strings.Join(types(before), " "); !strings.Contains(got, "system/task_started") || !strings.Contains(got, "system/background_tasks_changed") {
		t.Fatalf("events before the result %s; want task_started and background_tasks_changed", got)
	}
	s.until(is("system", "task_notification"))
	if code := s.end(); code != 0 {
		t.Fatalf("exit code %d; want 0", code)
	}
}

func TestUsageLimitGivesAnErrorResult(t *testing.T) {
	s := start(t, "usage-limit", t.TempDir())
	s.message(first, "Start.")
	r := s.until(is("result", ""))
	res := r[len(r)-1]
	if res["is_error"] != true || !strings.Contains(res["result"].(string), "usage limit") {
		t.Fatalf("result %v; want an error that names the usage limit", res)
	}
	if code := s.end(); code != 1 {
		t.Fatalf("exit code %d; want 1", code)
	}
}

func TestHarnessThatExitsMidTurnLeavesTheToolUseLast(t *testing.T) {
	s := start(t, "harness-exits-mid-turn", t.TempDir(), "--session-id", sessionID)
	s.message(first, "Run the tests.")
	s.until(toolUse)
	if code := s.end(); code != 1 {
		t.Fatalf("exit code %d; want 1", code)
	}
	lines := s.transcript(sessionID)
	if !toolUse(lines[len(lines)-1]) {
		t.Fatalf("last transcript line %v; want the assistant tool_use", lines[len(lines)-1])
	}
}

func TestStopHookStreamHasTwoTurns(t *testing.T) {
	s := start(t, "stop-hook-refuses-once", t.TempDir())
	s.message(first, "Reply APPLE.")
	s.until(is("result", "success"))
	s.message(second, "Reply CHERRY.")
	s.until(echoOf(second))
	s.until(is("result", "success"))
	if code := s.end(); code != 0 {
		t.Fatalf("exit code %d; want 0", code)
	}
}

func TestDisallowedToolIsNotInTheInitEvent(t *testing.T) {
	s := start(t, "turn-with-tool-call", t.TempDir())
	init := s.until(is("system", "init"))[0]
	tools := init["tools"].([]any)
	for _, tool := range tools {
		if tool == "AskUserQuestion" {
			t.Fatalf("init lists AskUserQuestion with --disallowedTools AskUserQuestion")
		}
	}
	if len(tools) != 25 || init["permissionMode"] != "bypassPermissions" || init["cwd"] != s.cwd {
		t.Fatalf("init has %d tools, mode %v, cwd %v; want 25, bypassPermissions, %s", len(tools), init["permissionMode"], init["cwd"], s.cwd)
	}
}

func TestResumeContinuesTheSameTranscript(t *testing.T) {
	config := t.TempDir()
	s := start(t, "turn-with-tool-call", config, "--session-id", sessionID)
	s.message(first, "One.")
	s.until(is("result", ""))
	s.end()
	n := len(s.transcript(sessionID))

	r := start(t, "turn-with-tool-call", config, "--resume", sessionID)
	r.message(second, "Two.")
	if e := r.until(echoOf(second)); e[len(e)-1]["session_id"] != sessionID {
		t.Fatalf("echo session id %v; want %s", e[len(e)-1]["session_id"], sessionID)
	}
	r.until(is("result", ""))
	r.end()
	lines := r.transcript(sessionID)
	if len(lines) <= n {
		t.Fatalf("transcript has %d lines after the resume, %d before; want more", len(lines), n)
	}
	checkParentChain(t, lines)
}

// checkParentChain checks that each transcript line with a uuid has the
// uuid of the line with a uuid before it as its parentUuid, and that the
// first has none. A transcript reader follows this chain.
func checkParentChain(t *testing.T, lines []map[string]any) {
	t.Helper()
	var previous any
	n := 0
	for i, l := range lines {
		id, ok := l["uuid"]
		if !ok {
			continue
		}
		if parent, ok := l["parentUuid"]; !ok || parent != previous {
			t.Fatalf("line %d: parentUuid %v; want %v", i, l["parentUuid"], previous)
		}
		previous = id
		n++
	}
	if n < 2 {
		t.Fatalf("transcript has %d lines with a uuid; want a chain of 2 or more", n)
	}
}

func TestStartWithASessionIDThatIsInUseFails(t *testing.T) {
	config := t.TempDir()
	s := start(t, "turn-with-tool-call", config, "--session-id", sessionID)
	s.message(first, "One.")
	s.until(is("result", ""))
	s.end()
	before, _ := os.ReadFile(filepath.Join(config, "projects", "-workspace-repo", sessionID+".jsonl"))

	again := start(t, "turn-with-tool-call", config, "--session-id", sessionID)
	if code := again.end(); code != 1 || !strings.Contains(again.stderr.String(), "Session ID "+sessionID+" is already in use.") {
		t.Fatalf("exit %d, stderr %q; want 1 and session ID already in use", code, again.stderr.String())
	}
	after, _ := os.ReadFile(filepath.Join(config, "projects", "-workspace-repo", sessionID+".jsonl"))
	if !bytes.Equal(before, after) {
		t.Fatalf("the transcript changed:\n%s\nwant:\n%s", after, before)
	}
}

func TestResumeOfAnAbsentSessionFails(t *testing.T) {
	s := start(t, "turn-with-tool-call", t.TempDir(), "--resume", sessionID)
	if code := s.end(); code != 1 || !strings.Contains(s.stderr.String(), "No conversation found with session ID: "+sessionID) {
		t.Fatalf("exit %d, stderr %q; want 1 and no conversation found", code, s.stderr.String())
	}
}

func TestStreamChosenByName(t *testing.T) {
	for _, name := range fakeharness.Names() {
		b, err := fakeharness.Stream(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fakeharness.ParseScript(b); err != nil {
			t.Errorf("stream %s: %v", name, err)
		}
	}
	_, err := fakeharness.Stream("no-such-stream")
	if err == nil || !strings.Contains(err.Error(), `no recorded stream named "no-such-stream"`) || !strings.Contains(err.Error(), "turn-with-tool-call") {
		t.Fatalf("error %v; want one that names the stream and the streams that there are", err)
	}
	s := start(t, "no-such-stream", t.TempDir())
	if code := s.end(); code != 2 || !strings.Contains(s.stderr.String(), `no recorded stream named "no-such-stream"`) {
		t.Fatalf("exit %d, stderr %q; want 2 and the clear error", code, s.stderr.String())
	}
}

func TestScriptFromATestFile(t *testing.T) {
	script := filepath.Join(t.TempDir(), "script.jsonl")
	os.WriteFile(script, []byte(`{"type":"system","subtype":"init","session_id":""}`+"\n"+`{"fake":"exit","code":7}`+"\n"), 0o600)
	cmd := exec.Command(os.Args[0], headless...)
	cmd.Env = append(os.Environ(), "FAKE_HARNESS=1", "FAKE_HARNESS_SCRIPT="+script, "CLAUDE_CONFIG_DIR="+t.TempDir())
	cmd.Dir = t.TempDir()
	out, _ := cmd.Output()
	if cmd.ProcessState.ExitCode() != 7 || !strings.Contains(string(out), `"init"`) {
		t.Fatalf("exit %d, stdout %q; want 7 after the init event", cmd.ProcessState.ExitCode(), out)
	}
}

// Each recorded stream is in a public repository: no secret, no name of the
// first team or of its systems, no private repository, host or person.
func TestStreamsHoldNoPrivateData(t *testing.T) {
	forbidden := []*regexp.Regexp{
		regexp.MustCompile(`/home/|/Users/|/root/`),
		regexp.MustCompile(`(?i)bitbucket\.org|github\.com|gitlab\.com|@[a-z0-9-]+\.(com|io|net|org)`),
		regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`),
		regexp.MustCompile(`sk-ant-|stk_|ghp_|xox[bp]-|AKIA[0-9A-Z]{16}|-----BEGIN|(?i)bearer\s`),
		regexp.MustCompile(`"signature"`),
	}
	forbidden = append(forbidden, localPatterns(t)...)
	for _, name := range fakeharness.Names() {
		b, _ := fakeharness.Stream(name)
		for _, re := range forbidden {
			if loc := re.FindIndex(b); loc != nil {
				t.Errorf("stream %s has %q, which matches %s", name, b[loc[0]:loc[1]], re)
			}
		}
	}
}

// localPatterns reads the names of the first team and of its systems from
// the checkout's own list (info/forbidden-patterns in the git directory,
// which the pre-commit hook also uses). The list is not in the repository,
// so that the repository does not name them. Where it is absent, only the
// generic checks run.
func localPatterns(t *testing.T) []*regexp.Regexp {
	out, err := exec.Command("git", "rev-parse", "--git-common-dir").Output()
	if err != nil {
		t.Log("no git directory: the local list of private names is not checked")
		return nil
	}
	b, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "info", "forbidden-patterns"))
	if err != nil {
		t.Log("no info/forbidden-patterns: the local list of private names is not checked")
		return nil
	}
	var res []*regexp.Regexp
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			res = append(res, regexp.MustCompile("(?i)"+line))
		}
	}
	return res
}
