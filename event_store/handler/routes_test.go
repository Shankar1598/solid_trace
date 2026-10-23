package handler_test

import (
	"database/sql"
	"encoding/json"
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
	"github.com/solidtrace/event_store/logtransform"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

type testApps struct {
	ingest *fiber.App
	query  *fiber.App
}

// newTestApps writes each event to both stores. Pebble assigns each event
// its UUID.
func newTestApps(t *testing.T, events ...*models.Event) testApps {
	t.Helper()
	return newTestAppsWithStores(t, events, true)
}

// newTestAppsWithStores writes the events to Pebble, and to ClickHouse only
// with indexed set, to model events that Event processing has not yet stored.
// The package's ClickHouse database starts empty.
func newTestAppsWithStores(t *testing.T, events []*models.Event, indexed bool) testApps {
	t.Helper()
	logger.Init()
	tmpDir := t.TempDir()

	pebbleWriter, err := storage.NewPebbleWriter(&config.Config{PebblePath: filepath.Join(tmpDir, "pebble")})
	if err != nil {
		t.Fatalf("Failed to create Pebble: %v", err)
	}
	t.Cleanup(pebbleWriter.Close)

	clickhousetest.Empty(t)
	clickHouse, err := storage.OpenClickHouse(clickhousetest.Config(), nil)
	if err != nil {
		t.Fatalf("Failed to open ClickHouse: %v", err)
	}
	t.Cleanup(func() { clickHouse.Close() })

	var rows []models.Event
	for _, event := range events {
		if err := pebbleWriter.WriteEvent(event); err != nil {
			t.Fatalf("Pebble WriteEvent failed: %v", err)
		}
		if indexed {
			rows = append(rows, *event)
		}
	}
	if len(rows) > 0 {
		r := storage.EventRange{Through: rows[len(rows)-1].EventUUID}
		if err := clickHouse.InsertEvents(r, rows); err != nil {
			t.Fatalf("Inserting the Events failed: %v", err)
		}
	}

	clickHouseLogs := storage.NewClickHouseLogs(clickHouse, pebbleWriter)

	healthHandler := handler.NewHealthHandler(pebbleWriter, answers{})

	ingestApp := fiber.New()
	handler.RegisterIngestRoutes(ingestApp, nil, healthHandler)

	queryApp := fiber.New()
	handler.RegisterQueryRoutes(queryApp, handler.NewEventsHandler(pebbleWriter, clickHouse), handler.NewLogsHandler(clickHouseLogs, storage.NewClickHouseSQL(clickHouse, storage.DefaultSQLLimits()), sqlOn{}, clock.System{}), handler.NewLogTransformHandler(logtransform.New(nil, clock.System{})), healthHandler, handler.NewConfigHandler(servedConfig, servedDiskLimit))

	return testApps{ingest: ingestApp, query: queryApp}
}

// sqlOn is an instance whose operator has turned SQL queries on.
// answers is a store that always answers /health's ping.
type answers struct{}

func (answers) Ping() error { return nil }

type sqlOn struct{}

func (sqlOn) Instance() (storage.InstanceSettings, error) {
	return storage.InstanceSettings{SQLQueriesEnabled: true, DefaultLogRetentionDays: 30}, nil
}

func (sqlOn) ProjectSlug(uint32) (string, error) {
	return "", nil
}

func newEvent(projectID uint32) *models.Event {
	return &models.Event{
		ProjectID:          projectID,
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
		"/api/1/logs/fields?from=2026-10-02T00:00:00Z&to=2026-10-03T00:00:00Z",
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

func TestLogQueryRoutesAreOnlyOnTheQueryApp(t *testing.T) {
	apps := newTestApps(t)
	post := func(app *fiber.App, path, body string) int {
		req, _ := http.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req, 5000)
		if err != nil {
			t.Fatalf("POST %s failed: %v", path, err)
		}
		return resp.StatusCode
	}
	run := `{"query": "", "from": "2026-10-02T00:00:00Z", "to": "2026-10-03T00:00:00Z"}`
	for path, body := range map[string]string{
		"/api/1/logs/query":           run,
		"/api/1/logs/validate":        `{"query": ""}`,
		"/api/1/logs/histogram":       run,
		"/api/1/logs/sql":             `{"sql": "SELECT 1", "from": "2026-10-02T00:00:00Z", "to": "2026-10-03T00:00:00Z"}`,
		"/api/1/logs/readable-sql":    run,
		"/api/1/logs/transform/check": `{"program": ".a = 1"}`,
	} {
		if status := post(apps.ingest, path, body); status != fiber.StatusNotFound {
			t.Errorf("ingest app %s: expected 404, got %d", path, status)
		}
		if status := post(apps.query, path, body); status != fiber.StatusOK {
			t.Errorf("query app %s: expected 200, got %d", path, status)
		}
	}
}

// servedConfig is the config the test query apps say EventStore loaded at
// start, with every secret set. Its disk limit is left at the default, half
// of ClickHouse's disk, which servedDiskLimit says came to 50 GB.
var servedConfig = &config.Config{
	Port:              "4000",
	QueryPort:         "4100",
	PebblePath:        "/storage/pebble",
	LogsDuckDBPath:    "/storage/duckdb/logs.duckdb",
	LogsParquetPath:   "/storage/duckdb/logs_parquet",
	LogsBinCacheBytes: 64 << 20,
	SQLitePath:        "/storage/sqlite/solid_trace.sqlite3",
	MessageQueuePath:  "/storage/sqlite/message_queue.sqlite3",
	IngestMaxWaiting:  10000,
	StorageTiers:      []config.StorageTier{{Kind: "local", Locator: "/storage/tier"}},
	CPUProfileDir:     "/storage/profiles",
	Debug:             true,
	Warnings:          []string{"duckdb_path is used only to find a DuckDB release's logs.duckdb"},
	ObjectStore: &config.ObjectStore{
		Kind: "gcs", Bucket: "acme-logs", Prefix: "solidtrace/", Region: "auto",
		Endpoint: "https://storage.googleapis.com", LocalCacheHours: 24,
		KeyID: "GOOG1EKEYIDVALUE", Secret: "s3cr3t-bucket-value",
	},
	ClickHouse: config.ClickHouse{
		Address: "127.0.0.1:4200", Database: "solidtrace", User: "solidtrace",
		Password: "cl1ckh0use-pw-value", ConfigDir: "/storage/clickhouse/conf.d",
		StoragePolicy: "test_policy",
	},
}

const servedDiskLimit = 50_000_000_000

func TestConfigAnswersTheLoadedConfigWithoutSecrets(t *testing.T) {
	apps := newTestApps(t)

	if resp := get(t, apps.ingest, "/config"); resp.StatusCode != fiber.StatusNotFound {
		t.Errorf("ingest app: expected 404, got %d", resp.StatusCode)
	}
	resp := get(t, apps.query, "/config")
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("query app: expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)

	var got map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unreadable answer %s: %v", body, err)
	}
	want := map[string]any{
		"ingest_port":        "4000",
		"query_port":         "4100",
		"pebble_path":        "/storage/pebble",
		"message_queue_path": "/storage/sqlite/message_queue.sqlite3",
		"sqlite_path":        "/storage/sqlite/solid_trace.sqlite3",
		"logs_parquet_path":  "/storage/duckdb/logs_parquet",
		"logs_disk_limit": map[string]any{
			"configured_bytes": float64(0),
			"effective_bytes":  float64(50_000_000_000),
		},
		"logs_bin_cache_bytes": float64(64 << 20),
		"ingest_max_waiting":   float64(10000),
		"clickhouse": map[string]any{
			"address": "127.0.0.1:4200", "database": "solidtrace", "user": "solidtrace",
			"password_set": true, "config_dir": "/storage/clickhouse/conf.d",
		},
		"object_store": map[string]any{
			"kind": "gcs", "bucket": "acme-logs", "prefix": "solidtrace/", "region": "auto",
			"endpoint": "https://storage.googleapis.com", "local_cache_hours": float64(24),
			"key_id_set": true, "secret_set": true,
		},
		"cpu_profile_dir": "/storage/profiles",
		"debug":           true,
	}
	gotJSON, _ := json.MarshalIndent(got, "", "  ")
	wantJSON, _ := json.MarshalIndent(want, "", "  ")
	if string(gotJSON) != string(wantJSON) {
		t.Errorf("answer:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}
	for _, secret := range []string{"cl1ckh0use-pw-value", "GOOG1EKEYIDVALUE", "s3cr3t-bucket-value"} {
		if strings.Contains(string(body), secret) {
			t.Errorf("the answer holds a secret's value %q", secret)
		}
	}
}

func TestConfigAnswersNoBucketAsNullAndAnUnsetPasswordAsUnset(t *testing.T) {
	cfg := *servedConfig
	cfg.ObjectStore = nil
	cfg.ClickHouse.Password = ""
	app := fiber.New()
	app.Get("/config", handler.NewConfigHandler(&cfg, 1).Config)

	body, _ := io.ReadAll(get(t, app, "/config").Body)
	var got struct {
		ClickHouse struct {
			PasswordSet bool `json:"password_set"`
		} `json:"clickhouse"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unreadable answer %s: %v", body, err)
	}
	if !strings.Contains(string(body), `"object_store":null`) {
		t.Errorf("expected object_store null, got %s", body)
	}
	if got.ClickHouse.PasswordSet {
		t.Errorf("expected password_set false, got %s", body)
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

func TestGetEventDoesNotWaitForEventProcessing(t *testing.T) {
	event := newEvent(1)
	app := newTestAppsWithStores(t, []*models.Event{event}, false).query

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
	for _, path := range []string{"/api/1/store?sentry_key=unknown", "/api/1/envelope?sentry_key=unknown", "/api/1/logs?sentry_key=unknown"} {
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
