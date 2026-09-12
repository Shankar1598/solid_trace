package handler_test

import (
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const testToken = "test-internal-token-0123456789abcdef"

func newTestApp(t *testing.T, events ...models.Event) *fiber.App {
	t.Helper()
	logger.Init()
	tmpDir := t.TempDir()

	pebbleWriter, err := storage.NewPebbleWriter(&config.Config{PebblePath: filepath.Join(tmpDir, "pebble")})
	if err != nil {
		t.Fatalf("Failed to create Pebble: %v", err)
	}
	t.Cleanup(pebbleWriter.Close)

	duckdbWriter, err := storage.NewDuckDBWriter(filepath.Join(tmpDir, "events.duckdb"), filepath.Join(tmpDir, "parquet"), "", "")
	if err != nil {
		t.Fatalf("Failed to create DuckDB: %v", err)
	}
	t.Cleanup(duckdbWriter.Close)

	if err := pebbleWriter.WriteBatch(events); err != nil {
		t.Fatalf("Pebble WriteBatch failed: %v", err)
	}
	if err := duckdbWriter.WriteBatch(events); err != nil {
		t.Fatalf("DuckDB WriteBatch failed: %v", err)
	}

	app := fiber.New()
	handler.RegisterRoutes(
		app,
		nil,
		handler.NewEventsHandler(pebbleWriter, duckdbWriter),
		handler.NewHealthHandler(pebbleWriter, duckdbWriter),
		testToken,
	)
	return app
}

func newEvent(projectID uint32) models.Event {
	return models.Event{
		ProjectID:          projectID,
		EventUUID:          uuid.Must(uuid.NewV7()).String(),
		IssueFingerprintID: 10,
		Timestamp:          time.Now().UTC(),
		RawJSON:            []byte(`{"message":"boom"}`),
		Tags:               map[string]string{},
	}
}

func get(t *testing.T, app *fiber.App, path, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set(handler.InternalTokenHeader, token)
	}
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("GET %s failed: %v", path, err)
	}
	return resp
}

func TestQueryRoutesRequireInternalToken(t *testing.T) {
	event := newEvent(1)
	app := newTestApp(t, event)

	paths := []string{
		"/api/1/events",
		"/api/1/events/count",
		"/api/1/events/context?fingerprint_ids=10",
		"/api/1/events/" + event.EventUUID,
	}

	for _, path := range paths {
		t.Run("missing token "+path, func(t *testing.T) {
			if resp := get(t, app, path, ""); resp.StatusCode != fiber.StatusUnauthorized {
				t.Errorf("expected 401, got %d", resp.StatusCode)
			}
		})
		t.Run("wrong token "+path, func(t *testing.T) {
			if resp := get(t, app, path, "wrong-internal-token-0123456789abcdef"); resp.StatusCode != fiber.StatusUnauthorized {
				t.Errorf("expected 401, got %d", resp.StatusCode)
			}
		})
		t.Run("valid token "+path, func(t *testing.T) {
			if resp := get(t, app, path, testToken); resp.StatusCode != fiber.StatusOK {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestHealthDoesNotRequireInternalToken(t *testing.T) {
	app := newTestApp(t, newEvent(1))

	if resp := get(t, app, "/health", ""); resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestInternalAuthRejectsAllRequestsWhenTokenIsEmpty(t *testing.T) {
	app := fiber.New()
	app.Get("/", handler.InternalAuth(""), func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	if resp := get(t, app, "/", ""); resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("expected 401, got %d", resp.StatusCode)
	}
}

func TestGetEventIsScopedToProject(t *testing.T) {
	event := newEvent(1)
	app := newTestApp(t, event)

	t.Run("event in project returns payload", func(t *testing.T) {
		resp := get(t, app, "/api/1/events/"+event.EventUUID, testToken)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != string(event.RawJSON) {
			t.Errorf("expected payload %s, got %s", event.RawJSON, body)
		}
	})

	t.Run("event in another project is not found", func(t *testing.T) {
		if resp := get(t, app, "/api/2/events/"+event.EventUUID, testToken); resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("unknown event is not found", func(t *testing.T) {
		if resp := get(t, app, "/api/1/events/"+uuid.NewString(), testToken); resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("malformed UUID is rejected", func(t *testing.T) {
		if resp := get(t, app, "/api/1/events/not-a-uuid", testToken); resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}
