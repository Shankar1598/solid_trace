package wrapper_test

import (
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
	"github.com/solidtrace/factory/internal/killboundary"
	"github.com/solidtrace/factory/wrapper"
)

// Lines of a turn that the interrupt request stops, as Claude Code gives
// them (the recorded stream interrupt).
const (
	awaitInterrupt = `{"fake":"await_interrupt","eof_code":1}`
	interrupted    = `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user for tool use]"}]},"parent_tool_use_id":null,"session_id":"","uuid":""}`
	errorResult    = `{"type":"result","subtype":"error_during_execution","is_error":true,"num_turns":1,"session_id":"","uuid":""}`
	// exitWhenStdinCloses waits for a message, and exits with code 1
	// when stdin closes, as Claude Code does after an interrupt.
	exitWhenStdinCloses = `{"fake":"await_message","eof_code":1}`
)

// ready is a step that writes an Evidence file, then a file that tells
// the test that the agent works.
func ready(t *testing.T) (step string, wait func()) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ready")
	step = shell(`printf 'the cart total is right' >"$SOLIDTRACE_EVIDENCE_DIR/notes.txt" && touch ` + path)
	return step, func() {
		waitUntil(t, "the agent works", func() bool {
			_, err := os.Stat(path)
			return err == nil
		})
	}
}

// interruptedTurn is a turn that waits in a tool call until the interrupt
// request comes.
func interruptedTurn(t *testing.T, step string) []string {
	return []string{takeTask, initLine(t), step, reply, awaitInterrupt, interrupted, errorResult, exitWhenStdinCloses}
}

// eventOf is the run event of kind, or fails the test.
func eventOf(t *testing.T, events []map[string]any, kind string) map[string]any {
	t.Helper()
	for _, ev := range events {
		if ev["event.kind"] == kind {
			return ev
		}
	}
	t.Fatalf("no %s event in %v", kind, kinds(events))
	return nil
}

// waitForEvent waits until the Run has an event of kind.
func waitForEvent(t *testing.T, env *factorytest.Env, run, kind string) map[string]any {
	t.Helper()
	waitUntil(t, "a "+kind+" event", func() bool { return slices.Contains(kinds(env.RunEvents(run)), kind) })
	return eventOf(t, env.RunEvents(run), kind)
}

// checkSaved checks what the preemption handler saved: the last bytes of
// the transcript and the Evidence files are at SolidTrace, and the
// preemption event says so.
func checkSaved(t *testing.T, env *factorytest.Env, run dispatcherapi.Run, reason string) map[string]any {
	t.Helper()
	execution := onlyExecution(t, run)
	if execution.EndReason != reason {
		t.Errorf("end reason %q, want %q", execution.EndReason, reason)
	}
	session := firstToken(t, env).AgentSessionIDs[0]
	onDisk, err := os.ReadFile(filepath.Join(env.Runner.ProjectDir(execution.ID), session+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(onDisk, []byte("Request interrupted")) {
		t.Errorf("the agent's turn was not interrupted; transcript:\n%s", onDisk)
	}
	if got := env.SolidTrace.Stored(session, session+".jsonl"); !bytes.Equal(got, onDisk) {
		t.Errorf("SolidTrace has not the last bytes of the transcript:\nstored:  %s\non disk: %s", got, onDisk)
	}
	if got := string(env.SolidTrace.Stored(session, "evidence/notes.txt")); got != "the cart total is right" {
		t.Errorf("the Evidence file at SolidTrace: %q", got)
	}
	events := env.WaitForRunEvents(run.ID, 5)
	order := kinds(events)
	if i, j, k := slices.Index(order, "preempting"), slices.Index(order, "preemption"),
		slices.Index(order, "execution_ended"); i < 0 || j < i || k < j {
		t.Errorf("the run events: %v, want preempting, then preemption, then execution_ended", order)
	}
	preemption := eventOf(t, events, "preemption")
	checkFields(t, preemption, map[string]any{"preemption.reason": reason, "execution.id": execution.ID,
		"preemption.transcript_offset": float64(len(onDisk))})
	if s, ok := preemption["preemption.seconds_to_saved"].(float64); !ok || s <= 0 || s >= 100 {
		t.Errorf("seconds from the notice to the saved state: %v", preemption["preemption.seconds_to_saved"])
	}
	if _, ok := preemption["preemption.checkpoint_age_seconds"]; ok {
		t.Errorf("a checkpoint age with no checkpoint: %v", preemption["preemption.checkpoint_age_seconds"])
	}
	checkFields(t, eventOf(t, events, "preempting"), map[string]any{"preemption.reason": reason,
		"execution.id": execution.ID})
	return preemption
}

func TestANoticeOnTheMachineSavesTheWorkAndEndsTheExecutionAsPreempted(t *testing.T) {
	env, ana := newEnv(t)
	step, working := ready(t)
	env.Runner.Agent(interruptedTurn(t, step)...)
	id := start(t, env, ana, "Fix the cart")
	working()
	execution := getRun(t, ana, id).Executions[0].ID

	env.Runner.Preempt(execution)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if run.Reason != dispatcherapi.EndPreempted {
		t.Errorf("the Run's reason %q", run.Reason)
	}
	preemption := checkSaved(t, env, run, dispatcherapi.EndPreempted)
	if problems, _ := preemption["preemption.problems"].([]any); len(problems) != 1 ||
		!strings.Contains(problems[0].(string), "no work-in-progress save") {
		t.Errorf("the problems: %v", preemption["preemption.problems"])
	}
	if _, err := killboundary.Reopen(wrapper.CgroupName(execution)); err == nil {
		t.Errorf("the Execution's child cgroup is not removed")
	}
	if code := waitForExit(t, env, execution); code != 0 {
		t.Errorf("the wrapper exited with code %d", code)
	}
}

func TestASIGTERMWithNoStopCommandEndsTheExecutionAsMachineStopped(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
		t.Run(sig.String(), func(t *testing.T) {
			env, ana := newEnv(t)
			step, working := ready(t)
			env.Runner.Agent(interruptedTurn(t, step)...)
			id := start(t, env, ana, "Fix the cart")
			working()

			env.Runner.Signal(getRun(t, ana, id).Executions[0].ID, sig)

			run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
			checkSaved(t, env, run, dispatcherapi.EndMachineStopped)
		})
	}
}

// With a 0-second notice, the shutdown's SIGTERM comes with the notice.
// The same steps run once, and the reason is preempted.
func TestWithA0SecondNoticeTheSameStepsRunOnceAndTheReasonIsPreempted(t *testing.T) {
	env, ana := newEnv(t)
	step, working := ready(t)
	env.Runner.Agent(interruptedTurn(t, step)...)
	id := start(t, env, ana, "Fix the cart")
	working()
	execution := getRun(t, ana, id).Executions[0].ID

	env.Runner.Preempt(execution)
	env.Runner.Signal(execution, syscall.SIGTERM)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	checkSaved(t, env, run, dispatcherapi.EndPreempted)
	got := kinds(env.RunEvents(id))
	for _, kind := range []string{"preempting", "preemption", "execution_ended"} {
		if n := countOf(got, kind); n != 1 {
			t.Errorf("%d %s events, want 1: %v", n, kind, got)
		}
	}
}

func countOf(list []string, s string) int {
	n := 0
	for _, v := range list {
		if v == s {
			n++
		}
	}
	return n
}

// After the notice, a report does not count, and the halt stops an agent
// that does not answer the interrupt request.
func TestAReportAfterTheNoticeIsRefusedAndTheHaltStopsAnAgentThatIgnoresTheInterrupt(t *testing.T) {
	env, ana := newEnv(t)
	step, working := ready(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), step, wait, reportStep("--outcome", "done", "--summary", "Fixed"),
		shell("sleep 60"), reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	working()
	execution := getRun(t, ana, id).Executions[0].ID

	began := time.Now()
	env.Runner.Preempt(execution)
	waitForEvent(t, env, id, "preempting")
	open()

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if run.Reason != dispatcherapi.EndPreempted || onlyExecution(t, run).EndReason != dispatcherapi.EndPreempted {
		t.Errorf("the Run is %s (%s): a report after the notice counted", run.State, run.Reason)
	}
	if took := time.Since(began); took > 15*time.Second {
		t.Errorf("the handler took %s, more than the halt's 10 s and the save", took)
	}
	if out := env.Runner.Fake.Output(execution); !strings.Contains(out, "the machine is being taken back") {
		t.Errorf("the agent's report was not refused with the reason; output:\n%s", out)
	}
	if _, err := killboundary.Reopen(wrapper.CgroupName(execution)); err == nil {
		t.Errorf("the Execution's child cgroup is not removed")
	}
}

// A last upload that gets no answer stops at its limit, and the
// Execution still ends with what was saved.
func TestALastUploadThatHangsStopsAtItsLimitAndTheExecutionStillEnds(t *testing.T) {
	env, ana := newEnv(t)
	step, working := ready(t)
	env.Runner.Agent(interruptedTurn(t, step)...)
	id := start(t, env, ana, "Fix the cart")
	working()
	execution := getRun(t, ana, id).Executions[0].ID
	env.SolidTrace.HangIngest(true)
	t.Cleanup(func() { env.SolidTrace.HangIngest(false) })

	began := time.Now()
	env.Runner.Preempt(execution)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	took := time.Since(began)
	if onlyExecution(t, run).EndReason != dispatcherapi.EndPreempted {
		t.Errorf("end reason %q", onlyExecution(t, run).EndReason)
	}
	if took < 10*time.Second || took > 20*time.Second {
		t.Errorf("the Execution ended %s after the notice, want after the last upload's 10 s limit", took)
	}
	preemption := eventOf(t, env.WaitForRunEvents(id, 5), "preemption")
	problems, _ := preemption["preemption.problems"].([]any)
	if !slices.ContainsFunc(problems, func(p any) bool { return strings.Contains(p.(string), "the last upload did not end") }) {
		t.Errorf("the problems: %v", problems)
	}
}

// An Evidence file that SolidTrace does not take is a problem of the
// last upload in the preemption event.
func TestAnEvidenceFileThatTheLastUploadDidNotSendIsAProblem(t *testing.T) {
	env, ana := newEnv(t)
	env.SolidTrace.RefusePath("evidence/notes.txt")
	step, working := ready(t)
	env.Runner.Agent(interruptedTurn(t, step)...)
	id := start(t, env, ana, "Fix the cart")
	working()
	execution := getRun(t, ana, id).Executions[0].ID

	env.Runner.Preempt(execution)

	run := waitFor(t, env, ana, id, dispatcherapi.StateFailed)
	if onlyExecution(t, run).EndReason != dispatcherapi.EndPreempted {
		t.Errorf("end reason %q", onlyExecution(t, run).EndReason)
	}
	preemption := eventOf(t, env.WaitForRunEvents(id, 5), "preemption")
	problems, _ := preemption["preemption.problems"].([]any)
	if !slices.ContainsFunc(problems, func(p any) bool {
		return strings.Contains(p.(string), "the last upload") && strings.Contains(p.(string), "evidence/notes.txt")
	}) {
		t.Errorf("the problems do not name the Evidence file that is not at SolidTrace: %v", problems)
	}
	if _, ok := preemption["preemption.transcript_offset"].(float64); !ok {
		t.Errorf("the transcript is at SolidTrace, but the event has no offset: %v", preemption)
	}
}

// The preemption event has each part of the wrapper's report: what was
// saved (a work-in-progress commit, the files left out, the transcript
// offset), the age of the last checkpoint and the times.
func TestThePreemptionEventRecordsWhatWasSavedAndTheAgeOfTheLastCheckpoint(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	t.Cleanup(open)
	env.Runner.Agent(takeTask, initLine(t), wait, reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	execution := waitFor(t, env, ana, id, dispatcherapi.StateRunning).Executions[0].ID
	identity := env.Runner.Identity(factorytest.Workload(execution))
	offset, age := uint64(8192), 240.5
	notice := time.Date(2026, 11, 15, 3, 0, 0, 0, time.UTC)
	end := dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndPreempted, Preemption: &dispatcherapi.Preemption{
		Reason: dispatcherapi.EndPreempted, NoticeAt: notice, SecondsToSaved: 21.5, SecondsToEnd: 22,
		WIPCommit: "0123abcd", Skipped: []string{"build/big.bin"}, TranscriptOffset: &offset,
		CheckpointAgeSeconds: &age, Problems: []string{"the halt did not end in 10s"}}}
	if got := env.API(identity, "POST", dispatcherapi.Path+"/executions/"+execution+"/end", end); got.Status != http.StatusOK &&
		got.Status != http.StatusNoContent {
		t.Fatalf("the end call: %d %s", got.Status, got.Body)
	}

	waitForEvent(t, env, id, "execution_ended")
	events := env.RunEvents(id)
	order := kinds(events)
	if i, j := slices.Index(order, "preemption"), slices.Index(order, "execution_ended"); i < 0 || j < i {
		t.Errorf("the run events: %v, want preemption, then execution_ended", order)
	}
	preemption := eventOf(t, events, "preemption")
	checkFields(t, preemption, map[string]any{"execution.id": execution, "preemption.reason": dispatcherapi.EndPreempted,
		"preemption.notice_at": "2026-11-15T03:00:00Z", "preemption.seconds_to_saved": 21.5,
		"preemption.seconds_to_end": float64(22), "preemption.wip_commit": "0123abcd",
		"preemption.transcript_offset": float64(8192), "preemption.checkpoint_age_seconds": 240.5})
	for key, want := range map[string]string{"preemption.skipped": "build/big.bin",
		"preemption.problems": "the halt did not end in 10s"} {
		if list, _ := preemption[key].([]any); len(list) != 1 || list[0] != want {
			t.Errorf("preemption event: %s = %#v, want [%q]", key, preemption[key], want)
		}
	}
}

func TestTheDispatcherRefusesAPreemptionCallThatIsNotValid(t *testing.T) {
	env, ana := newEnv(t)
	wait, open := gate(t)
	env.Runner.Agent(takeTask, initLine(t), wait, reportStep("--outcome", "done", "--summary", "Fixed"),
		reply, result, waitForEnd)
	id := start(t, env, ana, "Fix the cart")
	run := waitFor(t, env, ana, id, dispatcherapi.StateRunning)
	execution := run.Executions[0].ID
	identity := env.Runner.Identity(factorytest.Workload(execution))
	call := func(verb string, body any) factorytest.Answer {
		return env.API(identity, "POST", dispatcherapi.Path+"/executions/"+execution+"/"+verb, body)
	}

	if got := call("preempting", dispatcherapi.Preempting{Reason: "the cloud said so"}); got.Status != http.StatusBadRequest {
		t.Errorf("a preempting call with another reason: %d %s", got.Status, got.Body)
	}
	end := dispatcherapi.ExecutionEnd{Reason: dispatcherapi.EndPreempted,
		Preemption: &dispatcherapi.Preemption{Reason: dispatcherapi.EndMachineStopped}}
	if got := call("end", end); got.Status != http.StatusBadRequest {
		t.Errorf("an end whose preemption has another reason: %d %s", got.Status, got.Body)
	}
	if got := env.API(env.Runner.Identity(factorytest.Workload("other")), "POST",
		dispatcherapi.Path+"/executions/"+execution+"/preempting",
		dispatcherapi.Preempting{Reason: dispatcherapi.EndPreempted}); got.Status != http.StatusUnauthorized {
		t.Errorf("a preempting call with the JWT of another workload: %d %s", got.Status, got.Body)
	}

	open()
	waitFor(t, env, ana, id, dispatcherapi.StateDone)
	if got := call("preempting", dispatcherapi.Preempting{Reason: dispatcherapi.EndPreempted}); got.Status != http.StatusConflict {
		t.Errorf("a preempting call after the end: %d %s", got.Status, got.Body)
	}
	if slices.Contains(kinds(env.RunEvents(id)), "preempting") {
		t.Errorf("a refused call made a preempting event: %v", kinds(env.RunEvents(id)))
	}
}

// waitForExit waits until the Execution's wrapper exited, and gives its
// exit code.
func waitForExit(t *testing.T, env *factorytest.Env, execution string) int {
	t.Helper()
	waitUntil(t, "the wrapper exits", func() bool {
		state, _ := env.Runner.Fake.State(execution)
		return state.Ended()
	})
	ev, err := env.Runner.Fake.Wait(t.Context(), execution)
	if err != nil {
		t.Fatal(err)
	}
	return ev.ExitCode
}
