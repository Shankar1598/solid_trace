package handler_test

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/solidtrace/event_store/auth"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/ingest"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// updateLogRouteGoldens makes the test write the golden files, in place of
// comparing with them. Run it from event_store/:
//
//	go test -p 1 ./handler -run TestLogRoutesGolden -update-log-routes
var updateLogRouteGoldens = flag.Bool("update-log-routes", false, "write the golden files of TestLogRoutesGolden")

const logRouteGoldenDir = "testdata/log_routes"

// The body caps that the cases cross, by one byte: as sent on the Log route,
// as sent on the Event routes, and once decoded. They are the numbers of
// body.go, written out here so that a change of a cap fails the test.
const (
	logBodyOverLimit     = 10<<20 + 1
	eventBodyOverLimit   = 4<<20 + 1
	decodedBodyOverLimit = 50<<20 + 1
)

// memoryLogWriter is a LogWriter that keeps the Log batches in memory, as
// text. With err set, it fails each write with it.
type memoryLogWriter struct {
	err error

	mu      sync.Mutex
	batches []string
}

func (w *memoryLogWriter) WriteLogBatch(projectID uint32, logs []models.Log, _ time.Time) error {
	if w.err != nil {
		return w.err
	}
	// The text is made now, because the caller can reuse the Logs after this
	// call.
	var batch strings.Builder
	fmt.Fprintf(&batch, "  Log batch for Project %d, Logs: %d", projectID, len(logs))
	for _, log := range logs {
		batch.WriteString("\n    " + storedLogLine(log))
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.batches = append(w.batches, batch.String())
	return nil
}

// storedLogLine is one stored Log as one line of canonical JSON: the keys are
// sorted, and the id is left out, because it is random. The time is stable,
// because the clock of the test is a manual one, or the sender gave the time.
// A string over longStringBytes is shortened to its start, its length and a
// hash, so that a golden file stays readable and still pins each byte.
func storedLogLine(log models.Log) string {
	raw, err := json.Marshal(log)
	if err != nil {
		return fmt.Sprintf("(the Log does not encode: %v)", err)
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return fmt.Sprintf("(the Log does not decode: %v)", err)
	}
	delete(value.(map[string]any), "id")

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(shortenLongStrings(value)); err != nil {
		return fmt.Sprintf("(the Log does not encode again: %v)", err)
	}
	return strings.TrimSuffix(out.String(), "\n")
}

const longStringBytes = 120

// shortenLongStrings returns value with each string over longStringBytes
// replaced by its first 40 bytes, its length and the start of its SHA-256.
func shortenLongStrings(value any) any {
	switch v := value.(type) {
	case string:
		if len(v) <= longStringBytes {
			return v
		}
		sum := sha256.Sum256([]byte(v))
		return fmt.Sprintf("%s...[%d bytes, sha256 %x]", strings.ToValidUTF8(v[:40], "?"), len(v), sum[:6])
	case map[string]any:
		for name, element := range v {
			v[name] = shortenLongStrings(element)
		}
	case []any:
		for i, element := range v {
			v[i] = shortenLongStrings(element)
		}
	}
	return value
}

// stored says what the writer holds, as the lines of a golden file: for each
// Log batch, its Project and each of its Logs, in the order they were stored.
func (w *memoryLogWriter) stored() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.batches) == 0 {
		return "  nothing"
	}
	return strings.Join(w.batches, "\n")
}

// logRouteCase is one fixed request to the ingest app.
type logRouteCase struct {
	name   string
	path   string
	header map[string]string
	body   func() []byte

	// The app is a new one for each case. These options change it.
	overloaded  bool  // the ingest has no waiting place
	writeErr    error // the Log batch write fails with this
	noKeysTable bool  // the key lookup fails, as on a database error
	refuseHTTP  bool  // the app refuses what a proxy forwarded as plain HTTP
}

func text(s string) func() []byte { return func() []byte { return []byte(s) } }

func gzipped(body []byte) []byte {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	w.Write(body)
	w.Close()
	return out.Bytes()
}

// padded is a JSON object, `{}` and then spaces, of exactly size bytes.
func padded(size int) []byte {
	return append([]byte("{}"), bytes.Repeat([]byte(" "), size-2)...)
}

// with returns header with more names and values added; header stays as it is.
func with(header map[string]string, more ...string) map[string]string {
	merged := map[string]string{}
	for name, value := range header {
		merged[name] = value
	}
	for i := 0; i < len(more); i += 2 {
		merged[more[i]] = more[i+1]
	}
	return merged
}

const (
	jsonArray = `[{"message":"one","level":"info"},{"message":"two","service":"web"}]`
	oneObject = `{"message":"one","level":"warn","http.method":"GET"}`
	ndjson    = "{\"message\":\"one\"}\n{\"message\":\"two\"}\n{\"message\":\"three\"}\n"

	// An envelope with one log item of two records, in Sentry's format.
	envelopeWithLogs = `{"sent_at":"2026-11-23T12:00:00Z"}` + "\n" +
		`{"type":"log","item_count":2,"content_type":"application/vnd.sentry.items.log+json"}` + "\n" +
		`{"items":[{"timestamp":1795694400.5,"level":"info","body":"one","trace_id":"5b8efff798038103d269b633813fc60c"},{"timestamp":1795694401,"level":"warn","body":"two"}]}` + "\n"
	// The same, with a record that is not an object.
	envelopeWithBadRecord = `{}` + "\n" +
		`{"type":"log","item_count":2}` + "\n" +
		`{"items":[{"timestamp":1795694400.5,"level":"info","body":"one"},"not an object"]}` + "\n"
)

var (
	asJSON   = map[string]string{"Content-Type": "application/json"}
	asNDJSON = map[string]string{"Content-Type": "application/x-ndjson"}
)

// logRouteCases are the requests. Project 1 has the key key-one, and Project 2
// has the key key-two.
func logRouteCases() []logRouteCase {
	const withKey = "?sentry_key=key-one"
	return []logRouteCase{
		// The forms of the body, on the route of a Project.
		{name: "json-array", path: "/api/1/logs" + withKey, header: asJSON, body: text(jsonArray)},
		{name: "single-object", path: "/api/1/logs" + withKey, header: asJSON, body: text(oneObject)},
		{name: "ndjson", path: "/api/1/logs" + withKey, header: asNDJSON, body: text(ndjson)},
		{name: "ndjson-with-a-bad-line", path: "/api/1/logs" + withKey, header: asNDJSON,
			body: text("{\"message\":\"one\"}\nnot json\n[1]\n{\"message\":\"two\"}\n")},
		{name: "json-array-with-elements-that-are-not-objects", path: "/api/1/logs" + withKey, header: asJSON,
			body: text(`[{"message":"one"},"two",null,3]`)},
		{name: "ndjson-labelled-as-json", path: "/api/1/logs" + withKey, header: asJSON, body: text(ndjson)},
		{name: "no-content-type", path: "/api/1/logs" + withKey, body: text(oneObject)},
		{name: "empty-body", path: "/api/1/logs" + withKey, header: asJSON, body: text("")},
		{name: "a-string-over-64-kb-is-truncated", path: "/api/1/logs" + withKey, header: asJSON,
			body: text(`{"message":"` + strings.Repeat("a", 70<<10) + `"}`)},
		{name: "a-log-over-1-mb-is-dropped", path: "/api/1/logs" + withKey, header: asJSON,
			body: func() []byte {
				var attributes []string
				for i := range 30 {
					attributes = append(attributes, fmt.Sprintf(`"field%d":"%s"`, i, strings.Repeat("b", 60<<10)))
				}
				return []byte(`[{"message":"big",` + strings.Join(attributes, ",") + `},{"message":"small"}]`)
			}},

		// The route with only the key.
		{name: "key-only-json-array", path: "/api/logs" + withKey, header: asJSON, body: text(jsonArray)},
		{name: "key-only-ndjson", path: "/api/logs" + withKey, header: asNDJSON, body: text(ndjson)},
		{name: "key-only-with-the-key-of-the-second-project", path: "/api/logs?sentry_key=key-two", header: asJSON, body: text(oneObject)},

		// The forms of the key.
		{name: "bearer", path: "/api/1/logs", body: text(oneObject),
			header: with(asJSON, "Authorization", "Bearer key-one")},
		{name: "bearer-in-lower-case", path: "/api/1/logs", body: text(oneObject),
			header: with(asJSON, "Authorization", "bearer key-one")},
		{name: "bearer-on-the-key-only-route", path: "/api/logs", body: text(oneObject),
			header: with(asJSON, "Authorization", "Bearer key-one")},
		{name: "bearer-with-no-token", path: "/api/1/logs", body: text(oneObject),
			header: with(asJSON, "Authorization", "Bearer ")},
		{name: "bearer-with-an-unknown-key", path: "/api/1/logs", body: text(oneObject),
			header: with(asJSON, "Authorization", "Bearer nobody")},
		{name: "sentry-key-in-x-sentry-auth", path: "/api/1/logs", body: text(oneObject),
			header: with(asJSON, "X-Sentry-Auth", "Sentry sentry_version=7, sentry_key=key-one, sentry_client=sdk/1.0")},
		{name: "sentry-key-in-authorization", path: "/api/1/logs", body: text(oneObject),
			header: with(asJSON, "Authorization", "Sentry sentry_key=key-one")},
		{name: "sentry-key-in-the-query-string", path: "/api/1/logs" + withKey, header: asJSON, body: text(oneObject)},
		{name: "x-sentry-auth-wins-over-bearer", path: "/api/1/logs", body: text(oneObject),
			header: with(asJSON, "X-Sentry-Auth", "Sentry sentry_key=nobody", "Authorization", "Bearer key-one")},
		{name: "missing-key", path: "/api/1/logs", header: asJSON, body: text(oneObject)},
		{name: "missing-key-on-the-key-only-route", path: "/api/logs", header: asJSON, body: text(oneObject)},
		{name: "unknown-key", path: "/api/1/logs?sentry_key=nobody", header: asJSON, body: text(oneObject)},
		{name: "unknown-key-on-the-key-only-route", path: "/api/logs?sentry_key=nobody", header: asJSON, body: text(oneObject)},
		{name: "key-of-another-project", path: "/api/1/logs?sentry_key=key-two", header: asJSON, body: text(oneObject)},
		{name: "project-id-that-is-not-a-number", path: "/api/abc/logs" + withKey, header: asJSON, body: text(oneObject)},
		{name: "key-lookup-fails", path: "/api/1/logs" + withKey, header: asJSON, body: text(oneObject), noKeysTable: true},

		// The body caps and the content codings.
		{name: "gzip", path: "/api/1/logs" + withKey, header: with(asJSON, "Content-Encoding", "gzip"),
			body: func() []byte { return gzipped([]byte(jsonArray)) }},
		{name: "body-over-the-limit", path: "/api/1/logs" + withKey, header: asJSON,
			body: func() []byte { return padded(logBodyOverLimit) }},
		{name: "body-over-the-limit-once-decoded", path: "/api/1/logs" + withKey,
			header: with(asJSON, "Content-Encoding", "gzip"),
			body:   func() []byte { return gzipped(padded(decodedBodyOverLimit)) }},
		{name: "unknown-content-encoding", path: "/api/1/logs" + withKey,
			header: with(asJSON, "Content-Encoding", "compress"), body: text(oneObject)},
		{name: "content-encoding-applied-twice", path: "/api/1/logs" + withKey,
			header: with(asJSON, "Content-Encoding", "gzip, gzip"),
			body:   func() []byte { return gzipped(gzipped([]byte(oneObject))) }},
		{name: "gzip-body-that-does-not-decode", path: "/api/1/logs" + withKey,
			header: with(asJSON, "Content-Encoding", "gzip"), body: text(oneObject)},
		{name: "zstd-body-that-does-not-decode", path: "/api/1/logs" + withKey,
			header: with(asJSON, "Content-Encoding", "zstd"), body: text(oneObject)},
		{name: "key-is-checked-before-the-body", path: "/api/1/logs?sentry_key=nobody",
			header: with(asJSON, "Content-Encoding", "compress"), body: text(oneObject)},

		// Overload, shutdown and failures.
		{name: "overloaded-ingest", path: "/api/1/logs" + withKey, header: asJSON, body: text(oneObject), overloaded: true},
		{name: "overloaded-ingest-with-a-body-that-does-not-decode", path: "/api/1/logs" + withKey,
			header: with(asJSON, "Content-Encoding", "gzip"), body: text(oneObject), overloaded: true},
		{name: "overloaded-ingest-on-the-key-only-route", path: "/api/logs" + withKey, header: asJSON,
			body: text(oneObject), overloaded: true},
		{name: "store-is-shutting-down", path: "/api/1/logs" + withKey, header: asJSON, body: text(oneObject),
			writeErr: fmt.Errorf("write log batch: %w", storage.ErrShuttingDown)},
		{name: "store-fails", path: "/api/1/logs" + withKey, header: asJSON, body: text(oneObject),
			writeErr: errors.New("pebble: disk full")},
		{name: "plain-http-forwarded-by-a-proxy", path: "/api/1/logs" + withKey,
			header: with(asJSON, "X-Forwarded-Proto", "http"), body: text(oneObject), refuseHTTP: true},

		// The log items of a Sentry envelope.
		{name: "envelope-with-a-log-item", path: "/api/1/envelope" + withKey, body: text(envelopeWithLogs)},
		{name: "envelope-with-a-log-item-and-x-sentry-auth", path: "/api/1/envelope", body: text(envelopeWithLogs),
			header: map[string]string{"X-Sentry-Auth": "Sentry sentry_key=key-one"}},
		{name: "envelope-with-a-record-that-is-not-an-object", path: "/api/1/envelope" + withKey,
			body: text(envelopeWithBadRecord)},
		{name: "envelope-gzip", path: "/api/1/envelope" + withKey, header: map[string]string{"Content-Encoding": "gzip"},
			body: func() []byte { return gzipped([]byte(envelopeWithLogs)) }},
		{name: "envelope-that-is-not-an-envelope", path: "/api/1/envelope" + withKey, body: text("not json\n")},
		{name: "envelope-with-an-item-header-that-is-not-json", path: "/api/1/envelope" + withKey,
			body: text("{}\nnot json\n{}")},
		{name: "envelope-with-bearer-only", path: "/api/1/envelope", body: text(envelopeWithLogs),
			header: map[string]string{"Authorization": "Bearer key-one"}},
		{name: "envelope-with-an-unknown-key", path: "/api/1/envelope?sentry_key=nobody", body: text(envelopeWithLogs)},
		{name: "envelope-with-a-missing-key", path: "/api/1/envelope", body: text(envelopeWithLogs)},
		{name: "envelope-over-the-limit", path: "/api/1/envelope" + withKey,
			body: func() []byte { return padded(eventBodyOverLimit) }},
		{name: "envelope-overloaded", path: "/api/1/envelope" + withKey, body: text(envelopeWithLogs), overloaded: true},
		{name: "envelope-store-is-shutting-down", path: "/api/1/envelope" + withKey, body: text(envelopeWithLogs),
			writeErr: fmt.Errorf("write log batch: %w", storage.ErrShuttingDown)},
		{name: "envelope-store-fails", path: "/api/1/envelope" + withKey, body: text(envelopeWithLogs),
			writeErr: errors.New("pebble: disk full")},
	}
}

// headerOrNone is a response header's value, or "(none)", so that no line of a
// golden file ends in a space.
func headerOrNone(resp *http.Response, name string) string {
	if value := resp.Header.Get(name); value != "" {
		return value
	}
	return "(none)"
}

// sendLogRouteCase sends the case to a new ingest app, built as main builds
// it: the real handler and the real Log ingest, over a Console SQLite file
// with the two keys, with a Log writer in memory. This test itself uses no
// ClickHouse, but the package needs the dev ClickHouse to run: its TestMain
// (clickhousetest.Main) starts with it. It answers what a golden file holds.
func sendLogRouteCase(t *testing.T, c logRouteCase) string {
	t.Helper()
	logger.Init()
	sqlitePath := filepath.Join(t.TempDir(), "console.sqlite3")
	if !c.noKeysTable {
		db, err := sql.Open("sqlite3", sqlitePath)
		if err != nil {
			t.Fatalf("Failed to open SQLite: %v", err)
		}
		if _, err := db.Exec("CREATE TABLE project_keys (project_id INTEGER, public_key TEXT)"); err != nil {
			t.Fatalf("Failed to create project_keys: %v", err)
		}
		if _, err := db.Exec("INSERT INTO project_keys (project_id, public_key) VALUES (1, 'key-one'), (2, 'key-two')"); err != nil {
			t.Fatalf("Failed to add the keys: %v", err)
		}
		db.Close()
	}
	projectAuth, err := auth.NewProjectAuth(sqlitePath)
	if err != nil {
		t.Fatalf("Failed to create ProjectAuth: %v", err)
	}
	t.Cleanup(projectAuth.Close)

	writer := &memoryLogWriter{err: c.writeErr}
	maxWaiting := 10
	if c.overloaded {
		maxWaiting = 0
	}
	service := ingest.NewService(ingest.Config{
		Logs:       writer,
		NotifyLogs: func(uint32) {},
		Clock:      clock.NewManual(time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)),
		Retention:  ingest.FixedRetention(30),
		MaxWaiting: maxWaiting,
	})
	app := handler.NewIngestApp(handler.NewIngestHandler(projectAuth, service), nil,
		handler.IngestOptions{IdleTimeout: time.Second, RefuseForwardedHTTP: c.refuseHTTP})

	req := httptest.NewRequest(http.MethodPost, c.path, bytes.NewReader(c.body()))
	for name, value := range c.header {
		req.Header.Set(name, value)
	}
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("POST %s: %v", c.path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("POST %s: reading the body: %v", c.path, err)
	}

	// The body is all the bytes between the line "body:" and the last newline,
	// which the file adds so that it ends in one.
	var out bytes.Buffer
	fmt.Fprintf(&out, "status: %d\n", resp.StatusCode)
	fmt.Fprintf(&out, "content-type: %s\n", headerOrNone(resp, "Content-Type"))
	fmt.Fprintf(&out, "retry-after: %s\n", headerOrNone(resp, "Retry-After"))
	fmt.Fprintf(&out, "stored:\n%s\n", writer.stored())
	out.WriteString("body:\n")
	out.Write(body)
	out.WriteByte('\n')
	return out.String()
}

// The Log ingest routes answer each fixed request with the same bytes as
// before: their status, Content-Type, Retry-After, what they stored (each Log
// batch with its Project and its Logs, as normalised) and their body. A change
// of one byte fails the test. When an answer is meant to change, run it with
// -update-log-routes, and read the diff of the golden files.
//
// The routes are POST /api/:project_id/logs, POST /api/logs, and the log
// items on POST /api/:project_id/envelope.
func TestLogRoutesGolden(t *testing.T) {
	cases := logRouteCases()
	names := map[string]bool{}
	for _, c := range cases {
		if names[c.name] {
			t.Fatalf("Two cases are named %q", c.name)
		}
		names[c.name] = true
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sendLogRouteCase(t, c)
			file := filepath.Join(logRouteGoldenDir, c.name+".golden")

			if *updateLogRouteGoldens {
				if err := os.MkdirAll(logRouteGoldenDir, 0o755); err != nil {
					t.Fatalf("Failed to make %s: %v", logRouteGoldenDir, err)
				}
				if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
					t.Fatalf("Failed to write %s: %v", file, err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("Failed to read %s: %v\nRun with -update-log-routes to write it.", file, err)
			}
			if got != string(want) {
				t.Errorf("POST %s answered other bytes than %s holds.\ngot:\n%s\nwant:\n%s", c.path, file, got, want)
			}
		})
	}

	// A case that is deleted must not pass unseen: each golden file needs a
	// case, and with -update-log-routes the files with no case are deleted.
	files, err := filepath.Glob(filepath.Join(logRouteGoldenDir, "*.golden"))
	if err != nil {
		t.Fatalf("Failed to list the golden files: %v", err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".golden")
		if names[name] {
			continue
		}
		if *updateLogRouteGoldens {
			if err := os.Remove(file); err != nil {
				t.Errorf("Failed to delete %s: %v", file, err)
			}
			continue
		}
		t.Errorf("%s has no case. Run with -update-log-routes to delete it.", file)
	}
}
