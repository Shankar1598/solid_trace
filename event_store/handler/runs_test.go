package handler_test

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/storage"
)

// fakeRunReader records each read and has no Runs.
type fakeRunReader struct {
	lists, runs int
}

func (f *fakeRunReader) RunList(context.Context, uint32, storage.RunFilter) ([]storage.RunSummary, error) {
	f.lists++
	return nil, nil
}

func (f *fakeRunReader) RunEvents(context.Context, uint32, string) ([]models.Log, error) {
	f.runs++
	return nil, nil
}

func newRunsApp(reader *fakeRunReader) *fiber.App {
	app := fiber.New()
	handler.RegisterRunsQueryRoutes(app, handler.NewRunsHandler(reader, clock.NewManual(time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC))))
	return app
}

func TestRunsListValidatesTheRequest(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
		errorText  string
	}{
		{"limit 500", `{"limit":500}`, 200, ""},
		{"no limit", `{}`, 200, ""},
		{"no body", ``, 200, ""},
		{"limit 501", `{"limit":501}`, 400, "limit must be from 1 to 500"},
		{"negative limit", `{"limit":-1}`, 400, "limit must be from 1 to 500"},
		{"from after to", `{"from":"2026-11-15T10:00:00Z","to":"2026-11-14T10:00:00Z"}`, 400, "from must be before to"},
		{"from after now with no to", `{"from":"2026-11-16T10:00:00Z"}`, 400, "from must be before to"},
		{"from with no to", `{"from":"2026-11-14T10:00:00Z"}`, 200, ""},
		{"not JSON", `not json`, 400, "the body must be JSON"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reader := &fakeRunReader{}
			req := httptest.NewRequest("POST", "/api/7/runs", strings.NewReader(c.body))
			if c.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			resp, err := newRunsApp(reader).Test(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			if resp.StatusCode != c.status || !strings.Contains(string(body), c.errorText) {
				t.Fatalf("Expected %d with %q, got %d: %s", c.status, c.errorText, resp.StatusCode, body)
			}
			if wantReads := map[bool]int{true: 1, false: 0}[c.status == 200]; reader.lists != wantReads {
				t.Fatalf("Expected %d reads of the Runs, got %d", wantReads, reader.lists)
			}
		})
	}
}

// A Run id that is not a plain name of at most 64 characters is not read.
func TestOneRunRejectsAnIDThatIsNotAPlainName(t *testing.T) {
	for _, id := range []string{strings.Repeat("a", 65), "a.b", "%2e%2e", "a%2Fb"} {
		reader := &fakeRunReader{}
		resp, err := newRunsApp(reader).Test(httptest.NewRequest("GET", "/api/7/runs/"+id, nil))
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 404 || reader.runs != 0 {
			t.Fatalf("Run id %q: expected 404 with no read, got %d with %d reads", id, resp.StatusCode, reader.runs)
		}
	}
	reader := &fakeRunReader{}
	resp, err := newRunsApp(reader).Test(httptest.NewRequest("GET", "/api/7/runs/"+strings.Repeat("a", 64), nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 404 || reader.runs != 1 {
		t.Fatalf("A Run id of 64 characters: expected a read and 404, got %d with %d reads", resp.StatusCode, reader.runs)
	}
}
