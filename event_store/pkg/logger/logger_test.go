package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Under Docker, EventStore's output is a pipe, not a terminal. Vector ships
// it as Logs, so a colour code would end up inside a message.
func TestNoColourWhenTheOutputIsNotATerminal(t *testing.T) {
	out, err := os.Create(filepath.Join(t.TempDir(), "out.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	New(out).Info("Started", "port", 4000)

	written, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	line := string(written)
	if strings.Contains(line, "\033[") {
		t.Fatalf("line has a colour code: %q", line)
	}
	if !strings.Contains(line, "level=info") || !strings.Contains(line, "message=Started") {
		t.Fatalf("line is not logfmt with its level and message: %q", line)
	}
}

// A time with spaces in it would split into several logfmt keys.
func TestTheTimestampIsOneLogfmtValue(t *testing.T) {
	out, err := os.Create(filepath.Join(t.TempDir(), "out.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	New(out).Info("Started")

	written, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	value, _, _ := strings.Cut(strings.TrimPrefix(string(written), "timestamp="), " ")
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		t.Fatalf("timestamp is not one RFC 3339 value: %q", written)
	}
}
