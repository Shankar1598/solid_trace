package handler_test

import (
	"io"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/storage"
)

func TestTheIngestAppKeepsAnIdleConnectionAsLongAsItsOptionsSay(t *testing.T) {
	app := handler.NewIngestApp(nil, nil, handler.IngestOptions{IdleTimeout: 11 * time.Minute})

	if got := app.Config().IdleTimeout; got != 11*time.Minute {
		t.Errorf("IdleTimeout = %v, want 11m", got)
	}
	// The option changes only the idle time. The body limit sets ReadTimeout.
	other := handler.NewIngestApp(nil, nil, handler.IngestOptions{IdleTimeout: time.Second})
	if got, want := app.Config().ReadTimeout, other.Config().ReadTimeout; got != want {
		t.Errorf("ReadTimeout = %v, want %v, as with another IdleTimeout", got, want)
	}
}

// sendForwarded sends a request with X-Forwarded-Proto proto, or none if it's empty,
// and answers the status and body.
func sendForwarded(t *testing.T, app *fiber.App, method, path, proto string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	if proto != "" {
		req.Header.Set("X-Forwarded-Proto", proto)
	}
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

// The ingest handler is nil, so these requests must stop at the refusal, or
// at /health.
func TestTheIngestAppRefusesIngestThatAProxyForwardedFromPlainHTTP(t *testing.T) {
	pebbleWriter, err := storage.NewPebbleWriter(&config.Config{PebblePath: filepath.Join(t.TempDir(), "pebble")})
	if err != nil {
		t.Fatalf("Failed to create Pebble: %v", err)
	}
	t.Cleanup(pebbleWriter.Close)
	app := handler.NewIngestApp(nil, handler.NewHealthHandler(pebbleWriter, answers{}),
		handler.IngestOptions{IdleTimeout: time.Second, RefuseForwardedHTTP: true})

	for _, proto := range []string{"http", "HTTP", " http ", "https, http"} {
		for _, path := range []string{"/api/logs", "/api/1/logs", "/api/1/envelope", "/api/1/store", "/v1/logs"} {
			code, body := sendForwarded(t, app, "POST", path, proto)
			if code != fiber.StatusForbidden || !strings.Contains(body, "Use an https:// URL") {
				t.Errorf("POST %s with X-Forwarded-Proto %q: got %d %q, want 403 that says to use https", path, proto, code, body)
			}
		}
	}
	// A load balancer's health check sends no X-Forwarded-Proto.
	for _, proto := range []string{"", "http"} {
		if code, _ := sendForwarded(t, app, "GET", "/health", proto); code != fiber.StatusOK {
			t.Errorf("GET /health with X-Forwarded-Proto %q: got %d, want 200", proto, code)
		}
	}
}

func TestRefuseForwardedHTTPPassesHTTPSAndRequestsNoProxyForwarded(t *testing.T) {
	app := fiber.New()
	app.Use(handler.RefuseForwardedHTTP)
	app.Post("/api/logs", func(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusAccepted) })

	// The last value is the proxy's; one before it is the client's.
	for _, proto := range []string{"", "https", "HTTPS", "http, https"} {
		if code, _ := sendForwarded(t, app, "POST", "/api/logs", proto); code != fiber.StatusAccepted {
			t.Errorf("POST with X-Forwarded-Proto %q: got %d, want 202", proto, code)
		}
	}
}
