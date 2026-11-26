package capture_test

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/solidtrace/cli/agentapi"
	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/internal/fake"
)

type token string

func (t token) Token(context.Context) (string, error)             { return string(t), nil }
func (t token) Refreshed(context.Context, string) (string, error) { return string(t), nil }

const (
	sessionID = "1b2c3d4e-5f60-4718-8a9b-0c1d2e3f4a5b"
	firstLine = `{"type":"user","message":"Remember PELICAN"}` + "\n"
)

// capturedSession is a checkout of a captured repository, with a
// transcript of two lines in its Claude Code project folder. It gives
// the hook input of the Agent session.
func capturedSession(t *testing.T) capture.Input {
	t.Helper()
	repo, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init", "-q"}, {"remote", "add", "origin", "git@bitbucket.org:acme/shop.git"}} {
		if out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	project := filepath.Join(t.TempDir(), "projects", capture.ClaudeProjectFolder(repo))
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(project, sessionID+".jsonl")
	lines := firstLine + `{"type":"assistant","cwd":"` + repo + `","message":"OK"}` + "\n"
	if err := os.WriteFile(transcript, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	return capture.Input{SessionID: sessionID, TranscriptPath: transcript, Cwd: repo, HookEventName: "Stop"}
}

// A transcript that resume downloaded can be another person's or a Run's.
// A person's capture of it sends nothing, and the record says why. An
// older server does not say whose it is, and capture sends as before.
func TestAPersonsCaptureSendsOnlyTheirOwnAgentSessions(t *testing.T) {
	for _, c := range []struct {
		name    string
		session fake.Session
		older   bool
		sends   bool
		status  capture.Status
	}{
		{"another person's", fake.Session{AnotherPerson: true}, false, false, capture.NotYours},
		{"a Run's", fake.Session{RunID: "run_7"}, false, false, capture.NotYours},
		{"yours", fake.Session{}, false, true, capture.Uploaded},
		{"yours, on an older server", fake.Session{}, true, true, capture.Uploaded},
	} {
		t.Run(c.name, func(t *testing.T) {
			oauth := fake.NewOAuth(fake.Organization{Name: "Acme", Slug: "acme",
				Repositories: []string{"bitbucket.org/acme/shop"}})
			t.Cleanup(oauth.Close)
			api := fake.NewAgentAPI(oauth)
			t.Cleanup(api.Close)
			s := c.session
			s.Harness, s.Repository, s.Organization = capture.Harness, "bitbucket.org/acme/shop", "acme"
			s.Files = map[string]*fake.File{sessionID + ".jsonl": {Version: 1, End: uint64(len(firstLine)),
				Bytes: []byte(firstLine)}}
			api.Set(func(a *fake.AgentAPI) {
				a.Sessions = map[string]*fake.Session{sessionID: &s}
				a.OlderServer = c.older
			})
			dir := t.TempDir()
			capturer := &capture.Capturer{Dir: dir, Signin: "acme", Now: time.Now, Log: func(string, ...any) {},
				API: &agentapi.Client{IngestURL: api.URL(), HTTP: http.DefaultClient,
					Tokens: token(oauth.AccessToken("acme")), Version: "v0.19.0"}}
			in := capturedSession(t)

			if err := capturer.Capture(context.Background(), in); err != nil {
				t.Fatal(err)
			}

			sent := 0
			for _, r := range api.AllRequests() {
				if r.Method != http.MethodGet {
					sent++
				}
			}
			if got := sent > 0; got != c.sends {
				t.Errorf("uploads: %d, want some: %v", sent, c.sends)
			}
			if r, _ := capture.RecordOf(dir, sessionID); r.Status != c.status {
				t.Errorf("status %q (%s), want %q", r.Status, r.Error, c.status)
			}
		})
	}
}
