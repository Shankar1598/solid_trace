package harnesscontract_test

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/factory/internal/fakeharness"
	"github.com/solidtrace/factory/internal/harnesscontract"
)

// A recording of the real Claude Code, as a contract check keeps it: the
// output events with their times, the uuids of the messages that the check
// wrote, and how the process ended.
func sampleRecording(checkout string) harnesscontract.Recording {
	sid := "0f0f0f0f-1111-4222-8333-444444444444"
	ev := func(ms int, raw string) harnesscontract.Event {
		raw = strings.ReplaceAll(raw, "SID", sid)
		raw = strings.ReplaceAll(raw, "CHECKOUT", checkout)
		var e map[string]any
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			panic(raw)
		}
		return harnesscontract.Event{At: time.Duration(ms) * time.Millisecond, Data: e}
	}
	return harnesscontract.Recording{
		Name:    "sample",
		Notes:   "A sample.",
		Version: "2.1.325",
		Paths:   map[string]string{checkout: "/workspace/repo"},
		Sent:    map[string]bool{"u-1": true, "u-2": true},
		Events: []harnesscontract.Event{
			ev(90, `{"type":"command_lifecycle","command_uuid":"u-1","state":"queued","session_id":"SID","uuid":"x0"}`),
			ev(100, `{"type":"system","subtype":"init","cwd":"CHECKOUT","session_id":"SID","tools":["Bash","Read"],"model":"claude-haiku-5-5","permissionMode":"bypassPermissions","uuid":"x1","user_message_uuids":["u-1"],"messaging_socket_path":"/run/user/1001/x.sock","plugins":[{"name":"a-plugin"}]}`),
			ev(110, `{"type":"user","message":{"role":"user","content":"Run sleep."},"session_id":"SID","uuid":"u-1","isReplay":true,"parent_tool_use_id":null}`),
			ev(900, `{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"thinking","thinking":"I sleep.","signature":"c2VjcmV0"}]},"session_id":"SID","uuid":"x2","parent_tool_use_id":null}`),
			ev(950, `{"type":"assistant","message":{"id":"msg_1","role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"cd CHECKOUT && sleep 12"}}]},"session_id":"SID","uuid":"x3","parent_tool_use_id":null}`),
			ev(1000, `{"type":"control_response","response":{"subtype":"success","request_id":"other"}}`),
			ev(3000, `{"type":"command_lifecycle","command_uuid":"u-2","state":"queued","session_id":"SID","uuid":"x8"}`),
			ev(13000, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":""}]},"session_id":"SID","uuid":"x4","parent_tool_use_id":null}`),
			ev(13010, `{"type":"user","message":{"role":"user","content":"Also BANANA."},"session_id":"SID","uuid":"u-2","isReplay":true,"parent_tool_use_id":null}`),
			ev(14000, `{"type":"assistant","message":{"id":"msg_2","role":"assistant","content":[{"type":"text","text":"BANANA"}]},"session_id":"SID","uuid":"x5","parent_tool_use_id":null}`),
			ev(14100, `{"type":"result","subtype":"success","result":"BANANA","session_id":"SID","uuid":"x6","total_cost_usd":0.002}`),
			ev(19100, `{"type":"system","subtype":"task_notification","task_id":"b1","status":"completed","session_id":"SID","uuid":"x7"}`),
		},
		StdinClosed: true,
		ExitCode:    0,
	}
}

func TestScriptPlaysTheRecordedEventsInOrder(t *testing.T) {
	checkout := t.TempDir()
	rec := sampleRecording(checkout)
	script, err := harnesscontract.Script(rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fakeharness.ParseScript(script); err != nil {
		t.Fatalf("the scripted Harness refuses the script: %v", err)
	}

	events, transcript, code := play(t, script, []string{"u-1", "u-2"})
	want := []string{"command_lifecycle", "system/init", "user", "assistant", "assistant", "command_lifecycle", "user", "user", "assistant", "result/success", "system/task_notification"}
	if got := typesOf(events); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("played %v; want %v", got, want)
	}
	if code != 0 {
		t.Errorf("exit %d; want 0, as the recorded process at stdin close", code)
	}
	// Each event names the uuid that the wrapper gave, and the echo of the
	// second message comes after the tool result, as the real Claude Code
	// gave it: a message during a tool call.
	if events[0]["command_uuid"] != "u-1" || events[1]["user_message_uuids"].([]any)[0] != "u-1" || events[5]["command_uuid"] != "u-2" {
		t.Errorf("events %v; want the wrapper's uuids in the lifecycle and init events", events[:6])
	}
	if events[7]["uuid"] != "u-2" || events[7]["isReplay"] != true {
		t.Errorf("event 7 is %v; want the replay echo of u-2", events[7])
	}
	var queued bool
	for _, l := range transcript {
		if l["type"] == "queue-operation" && l["operation"] == "remove" && l["commandUuid"] == "u-2" {
			queued = true
		}
	}
	if !queued {
		t.Errorf("no absorbed_mid_turn line for u-2 in the transcript: %v", transcript)
	}
}

func TestScriptKeepsNoPrivateValues(t *testing.T) {
	checkout := t.TempDir()
	script, err := harnesscontract.Script(sampleRecording(checkout))
	if err != nil {
		t.Fatal(err)
	}
	s := string(script)
	for _, bad := range []string{checkout, `"signature"`, "c2VjcmV0", "0f0f0f0f-1111", `"x3"`, "/run/user", "a-plugin", `"u-1"`, `"u-2"`} {
		if strings.Contains(s, bad) {
			t.Errorf("the script has %q:\n%s", bad, s)
		}
	}
	if !strings.Contains(s, "cd /workspace/repo && sleep 12") {
		t.Errorf("the checkout path is not replaced in a tool input:\n%s", s)
	}
	if !strings.Contains(s, `"claude_code":"2.1.325"`) {
		t.Errorf("the about line does not give the version:\n%s", s)
	}
}

func TestScriptWaitsForTheInterrupt(t *testing.T) {
	rec := harnesscontract.Recording{
		Name: "interrupt", Version: "2.1.325",
		Sent:       map[string]bool{"u-1": true},
		Interrupts: map[string]bool{"int-1": true},
		Events: []harnesscontract.Event{
			{At: 0, Data: map[string]any{"type": "user", "uuid": "u-1", "isReplay": true, "session_id": "s", "message": map[string]any{"role": "user", "content": "go"}}},
			{At: time.Second, Data: map[string]any{"type": "assistant", "uuid": "a", "session_id": "s", "message": map[string]any{"id": "m", "content": []any{}}}},
			{At: 9 * time.Second, Data: map[string]any{"type": "control_response", "response": map[string]any{"subtype": "success", "request_id": "int-1"}}},
			{At: 9 * time.Second, Data: map[string]any{"type": "result", "subtype": "error_during_execution", "session_id": "s", "uuid": "r"}},
		},
		StdinClosed: true, ExitCode: 1,
	}
	script, err := harnesscontract.Script(rec)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(script), `{"eof_code":1,"fake":"await_interrupt"}`) {
		t.Fatalf("no await_interrupt step:\n%s", script)
	}
	if !strings.HasSuffix(strings.TrimSpace(string(script)), `{"eof_code":1,"fake":"await_message"}`) {
		t.Fatalf("the script does not end with a wait that exits 1 at stdin close:\n%s", script)
	}
}

func TestScriptOfAProcessThatExitedBeforeStdinClosed(t *testing.T) {
	rec := harnesscontract.Recording{
		Name: "x", Version: "2.1.325", Sent: map[string]bool{"u-1": true},
		Events: []harnesscontract.Event{
			{At: 0, Data: map[string]any{"type": "user", "uuid": "u-1", "isReplay": true, "message": map[string]any{"content": "go"}}},
		},
		ExitCode: 1,
	}
	script, _ := harnesscontract.Script(rec)
	if !strings.HasSuffix(strings.TrimSpace(string(script)), `{"code":1,"fake":"exit"}`) {
		t.Fatalf("want an exit step:\n%s", script)
	}
}

// play runs the scripted Harness on a script, writes the messages, closes
// stdin at the end, and gives the events, the transcript and the exit code.
func play(t *testing.T, script []byte, messages []string) ([]map[string]any, []map[string]any, int) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "script.jsonl")
	if err := os.WriteFile(file, script, 0o600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "config")
	env := map[string]string{"FAKE_HARNESS_SCRIPT": file, "CLAUDE_CONFIG_DIR": config, "CLAUDE_CODE_PROJECT_DIR_NAME": "p"}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	codeC := make(chan int, 1)
	go func() {
		codeC <- fakeharness.Main([]string{"-p", "--input-format", "stream-json", "--output-format", "stream-json",
			"--replay-user-messages", "--session-id", "5e551011-0000-4000-8000-00000000c0de"},
			inR, outW, io.Discard, func(k string) string { return env[k] })
		outW.Close()
	}()
	for _, m := range messages {
		b, _ := json.Marshal(map[string]any{"type": "user", "uuid": m, "message": map[string]any{"role": "user", "content": m}})
		inW.Write(append(b, '\n'))
	}
	var events []map[string]any
	sc := bufio.NewScanner(outR)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var e map[string]any
		json.Unmarshal(sc.Bytes(), &e)
		events = append(events, e)
		if e["type"] == "result" {
			// The rest of the stream comes after the result, then the
			// Harness waits for stdin to close.
			go func() { time.Sleep(time.Second); inW.Close() }()
		}
	}
	code := <-codeC
	b, err := os.ReadFile(filepath.Join(config, "projects", "p", "5e551011-0000-4000-8000-00000000c0de.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var o map[string]any
		json.Unmarshal([]byte(l), &o)
		lines = append(lines, o)
	}
	return events, lines, code
}

func typesOf(events []map[string]any) []string {
	var out []string
	for _, e := range events {
		s, _ := e["type"].(string)
		if st, ok := e["subtype"].(string); ok {
			s += "/" + st
		}
		out = append(out, s)
	}
	return out
}
