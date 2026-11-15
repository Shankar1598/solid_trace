// Package harnesscontract is Seam 4, the Harness contract checks. Its
// opt-in tests run the real Claude Code at the pinned version with a cheap
// model, check the behaviour that the wrapper depends on, and save the
// output streams that they record for the scripted Harness of Seam 3
// (factory/internal/fakeharness/streams).
//
// This file turns a recording into a script that the scripted Harness
// plays. The tests are in contract_test.go; they run only when
// FACTORY_HARNESS_CONTRACT=1.
package harnesscontract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Event is one line that the real Claude Code wrote to stdout, and the
// time it came, from the start of the process.
type Event struct {
	At   time.Duration
	Data map[string]any
}

// Recording is what a contract check keeps of one Claude Code process.
type Recording struct {
	Name    string // the stream's name in fakeharness.Names
	Notes   string // what the stream shows, for the about line
	Version string // the Claude Code version that made it
	Model   string // the model that the check asked for
	// Paths maps each local path (the checkout, the config directory, the
	// home directory) to the text that takes its place in the script.
	Paths map[string]string
	// Sent holds the uuids of the messages that the check wrote, and
	// Interrupts the request ids of its interrupt control requests.
	Sent       map[string]bool
	Interrupts map[string]bool
	Events     []Event
	// StdinClosed is true when the process exited after the check closed
	// its stdin, and false when it exited first.
	StdinClosed bool
	ExitCode    int
}

// maxSleep is the longest wait that a script keeps. A recorded gap after a
// result (a background task that ends later) becomes a short wait, so that
// Seam 3 stays fast.
const maxSleep = 200 * time.Millisecond

// Script gives the stream file for the scripted Harness:
//
//   - the replay echo of a message that the check wrote becomes an
//     await_message step (mid_turn when the turn had started), because the
//     scripted Harness gives the echo itself. When an event names the
//     message's uuid before its echo (command_lifecycle, the init event's
//     user_message_uuids), the script takes the message there with
//     await_message and echo false, and delivers it at the echo with a
//     deliver step. In each event, the uuid of the N-th message becomes
//     "$message:N", which the player fills;
//   - the answer to an interrupt request becomes an await_interrupt step,
//     and the answers to other control requests are dropped;
//   - a gap of more than a second after a result becomes a short sleep;
//   - session ids, uuids and the cwd become "", which the player fills;
//     local paths become their replacements; thinking signatures, the
//     credential's expiry and the local setup in the init event (socket,
//     memory paths, plugins, skills, slash commands) go;
//   - the end is an await_message with the exit code at stdin close, or
//     an exit step when the process exited first.
func Script(rec Recording) ([]byte, error) {
	var buf bytes.Buffer
	enc := func(v any) error {
		var line bytes.Buffer
		j := json.NewEncoder(&line)
		j.SetEscapeHTML(false)
		if err := j.Encode(v); err != nil {
			return err
		}
		buf.Write(replacePaths(line.Bytes(), rec.Paths))
		return nil
	}
	source := "recorded by the Harness contract checks (factory/internal/harnesscontract)"
	if rec.Model != "" {
		source += ", model " + rec.Model
	}
	if err := enc(map[string]any{"fake": "about", "stream": rec.Name, "claude_code": rec.Version,
		"source": source, "notes": rec.Notes}); err != nil {
		return nil, err
	}
	inTurn, afterResult := false, false
	var last time.Duration
	taken := map[string]int{} // the number of each message that the script took
	for _, ev := range rec.Events {
		e := ev.Data
		echo := e["type"] == "user" && e["isReplay"] == true && rec.Sent[str(e["uuid"])]
		if !echo {
			for _, id := range sentIn(e, rec.Sent) {
				if _, ok := taken[id]; !ok {
					taken[id] = len(taken)
					if err := enc(map[string]any{"fake": "await_message", "echo": false}); err != nil {
						return nil, err
					}
				}
			}
		}
		switch {
		case echo:
			id := str(e["uuid"])
			step := map[string]any{"fake": "await_message"}
			if n, ok := taken[id]; ok {
				step = map[string]any{"fake": "deliver", "message": n}
			} else {
				taken[id] = len(taken)
			}
			if inTurn {
				step["mid_turn"] = true
			}
			if err := enc(step); err != nil {
				return nil, err
			}
			inTurn, afterResult = true, false
			last = ev.At
			continue
		case e["type"] == "control_response":
			resp, _ := e["response"].(map[string]any)
			if rec.Interrupts[str(resp["request_id"])] {
				if err := enc(map[string]any{"fake": "await_interrupt", "eof_code": rec.ExitCode}); err != nil {
					return nil, err
				}
			}
			continue
		}
		if afterResult && ev.At-last > time.Second {
			if err := enc(map[string]any{"fake": "sleep", "ms": min(ev.At-last, maxSleep).Milliseconds()}); err != nil {
				return nil, err
			}
		}
		if err := enc(clean(e, taken)); err != nil {
			return nil, err
		}
		last = ev.At
		switch e["type"] {
		case "assistant":
			inTurn = true
		case "result":
			inTurn, afterResult = false, true
		}
	}
	end := map[string]any{"fake": "exit", "code": rec.ExitCode}
	if rec.StdinClosed {
		end = map[string]any{"fake": "await_message", "eof_code": rec.ExitCode}
	}
	if err := enc(end); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// private holds the keys that the script drops: values of the machine
// and the account that ran the check, which say nothing of the protocol.
var private = map[string]bool{"signature": true, "credential_expires_at": true,
	"messaging_socket_path": true, "memory_paths": true}

// clean gives a copy of an event with the values that the player fills
// set to "" or "$message:N", and with no private value.
func clean(e map[string]any, taken map[string]int) map[string]any {
	out := rewrite(e, taken).(map[string]any)
	for _, k := range []string{"session_id", "uuid"} {
		if _, ok := out[k]; ok {
			out[k] = ""
		}
	}
	if out["type"] == "system" && out["subtype"] == "init" {
		out["cwd"] = ""
		out["permissionMode"] = ""
		for _, k := range []string{"plugins", "skills", "slash_commands", "terminal_slash_commands"} {
			if _, ok := out[k]; ok {
				out[k] = []any{}
			}
		}
	}
	return out
}

// rewrite gives a deep copy of v with no private key, and with
// "$message:N" in place of the uuid of each message that the script took.
func rewrite(v any, taken map[string]int) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			if !private[k] {
				out[k] = rewrite(x, taken)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = rewrite(x, taken)
		}
		return out
	case string:
		if n, ok := taken[v]; ok {
			return fmt.Sprintf("$message:%d", n)
		}
	}
	return v
}

// sentIn gives the uuids of sent messages that an event names, in the
// order that they come in its JSON text.
func sentIn(e map[string]any, sent map[string]bool) []string {
	b, _ := json.Marshal(e)
	s := string(b)
	type at struct {
		id  string
		pos int
	}
	var found []at
	for id := range sent {
		if i := strings.Index(s, id); i >= 0 {
			found = append(found, at{id, i})
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].pos < found[j].pos })
	var ids []string
	for _, f := range found {
		ids = append(ids, f.id)
	}
	return ids
}

// replacePaths puts each replacement in place of its path, the longest
// path first, so that a checkout inside the home directory keeps its own
// replacement. It works on the JSON text, so it also finds a path inside a
// tool's input or output.
func replacePaths(b []byte, paths map[string]string) []byte {
	var keys []string
	for k := range paths {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	s := string(b)
	for _, k := range keys {
		// A path in JSON text has its "/" as is; Go's encoder does not
		// escape it.
		s = strings.ReplaceAll(s, k, paths[k])
	}
	return []byte(s)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// String gives a short form of an event for a test's log.
func (e Event) String() string {
	s := str(e.Data["type"])
	if st := str(e.Data["subtype"]); st != "" {
		s += "/" + st
	}
	return fmt.Sprintf("%6.1fs %s", e.At.Seconds(), s)
}
