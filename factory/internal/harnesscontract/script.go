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
//     scripted Harness gives the echo itself;
//   - the answer to an interrupt request becomes an await_interrupt step,
//     and the answers to other control requests are dropped;
//   - a gap of more than a second after a result becomes a short sleep;
//   - session ids, uuids and the cwd become "", which the player fills;
//     local paths become their replacements, and thinking signatures go;
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
	for _, ev := range rec.Events {
		e := ev.Data
		switch {
		case e["type"] == "user" && e["isReplay"] == true && rec.Sent[str(e["uuid"])]:
			step := map[string]any{"fake": "await_message"}
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
		if err := enc(clean(e)); err != nil {
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

// clean gives a copy of an event with the values that the player fills
// set to "", and with no thinking signature.
func clean(e map[string]any) map[string]any {
	out := drop(e, "signature").(map[string]any)
	for _, k := range []string{"session_id", "uuid"} {
		if _, ok := out[k]; ok {
			out[k] = ""
		}
	}
	if out["type"] == "system" && out["subtype"] == "init" {
		out["cwd"] = ""
		out["permissionMode"] = ""
	}
	return out
}

// drop gives a deep copy of v with no object key named key.
func drop(v any, key string) any {
	switch v := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, x := range v {
			if k != key {
				out[k] = drop(x, key)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = drop(x, key)
		}
		return out
	}
	return v
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
