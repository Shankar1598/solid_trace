package wrapper_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/recorder"
)

// secondReply is an assistant line that the agent writes after a gate.
const secondReply = `{"type":"assistant","message":{"id":"msg_02Second","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"text","text":"The second reply."}],"stop_reason":"end_turn"},"parent_tool_use_id":null,"session_id":"","uuid":""}`

// A new Agent session id, as a /clear makes one.
const clearedID = "0b0f6a52-1c1e-4d7e-9a43-5d2f0c7e9b11"

// waitUntil waits up to 20 s for ok.
func waitUntil(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(20 * time.Second); !ok(); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("after 20 s: %s", what)
		}
	}
}

// firstToken is the child token of the Run's first Execution, made at its
// task call.
func firstToken(t *testing.T, env *factorytest.Env) recorder.ChildToken {
	t.Helper()
	waitUntil(t, "the Dispatcher made a child token", func() bool { return len(env.SolidTrace.ChildTokens()) > 0 })
	return env.SolidTrace.ChildTokens()[0]
}

// allRevoked waits until each child token is revoked: the end step
// revokes them after the Execution shows ended.
func allRevoked(t *testing.T, env *factorytest.Env) {
	t.Helper()
	waitUntil(t, "each child token is revoked", func() bool {
		for _, tok := range env.SolidTrace.ChildTokens() {
			if !tok.Revoked {
				return false
			}
		}
		return true
	})
}

// ingest calls the recorder's agent ingest API with token, as a client
// that holds it would.
func ingest(t *testing.T, env *factorytest.Env, token, session string) int {
	t.Helper()
	req, _ := http.NewRequest("GET", env.SolidTrace.URL()+recorder.AgentSessionsPath+session+"/files", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestTheRecorderGetsTheRunsTranscriptDuringTheExecutionAndTheLastBytesAtTheEnd(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	// The end deletes the run user's home: the agent keeps a copy of the
	// project directory after its last reply.
	kept := filepath.Join(t.TempDir(), "kept")
	env.Runner.Agent(takeTask, initLine(t), reply, wait, secondReply,
		reportStep("--outcome", "done", "--summary", "Fixed"),
		shell(`cp -R "$CLAUDE_CONFIG_DIR/projects/$CLAUDE_CODE_PROJECT_DIR_NAME" `+kept), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	session := firstToken(t, env).AgentSessionIDs[0]
	transcript := session + ".jsonl"
	// While the agent waits, the first reply is uploaded.
	waitUntil(t, "the first reply is uploaded during the Execution", func() bool {
		return bytes.Contains(env.SolidTrace.Stored(session, transcript), []byte("msg_01Thin"))
	})
	if bytes.Contains(env.SolidTrace.Stored(session, transcript), []byte("msg_02Second")) {
		t.Fatal("the second reply is uploaded before the agent wrote it")
	}
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	onDisk, err := os.ReadFile(filepath.Join(kept, transcript))
	if err != nil {
		t.Fatal(err)
	}
	if got := env.SolidTrace.Stored(session, transcript); !bytes.Equal(got, onDisk) || !bytes.Contains(got, []byte("msg_02Second")) {
		t.Errorf("the stored transcript is not the one on disk at the end:\nstored:  %s\non disk: %s", got, onDisk)
	}
	for _, up := range env.SolidTrace.Uploads() {
		if up.RunID != id || up.Project != "shop" || up.Repository != "bitbucket.org/example/shop" || up.Session != session {
			t.Errorf("an upload names Run %q, Project %q, repository %q, Agent session %q", up.RunID, up.Project,
				up.Repository, up.Session)
		}
	}
}

func TestTheExecutionsTokenIsForTheRunsAgentSessionUntilTheTotalLimitPlusOneHourAndIsRevokedAtTheEnd(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), reply, reportStep("--outcome", "done", "--summary", "Fixed"), result, waitForEnd)
	started := env.Clock.Now()

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	allRevoked(t, env)

	tokens := env.SolidTrace.ChildTokens()
	if len(tokens) != 1 {
		t.Fatalf("want one child token, got %+v", tokens)
	}
	tok := tokens[0]
	if !slices.Equal(tok.Scopes, []string{"sessions:write", "sessions:read"}) || len(tok.AgentSessionIDs) != 1 {
		t.Errorf("scopes %v, Agent sessions %v", tok.Scopes, tok.AgentSessionIDs)
	}
	// The total limit is 8 hours.
	if want := started.Add(9 * time.Hour); !tok.ExpiresAt.Equal(want) {
		t.Errorf("the token expires at %s, not %s", tok.ExpiresAt, want)
	}
	if !tok.Revoked {
		t.Error("the end step did not revoke the token")
	}
	if status := ingest(t, env, tok.Secret, tok.AgentSessionIDs[0]); status != http.StatusUnauthorized {
		t.Errorf("the token after the end: %d", status)
	}
	if len(env.SolidTrace.Stored(tok.AgentSessionIDs[0], tok.AgentSessionIDs[0]+".jsonl")) == 0 {
		t.Error("the transcript was not uploaded before the revocation")
	}
}

func TestTheRunUserNeverGetsTheExecutionsToken(t *testing.T) {
	env, ana := newEnv(t)
	// The agent looks for a child token in its environment and in each
	// file of the Execution's directory that it can read (its parent).
	env.Runner.Agent(takeTask, initLine(t),
		shell(`! env | grep -q "stk_""child" && ! grep -rqs "stk_""child" .. && `+
			`solidtrace report --outcome done --summary "No token here"`),
		reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if run.Summary != "No token here" {
		t.Errorf("summary %q, reason %q", run.Summary, run.Reason)
	}
}

func TestFilesUnderEvidenceUploadWithTheTranscriptAndNoLinkLeavesTheProjectDirectory(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t),
		shell(`printf 'PNG' > "$SOLIDTRACE_EVIDENCE_DIR/cart.png" && mkdir -p "$SOLIDTRACE_EVIDENCE_DIR/logs" && `+
			`printf 'all green' > "$SOLIDTRACE_EVIDENCE_DIR/logs/check.txt" && `+
			`ln -s "$PWD/../secrets/forge-token" "$SOLIDTRACE_EVIDENCE_DIR/token" && `+
			`ln -s "$PWD/../secrets" "$SOLIDTRACE_EVIDENCE_DIR/secrets"`),
		reply, reportStep("--outcome", "done", "--summary", "Fixed"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	session := firstToken(t, env).AgentSessionIDs[0]
	if got := string(env.SolidTrace.Stored(session, "evidence/cart.png")); got != "PNG" {
		t.Errorf("evidence/cart.png: %q", got)
	}
	if got := string(env.SolidTrace.Stored(session, "evidence/logs/check.txt")); got != "all green" {
		t.Errorf("evidence/logs/check.txt: %q", got)
	}
	for _, up := range env.SolidTrace.Uploads() {
		if bytes.Contains(up.Bytes, []byte(env.Forge.Token)) || strings.Contains(up.Path, "secrets") ||
			strings.HasSuffix(up.Path, "token") {
			t.Errorf("an upload followed a link out of the project directory: %s", up.Path)
		}
	}
	if len(env.SolidTrace.Stored(session, session+".jsonl")) == 0 {
		t.Error("no transcript")
	}
}

func TestANewAgentSessionIDIsAddedToTheRunWithANewToken(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	// A /clear: Claude Code goes on in a new Agent session file.
	env.Runner.Agent(takeTask, initLine(t), reply,
		shell(`printf '{"type":"user","message":{"role":"user","content":"after the clear"}}\n' > `+
			`"$CLAUDE_CONFIG_DIR/projects/$CLAUDE_CODE_PROJECT_DIR_NAME/`+clearedID+`.jsonl"`),
		wait, reportStep("--outcome", "done", "--summary", "Fixed"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	first := firstToken(t, env).AgentSessionIDs[0]
	waitUntil(t, "the new Agent session is uploaded", func() bool {
		return bytes.Contains(env.SolidTrace.Stored(clearedID, clearedID+".jsonl"), []byte("after the clear"))
	})
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	allRevoked(t, env)

	tokens := env.SolidTrace.ChildTokens()
	if len(tokens) != 2 {
		t.Fatalf("want a second token for the new Agent session, got %+v", tokens)
	}
	if !slices.Equal(tokens[0].AgentSessionIDs, []string{first}) ||
		!slices.Equal(tokens[1].AgentSessionIDs, []string{first, clearedID}) {
		t.Errorf("the tokens' Agent sessions: %v, then %v", tokens[0].AgentSessionIDs, tokens[1].AgentSessionIDs)
	}
	if !tokens[1].ExpiresAt.Equal(tokens[0].ExpiresAt) {
		t.Errorf("the new token expires at %s, after the Execution's limit %s", tokens[1].ExpiresAt, tokens[0].ExpiresAt)
	}
	for _, tok := range tokens {
		if !tok.Revoked {
			t.Errorf("the end step did not revoke token %s", tok.ID)
		}
	}
	for _, up := range env.SolidTrace.Uploads() {
		if up.Session == clearedID && up.Token != tokens[1].Secret {
			t.Errorf("an upload of the new Agent session used another token")
		}
	}
}

func TestANewAgentSessionFileDoesNotMoveTheRunsCurrentAgentSession(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	// A tool starts a second Claude Code process: it gets the harness's
	// environment, so its transcript goes into the Run's project directory.
	env.Runner.Agent(takeTask, initLine(t), reply,
		shell(`printf '{"type":"user","message":{"role":"user","content":"summarise the diff"}}\n' > `+
			`"$CLAUDE_CONFIG_DIR/projects/$CLAUDE_CODE_PROJECT_DIR_NAME/`+clearedID+`.jsonl"`),
		wait, shell(`printf 'all green' > "$SOLIDTRACE_EVIDENCE_DIR/check.txt"`),
		reportStep("--outcome", "done", "--summary", "Fixed"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	first := firstToken(t, env).AgentSessionIDs[0]
	waitUntil(t, "the second Agent session is added to the Run", func() bool {
		return len(env.SolidTrace.ChildTokens()) == 2
	})
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	// evidence/ stays with the harness's own Agent session, which the
	// next Execution resumes.
	if got := string(env.SolidTrace.Stored(first, "evidence/check.txt")); got != "all green" {
		t.Errorf("evidence/check.txt of the Run's Agent session: %q", got)
	}
	if got := env.SolidTrace.Stored(clearedID, "evidence/check.txt"); len(got) != 0 {
		t.Errorf("evidence/ went with the second Agent session: %q", got)
	}
}

func TestAnAgentSessionIDOfAnotherRunIsNotAddedAndNotReached(t *testing.T) {
	env, ana := newEnv(t)
	env.Runner.Agent(takeTask, initLine(t), reply, reportStep("--outcome", "done", "--summary", "First"), result, waitForEnd)
	first := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, first, dispatcherapi.StateDone)
	other := firstToken(t, env).AgentSessionIDs[0]
	before := env.SolidTrace.Stored(other, other+".jsonl")

	// The second Run's agent writes a file with the first Run's id, to
	// get a token for it.
	env.Runner.Agent(takeTask, initLine(t),
		shell(`printf '{"type":"user","message":{"role":"user","content":"stolen"}}\n' > `+
			`"$CLAUDE_CONFIG_DIR/projects/$CLAUDE_CODE_PROJECT_DIR_NAME/`+other+`.jsonl" && sleep 0.5`),
		reportStep("--outcome", "done", "--summary", "Second"), reply, result, waitForEnd)
	second := start(t, env, ana, "Fix the cart again")
	waitFor(t, env, ana, second, dispatcherapi.StateDone)

	for _, tok := range env.SolidTrace.ChildTokens()[1:] {
		if slices.Contains(tok.AgentSessionIDs, other) {
			t.Errorf("the second Run got a token for the first Run's Agent session: %+v", tok)
		}
	}
	if got := env.SolidTrace.Stored(other, other+".jsonl"); !bytes.Equal(got, before) {
		t.Errorf("the first Run's Agent session changed:\n%s", got)
	}
	if !strings.Contains(env.Runner.Fake.Output(getRun(t, ana, second).Executions[0].ID), "another Run") {
		t.Errorf("the wrapper did not log the refusal:\n%s", env.Runner.Fake.Output(getRun(t, ana, second).Executions[0].ID))
	}
}

func TestTheTokenCallRefusesWhatIsNotTheRunningExecutionsToAsk(t *testing.T) {
	env, ana := newEnv(t)
	runs, executions, opens := twoRunningRuns(t, env, ana, reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)
	a, b := executions[0], executions[1]
	token := func(identity, execution string, body any) factorytest.Answer {
		return env.API(identity, "POST", dispatcherapi.Path+"/executions/"+execution+"/token", body)
	}
	identityA := env.Runner.Identity(factorytest.Workload(a))

	if got := token(identityA, b, dispatcherapi.TokenRequest{}); got.Status != http.StatusUnauthorized {
		t.Errorf("A's JWT asks for B's token: %d %s", got.Status, got.Body)
	}
	for _, bad := range []string{"not-a-uuid", strings.ToUpper(clearedID), "../" + clearedID} {
		if got := token(identityA, a, dispatcherapi.TokenRequest{AgentSessionID: bad}); got.Status != http.StatusBadRequest {
			t.Errorf("Agent session id %q: %d %s", bad, got.Status, got.Body)
		}
	}
	sessionB := ""
	for _, tok := range env.SolidTrace.ChildTokens() {
		if strings.Contains(tok.Name, b) {
			sessionB = tok.AgentSessionIDs[0]
		}
	}
	if got := token(identityA, a, dispatcherapi.TokenRequest{AgentSessionID: sessionB}); got.Status != http.StatusConflict {
		t.Errorf("B's Agent session for A: %d %s", got.Status, got.Body)
	}

	// A Run has at most 32 Agent sessions: its first and 31 more.
	for i := 1; i <= 32; i++ {
		id := strings.Replace(clearedID, "9b11", "9b"+string(rune('a'+i/16))+string("0123456789abcdef"[i%16]), 1)
		got := token(identityA, a, dispatcherapi.TokenRequest{AgentSessionID: id})
		if want := http.StatusOK; i == 32 {
			if got.Status != http.StatusConflict {
				t.Errorf("the 33rd Agent session: %d %s", got.Status, got.Body)
			}
		} else if got.Status != want {
			t.Fatalf("Agent session %d: %d %s", i+1, got.Status, got.Body)
		}
	}
	var tok dispatcherapi.SessionToken
	got := token(identityA, a, dispatcherapi.TokenRequest{})
	got.JSON(t, &tok)
	if got.Status != http.StatusOK || len(tok.AgentSessionIDs) != 32 || tok.Token == "" {
		t.Errorf("a new token with no new Agent session: %d, %d Agent sessions", got.Status, len(tok.AgentSessionIDs))
	}

	opens[0]()
	opens[1]()
	waitFor(t, env, ana, runs[0], dispatcherapi.StateDone)
	if got := token(identityA, a, dispatcherapi.TokenRequest{}); got.Status != http.StatusConflict {
		t.Errorf("a token after the end: %d %s", got.Status, got.Body)
	}
	// The end step revokes each of them, after the Execution shows ended.
	waitUntil(t, "each token of the ended Execution is revoked", func() bool {
		for _, tok := range env.SolidTrace.ChildTokens() {
			if strings.Contains(tok.Name, a) && !tok.Revoked {
				return false
			}
		}
		return true
	})
}

func TestARefusedTokenGetsANewOneAndTheUploadsGoOn(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), reply, wait, secondReply,
		reportStep("--outcome", "done", "--summary", "Fixed"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	first := firstToken(t, env)
	session := first.AgentSessionIDs[0]
	waitUntil(t, "the first upload", func() bool { return len(env.SolidTrace.Stored(session, session+".jsonl")) > 0 })
	// SolidTrace stops the token (as when it was revoked there).
	env.SolidTrace.Revoke(first.Secret)
	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	if got := env.SolidTrace.Stored(session, session+".jsonl"); !bytes.Contains(got, []byte("msg_02Second")) {
		t.Errorf("the last bytes were not uploaded with a new token:\n%s", got)
	}
	if tokens := env.SolidTrace.ChildTokens(); len(tokens) < 2 || !slices.Equal(tokens[1].AgentSessionIDs, []string{session}) {
		t.Errorf("tokens: %+v", tokens)
	}
}

func TestAnExecutionWithNoTokenFailsItsRunAndStartsNoHarness(t *testing.T) {
	env, ana := newEnv(t)
	env.SolidTrace.RefuseChildTokens(http.StatusServiceUnavailable)
	started := filepath.Join(t.TempDir(), "started")
	env.Runner.Agent(takeTask, initLine(t), shell("touch "+started), reply, result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)

	if !strings.Contains(run.Reason, "token") {
		t.Errorf("reason %q", run.Reason)
	}
	if _, err := os.Stat(started); err == nil {
		t.Error("the harness started with no token")
	}
}

func TestALostMachineRevokesTheExecutionsToken(t *testing.T) {
	env, ana := newEnv(t)
	wait, _ := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait)

	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	tok := firstToken(t, env)
	env.Runner.HoldMachines()
	if err := env.Runner.Fake.LoseMachine(run.Executions[0].ID); err != nil {
		t.Fatal(err)
	}
	waitForRestart(t, env, ana, id)
	waitUntil(t, "the token is revoked", func() bool { return env.SolidTrace.ChildTokens()[0].Revoked })
	if status := ingest(t, env, tok.Secret, tok.AgentSessionIDs[0]); status != http.StatusUnauthorized {
		t.Errorf("the token after the lost machine: %d", status)
	}
}

func TestUploadsGoThroughTheProxyAndTheDispatcherCallsDoNot(t *testing.T) {
	env := factorytest.New(t, factorytest.Executions, factorytest.ThroughProxy)
	env.Project("shop")
	ana := env.Member("Ana")
	env.Runner.Agent(takeTask, initLine(t), reply, reportStep("--outcome", "done", "--summary", "Fixed"), result, waitForEnd)

	id := start(t, env, ana, "Fix the cart")
	waitFor(t, env, ana, id, dispatcherapi.StateDone)

	// The ingest host is in no DNS: only the proxy reaches it.
	session := firstToken(t, env).AgentSessionIDs[0]
	if len(env.SolidTrace.Stored(session, session+".jsonl")) == 0 {
		t.Fatal("no upload reached SolidTrace")
	}
	var ingests int
	for _, u := range env.Proxy.Requests() {
		if strings.Contains(u, dispatcherapi.Path+"/executions/") {
			t.Errorf("a Dispatcher call went through the proxy: %s", u)
		}
		if strings.HasPrefix(u, "http://"+factorytest.IngestHost) {
			ingests++
		}
	}
	if ingests == 0 {
		t.Errorf("the proxy saw no upload: %v", env.Proxy.Requests())
	}
}
