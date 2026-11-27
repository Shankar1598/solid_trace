package handler

import (
	"context"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type downloadSettings struct{ AgentSettings }

func (downloadSettings) AgentProject(int64) (uint32, bool, error) { return 7, true, nil }
func (downloadSettings) RedactionPatterns() ([]string, error)     { return nil, nil }

type downloadFiles struct{ AgentFiles }

func (downloadFiles) AgentSessionFiles(uint32, uuid.UUID) (storage.AgentSession, bool, []storage.AgentFileState, error) {
	return storage.AgentSession{}, true, nil, nil
}

func (downloadFiles) AgentFilesInserted(uint32, []storage.AgentFileState) (bool, error) {
	return true, nil
}

// heldDownloads reads one file of one byte, but only after release is
// closed: a download at the pace of a slow client.
type heldDownloads struct {
	reading chan struct{}
	release chan struct{}
}

func (heldDownloads) AgentDownloadFiles(context.Context, uint32, uuid.UUID) ([]storage.AgentDownloadFile, error) {
	return []storage.AgentDownloadFile{{Path: "session.jsonl", Version: 1, Size: 1, EndOffset: 1}}, nil
}

func (d heldDownloads) ReadAgentDownload(_ context.Context, _ uint32, _ uuid.UUID, _ []storage.AgentDownloadFile, write func(int, []byte) error) error {
	d.reading <- struct{}{}
	<-d.release
	return write(0, []byte("x"))
}

type answer struct {
	status     int
	retryAfter string
	body       string
}

// heldDownloadServer serves the download of one Agent session with held
// reads. get asks for it with the token of a query: user=N is the person
// N's token with sessions:download, list=1 gives it a list of ids in
// place of the Scope, and run=1 makes it a Run's child token. release
// lets the held reads end.
func heldDownloadServer(t *testing.T) (get func(query string) answer, downloads heldDownloads, release func()) {
	t.Helper()
	logger.Init()
	downloads = heldDownloads{reading: make(chan struct{}, 10), release: make(chan struct{})}
	h := NewAgentHandler(AgentConfig{Settings: downloadSettings{}, Files: downloadFiles{}, Downloads: downloads})
	app := fiber.New()
	sessionID := uuid.NewString()
	app.Get("/agent-sessions/:session_id/download", func(c *fiber.Ctx) error {
		user, _ := strconv.ParseInt(c.Query("user"), 10, 64)
		token := auth.Token{UserID: user, Organization: auth.Organization{ID: 1}, Scopes: []string{ScopeSessionsDownload}}
		if c.Query("list") != "" || c.Query("run") != "" {
			// A person's own API token with a list of ids is not a child.
			token.Scopes = []string{ScopeSessionsRead}
			token.AgentSessionIDs = []string{sessionID}
			token.Child = c.Query("run") != ""
		}
		c.Locals(agentTokenKey, token)
		return c.Next()
	}, h.DownloadAgentSession)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(listener) }()
	// A failed check must not leave held reads that keep the Shutdown.
	release = sync.OnceFunc(func() { close(downloads.release) })
	t.Cleanup(func() {
		release()
		_ = app.Shutdown()
	})
	url := "http://" + listener.Addr().String() + "/agent-sessions/" + sessionID + "/download"

	// A download over the limit that waits for the held reads fails at
	// the timeout, and does not keep the test.
	client := &http.Client{Timeout: 5 * time.Second}
	get = func(query string) answer {
		resp, err := client.Get(url + query)
		if err != nil {
			t.Error(err)
			return answer{}
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Error(err)
		}
		return answer{resp.StatusCode, resp.Header.Get(fiber.HeaderRetryAfter), string(body)}
	}
	return get, downloads, release
}

// awaitRead waits until a download reads, and fails if done answers
// first.
func awaitRead(t *testing.T, downloads heldDownloads, done <-chan answer, what string) {
	t.Helper()
	select {
	case <-downloads.reading:
	case got := <-done:
		t.Fatalf("Expected %s to read, got %d with %q: %s", what, got.status, got.retryAfter, got.body)
	case <-time.After(5 * time.Second):
		t.Fatalf("Expected %s to read", what)
	}
}

// expectBusy fails unless got is the 503 with Retry-After of the limit.
func expectBusy(t *testing.T, got answer, what string) {
	t.Helper()
	if got.status != http.StatusServiceUnavailable || got.retryAfter != agentDownloadRetryAfter ||
		!strings.Contains(got.body, downloadsBusy) {
		t.Errorf("Expected 503 with Retry-After %s and the busy text for %s, got %d with %q: %s",
			agentDownloadRetryAfter, what, got.status, got.retryAfter, got.body)
	}
}

// While agentDownloadsAtOnce downloads by people's tokens read, the next
// person's download gets 503 with Retry-After, and the ClickHouse pool
// keeps connections for the rest of the server. A Run's token is not in
// the limit: its download reads, so members' slow downloads do not stop
// the restores of Runs. When they end, a download gets 200.
func TestAgentDownloadsOverTheLimitGet503WithRetryAfter(t *testing.T) {
	get, downloads, release := heldDownloadServer(t)

	held := make(chan answer, agentDownloadsAtOnce)
	for user := range agentDownloadsAtOnce {
		go func() { held <- get("?user=" + strconv.Itoa(user+1)) }()
		awaitRead(t, downloads, held, "a download in the limit")
	}

	expectBusy(t, get("?user=10"), "a download over the limit")
	expectBusy(t, get("?user=11&list=1"), "a person's token with a list over the limit")

	run := make(chan answer, 1)
	go func() { run <- get("?user=1&run=1") }()
	awaitRead(t, downloads, run, "a Run's download while people's downloads are held")

	release()
	for range agentDownloadsAtOnce {
		if got := <-held; got.status != http.StatusOK {
			t.Errorf("Expected 200 for a download in the limit, got %d: %s", got.status, got.body)
		}
	}
	if got := <-run; got.status != http.StatusOK {
		t.Errorf("Expected 200 for a Run's download, got %d: %s", got.status, got.body)
	}
	if got := get("?user=10"); got.status != http.StatusOK {
		t.Errorf("Expected 200 after the downloads ended, got %d: %s", got.status, got.body)
	}
}

// One person has at most 1 download that reads: with slow reads, a person
// must not take all of agentDownloadsAtOnce and stop each other person's
// resume. Another person's download in the limit reads, and so does a
// Run's child token of the same person. When the download ends, the
// person can download again.
func TestAgentDownloadsLimitEachPersonToOne(t *testing.T) {
	get, downloads, release := heldDownloadServer(t)

	first := make(chan answer, 1)
	go func() { first <- get("?user=1") }()
	awaitRead(t, downloads, first, "the person's first download")

	expectBusy(t, get("?user=1"), "the person's second download")
	expectBusy(t, get("?user=1&list=1"), "the person's second download with a list of ids")

	other := make(chan answer, 1)
	go func() { other <- get("?user=2") }()
	awaitRead(t, downloads, other, "another person's download in the limit")

	run := make(chan answer, 1)
	go func() { run <- get("?user=1&run=1") }()
	awaitRead(t, downloads, run, "a Run's download of the same person")

	release()
	for what, done := range map[string]chan answer{"the person's first": first, "the other person's": other, "the Run's": run} {
		if got := <-done; got.status != http.StatusOK {
			t.Errorf("Expected 200 for %s download, got %d: %s", what, got.status, got.body)
		}
	}
	if got := get("?user=1"); got.status != http.StatusOK {
		t.Errorf("Expected 200 for the person's download after the first ended, got %d: %s", got.status, got.body)
	}
}
