package command_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/command"
)

// A list that was kept for one sign-in is never used for another: after
// the person signs in to another Organization, a repository of the first
// Organization sends nothing.
func TestTheListOfAnotherSignInIsNotUsed(t *testing.T) {
	w := newWorld(t, acme, beta)
	w.oauth.Pick = "acme"
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	s := w.session(repo)
	s.write("", "{\"n\":1}\n")
	s.stop()
	if s.stored(s.main()) != "{\"n\":1}\n" {
		t.Fatalf("not uploaded for acme; log: %s", w.captureLog())
	}

	// The list of acme is still younger than ten minutes.
	w.clock.Add(time.Minute)
	w.oauth.Pick = "beta"
	w.signIn()
	s.add("", "{\"n\":2}\n")
	before := len(w.agent.AllRequests())
	s.stop()
	if after := len(w.agent.AllRequests()); after != before {
		t.Errorf("capture sent %d requests with the list of another sign-in", after-before)
	}
	if got := s.stored(s.main()); got != "{\"n\":1}\n" {
		t.Errorf("stored: %q", got)
	}
}

// A clock that went back must not keep a list for longer than ten minutes.
func TestAListFromTheFutureIsNotUsed(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	s := w.session(w.gitRepo("origin=https://bitbucket.org/acme/shop"))
	s.write("", "{\"n\":1}\n")
	s.stop()
	calls := w.agent.Calls

	w.clock.Add(-time.Hour)
	s.add("", "{\"n\":2}\n")
	s.stop()
	if w.agent.Calls != calls+1 {
		t.Errorf("the list of a later time was used: %d list calls, want 1", w.agent.Calls-calls)
	}
}

// The hook gives an absolute transcript path. Any other path sends nothing,
// as the path is then relative to a directory that the hook did not name.
func TestCaptureRefusesATranscriptPathThatIsNotAbsolute(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	s := w.session(repo)
	s.write("", "{\"n\":1}\n")
	t.Chdir(s.dir)

	input, _ := json.Marshal(map[string]any{
		"session_id":      s.id,
		"transcript_path": s.id + ".jsonl",
		"cwd":             repo,
		"hook_event_name": "Stop",
	})
	env := w.env()
	env.Stdin = strings.NewReader(string(input))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if code := command.Run(ctx, []string{"capture"}, env); code != 0 {
		t.Fatalf("capture exit %d, want 0", code)
	}
	if requests := w.agent.AllRequests(); len(requests) != 0 {
		t.Errorf("capture sent %d requests for a relative transcript path", len(requests))
	}
	if !strings.Contains(w.captureLog(), "is not <Agent session id>.jsonl") {
		t.Errorf("log: %s", w.captureLog())
	}
}

// The hook of one Agent session may run in a shell with GIT_DIR set. It must
// not make the working directory of another repository look like a
// captured one.
func TestCaptureReadsTheRemotesOfTheWorkingDirectoryNotOfGITDIR(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	captured := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	private := w.gitRepo("origin=https://github.com/someone/private.git")
	s := w.session(private)
	s.write("", "{\"secret\":1}\n")
	t.Setenv("GIT_DIR", filepath.Join(captured, ".git"))

	s.stop()
	if requests := w.agent.AllRequests(); len(requests) != 0 {
		t.Errorf("capture sent %d requests for a repository that is not captured", len(requests))
	}
}
