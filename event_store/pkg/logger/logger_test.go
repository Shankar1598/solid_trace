package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
