package command_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/command"
	"github.com/solidtrace/cli/internal/fake"
)

const sessionID = "6f1c2a7e-3b4d-4e5f-8a9b-0c1d2e3f4a5b"

// agentSession is one Claude Code Agent session on the machine: its
// working directory and its files under the Claude projects directory.
type agentSession struct {
	w   *world
	id  string
	cwd string
	// dir is the Claude project directory that holds <id>.jsonl and <id>/.
	dir string
}

// gitRepo makes a git repository in a temp dir with the remotes, as
// name=url pairs, and returns its path.
func (w *world) gitRepo(remotes ...string) string {
	w.t.Helper()
	// capture's git must not read the machine's own git config.
	w.t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	w.t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := filepath.Join(w.t.TempDir(), "repo")
	w.git("", "init", "-q", repo)
	for _, r := range remotes {
		name, url, _ := strings.Cut(r, "=")
		w.git(repo, "remote", "add", name, url)
	}
	return repo
}

func (w *world) git(dir string, args ...string) string {
	w.t.Helper()
	if dir != "" {
		args = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", args...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		w.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

// session is an Agent session that Claude Code started in cwd.
func (w *world) session(cwd string) *agentSession {
	w.t.Helper()
	return w.sessionWithID(sessionID, cwd)
}

// sessionWithID is an Agent session with its own id, in the machine's one
// Claude projects directory.
func (w *world) sessionWithID(id, cwd string) *agentSession {
	w.t.Helper()
	dir := w.projectFolder(cwd)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		w.t.Fatal(err)
	}
	return &agentSession{w: w, id: id, cwd: cwd, dir: dir}
}

// projectsDir is the machine's Claude projects directory.
func (w *world) projectsDir() string {
	if w.projects == "" {
		w.projects = filepath.Join(w.t.TempDir(), "claude", "projects")
	}
	return w.projects
}

// write writes a file of the Agent session: "" is <id>.jsonl, other paths
// are under <id>/.
func (s *agentSession) write(path, content string) {
	s.w.t.Helper()
	full := s.file(path)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		s.w.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		s.w.t.Fatal(err)
	}
}

// add adds content at the end of a file of the Agent session.
func (s *agentSession) add(path, content string) {
	s.w.t.Helper()
	f, err := os.OpenFile(s.file(path), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		s.w.t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		s.w.t.Fatal(err)
	}
}

func (s *agentSession) file(path string) string {
	if path == "" {
		return filepath.Join(s.dir, s.id+".jsonl")
	}
	return filepath.Join(s.dir, s.id, filepath.FromSlash(path))
}

// main is the server path of the main .jsonl file.
func (s *agentSession) main() string { return s.id + ".jsonl" }

// hook runs capture as a hook of the event runs it, with the hook input on
// stdin, and checks that it exits 0 with nothing on stderr.
func (s *agentSession) hook(event string) {
	s.w.t.Helper()
	input, _ := json.Marshal(map[string]any{
		"session_id":      s.id,
		"transcript_path": s.file(""),
		"cwd":             s.cwd,
		"hook_event_name": event,
	})
	env := s.w.env()
	env.Stdin = strings.NewReader(string(input))
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if code := command.Run(ctx, []string{"capture"}, env); code != 0 {
		s.w.t.Fatalf("capture exit %d, want 0", code)
	}
	if out := s.w.stderr.String(); out != "" {
		s.w.t.Fatalf("capture wrote to stderr: %s", out)
	}
}

func (s *agentSession) stop() { s.w.t.Helper(); s.hook("Stop") }

// stored is what the server has of a path, "" when it has nothing.
func (s *agentSession) stored(path string) string {
	if f := s.w.agent.Stored(s.id, path); f != nil {
		return string(f.Bytes)
	}
	return ""
}

// uploads are the upload requests that the server stored or repeated.
func (w *world) uploads() []fake.Request { return w.agent.Uploads() }

// captureLog is the local log of capture.
func (w *world) captureLog() string {
	data, _ := os.ReadFile(filepath.Join(w.dir, "capture.log"))
	return string(data)
}

func TestCaptureUploadsTheNewLinesOfAnAgentSessionInACapturedRepository(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
	s := w.session(repo)
	s.write("", "{\"n\":1}\n{\"n\":2}\n")

	s.stop()
	if got := s.stored(s.main()); got != "{\"n\":1}\n{\"n\":2}\n" {
		t.Fatalf("stored after the first call: %q\nlog: %s", got, w.captureLog())
	}
	who := w.agent.StoredSession(s.id)
	if who.Repository != "bitbucket.org/acme/shop" || who.Harness != "claude-code" || who.Folder != "" {
		t.Errorf("who and where: %+v", who)
	}

	s.add("", "{\"n\":3}\n")
	s.stop()
	if got := s.stored(s.main()); got != "{\"n\":1}\n{\"n\":2}\n{\"n\":3}\n" {
		t.Fatalf("stored after the second call: %q", got)
	}
	uploads := w.uploads()
	if len(uploads) != 2 {
		t.Fatalf("uploads: %d, want 2", len(uploads))
	}
	if second := uploads[1]; string(second.Body) != "{\"n\":3}\n" || second.Query.Get("offset") != "16" {
		t.Errorf("second upload: offset %s, body %q; want only the new line at 16", second.Query.Get("offset"), second.Body)
	}

	s.stop()
	if n := len(w.uploads()); n != 2 {
		t.Errorf("a call with no new bytes uploaded: %d uploads", n)
	}
}

func TestCaptureMatchesTheSSHAndHTTPSFormsOfARemote(t *testing.T) {
	for _, remote := range []string{
		"git@bitbucket.org:acme/shop.git",
		"ssh://git@BitBucket.org/acme/shop",
		"ssh://git@bitbucket.org:22/acme/shop.git/",
		"https://someone@Bitbucket.ORG/acme/shop.git",
		"git@bitbucket.org:acme-tools/deploy.git",
	} {
		t.Run(remote, func(t *testing.T) {
			w := newWorld(t, acme)
			w.signIn()
			s := w.session(w.gitRepo("upstream=" + remote))
			s.write("", "{\"n\":1}\n")
			s.stop()
			if s.stored(s.main()) != "{\"n\":1}\n" {
				t.Fatalf("not uploaded; log: %s", w.captureLog())
			}
		})
	}
}

func TestCaptureMatchesAWorktreeThroughItsRepositoryAndRecordsTheFolder(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=https://bitbucket.org/acme/shop")
	w.git(repo, "commit", "-q", "--allow-empty", "-m", "first")
	worktree := filepath.Join(t.TempDir(), "wt")
	w.git(repo, "worktree", "add", "-q", worktree)
	cwd := filepath.Join(worktree, "services", "api")
	if err := os.MkdirAll(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	s := w.session(cwd)
	s.write("", "{\"n\":1}\n")
	s.stop()
	if s.stored(s.main()) != "{\"n\":1}\n" {
		t.Fatalf("not uploaded; log: %s", w.captureLog())
	}
	if who := w.agent.StoredSession(s.id); who.Repository != "bitbucket.org/acme/shop" || who.Folder != "services/api" {
		t.Errorf("who and where: %+v", who)
	}
}

func TestCaptureSendsNothingOutsideACapturedRepository(t *testing.T) {
	for name, cwd := range map[string]func(w *world) string{
		"outside a git repository": func(w *world) string { return w.t.TempDir() },
		"a repository not in the list": func(w *world) string {
			return w.gitRepo("origin=https://github.com/someone/private.git")
		},
		"a repository with no remote": func(w *world) string { return w.gitRepo() },
		"a workspace that only starts the same": func(w *world) string {
			return w.gitRepo("origin=git@bitbucket.org:acme-toolsets/x.git")
		},
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t, acme)
			w.signIn()
			s := w.session(cwd(w))
			s.write("", "{\"secret\":1}\n")
			s.write("tool-results/out.txt", "secret output")
			s.stop()
			s.hook("SessionEnd")
			if requests := w.agent.AllRequests(); len(requests) != 0 {
				t.Errorf("capture sent %d requests for an Agent session that is not captured", len(requests))
			}
			if !strings.Contains(w.captureLog(), "not in the captured repositories") {
				t.Errorf("log: %s", w.captureLog())
			}
		})
	}
}

func TestCaptureSendsNothingWithNoListInHand(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	s := w.session(w.gitRepo("origin=https://bitbucket.org/acme/shop"))
	s.write("", "{\"n\":1}\n")
	w.agent.Set(func(f *fake.AgentAPI) { f.Down = true })

	s.stop()
	if requests := w.agent.AllRequests(); len(requests) != 0 {
		t.Errorf("capture sent %d requests with no list", len(requests))
	}
	if !strings.Contains(w.captureLog(), "no captured repositories list") {
		t.Errorf("log: %s", w.captureLog())
	}

	w.agent.Set(func(f *fake.AgentAPI) { f.Down = false })
	s.stop()
	if s.stored(s.main()) != "{\"n\":1}\n" {
		t.Fatalf("not uploaded when the list came; log: %s", w.captureLog())
	}
}

func TestCaptureKeepsTheListForTenMinutes(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	s := w.session(w.gitRepo("origin=https://bitbucket.org/acme/shop"))
	s.write("", "{\"n\":1}\n")
	s.stop()
	calls := w.agent.Calls

	w.clock.Add(9 * time.Minute)
	s.add("", "{\"n\":2}\n")
	s.stop()
	if w.agent.Calls != calls {
		t.Errorf("the list was asked again in its 10 minutes")
	}

	// An Admin removes the repository: after 10 minutes, its uploads stop.
	w.oauth.Lock()
	w.oauth.Organizations["acme"].Repositories = []string{"bitbucket.org/acme/other"}
	w.oauth.Unlock()
	w.clock.Add(time.Minute)
	s.add("", "{\"n\":3}\n")
	before := len(w.agent.AllRequests())
	s.stop()
	if w.agent.Calls != calls+1 {
		t.Errorf("the list was not asked again after 10 minutes")
	}
	if after := len(w.agent.AllRequests()); after != before {
		t.Errorf("capture sent %d requests after the repository was removed", after-before)
	}
	if got := s.stored(s.main()); got != "{\"n\":1}\n{\"n\":2}\n" {
		t.Errorf("stored: %q", got)
	}
}

func TestCaptureMatchesARemoteThatGitRewrites(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.gitRepo("origin=bb:acme/shop")
	w.git(repo, "config", "url.https://bitbucket.org/.insteadOf", "bb:")
	s := w.session(repo)
	s.write("", "{\"n\":1}\n")
	s.stop()
	if s.stored(s.main()) != "{\"n\":1}\n" {
		t.Fatalf("not uploaded; log: %s", w.captureLog())
	}
}

// The match uses the directory where the Agent session started (the cwd of
// the first line of <id>.jsonl that has one), not the hook's cwd, which
// follows each cd of the agent.
func TestCaptureMatchesTheDirectoryWhereTheAgentSessionStarted(t *testing.T) {
	t.Run("started outside, the hook cwd is captured", func(t *testing.T) {
		w := newWorld(t, acme)
		w.signIn()
		s := w.session(w.gitRepo("origin=https://github.com/someone/private.git"))
		private := s.cwd
		s.cwd = w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
		s.write("", `{"type":"summary"}`+"\n"+`{"cwd":"`+private+`","secret":1}`+"\n"+`{"cwd":"`+s.cwd+`"}`+"\n")
		s.stop()
		s.hook("SessionEnd")
		if requests := w.agent.AllRequests(); len(requests) != 0 {
			t.Errorf("capture sent %d requests for an Agent session that started outside the list", len(requests))
		}
	})
	t.Run("started in a captured repository, the hook cwd is outside", func(t *testing.T) {
		w := newWorld(t, acme)
		w.signIn()
		repo := w.gitRepo("origin=https://bitbucket.org/acme/shop.git")
		sub := filepath.Join(repo, "services")
		if err := os.MkdirAll(sub, 0o700); err != nil {
			t.Fatal(err)
		}
		s := w.session(sub)
		s.cwd = t.TempDir()
		line := `{"cwd":"` + sub + `"}` + "\n"
		s.write("", line)
		s.stop()
		if s.stored(s.main()) != line {
			t.Fatalf("not uploaded; log: %s", w.captureLog())
		}
		if who := w.agent.StoredSession(s.id); who.Repository != "bitbucket.org/acme/shop" || who.Folder != "services" {
			t.Errorf("who and where: %+v", who)
		}
	})
}
