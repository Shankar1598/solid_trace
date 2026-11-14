package command_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/internal/fake"
)

// The SessionStart sweep: each Agent session on the machine whose
// repository is captured gets what it did not send, also after a crash, a
// failed upload or a change to the list.

const (
	crashedID = "0a5b8c1d-2e3f-4a5b-8c6d-7e8f9a0b1c2d"
	newID     = "9e8d7c6b-5a4f-4e3d-8c2b-1a0f9e8d7c6b"
	otherID   = "3c4d5e6f-7a8b-4c9d-8e0f-1a2b3c4d5e6f"
)

// begin writes <id>.jsonl as Claude Code does: its first line has the
// working directory where the Agent session started. The sweep has no
// hook input, so it reads that line.
func (s *agentSession) begin(content string) {
	s.w.t.Helper()
	s.write("", s.started(content))
}

// started is content after the first line that begin writes.
func (s *agentSession) started(content string) string {
	return `{"type":"user","cwd":"` + s.cwd + `"}` + "\n" + content
}

// startSession runs the SessionStart hook of a new Agent session in cwd:
// it starts the sweep.
func (w *world) startSession(cwd string) *agentSession {
	w.t.Helper()
	s := w.sessionWithID(newID, cwd)
	s.hook("SessionStart")
	return s
}

// age makes each file of the Agent session older than the clock by d, as
// files that a crashed Claude Code left.
func (s *agentSession) age(d time.Duration) {
	s.w.t.Helper()
	at := s.w.clock.Now().Add(-d)
	for _, p := range []string{s.file(""), filepath.Join(s.dir, s.id)} {
		_ = filepath.Walk(p, func(path string, _ os.FileInfo, err error) error {
			if err == nil {
				_ = os.Chtimes(path, at, at)
			}
			return nil
		})
	}
}

func (w *world) status() string {
	w.t.Helper()
	before := len(w.stdout.String())
	if code := w.run("capture", "status"); code != 0 {
		w.t.Fatalf("capture status: exit %d\nstderr: %s", code, w.stderr)
	}
	return w.stdout.String()[before:]
}

// statusOf is the line of 'capture status' for an Agent session.
func (w *world) statusOf(id string) string {
	w.t.Helper()
	for _, line := range strings.Split(w.status(), "\n") {
		if strings.Contains(line, id) {
			return line
		}
	}
	w.t.Fatalf("capture status has no line for %s:\n%s", id, w.status())
	return ""
}

func TestTheSweepUploadsWhatACrashedClaudeCodeLeft(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	crashed := w.sessionWithID(crashedID, repo)
	crashed.begin("{\"n\":1}\n")
	crashed.stop()
	// Claude Code crashed after these: no hook ran.
	crashed.add("", "{\"n\":2}\n")
	crashed.write("tool-results/t1.txt", "output that no hook saw")
	crashed.age(time.Hour)

	w.startSession(repo)
	if got := crashed.stored(crashed.main()); got != crashed.started("{\"n\":1}\n{\"n\":2}\n") {
		t.Errorf("stored lines after the sweep: %q\nlog: %s", got, w.captureLog())
	}
	if got := crashed.stored(crashed.id + "/tool-results/t1.txt"); got != "output that no hook saw" {
		t.Errorf("stored tool result after the sweep: %q", got)
	}
	if line := w.statusOf(crashedID); !strings.Contains(line, "uploaded") || !strings.Contains(line, "bitbucket.org/acme/shop") {
		t.Errorf("status: %q", line)
	}
}

func TestTheSweepWaitsForAFileThatChangesNow(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	open := w.sessionWithID(otherID, repo)
	open.begin("{\"n\":1}\n")
	open.write("tool-results/t1.txt", "being written")
	open.age(0)

	w.startSession(repo)
	if got := open.stored(open.id + "/tool-results/t1.txt"); got != "" {
		t.Errorf("the sweep sent a file that changed at once: %q", got)
	}
	if got := open.stored(open.main()); got != open.started("{\"n\":1}\n") {
		t.Errorf("stored lines: %q", got)
	}
}

func TestTheSweepSendsNoRequestForAnAgentSessionThatDidNotChange(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	done := w.sessionWithID(crashedID, repo)
	done.begin("{\"n\":1}\n")
	done.hook("SessionEnd")

	w.startSession(repo)
	before := len(w.agent.AllRequests())
	w.sessionWithID(newID, repo).hook("SessionStart")
	for _, r := range w.agent.AllRequests()[before:] {
		if r.SessionID == crashedID {
			t.Errorf("the second sweep sent %s %v for an Agent session that did not change", r.Method, r.Query)
		}
	}
}

func TestTheSweepUploadsARepositoryAddedLaterAndStopsForARemovedOne(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	later := w.gitRepo("origin=https://github.com/acme/later.git")
	s := w.sessionWithID(crashedID, later)
	s.begin("{\"n\":1}\n")
	s.age(time.Hour)

	w.startSession(t.TempDir())
	if s.stored(s.main()) != "" {
		t.Fatalf("uploaded before the repository was in the list")
	}
	if line := w.statusOf(crashedID); !strings.Contains(line, "not in the list") {
		t.Errorf("status before: %q", line)
	}

	w.oauth.Lock()
	w.oauth.Organizations["acme"].Repositories = []string{"github.com/acme/later"}
	w.oauth.Unlock()
	w.clock.Add(10 * time.Minute)
	w.startSession(t.TempDir())
	if got := s.stored(s.main()); got != s.started("{\"n\":1}\n") {
		t.Fatalf("not uploaded at the sweep after the repository was added: %q\nlog: %s", got, w.captureLog())
	}
	if line := w.statusOf(crashedID); !strings.Contains(line, "uploaded") {
		t.Errorf("status after: %q", line)
	}

	// An Admin removes it again: the new lines stay on the machine.
	w.oauth.Lock()
	w.oauth.Organizations["acme"].Repositories = []string{"bitbucket.org/acme/shop"}
	w.oauth.Unlock()
	w.clock.Add(10 * time.Minute)
	s.add("", "{\"n\":2}\n")
	s.age(time.Hour)
	before := len(w.agent.AllRequests())
	w.startSession(t.TempDir())
	for _, r := range w.agent.AllRequests()[before:] {
		if r.SessionID == crashedID {
			t.Errorf("the sweep sent %s for a removed repository", r.Method)
		}
	}
	if line := w.statusOf(crashedID); !strings.Contains(line, "not in the list") {
		t.Errorf("status after the removal: %q", line)
	}
}

func TestAfterNoNetworkTheNextCallOrSweepSendsWhatIsMissing(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	s := w.sessionWithID(crashedID, repo)
	s.begin("{\"n\":1}\n")
	s.stop()
	w.agent.Set(func(f *fake.AgentAPI) { f.Down = true })
	s.add("", "{\"n\":2}\n")
	s.stop()
	if line := w.statusOf(crashedID); !strings.Contains(line, "failed") {
		t.Errorf("status with no network: %q", line)
	}

	w.agent.Set(func(f *fake.AgentAPI) { f.Down = false })
	w.startSession(repo)
	if got := s.stored(s.main()); got != s.started("{\"n\":1}\n{\"n\":2}\n") {
		t.Errorf("stored after the sweep: %q\nlog: %s", got, w.captureLog())
	}
	if line := w.statusOf(crashedID); !strings.Contains(line, "uploaded") {
		t.Errorf("status after the sweep: %q", line)
	}
}

func TestAfterAnExpiredSignInTheSweepSendsWhatIsMissing(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	s := w.sessionWithID(crashedID, repo)
	s.begin("{\"n\":1}\n")
	s.stop()

	// The sign-in ends: the access token expires and its Grant is revoked,
	// so the rotating refresh token is refused.
	w.oauth.RevokeGrants()
	w.oauth.ExpireAccessTokens()
	s.add("", "{\"n\":2}\n")
	s.stop()
	if !strings.Contains(w.captureLog(), "solidtrace login") {
		t.Errorf("the log does not say to sign in again: %s", w.captureLog())
	}
	if got := s.stored(s.main()); got != s.started("{\"n\":1}\n") {
		t.Fatalf("stored with an expired sign-in: %q", got)
	}

	w.signIn()
	s.age(time.Hour)
	w.startSession(repo)
	if got := s.stored(s.main()); got != s.started("{\"n\":1}\n{\"n\":2}\n") {
		t.Errorf("stored after the new sign-in and the sweep: %q\nlog: %s", got, w.captureLog())
	}
}

func TestTheSweepStopsAtAFailureOfEachUploadAndMarksTheRestFailed(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	a := w.sessionWithID(crashedID, repo)
	a.begin("{\"a\":1}\n")
	b := w.sessionWithID(otherID, repo)
	b.begin("{\"b\":1}\n")
	w.agent.Set(func(f *fake.AgentAPI) { f.UploadsDown = true })

	w.startSession(repo)
	if n := len(w.agent.AllRequests()); n > 2 {
		// The list call is not in Requests: one offsets call and one upload
		// at most, for the first Agent session with new bytes.
		t.Errorf("the sweep went on after a server failure: %d requests", n)
	}
	for _, id := range []string{crashedID, otherID} {
		if line := w.statusOf(id); !strings.Contains(line, "failed") || !strings.Contains(line, "500") {
			t.Errorf("status of %s: %q", id, line)
		}
	}
}

func TestTheServersMinimumVersionGoesToTheLogAndToCaptureStatus(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	s := w.sessionWithID(crashedID, w.gitRepo("origin=https://bitbucket.org/acme/shop.git"))
	s.begin("{\"n\":1}\n")
	s.stop()
	w.agent.Set(func(f *fake.AgentAPI) { f.MinimumVersion = "0.20.0" })
	s.add("", "{\"n\":2}\n")
	s.stop()
	if !strings.Contains(w.captureLog(), "install 0.20.0 or later") {
		t.Errorf("log: %s", w.captureLog())
	}
	out := w.status()
	if !strings.Contains(out, "install 0.20.0 or later") {
		t.Errorf("capture status does not name the minimum version:\n%s", out)
	}
	if line := w.statusOf(crashedID); !strings.Contains(line, "failed") {
		t.Errorf("status line: %q", line)
	}
}

func TestCaptureStatusShowsTheOrganizationItsListAndWhoCanReadTheUploads(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	captured := w.sessionWithID(crashedID, w.gitRepo("origin=https://bitbucket.org/acme/shop.git"))
	captured.begin("{\"n\":1}\n")
	captured.stop()
	private := w.sessionWithID(otherID, t.TempDir())
	private.begin("{\"n\":1}\n")
	private.stop()
	unchecked := w.sessionWithID("5f6e7d8c-9b0a-4f1e-8d2c-3b4a5f6e7d8c", t.TempDir())
	unchecked.begin("{\"n\":1}\n")

	out := w.status()
	for _, want := range []string{
		"Acme", "bitbucket.org/acme/shop", "bitbucket.org/acme-tools/*",
		"Each member of Acme can read",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("capture status has no %q:\n%s", want, out)
		}
	}
	for id, want := range map[string]string{crashedID: "uploaded", otherID: "not in the list",
		unchecked.id: "not checked yet"} {
		if line := w.statusOf(id); !strings.Contains(line, want) {
			t.Errorf("status of %s: %q, want %q", id, line, want)
		}
	}
}

func TestCaptureStatusWithAnEmptyListSaysThatNothingUploads(t *testing.T) {
	w := newWorld(t, fake.Organization{Name: "Acme", Slug: "acme"})
	w.signIn()
	if out := w.status(); !strings.Contains(out, "nothing uploads") {
		t.Errorf("capture status:\n%s", out)
	}
}

func TestCaptureStatusWithNoSignIn(t *testing.T) {
	w := newWorld(t, acme)
	if code := w.run("capture", "status"); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	if !strings.Contains(w.stdout.String()+w.stderr.String(), "solidtrace login") {
		t.Errorf("out: %s %s", w.stdout, w.stderr)
	}
}

func TestWithNoListTheSweepMarksTheChangedAgentSessionsOfACapturedRepositoryFailed(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	crashed := w.sessionWithID(crashedID, repo)
	crashed.begin("{\"n\":1}\n")
	crashed.stop()
	private := w.sessionWithID(otherID, t.TempDir())
	private.begin("{\"n\":1}\n")
	private.stop()
	// Claude Code crashed after these lines.
	crashed.add("", "{\"n\":2}\n")
	private.add("", "{\"n\":2}\n")
	crashed.age(time.Hour)
	private.age(time.Hour)

	// The cached list expires, and the sweep has no network.
	w.clock.Add(10 * time.Minute)
	w.agent.Set(func(f *fake.AgentAPI) { f.Down = true })
	w.startSession(repo)
	if line := w.statusOf(crashedID); !strings.Contains(line, "failed") {
		t.Errorf("status of an Agent session with bytes not sent, after a sweep with no list: %q", line)
	}
	if line := w.statusOf(otherID); !strings.Contains(line, "not in the list") {
		t.Errorf("status of an Agent session not in the list: %q", line)
	}
}

func TestTheSweepSendsTheNewLinesOfATranscriptWithNoWorkingDirectory(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	s := w.sessionWithID(crashedID, repo)
	// No line has a cwd: the hook's cwd matched the repository.
	s.write("", "{\"n\":1}\n")
	s.stop()
	s.add("", "{\"n\":2}\n")
	s.age(time.Hour)

	w.startSession(repo)
	if got := s.stored(s.main()); got != "{\"n\":1}\n{\"n\":2}\n" {
		t.Errorf("stored after the sweep: %q\nlog: %s", got, w.captureLog())
	}
	if line := w.statusOf(crashedID); !strings.Contains(line, "uploaded") {
		t.Errorf("status: %q", line)
	}
}
