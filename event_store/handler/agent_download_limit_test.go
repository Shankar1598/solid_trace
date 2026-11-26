package handler

import (
	"context"
	"io"
	"net"
	"net/http"
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

// While agentDownloadsAtOnce downloads by a person's token read, the next
// person's download gets 503 with Retry-After, and the ClickHouse pool
// keeps connections for the rest of the server. A Run's token is not in
// the limit: its download reads, so a member's slow downloads do not stop
// the restores of Runs. When they end, a download gets 200.
func TestAgentDownloadsOverTheLimitGet503WithRetryAfter(t *testing.T) {
	logger.Init()
	downloads := heldDownloads{reading: make(chan struct{}, 10), release: make(chan struct{})}
	h := NewAgentHandler(AgentConfig{Settings: downloadSettings{}, Files: downloadFiles{}, Downloads: downloads})
	app := fiber.New()
	sessionID := uuid.NewString()
	app.Get("/agent-sessions/:session_id/download", func(c *fiber.Ctx) error {
		token := auth.Token{Organization: auth.Organization{ID: 1}, Scopes: []string{ScopeSessionsDownload}}
		if c.Query("token") == "run" {
			token = auth.Token{Organization: auth.Organization{ID: 1}, Scopes: []string{ScopeSessionsRead},
				AgentSessionIDs: []string{sessionID}}
		}
		c.Locals(agentTokenKey, token)
		return c.Next()
	}, h.DownloadAgentSession)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = app.Listener(listener) }()
	defer func() { _ = app.Shutdown() }()
	// A failed check must not leave held reads that keep the Shutdown.
	release := sync.OnceFunc(func() { close(downloads.release) })
	defer release()
	url := "http://" + listener.Addr().String() + "/agent-sessions/" + sessionID + "/download"

	type answer struct {
		status     int
		retryAfter string
		body       string
	}
	// A download over the limit that waits for the held reads fails at
	// the timeout, and does not keep the test.
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(query string) answer {
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

	held := make(chan answer, agentDownloadsAtOnce)
	for range agentDownloadsAtOnce {
		go func() { held <- get("") }()
	}
	for range agentDownloadsAtOnce {
		select {
		case <-downloads.reading:
		case <-time.After(5 * time.Second):
			t.Fatal("Expected the first downloads to read")
		}
	}

	if got := get(""); got.status != http.StatusServiceUnavailable || got.retryAfter != agentDownloadRetryAfter {
		t.Errorf("Expected 503 with Retry-After %s over the limit, got %d with %q: %s",
			agentDownloadRetryAfter, got.status, got.retryAfter, got.body)
	}

	run := make(chan answer, 1)
	go func() { run <- get("?token=run") }()
	select {
	case <-downloads.reading:
	case got := <-run:
		t.Fatalf("Expected a Run's download to read while the person's slots are held, got %d with %q: %s",
			got.status, got.retryAfter, got.body)
	case <-time.After(5 * time.Second):
		t.Fatal("Expected a Run's download to read while the person's slots are held")
	}

	release()
	for range agentDownloadsAtOnce {
		if got := <-held; got.status != http.StatusOK {
			t.Errorf("Expected 200 for a download in the limit, got %d: %s", got.status, got.body)
		}
	}
	if got := <-run; got.status != http.StatusOK {
		t.Errorf("Expected 200 for a Run's download, got %d: %s", got.status, got.body)
	}
	if got := get(""); got.status != http.StatusOK {
		t.Errorf("Expected 200 after the downloads ended, got %d: %s", got.status, got.body)
	}
}
