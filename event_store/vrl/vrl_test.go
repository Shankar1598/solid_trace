package vrl_test

import (
	"strings"
	"testing"

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
