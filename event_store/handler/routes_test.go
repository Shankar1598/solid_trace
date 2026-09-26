package handler_test

import (
	"database/sql"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type testApps struct {
	ingest *fiber.App
	query  *fiber.App
}

func newTestApps(t *testing.T, events ...models.Event) testApps {
	t.Helper()
	return newTestAppsWithStores(t, events, events)
}

// newTestAppsWithStores writes different events to each store, to model events
// that Pebble has committed but DuckDB has not yet ingested.
func newTestAppsWithStores(t *testing.T, pebbleEvents, duckdbEvents []models.Event) testApps {
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

	if err := pebbleWriter.WriteBatch(pebbleEvents); err != nil {
		t.Fatalf("Pebble WriteBatch failed: %v", err)
	}
	if err := duckdbWriter.WriteBatch(duckdbEvents); err != nil {
		t.Fatalf("DuckDB WriteBatch failed: %v", err)
	}

	healthHandler := handler.NewHealthHandler(pebbleWriter, duckdbWriter)

	ingestApp := fiber.New()
	handler.RegisterIngestRoutes(ingestApp, nil, healthHandler)

	queryApp := fiber.New()
	handler.RegisterQueryRoutes(queryApp, handler.NewEventsHandler(pebbleWriter, duckdbWriter), healthHandler)

	return testApps{ingest: ingestApp, query: queryApp}
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

func send(t *testing.T, app *fiber.App, method, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, path, strings.NewReader("{}"))
	resp, err := app.Test(req, 5000)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, path, err)
	}
	return resp
}

func get(t *testing.T, app *fiber.App, path string) *http.Response {
	t.Helper()
	return send(t, app, http.MethodGet, path)
}

func TestQueryRoutesAreOnlyOnTheQueryApp(t *testing.T) {
	event := newEvent(1)
	apps := newTestApps(t, event)

	paths := []string{
		"/api/1/events",
		"/api/1/events/count",
		"/api/1/events/context?fingerprint_ids=10",
		"/api/1/events/" + event.EventUUID,
	}

	for _, path := range paths {
		t.Run("ingest app "+path, func(t *testing.T) {
			if resp := get(t, apps.ingest, path); resp.StatusCode != fiber.StatusNotFound {
				t.Errorf("expected 404, got %d", resp.StatusCode)
			}
		})
		t.Run("query app "+path, func(t *testing.T) {
			if resp := get(t, apps.query, path); resp.StatusCode != fiber.StatusOK {
				t.Errorf("expected 200, got %d", resp.StatusCode)
			}
		})
	}
}

func TestHealthIsOnBothApps(t *testing.T) {
	apps := newTestApps(t)

	for name, app := range map[string]*fiber.App{"ingest": apps.ingest, "query": apps.query} {
		if resp := get(t, app, "/health"); resp.StatusCode != fiber.StatusOK {
			t.Errorf("%s app: expected 200, got %d", name, resp.StatusCode)
		}
	}
}

func TestGetEventIsScopedToProject(t *testing.T) {
	event := newEvent(1)
	app := newTestApps(t, event).query

	t.Run("event in project returns payload", func(t *testing.T) {
		resp := get(t, app, "/api/1/events/"+event.EventUUID)
		if resp.StatusCode != fiber.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		if string(body) != string(event.RawJSON) {
			t.Errorf("expected payload %s, got %s", event.RawJSON, body)
		}
	})

	t.Run("event in another project is not found", func(t *testing.T) {
		if resp := get(t, app, "/api/2/events/"+event.EventUUID); resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("unknown event is not found", func(t *testing.T) {
		if resp := get(t, app, "/api/1/events/"+uuid.NewString()); resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("malformed UUID is rejected", func(t *testing.T) {
		if resp := get(t, app, "/api/1/events/not-a-uuid"); resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}

func TestGetEventDoesNotWaitForDuckDB(t *testing.T) {
	event := newEvent(1)
	app := newTestAppsWithStores(t, []models.Event{event}, nil).query

	if resp := get(t, app, "/api/1/events/"+event.EventUUID); resp.StatusCode != fiber.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// newIngestApp serves ingest routes backed by a SQLite file. With withKeysTable
// false the key lookup fails, as it would on a database error.
func newIngestApp(t *testing.T, withKeysTable bool) *fiber.App {
	t.Helper()
	logger.Init()
	sqlitePath := filepath.Join(t.TempDir(), "console.sqlite3")

	if withKeysTable {
		db, err := sql.Open("sqlite3", sqlitePath)
		if err != nil {
			t.Fatalf("Failed to open SQLite: %v", err)
		}
		if _, err := db.Exec("CREATE TABLE project_keys (project_id INTEGER, public_key TEXT)"); err != nil {
			t.Fatalf("Failed to create project_keys: %v", err)
		}
		db.Close()
	}

	projectAuth, err := auth.NewProjectAuth(sqlitePath)
	if err != nil {
		t.Fatalf("Failed to create ProjectAuth: %v", err)
	}
	t.Cleanup(projectAuth.Close)

	app := fiber.New()
	handler.RegisterIngestRoutes(app, handler.NewIngestHandler(projectAuth, nil), nil)
	return app
}

func TestIngestKeyLookupStatus(t *testing.T) {
	for _, path := range []string{"/api/1/store?sentry_key=unknown", "/api/1/envelope?sentry_key=unknown"} {
		t.Run("unknown key is 401 "+path, func(t *testing.T) {
			if resp := send(t, newIngestApp(t, true), http.MethodPost, path); resp.StatusCode != fiber.StatusUnauthorized {
				t.Errorf("expected 401, got %d", resp.StatusCode)
			}
		})
		t.Run("lookup failure is 500 "+path, func(t *testing.T) {
			if resp := send(t, newIngestApp(t, false), http.MethodPost, path); resp.StatusCode != fiber.StatusInternalServerError {
				t.Errorf("expected 500, got %d", resp.StatusCode)
			}
		})
	}
}
