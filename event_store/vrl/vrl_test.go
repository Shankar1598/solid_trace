package vrl_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/vrl"
)

func compile(t *testing.T, source string) *vrl.Program {
	t.Helper()
	program, diagnostics := vrl.Compile(source)
	if program == nil {
		t.Fatalf("Compile(%q) failed: %+v", source, diagnostics)
	}
	t.Cleanup(program.Free)
	return program
}

func TestCompileGivesDiagnosticsWithSpans(t *testing.T) {
	source := ".level = \"info\"\n.x = nope(.message)"
	program, diagnostics := vrl.Compile(source)
	if program != nil {
		program.Free()
		t.Fatal("Compile returned a program for a call to an undefined function")
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v, want one", diagnostics)
	}
	d := diagnostics[0]
	if d.Severity != "error" || d.Code != 105 || d.Message != "call to undefined function" {
		t.Errorf("diagnostic = %q %d %q, want error 105 call to undefined function", d.Severity, d.Code, d.Message)
	}
	var primary *vrl.Label
	for i := range d.Labels {
		if d.Labels[i].Primary {
			primary = &d.Labels[i]
		}
	}
	if primary == nil {
		t.Fatalf("labels = %+v, want a primary one", d.Labels)
	}
	if got := source[primary.Start:primary.End]; got != "nope" {
		t.Errorf("primary label spans %q, want %q", got, "nope")
	}
	if !strings.Contains(d.Formatted, "error[E105]: call to undefined function") || !strings.Contains(d.Formatted, ".x = nope(.message)") {
		t.Errorf("formatted = %q, want VRL's text quoting the line", d.Formatted)
	}
}

func TestCompileRefusesNetworkAndEnvironmentFunctions(t *testing.T) {
	for _, source := range []string{
		`.body = http_request!("https://example.com")`,
		`.home = get_env_var!("HOME")`,
		`.host = get_hostname!()`,
		`.ips = dns_lookup!("example.com")`,
		`.raw = decode_gzip!(.message)`,
	} {
		program, diagnostics := vrl.Compile(source)
		if program != nil {
			program.Free()
			t.Errorf("Compile(%q) returned a program", source)
			continue
		}
		if len(diagnostics) == 0 || diagnostics[0].Code != 105 {
			t.Errorf("Compile(%q) diagnostics = %+v, want E105", source, diagnostics)
		}
	}
}

func TestCompileKeepsWarningsWithTheProgram(t *testing.T) {
	program, diagnostics := vrl.Compile(".a = 1\n\"unused\"\n.b = 2")
	if program == nil {
		t.Fatalf("Compile failed: %+v", diagnostics)
	}
	program.Free()
	if len(diagnostics) != 1 || diagnostics[0].Severity != "warning" {
		t.Errorf("diagnostics = %+v, want one warning", diagnostics)
	}
}

// sidekiqProgram is the spec's probe program: it parses a sidekiq.log job
// line into four Attributes and deletes the message.
const sidekiqProgram = `
parsed, err = parse_regex(.message, r'^\S+ (?P<pid>\d+) TID-(?P<tid>\S+) (?P<worker>\S+) JID-(?P<jid>\S+) INFO: (?P<status>start|done|fail)(?:: (?P<duration>[\d.]+) sec)?')
if err == null {
  .sidekiq.worker = parsed.worker
  .sidekiq.jid = parsed.jid
  .sidekiq.status = parsed.status
  .sidekiq.duration = to_float(parsed.duration) ?? null
  del(.message)
}
`

func TestRunReturnsTheLogsTheProgramChanged(t *testing.T) {
	program := compile(t, sidekiqProgram)
	at := time.Date(2026, 10, 21, 6, 58, 49, 598_000_000, time.UTC)
	outcomes, err := program.Run([]map[string]any{
		{"time": at, "level": "info", "message": "2026-10-21T06:58:49.598Z 9515 TID-gmxjgddu8 AnalyticsEventProcessorWorker JID-fb60f1964d1872965850d482 INFO: done: 0.575 sec", "file": "sidekiq.log", "count": int64(3)},
		{"time": at, "message": "Booting Sidekiq 7.2.4", "file": "sidekiq.log"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []vrl.Outcome{
		{Log: map[string]any{"time": at, "level": "info", "file": "sidekiq.log", "count": int64(3), "sidekiq": map[string]any{
			"worker": "AnalyticsEventProcessorWorker", "jid": "fb60f1964d1872965850d482", "status": "done", "duration": 0.575,
		}}},
		{Log: map[string]any{"time": at, "message": "Booting Sidekiq 7.2.4", "file": "sidekiq.log"}},
	}
	if !reflect.DeepEqual(outcomes, want) {
		t.Errorf("outcomes =\n%#v\nwant\n%#v", outcomes, want)
	}
}

func TestRunDropsTheLogsTheProgramAborts(t *testing.T) {
	program := compile(t, `if .level == "debug" { abort }`)
	outcomes, err := program.Run([]map[string]any{{"level": "debug"}, {"level": "info"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []vrl.Outcome{{Dropped: true}, {Log: map[string]any{"level": "info"}}}
	if !reflect.DeepEqual(outcomes, want) {
		t.Errorf("outcomes = %#v, want %#v", outcomes, want)
	}
}

func TestRunFailsTheLogsTheProgramErrsOn(t *testing.T) {
	program := compile(t, `.parsed = parse_json!(.message)`)
	outcomes, err := program.Run([]map[string]any{{"message": "not json"}, {"message": `{"a":1}`}})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Log != nil || outcomes[0].Dropped || !strings.Contains(outcomes[0].Error, "function call error for \"parse_json\"") {
		t.Errorf("outcomes[0] = %#v, want a parse_json failure", outcomes[0])
	}
	if got := outcomes[1].Log["parsed"]; !reflect.DeepEqual(got, map[string]any{"a": int64(1)}) {
		t.Errorf("outcomes[1].parsed = %#v, want {a: 1}", got)
	}
}

func TestRunFailsWhenTheProgramLeavesANonObject(t *testing.T) {
	program := compile(t, `. = "gone"`)
	outcomes, err := program.Run([]map[string]any{{"message": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Log != nil || outcomes[0].Error != ". is a string, not an object" {
		t.Errorf("outcomes = %#v, want a failure naming the string", outcomes)
	}
}

func TestRunTurnsAPanicIntoAFailureForThatLog(t *testing.T) {
	// encode_charset panics on bytes that aren't UTF-8, such as 0xff.
	program := compile(t, `.out = encode_charset!(decode_base64!(.data), "utf-8")`)
	outcomes, err := program.Run([]map[string]any{{"data": "/w=="}, {"data": "aGk="}})
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Log != nil || !strings.HasPrefix(outcomes[0].Error, "panic: ") {
		t.Errorf("outcomes[0] = %#v, want a panic failure", outcomes[0])
	}
	if got := outcomes[1].Log["out"]; got != "hi" {
		t.Errorf("outcomes[1].out = %#v, want hi", got)
	}
}

func TestRunWorksInUTCWhateverTheHostTimeZone(t *testing.T) {
	t.Setenv("TZ", "Asia/Kolkata")
	program := compile(t, `.at = parse_timestamp!("2026-10-21 06:58:49", "%Y-%m-%d %H:%M:%S")`)
	outcomes, err := program.Run([]map[string]any{{}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := outcomes[0].Log["at"], time.Date(2026, 10, 21, 6, 58, 49, 0, time.UTC); got != want {
		t.Errorf("at = %v, want %v", got, want)
	}
}

func TestRunFailsOnlyTheLogNestedTooDeep(t *testing.T) {
	deep := map[string]any{"leaf": true}
	for range 70 {
		deep = map[string]any{"a": deep}
	}
	program := compile(t, `.seen = true`)
	outcomes, err := program.Run([]map[string]any{deep, {"message": "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []vrl.Outcome{{Error: "the Log nests more than 64 levels deep"}, {Log: map[string]any{"message": "hi", "seen": true}}}
	if !reflect.DeepEqual(outcomes, want) {
		t.Errorf("outcomes = %#v, want %#v", outcomes, want)
	}
}

func TestRunReadsStringsThatAreNotUTF8(t *testing.T) {
	program := compile(t, `.copy = .message`)
	outcomes, err := program.Run([]map[string]any{{"message": "caf\xe9"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := outcomes[0].Log["copy"]; got != "caf�" {
		t.Errorf("copy = %q, want %q", got, "caf�")
	}
}

func TestRunRefusesAFreedProgram(t *testing.T) {
	program, _ := vrl.Compile(`.a = 1`)
	program.Free()
	if _, err := program.Run([]map[string]any{{}}); err == nil {
		t.Error("Run on a freed program succeeded")
	}
}
