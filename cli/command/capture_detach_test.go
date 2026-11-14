package command_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/command"
)

// detachHook runs `capture --detach` as the plugin's hook does, with a
// Detach that keeps the input that it was given.
func (s *agentSession) detachHook(event string, extra map[string]any, detach func([]byte) error) int {
	s.w.t.Helper()
	input := map[string]any{
		"session_id":      s.id,
		"transcript_path": s.file(""),
		"cwd":             s.cwd,
		"hook_event_name": event,
	}
	for k, v := range extra {
		input[k] = v
	}
	data, _ := json.Marshal(input)
	env := s.w.env()
	env.Stdin = bytes.NewReader(data)
	env.Detach = detach
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return command.Run(ctx, []string{"capture", "--detach"}, env)
}

func TestTheHookStartsCaptureInTheBackgroundAndThatCaptureUploads(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	s := w.session(repo)
	s.write("", "{\"n\":1}\n")

	before := w.stdout.String() + w.stderr.String()
	var started [][]byte
	code := s.detachHook("Stop", map[string]any{"last_assistant_message": strings.Repeat("x", 200_000)},
		func(input []byte) error { started = append(started, input); return nil })

	if code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if out := strings.TrimPrefix(w.stdout.String()+w.stderr.String(), before); out != "" {
		t.Fatalf("the hook wrote %q; Claude Code shows or reads a hook's output", out)
	}
	if len(started) != 1 {
		t.Fatalf("background captures started: %d, want 1", len(started))
	}
	if len(w.agent.AllRequests()) != 0 {
		t.Fatal("the hook sent a request itself: it must return at once and leave the network to the background capture")
	}
	if len(started[0]) > 4096 {
		t.Fatalf("the background capture gets %d bytes; it needs only the four fields", len(started[0]))
	}

	// The background process is `solidtrace capture` with that input.
	env := w.env()
	env.Stdin = bytes.NewReader(started[0])
	if code := command.Run(context.Background(), []string{"capture"}, env); code != 0 {
		t.Fatalf("background capture exit %d", code)
	}
	if got := s.stored(s.main()); got != "{\"n\":1}\n" {
		t.Fatalf("server has %q", got)
	}
}

func TestTheHookLogsWhenTheBackgroundCaptureDoesNotStart(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	s := w.session(w.gitRepo("origin=https://bitbucket.org/acme/shop.git"))

	code := s.detachHook("SessionEnd", nil, func([]byte) error { return errors.New("fork: no memory") })

	if code != 0 {
		t.Fatalf("exit %d, want 0: a hook never fails to Claude Code", code)
	}
	if log := w.captureLog(); !strings.Contains(log, "fork: no memory") {
		t.Fatalf("capture.log = %q", log)
	}
}

func TestTheHookStartsNothingForInputThatIsNotJSON(t *testing.T) {
	w := newWorld(t, acme)
	env := w.env()
	env.Stdin = strings.NewReader("not json")
	started := 0
	env.Detach = func([]byte) error { started++; return nil }

	if code := command.Run(context.Background(), []string{"capture", "--detach"}, env); code != 0 {
		t.Fatalf("exit %d, want 0", code)
	}
	if started != 0 {
		t.Fatal("a background capture started for input that is not JSON")
	}
	if log := w.captureLog(); !strings.Contains(log, "not JSON") {
		t.Fatalf("capture.log = %q", log)
	}
}
