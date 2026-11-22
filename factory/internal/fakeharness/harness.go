// Package fakeharness is the scripted Harness of Seam 3. It takes the place
// of `claude -p --input-format stream-json --output-format stream-json`: it
// reads the wrapper's messages on stdin, plays a recorded stream on stdout,
// gives a replay echo with the uuid of each message that it delivers, and
// writes the Agent session's transcript directory, as Claude Code does.
//
// A stream is a .jsonl file. Each line is an event that the Harness writes
// to stdout, or a step for the player (an object with a "fake" key):
//
//	{"fake":"about", ...}                  notes on the stream; the player skips it
//	{"fake":"await_message"}               wait for the next message and deliver it at a turn start
//	{"fake":"await_message","mid_turn":true}  the same, delivered in the middle of a turn
//	{"fake":"await_message","eof_code":1}  exit code if stdin closes first (default 0)
//	{"fake":"await_message","echo":false}  wait for the next message, and deliver it later
//	{"fake":"deliver","message":0}         deliver message 0 (the first message read)
//	{"fake":"deliver","message":1,"mid_turn":true}  the same, in the middle of a turn
//	{"fake":"await_interrupt"}             wait for the interrupt control request
//	{"fake":"await_eof"}                   wait until stdin closes, then go on: a turn that Claude Code
//	                                       starts by itself, as at a background task's end
//	{"fake":"busy","ms":20,"event":{...}}  write the event every 20 ms until the interrupt control
//	                                       request (eof_code as for await_interrupt): an agent that works
//	                                       for a long time with output
//	{"fake":"sleep","ms":100}              wait in real time
//	{"fake":"exit","code":1}               exit at once
//	{"fake":"run","args":["solidtrace","report","--outcome","done"]}
//	                                       run a program in the working directory, as the agent's
//	                                       Bash tool does; its output goes to stderr
//	{"fake":"tool","args":["sleep","300"],"eof_code":7}
//	                                       the same, as a foreground tool call that the interrupt control
//	                                       request stops: the Harness kills the program's process group,
//	                                       as Claude Code stops a tool's processes, and goes on (eof_code
//	                                       when stdin closes first). A program that exits first goes on too
//	{"fake":"stop","replies":[[...],[...]]}  end the turn: run the Stop hooks of --settings, as Claude
//	                                       Code does before the result. Each refusal gives the feedback
//	                                       event, then the lines of the next reply (the last one again
//	                                       when there are more refusals), and the hooks run again, until
//	                                       they allow the stop. After 10 refusals the Harness exits
//	                                       with code 3.
//
// At the end of the stream the Harness exits with code 0. A stream that
// ends with await_message waits between turns, as Claude Code does: for
// each later message it gives the replay echo, then plays a default turn
// (the stream's init event, an assistant text and a success result), and
// waits again, until stdin closes.
// In each event, the
// player sets "session_id" to the Agent session id, gives "uuid" a new
// value, and puts the uuid of message N in place of each string
// "$message:N" (Claude Code names a message's uuid in events such as
// command_lifecycle before it delivers the message); in the init event it also sets "cwd" and "permissionMode" and
// removes the --disallowedTools from "tools".
//
// A test or the wrapper chooses the stream with FAKE_HARNESS_STREAM (a name
// from Names) or FAKE_HARNESS_SCRIPT (a file path).
package fakeharness

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed streams/*.jsonl
var streams embed.FS

// Version is the Claude Code version that the recorded streams follow.
const Version = "2.1.325"

// Names gives the names of the recorded streams, in order.
func Names() []string {
	entries, _ := streams.ReadDir("streams")
	var names []string
	for _, e := range entries {
		names = append(names, strings.TrimSuffix(e.Name(), ".jsonl"))
	}
	sort.Strings(names)
	return names
}

// Stream gives the recorded stream with this name, or an error that names
// the streams that there are.
func Stream(name string) ([]byte, error) {
	b, err := streams.ReadFile(path.Join("streams", name+".jsonl"))
	if err != nil || strings.ContainsAny(name, "/\\") {
		return nil, fmt.Errorf("no recorded stream named %q (there are: %s)", name, strings.Join(Names(), ", "))
	}
	return b, nil
}

// MainIfAsked runs the Harness and exits when the environment asks for it
// (FAKE_HARNESS=1). Call it first in a TestMain, so that the test binary
// itself is the harness program: see Command.
func MainIfAsked() {
	if os.Getenv("FAKE_HARNESS") == "1" {
		os.Exit(Main(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, os.Getenv))
	}
}

// Command gives the program and the environment that start the Harness
// with the stream that has this name, when the test binary calls
// MainIfAsked in its TestMain.
func Command(stream string) (program string, env []string) {
	return os.Args[0], []string{"FAKE_HARNESS=1", "FAKE_HARNESS_STREAM=" + stream}
}

// Main is the Harness program. args are the command-line arguments after
// the program name.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer, getenv func(string) string) int {
	f, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	if !f.print {
		return interactive(f, stdin, stdout, stderr, getenv)
	}
	script, err := loadScript(getenv)
	if err != nil {
		fmt.Fprintln(stderr, "fake harness:", err)
		return 2
	}
	cwd, _ := os.Getwd()
	h := &harness{flags: f, cwd: cwd, out: stdout, in: make(chan inbound, 64), interrupts: make(chan struct{}, 8)}
	if err := h.openTranscript(getenv); err != nil {
		fmt.Fprintln(stderr, "Error:", err)
		return 1
	}
	defer h.transcript.Close()
	go h.read(stdin)
	return h.play(script, stderr)
}

type flags struct {
	sessionID, resume, permissionMode string
	settings                          string
	// mcpConfig is the file of --mcp-config ("": none).
	mcpConfig  string
	replay     bool
	disallowed map[string]bool
	// print is the headless form (-p); with no -p, the interactive form.
	print bool
}

func parseFlags(args []string) (flags, error) {
	f := flags{disallowed: map[string]bool{}, permissionMode: "default"}
	var input, output string
	value := func(i *int) string {
		if *i+1 < len(args) {
			*i++
			return args[*i]
		}
		return ""
	}
	for i := 0; i < len(args); i++ {
		a, v, inline := strings.Cut(args[i], "=")
		get := func() string {
			if inline {
				return v
			}
			return value(&i)
		}
		switch a {
		case "-p", "--print":
			f.print = true
		case "--input-format":
			input = get()
		case "--output-format":
			output = get()
		case "--replay-user-messages":
			f.replay = true
		case "--session-id":
			f.sessionID = get()
		case "--resume", "-r":
			f.resume = get()
		case "--permission-mode":
			f.permissionMode = get()
		case "--dangerously-skip-permissions":
			f.permissionMode = "bypassPermissions"
		case "--disallowedTools", "--disallowed-tools":
			for _, t := range strings.FieldsFunc(get(), func(r rune) bool { return r == ',' || r == ' ' }) {
				f.disallowed[t] = true
			}
		case "--settings":
			f.settings = get()
		case "--mcp-config":
			f.mcpConfig = get()
		case "--model", "--setting-sources", "--append-system-prompt", "--allowedTools", "--allowed-tools", "--add-dir":
			get()
		}
	}
	switch {
	case !f.print && (input != "" || output != ""):
		return f, errors.New("--input-format and --output-format need --print")
	case !f.print:
	case input != "stream-json" || output != "stream-json":
		return f, errors.New("the fake harness needs --input-format stream-json and --output-format stream-json")
	case f.sessionID != "" && f.resume != "":
		return f, errors.New("--session-id can only be used with --resume if --fork-session is also specified")
	}
	return f, nil
}

func loadScript(getenv func(string) string) ([]map[string]any, error) {
	var b []byte
	var err error
	switch {
	case getenv("FAKE_HARNESS_SCRIPT") != "":
		b, err = os.ReadFile(getenv("FAKE_HARNESS_SCRIPT"))
	case getenv("FAKE_HARNESS_STREAM") != "":
		b, err = Stream(getenv("FAKE_HARNESS_STREAM"))
	default:
		err = errors.New("no stream: set FAKE_HARNESS_STREAM or FAKE_HARNESS_SCRIPT")
	}
	if err != nil {
		return nil, err
	}
	return ParseScript(b)
}

// ParseScript reads a stream's lines, and refuses a line that is not a
// JSON object or a step that the player does not know.
func ParseScript(b []byte) ([]map[string]any, error) {
	var lines []map[string]any
	for n, raw := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		var o map[string]any
		if err := json.Unmarshal([]byte(raw), &o); err != nil {
			return nil, fmt.Errorf("line %d: %v", n+1, err)
		}
		if step, ok := o["fake"]; ok {
			switch step {
			case "about", "await_message", "await_interrupt", "busy", "deliver", "sleep", "exit", "run", "tool", "stop",
				"await_eof":
			default:
				return nil, fmt.Errorf("line %d: no step %q", n+1, step)
			}
		}
		lines = append(lines, o)
	}
	return lines, nil
}

type inbound struct {
	msg map[string]any
	eof bool
}

type harness struct {
	flags      flags
	cwd        string
	sessionID  string
	out        io.Writer
	outMu      sync.Mutex
	transcript *os.File
	lastUUID   any
	in         chan inbound
	interrupts chan struct{}
	messages   []map[string]any // the messages read, in order
}

// openTranscript makes the transcript directory, as Claude Code does:
// <config dir>/projects/<project dir name>/<session id>.jsonl, and the
// <session id>/ directory beside it.
func (h *harness) openTranscript(getenv func(string) string) error {
	config := getenv("CLAUDE_CONFIG_DIR")
	if config == "" {
		config = filepath.Join(getenv("HOME"), ".claude")
	}
	project := getenv("CLAUDE_CODE_PROJECT_DIR_NAME")
	if project == "" {
		project = ProjectDirName(h.cwd)
	}
	dir := filepath.Join(config, "projects", project)
	h.sessionID = h.flags.sessionID
	if h.flags.resume != "" {
		h.sessionID = h.flags.resume
	}
	if h.sessionID == "" {
		h.sessionID = newUUID()
	}
	file := filepath.Join(dir, h.sessionID+".jsonl")
	_, statErr := os.Stat(file)
	switch {
	case h.flags.resume != "" && statErr != nil:
		return fmt.Errorf("No conversation found with session ID: %s", h.sessionID)
	case h.flags.resume == "" && statErr == nil:
		return fmt.Errorf("Session ID %s is already in use.", h.sessionID)
	}
	if h.flags.resume != "" {
		// A resumed session continues the parentUuid chain from the last
		// line of the transcript that has a uuid.
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		for _, l := range strings.Split(string(b), "\n") {
			var line map[string]any
			if json.Unmarshal([]byte(l), &line) == nil && line["uuid"] != nil {
				h.lastUUID = line["uuid"]
			}
		}
	}
	if err := os.MkdirAll(filepath.Join(dir, h.sessionID), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	h.transcript = f
	return nil
}

// ProjectDirName is the project directory name that Claude Code gives a
// working directory: each character that is not a letter or a digit
// becomes "-".
func ProjectDirName(cwd string) string {
	return strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, cwd)
}

// read takes stdin lines. It answers each control request at once, and
// keeps each user message for the player to deliver.
func (h *harness) read(stdin io.Reader) {
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var o map[string]any
		if json.Unmarshal(sc.Bytes(), &o) != nil {
			continue
		}
		switch o["type"] {
		case "control_request":
			req, _ := o["request"].(map[string]any)
			h.write(map[string]any{"type": "control_response", "response": map[string]any{
				"subtype": "success", "request_id": o["request_id"]}})
			if req["subtype"] == "interrupt" {
				select {
				case h.interrupts <- struct{}{}:
				default:
				}
			}
		case "user":
			h.in <- inbound{msg: o}
		}
	}
	h.in <- inbound{eof: true}
	close(h.interrupts)
}

func (h *harness) play(script []map[string]any, stderr io.Writer) int {
	var last map[string]any // the last line that is not notes
	for _, line := range script {
		if line["fake"] != "about" {
			last = line
		}
		if code, exit := h.step(line, stderr); exit {
			return code
		}
	}
	if last == nil || last["fake"] != "await_message" {
		return 0
	}
	// A message came after the stream's last turn.
	for {
		h.defaultTurn(script)
		next := <-h.in
		if next.eof {
			return intOf(last["eof_code"])
		}
		h.messages = append(h.messages, next.msg)
		h.deliver(next.msg, false)
	}
}

// step plays one line of a stream. With exit, the Harness exits with code.
func (h *harness) step(line map[string]any, stderr io.Writer) (code int, exit bool) {
	switch line["fake"] {
	case nil:
		h.event(line)
	case "await_message":
		next := <-h.in
		if next.eof {
			h.in <- next
			return intOf(line["eof_code"]), true
		}
		h.messages = append(h.messages, next.msg)
		if line["echo"] != false {
			h.deliver(next.msg, line["mid_turn"] == true)
		}
	case "deliver":
		n := intOf(line["message"])
		if n >= len(h.messages) {
			fmt.Fprintf(stderr, "fake harness: deliver message %d, but %d messages were read\n", n, len(h.messages))
			return 2, true
		}
		h.deliver(h.messages[n], line["mid_turn"] == true)
	case "await_interrupt":
		if _, ok := <-h.interrupts; !ok {
			return intOf(line["eof_code"]), true
		}
	case "await_eof":
		// The messages that come first are read, not delivered.
		for next := <-h.in; ; next = <-h.in {
			if next.eof {
				h.in <- next
				break
			}
			h.messages = append(h.messages, next.msg)
		}
	case "busy":
		if !h.busy(line) {
			return intOf(line["eof_code"]), true
		}
	case "sleep":
		time.Sleep(time.Duration(intOf(line["ms"])) * time.Millisecond)
	case "exit":
		return intOf(line["code"]), true
	case "run":
		h.run(line, stderr)
	case "tool":
		if !h.tool(line, stderr) {
			return intOf(line["eof_code"]), true
		}
	case "stop":
		return h.stop(line, stderr)
	}
	return 0, false
}

// maxStopRefusals is how many refusals of one stop the Harness takes
// before it exits with code 3: a hook that never allows a stop is a fault
// that a test must see.
const maxStopRefusals = 10

// stop runs the Stop hooks until they allow the stop. After each refusal
// it gives the feedback event, and plays the agent's next reply.
func (h *harness) stop(line map[string]any, stderr io.Writer) (int, bool) {
	replies, _ := line["replies"].([]any)
	for refusals := 0; ; refusals++ {
		reason, refused := h.runStopHooks(refusals > 0, stderr)
		if !refused {
			return 0, false
		}
		if refusals == maxStopRefusals {
			fmt.Fprintf(stderr, "fake harness: the Stop hook refused %d times\n", maxStopRefusals)
			return 3, true
		}
		content := []any{map[string]any{"type": "text", "text": "Stop hook feedback:\n" + reason}}
		h.event(map[string]any{"type": "user", "isSynthetic": true, "message": map[string]any{"role": "user",
			"content": content}, "parent_tool_use_id": nil, "session_id": "", "timestamp": timestamp(), "uuid": ""})
		if len(replies) == 0 {
			continue
		}
		reply, _ := replies[min(refusals, len(replies)-1)].([]any)
		for _, l := range reply {
			l, _ := l.(map[string]any)
			if code, exit := h.step(l, stderr); exit {
				return code, true
			}
		}
	}
}

// runStopHooks runs each Stop hook command of the --settings file with the
// hook input on stdin, as Claude Code does. A hook refuses the stop with
// the decision block on stdout (exit code 0), or with exit code 2 (the
// reason on stderr). Another exit code is an error that does not refuse.
func (h *harness) runStopHooks(active bool, stderr io.Writer) (reason string, refused bool) {
	if h.flags.settings == "" {
		return "", false
	}
	b, err := os.ReadFile(h.flags.settings)
	if err != nil {
		b = []byte(h.flags.settings) // --settings can be the JSON text
	}
	var settings struct {
		Hooks struct {
			Stop []struct {
				Hooks []struct {
					Type    string `json:"type"`
					Command string `json:"command"`
				} `json:"hooks"`
			} `json:"Stop"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &settings); err != nil {
		fmt.Fprintln(stderr, "fake harness: the --settings are not JSON:", err)
		return "", false
	}
	input, _ := json.Marshal(map[string]any{"session_id": h.sessionID, "transcript_path": h.transcript.Name(),
		"cwd": h.cwd, "permission_mode": h.flags.permissionMode, "hook_event_name": "Stop", "stop_hook_active": active})
	var reasons []string
	for _, matcher := range settings.Hooks.Stop {
		for _, hook := range matcher.Hooks {
			if hook.Type != "command" {
				continue
			}
			cmd := exec.Command("sh", "-c", hook.Command)
			cmd.Dir = h.cwd
			cmd.Stdin = bytes.NewReader(input)
			var out, errOut bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &errOut
			err := cmd.Run()
			var decision struct{ Decision, Reason string }
			switch {
			case err == nil && json.Unmarshal(out.Bytes(), &decision) == nil && decision.Decision == "block":
				reasons = append(reasons, decision.Reason)
			case cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 2:
				reasons = append(reasons, strings.TrimSpace(errOut.String()))
			case err != nil:
				fmt.Fprintf(stderr, "fake harness: the Stop hook failed (%v): %s\n", err, strings.TrimSpace(errOut.String()))
			}
		}
	}
	return strings.Join(reasons, "\n"), len(reasons) > 0
}

// defaultTurn plays a short turn for a message that the stream does not
// script: the stream's init event, an assistant text and a success result.
func (h *harness) defaultTurn(script []map[string]any) {
	model := "claude-haiku-5-5"
	for _, line := range script {
		if line["type"] == "system" && line["subtype"] == "init" {
			h.event(line)
			if m, ok := line["model"].(string); ok {
				model = m
			}
			break
		}
	}
	const text = "Done."
	h.event(map[string]any{"type": "assistant", "message": map[string]any{"model": model, "id": "msg_01FakeDefaultTurn",
		"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "text", "text": text}},
		"stop_reason": "end_turn", "stop_sequence": nil}, "parent_tool_use_id": nil, "session_id": "", "uuid": ""})
	h.event(map[string]any{"type": "result", "subtype": "success", "is_error": false, "duration_ms": 1, "duration_api_ms": 1,
		"num_turns": 1, "result": text, "stop_reason": "end_turn", "session_id": "", "total_cost_usd": 0})
}

// deliver gives the replay echo of a message and writes it to the
// transcript: as a user line at a turn start, or as the three queue lines
// of a message absorbed in the middle of a turn.
func (h *harness) deliver(msg map[string]any, midTurn bool) {
	id, _ := msg["uuid"].(string)
	if id == "" {
		id = newUUID()
	}
	now := timestamp()
	if h.flags.replay {
		h.write(map[string]any{"type": "user", "message": msg["message"], "session_id": h.sessionID,
			"parent_tool_use_id": nil, "uuid": id, "timestamp": now, "isReplay": true})
	}
	message, _ := msg["message"].(map[string]any)
	if !midTurn {
		h.writeTranscript(map[string]any{"type": "user", "message": message, "uuid": id, "promptSource": "sdk"})
		return
	}
	delivery := newUUID()
	h.writeTranscript(map[string]any{"type": "queue-operation", "operation": "enqueue", "content": message["content"]})
	h.writeTranscript(map[string]any{"type": "attachment", "attachment": map[string]any{"type": "queued_command",
		"prompt": message["content"], "source_uuid": id, "delivery_id": delivery, "timestamp": now}, "uuid": newUUID()})
	h.writeTranscript(map[string]any{"type": "queue-operation", "operation": "remove", "reason": "absorbed_mid_turn",
		"commandUuid": id, "deliveryId": delivery})
}

// busy writes the step's event every ms milliseconds until the interrupt
// control request. It is false when stdin closed first.
func (h *harness) busy(step map[string]any) bool {
	ev, _ := step["event"].(map[string]any)
	tick := time.NewTicker(time.Duration(max(intOf(step["ms"]), 1)) * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case _, ok := <-h.interrupts:
			return ok
		case <-tick.C:
			if ev != nil {
				h.event(ev)
			}
		}
	}
}

func (h *harness) event(line map[string]any) {
	e := h.fill(line).(map[string]any)
	if _, ok := e["session_id"]; ok {
		e["session_id"] = h.sessionID
	}
	if _, ok := e["uuid"]; ok {
		e["uuid"] = newUUID()
	}
	if e["type"] == "system" && e["subtype"] == "init" {
		e["cwd"] = h.cwd
		e["permissionMode"] = h.flags.permissionMode
		if tools, ok := e["tools"].([]any); ok {
			kept := []any{}
			for _, t := range tools {
				if name, _ := t.(string); !h.flags.disallowed[name] {
					kept = append(kept, t)
				}
			}
			e["tools"] = kept
		}
	}
	h.write(e)
	// Claude Code keeps the messages in the transcript, not the stream's
	// system and result events. The Stop hook's feedback is a line with
	// isMeta.
	switch {
	case e["type"] == "user" && e["isSynthetic"] == true:
		h.writeTranscript(map[string]any{"type": "user", "isMeta": true, "message": e["message"], "uuid": e["uuid"]})
	case e["type"] == "assistant" || e["type"] == "user":
		h.writeTranscript(map[string]any{"type": e["type"], "message": e["message"], "uuid": e["uuid"]})
	}
}

// fill gives a deep copy of v with the uuid of message N in place of each
// string "$message:N".
func (h *harness) fill(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			out[k] = h.fill(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = h.fill(x)
		}
		return out
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "$message:%d", &n); err == nil && n < len(h.messages) {
			id, _ := h.messages[n]["uuid"].(string)
			return id
		}
	}
	return v
}

func (h *harness) write(e map[string]any) {
	b, _ := json.Marshal(e)
	h.outMu.Lock()
	defer h.outMu.Unlock()
	h.out.Write(append(b, '\n'))
}

func (h *harness) writeTranscript(line map[string]any) {
	line["sessionId"] = h.sessionID
	line["cwd"] = h.cwd
	line["version"] = Version
	line["isSidechain"] = false
	if _, ok := line["timestamp"]; !ok {
		line["timestamp"] = timestamp()
	}
	if id, ok := line["uuid"]; ok {
		line["parentUuid"] = h.lastUUID
		h.lastUUID = id
	}
	b, _ := json.Marshal(line)
	h.transcript.Write(append(b, '\n'))
}

// run runs the step's program with the harness's environment, in its
// working directory, and waits for it.
func (h *harness) run(line map[string]any, stderr io.Writer) {
	raw, _ := line["args"].([]any)
	var args []string
	for _, a := range raw {
		s, _ := a.(string)
		args = append(args, s)
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "fake harness: a run step needs args")
		return
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = h.cwd
	cmd.Stdout, cmd.Stderr = stderr, stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(stderr, "fake harness: run %s: %v\n", strings.Join(args, " "), err)
	}
}

// tool runs the step's program as a foreground tool call, in a process
// group of its own, until it exits or the interrupt control request
// comes. At the interrupt it kills the group and waits for the program.
// It is false when stdin closed first (the group is killed then too).
func (h *harness) tool(line map[string]any, stderr io.Writer) bool {
	raw, _ := line["args"].([]any)
	var args []string
	for _, a := range raw {
		s, _ := a.(string)
		args = append(args, s)
	}
	if len(args) == 0 {
		fmt.Fprintln(stderr, "fake harness: a tool step needs args")
		return true
	}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = h.cwd
	cmd.Stdout, cmd.Stderr = stderr, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "fake harness: start %s: %v\n", strings.Join(args, " "), err)
		return true
	}
	exited := make(chan struct{})
	go func() {
		cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
		return true
	case _, ok := <-h.interrupts:
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-exited
		return ok
	}
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}

func timestamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
