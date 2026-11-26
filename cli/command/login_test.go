package command_test

import (
	"bytes"
	"cmp"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/cli/command"
	"github.com/solidtrace/cli/internal/fake"
)

// world is one engineer's machine and one SolidTrace: the fake OAuth server
// and the fake agent ingest API.
type world struct {
	t     *testing.T
	oauth *fake.OAuth
	agent *fake.AgentAPI
	dir   string
	// projects is the machine's Claude projects directory.
	projects string
	stdout   *syncBuffer
	stderr   *syncBuffer
	// httpTimeout is the HTTP client's Timeout; 0 is 10 s.
	httpTimeout   time.Duration
	browser       func(url string) error
	opened        []string
	clock         *clock
	slept         []time.Duration
	version       string
	claudeSession string
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

var (
	acme = fake.Organization{Name: "Acme", Slug: "acme",
		Repositories: []string{"bitbucket.org/acme/shop", "bitbucket.org/acme-tools/*"}}
	beta = fake.Organization{Name: "Beta Labs", Slug: "beta", Repositories: []string{"github.com/beta/app"}}
)

func newWorld(t *testing.T, orgs ...fake.Organization) *world {
	t.Helper()
	w := &world{t: t, dir: filepath.Join(t.TempDir(), "solidtrace"), stdout: &syncBuffer{}, stderr: &syncBuffer{},
		clock: &clock{now: time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)}, version: "v0.19.0"}
	w.oauth = fake.NewOAuth(orgs...)
	w.oauth.Now = w.clock.Now
	w.agent = fake.NewAgentAPI(w.oauth)
	w.agent.Version = "v0.19.0"
	t.Cleanup(w.oauth.Close)
	t.Cleanup(w.agent.Close)
	// The person's browser: it opens the approval page, which redirects
	// to the CLI's loopback callback at once.
	w.browser = func(url string) error {
		w.opened = append(w.opened, url)
		go func() {
			resp, err := http.Get(url)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				resp.Body.Close()
			}
		}()
		return nil
	}
	return w
}

func (w *world) env() command.Env {
	return command.Env{
		Stdout:      w.stdout,
		Stderr:      w.stderr,
		ConfigDir:   w.dir,
		HTTP:        &http.Client{Timeout: cmp.Or(w.httpTimeout, 10*time.Second)},
		OpenBrowser: w.browser,
		Now:         w.clock.Now,
		Sleep: func(ctx context.Context, d time.Duration) error {
			w.slept = append(w.slept, d)
			w.clock.Add(d)
			return ctx.Err()
		},
		Version:           w.version,
		ClaudeProjectsDir: w.projectsDir(),
		ClaudeSessionID:   w.claudeSession,
	}
}

// run runs the CLI with args and returns its exit code.
func (w *world) run(args ...string) int {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return command.Run(ctx, args, w.env())
}

func (w *world) login(args ...string) int {
	w.t.Helper()
	return w.run(append([]string{"login", "--ingest-url", w.agent.URL()}, append(args, w.oauth.URL())...)...)
}

func (w *world) credentialsFile() string {
	return filepath.Join(w.dir, "credentials.json")
}

func TestLoginWithABrowserStoresTheSignInAndPrintsTheNotice(t *testing.T) {
	w := newWorld(t, acme)

	if code := w.login(); code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, w.stdout, w.stderr)
	}

	if len(w.opened) != 1 {
		t.Fatalf("browser opened %d times", len(w.opened))
	}
	if len(w.oauth.Authorizations) != 1 {
		t.Fatalf("approval page asked %d times", len(w.oauth.Authorizations))
	}
	q := w.oauth.Authorizations[0].Query
	for key, want := range map[string]string{
		"client_id":             "solidtrace-cli",
		"scope":                 "sessions:write factory",
		"resource":              w.oauth.Resource(),
		"response_type":         "code",
		"code_challenge_method": "S256",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("authorize %s = %q, want %q", key, got, want)
		}
	}
	if !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") || !strings.HasSuffix(q.Get("redirect_uri"), "/callback") {
		t.Errorf("redirect_uri = %q", q.Get("redirect_uri"))
	}
	if len(q.Get("state")) < 32 {
		t.Errorf("state %q is too short", q.Get("state"))
	}

	out := w.stdout.String()
	for _, want := range []string{"Acme", "bitbucket.org/acme/shop", "bitbucket.org/acme-tools/*", "each member"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout has no %q:\n%s", want, out)
		}
	}

	info, err := os.Stat(w.credentialsFile())
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("credentials file mode %o, want 600", mode)
	}
	dirInfo, err := os.Stat(w.dir)
	if err != nil {
		t.Fatal(err)
	}
	if mode := dirInfo.Mode().Perm(); mode != 0o700 {
		t.Errorf("config dir mode %o, want 700", mode)
	}
	stored, _ := os.ReadFile(w.credentialsFile())
	for _, want := range []string{w.oauth.URL(), w.agent.URL(), "sto_", "str_"} {
		if !bytes.Contains(stored, []byte(want)) {
			t.Errorf("credentials file has no %q", want)
		}
	}
}
