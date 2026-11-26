package command_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/capture"
	"github.com/solidtrace/cli/internal/fake"
)

const (
	resumeID         = "1b2c3d4e-5f60-4718-8a9b-0c1d2e3f4a5b"
	resumeTranscript = `{"type":"user","gitBranch":"main","message":"Remember PELICAN"}` + "\n" +
		`{"type":"assistant","gitBranch":"main","message":"OK"}` + "\n"
)

// checkout is a git checkout of acme's shop, and the current folder of the
// command.
func (w *world) checkout() string {
	w.t.Helper()
	repo := w.gitRepo("origin=git@bitbucket.org:acme/shop.git")
	repo, err := filepath.EvalSymlinks(repo)
	if err != nil {
		w.t.Fatal(err)
	}
	w.cwd = repo
	return repo
}

// inSolidTrace stores an Agent session of acme's shop in the fake, with
// files (path: bytes).
func (w *world) inSolidTrace(id string, s fake.Session, files map[string]string) {
	w.t.Helper()
	s.Harness, s.Organization = "claude-code", "acme"
	if s.Repository == "" {
		s.Repository = "bitbucket.org/acme/shop"
	}
	s.Files = map[string]*fake.File{}
	for path, content := range files {
		s.Files[path] = &fake.File{Version: 1, End: uint64(len(content)), Bytes: []byte(content)}
	}
	w.agent.Set(func(a *fake.AgentAPI) {
		if a.Sessions == nil {
			a.Sessions = map[string]*fake.Session{}
		}
		a.Sessions[id] = &s
	})
}

func (w *world) resume(args ...string) int {
	w.t.Helper()
	return w.run(append([]string{"resume"}, args...)...)
}

// projectFolder is Claude Code's project folder of the folder.
func (w *world) projectFolder(folder string) string {
	return filepath.Join(w.projectsDir(), capture.ClaudeProjectFolder(folder))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (w *world) downloads() int {
	var n int
	w.agent.Set(func(a *fake.AgentAPI) { n = a.Downloads })
	return n
}

func TestResumeDownloadsYourAgentSessionAndStartsClaudeCodeWithTheSameID(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.checkout()
	w.inSolidTrace(resumeID, fake.Session{}, map[string]string{
		resumeID + ".jsonl":                    resumeTranscript,
		resumeID + "/subagents/agent-a1.jsonl": `{"type":"user"}` + "\n",
	})

	if code := w.resume(resumeID); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}

	project := w.projectFolder(repo)
	if got := readFile(t, filepath.Join(project, resumeID+".jsonl")); got != resumeTranscript {
		t.Errorf("transcript = %q", got)
	}
	if got := readFile(t, filepath.Join(project, resumeID, "subagents", "agent-a1.jsonl")); got != `{"type":"user"}`+"\n" {
		t.Errorf("subagent transcript = %q", got)
	}
	if want := [][]string{{repo, "--resume", resumeID}}; !slices.EqualFunc(w.started, want, slices.Equal) {
		t.Errorf("started %q, want %q", w.started, want)
	}
	if entries, _ := os.ReadDir(w.projectsDir()); len(entries) != 1 {
		t.Errorf("the projects folder has %d entries, want only the project folder", len(entries))
	}
}

func TestResumeForksAnotherPersonsARunsOrWithForkAndElseKeepsTheID(t *testing.T) {
	for _, c := range []struct {
		name    string
		session fake.Session
		args    []string
		fork    bool
	}{
		{"yours", fake.Session{}, nil, false},
		{"another person's", fake.Session{AnotherPerson: true}, nil, true},
		{"a Run's", fake.Session{RunID: "run_7"}, nil, true},
		{"yours with --fork", fake.Session{}, []string{"--fork"}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, acme)
			w.signIn()
			repo := w.checkout()
			w.inSolidTrace(resumeID, c.session, map[string]string{resumeID + ".jsonl": resumeTranscript})

			if code := w.resume(append([]string{resumeID, "--print"}, c.args...)...); code != 0 {
				t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
			}

			want := "cd " + repo + " && claude --resume " + resumeID
			if c.fork {
				want += " --fork-session"
			}
			lines := strings.Split(strings.TrimSpace(w.stdout.String()), "\n")
			if got := lines[len(lines)-1]; got != want {
				t.Errorf("printed %q\n      want %q", got, want)
			}
			if len(w.started) != 0 {
				t.Errorf("--print started Claude Code: %q", w.started)
			}
		})
	}
}

func TestASecondResumeDownloadsNothingAndStartsTheSameAgentSession(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.checkout()
	w.inSolidTrace(resumeID, fake.Session{}, map[string]string{resumeID + ".jsonl": resumeTranscript})

	for i := 0; i < 2; i++ {
		if code := w.resume(resumeID); code != 0 {
			t.Fatalf("run %d: exit %d\nstderr: %s", i+1, code, w.stderr)
		}
	}

	if got := w.downloads(); got != 1 {
		t.Errorf("downloads: %d, want 1", got)
	}
	want := []string{repo, "--resume", resumeID}
	if len(w.started) != 2 || !slices.Equal(w.started[1], want) {
		t.Errorf("started %q, want twice %q", w.started, want)
	}
}

func TestResumeDownloadsAgainWhenTheTranscriptOnThisMachineIsShorter(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.checkout()
	w.inSolidTrace(resumeID, fake.Session{}, map[string]string{resumeID + ".jsonl": resumeTranscript})
	project := w.projectFolder(repo)
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	first := strings.SplitAfter(resumeTranscript, "\n")[0]
	if err := os.WriteFile(filepath.Join(project, resumeID+".jsonl"), []byte(first), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := w.resume(resumeID, "--print"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}

	if got := readFile(t, filepath.Join(project, resumeID+".jsonl")); got != resumeTranscript {
		t.Errorf("transcript = %q, want the server's", got)
	}
}

func TestAResumeAfterACrashBeforeTheRenamesReplacesTheFolderThatTheCrashLeft(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.checkout()
	w.inSolidTrace(resumeID, fake.Session{}, map[string]string{resumeID + ".jsonl": resumeTranscript})
	// A crash after the download and before the renames left this.
	left := filepath.Join(w.projectsDir(), ".solidtrace-resume-"+resumeID)
	if err := os.MkdirAll(filepath.Join(left, "files", resumeID), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(left, "files", resumeID+".jsonl"), []byte(`{"half`), 0o600); err != nil {
		t.Fatal(err)
	}

	if code := w.resume(resumeID, "--print"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}

	if got := readFile(t, filepath.Join(w.projectFolder(repo), resumeID+".jsonl")); got != resumeTranscript {
		t.Errorf("transcript = %q", got)
	}
	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Errorf("the folder that the crash left is still there: %v", err)
	}
}

func TestResumeWaitsWhileSolidTraceMakesTheLastUploadsReady(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	w.checkout()
	w.inSolidTrace(resumeID, fake.Session{}, map[string]string{resumeID + ".jsonl": resumeTranscript})
	w.agent.Set(func(a *fake.AgentAPI) { a.DownloadNotReady = 2 })

	if code := w.resume(resumeID, "--print"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}

	if want := []time.Duration{time.Second, time.Second}; !slices.Equal(w.slept, want) {
		t.Errorf("slept %v, want %v", w.slept, want)
	}
}

func TestResumeRefusesAPartialOrCutTranscript(t *testing.T) {
	for _, flag := range []string{"partial", "cut"} {
		t.Run(flag, func(t *testing.T) {
			w := newWorld(t, acme)
			w.signIn()
			repo := w.checkout()
			w.inSolidTrace(resumeID, fake.Session{}, map[string]string{resumeID + ".jsonl": resumeTranscript})
			w.agent.Set(func(a *fake.AgentAPI) {
				f := a.Sessions[resumeID].Files[resumeID+".jsonl"]
				f.Partial, f.Cut = flag == "partial", flag == "cut"
			})

			if code := w.resume(resumeID); code != 1 {
				t.Fatalf("exit %d, want 1\nstderr: %s", code, w.stderr)
			}

			want := "solidtrace resume: the transcript of the Agent session is " + flag +
				" in SolidTrace, so a resume is not promised. Start a new Agent session.\n"
			if got := w.stderr.String(); got != want {
				t.Errorf("stderr %q\n  want %q", got, want)
			}
			if _, err := os.Stat(filepath.Join(w.projectFolder(repo), resumeID+".jsonl")); !os.IsNotExist(err) {
				t.Errorf("the transcript is on the machine: %v", err)
			}
			if len(w.started) != 0 {
				t.Errorf("started Claude Code: %q", w.started)
			}
		})
	}
}

func TestResumeRefusesWithWhatToDo(t *testing.T) {
	for _, c := range []struct {
		name  string
		setup func(w *world)
		want  string
	}{
		{"another repository", func(w *world) {
			w.cwd = w.gitRepo("origin=git@github.com:beta/app.git")
		}, "The Agent session is in bitbucket.org/acme/shop, and this checkout is not. Clone bitbucket.org/acme/shop, " +
			"and run solidtrace resume in that checkout."},
		{"no git checkout", func(w *world) {
			w.cwd = w.t.TempDir()
		}, "is not in a git checkout. Run solidtrace resume in a checkout of bitbucket.org/acme/shop."},
		{"an old sign-in", func(w *world) {
			w.checkout()
			w.signedInWithScope("sessions:write factory")
		}, "Run solidtrace login again: this sign-in cannot download Agent sessions."},
		{"a folder out of the checkout", func(w *world) {
			w.checkout()
			w.agent.Set(func(a *fake.AgentAPI) { a.Sessions[resumeID].Folder = "../.ssh" })
		}, `The folder "../.ssh" of the Agent session is not a folder in its repository.`},
		{"an Agent session that SolidTrace does not have", func(w *world) {
			w.checkout()
			w.agent.Set(func(a *fake.AgentAPI) { delete(a.Sessions, resumeID) })
		}, "SolidTrace has no Agent session " + resumeID + " in Acme."},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, acme)
			w.signIn()
			w.inSolidTrace(resumeID, fake.Session{}, map[string]string{resumeID + ".jsonl": resumeTranscript})
			c.setup(w)

			if code := w.resume(resumeID); code != 1 {
				t.Fatalf("exit %d, want 1\nstderr: %s", code, w.stderr)
			}

			got := w.stderr.String()
			if !strings.HasPrefix(got, "solidtrace resume: ") || !strings.HasSuffix(got, c.want+"\n") {
				t.Errorf("stderr %q\nwant it to end with %q", got, c.want)
			}
			if w.downloads() != 0 || len(w.started) != 0 {
				t.Errorf("downloads %d, starts %q: want none", w.downloads(), w.started)
			}
		})
	}
}

func TestResumeOfAnOldSignInAsksTheServerNothing(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	w.checkout()
	w.signedInWithScope("sessions:write factory")
	before := len(w.agent.AllRequests())

	if code := w.resume(resumeID); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}

	if got := len(w.agent.AllRequests()); got != before {
		t.Errorf("requests: %d after the resume, %d before", got, before)
	}
}

// signedInWithScope changes the Scope of the stored sign-in, as a sign-in
// of an older solidtrace has.
func (w *world) signedInWithScope(scope string) {
	w.t.Helper()
	var c map[string]any
	if err := json.Unmarshal([]byte(readFile(w.t, w.credentialsFile())), &c); err != nil {
		w.t.Fatal(err)
	}
	c["scope"] = scope
	data, _ := json.Marshal(c)
	if err := os.WriteFile(w.credentialsFile(), data, 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func TestResumeTellsHowToGetTheBranchOfTheAgentSessionAndDoesNotChangeTheCheckout(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.checkout()
	w.git(repo, "commit", "-q", "--allow-empty", "-m", "first")
	head := w.git(repo, "rev-parse", "HEAD")
	w.inSolidTrace(resumeID, fake.Session{}, map[string]string{resumeID + ".jsonl": resumeTranscript +
		`{"type":"user","gitBranch":"feature/pelican","message":"go on"}` + "\n"})

	if code := w.resume(resumeID, "--print"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}

	want := "The Agent session worked on the branch feature/pelican, which this checkout does not have. To get it:\n" +
		"  git fetch\n  git switch feature/pelican\nWork that was not pushed is not in SolidTrace.\n"
	if got := w.stderr.String(); got != want {
		t.Errorf("stderr %q\n  want %q", got, want)
	}
	if got := w.git(repo, "rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved from %s to %s", head, got)
	}

	w.git(repo, "branch", "feature/pelican")
	w.stderr = &syncBuffer{}
	if code := w.resume(resumeID, "--print"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}
	if got := w.stderr.String(); !strings.Contains(got, "this checkout is on ") ||
		!strings.Contains(got, "\n  git switch feature/pelican\n") || strings.Contains(got, "git fetch") {
		t.Errorf("stderr %q, want the git switch line only", got)
	}
}

func TestResumeStartsClaudeCodeInTheFolderOfTheAgentSessionWithTheArgumentsAfterTheDashes(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.checkout()
	w.inSolidTrace(resumeID, fake.Session{Folder: "web/app"}, map[string]string{resumeID + ".jsonl": resumeTranscript})

	if code := w.resume(resumeID); code != 1 {
		t.Fatalf("exit %d with no web/app, want 1\nstderr: %s", code, w.stderr)
	}
	if got := w.stderr.String(); !strings.Contains(got, "the folder "+filepath.Join(repo, "web", "app")+
		" of the Agent session is not in this checkout") {
		t.Errorf("stderr %q", got)
	}

	if err := os.MkdirAll(filepath.Join(repo, "web", "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	if code := w.resume(resumeID, "--", "-p", "--model", "haiku", "Say the word"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}

	folder := filepath.Join(repo, "web", "app")
	want := []string{folder, "--resume", resumeID, "-p", "--model", "haiku", "Say the word"}
	if len(w.started) != 1 || !slices.Equal(w.started[0], want) {
		t.Errorf("started %q, want %q", w.started, want)
	}
	if _, err := os.Stat(filepath.Join(w.projectFolder(folder), resumeID+".jsonl")); err != nil {
		t.Errorf("the transcript is not in the project folder of %s: %v", folder, err)
	}
}

func TestResumeRefusesWhenThisMachineHasTheAgentSessionInAnotherFolder(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"yours", nil, "claude --resume " + resumeID},
		{"with --fork", []string{"--fork"}, "claude --resume " + resumeID + " --fork-session"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, acme)
			w.signIn()
			w.checkout()
			w.inSolidTrace(resumeID, fake.Session{}, map[string]string{resumeID + ".jsonl": resumeTranscript})
			// A second checkout of the repository on this machine has the
			// Agent session.
			other := w.t.TempDir()
			project := w.projectFolder(other)
			if err := os.MkdirAll(project, 0o700); err != nil {
				t.Fatal(err)
			}
			line := `{"type":"user","cwd":"` + other + `"}` + "\n"
			if err := os.WriteFile(filepath.Join(project, resumeID+".jsonl"), []byte(line), 0o600); err != nil {
				t.Fatal(err)
			}

			if code := w.resume(append([]string{resumeID}, c.args...)...); code != 1 {
				t.Fatalf("exit %d, want 1\nstderr: %s", code, w.stderr)
			}

			want := "solidtrace resume: This machine has the Agent session in " + other +
				". Run this command in that folder:\n  " + c.want + "\n"
			if got := w.stderr.String(); got != want {
				t.Errorf("stderr %q\n  want %q", got, want)
			}
			if w.downloads() != 0 || len(w.started) != 0 {
				t.Errorf("downloads %d, starts %q: want none", w.downloads(), w.started)
			}
		})
	}
}

func TestTheSweepSendsNothingOfAnotherPersonsResumedAgentSessionAndStatusSaysWhy(t *testing.T) {
	w := newWorld(t, acme)
	w.signIn()
	repo := w.checkout()
	transcript := `{"type":"user","cwd":"` + repo + `","message":"Remember PELICAN"}` + "\n"
	w.inSolidTrace(resumeID, fake.Session{AnotherPerson: true}, map[string]string{resumeID + ".jsonl": transcript})
	if code := w.resume(resumeID, "--print"); code != 0 {
		t.Fatalf("exit %d\nstderr: %s", code, w.stderr)
	}

	w.startSession(t.TempDir())

	for _, r := range w.agent.AllRequests() {
		if r.SessionID == resumeID && r.Method != http.MethodGet {
			t.Errorf("capture sent %s %v for another person's Agent session", r.Method, r.Query)
		}
	}
	if line := w.statusOf(resumeID); !strings.Contains(line, "not yours") {
		t.Errorf("status line %q, want not yours", line)
	}
	if out := w.status(); !strings.Contains(out, "1  not yours: you resumed another person's or a Run's") {
		t.Errorf("capture status does not count it as not yours:\n%s", out)
	}
}
