package main_test

import (
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2/middleware/adaptor"
)

// The Log transform docs/traces.md documents, which the Meta Project is also
// seeded with: it moves a trace=<id> tag on a line into the Log's trace id.
const traceTagTransform = "../console/config/trace_tag_log_transform.vrl"

// readRepoFile reads a file of the repository by its path from event_store/.
func readRepoFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(path))
	if err != nil {
		t.Fatalf("Reading %s failed: %v", path, err)
	}
	return string(raw)
}

// expectInTracesDocs fails unless docs/traces.md shows the file at path as it
// is, so the page shows the code these tests run.
func expectInTracesDocs(t *testing.T, path string) {
	t.Helper()
	docs := readRepoFile(t, "../docs/traces.md")
	if !strings.Contains(docs, strings.TrimSpace(readRepoFile(t, path))) {
		t.Errorf("Expected docs/traces.md to show %s as it is", path)
	}
}

// sendThroughDualTransport runs docs/traces' sender for an SDK, which sends a
// Transaction and an Event in it, in the trace traceID, through its
// DualTransport to sentry.io's DSN and SolidTrace's.
func sendThroughDualTransport(t *testing.T, sdk, sentryDSN, solidTraceDSN, traceID string) {
	t.Helper()
	var cmd *exec.Cmd
	switch sdk {
	case "ruby":
		// Console's bundle has sentry-ruby, as the root tests' db:reset needs it.
		cmd = exec.Command("bundle", "exec", "ruby", "../docs/traces/ruby/send.rb", sentryDSN, solidTraceDSN, traceID)
		cmd.Dir = filepath.Join("..", "console")
	case "go":
		cmd = exec.Command("go", "run", ".", sentryDSN, solidTraceDSN, traceID)
		cmd.Dir = filepath.Join("..", "docs", "traces", "go")
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Sending through %s's DualTransport failed: %v\n%s", sdk, err, output)
	}
}

func TestBothDualTransportsDeliverEachEnvelopeToTwoEventStores(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 124, "solidtrace_public_key")
	expectInTracesDocs(t, "../docs/traces/ruby/dual_transport.rb")
	expectInTracesDocs(t, "../docs/traces/go/transport.go")

	// Two EventStores, as sentry.io and SolidTrace are to an app sending to
	// both: this one's ingest on two addresses, each with a Project's DSN.
	// The second's envelope names the first's DSN and key, but its auth
	// header has its own.
	dsn := func(key string) string {
		server := httptest.NewServer(adaptor.FiberApp(env.App))
		t.Cleanup(server.Close)
		return strings.Replace(server.URL, "http://", "http://"+key+"@", 1) + "/1"
	}
	sentryDSN, solidTraceDSN := dsn("test_public_key"), dsn("solidtrace_public_key")

	traces := map[string]string{"ruby": "7a1f0e4a9b3d4e6f8a7b6c5d4e3f2c10", "go": "7b1f0e4a9b3d4e6f8a7b6c5d4e3f2c11"}
	for sdk, traceID := range traces {
		sendThroughDualTransport(t, sdk, sentryDSN, solidTraceDSN, traceID)
	}
	env.Process(t)
	env.ProcessSpans(t)

	for sdk, traceID := range traces {
		var spans [2][]string
		for i, projectID := range []uint32{123, 124} {
			got := readTrace(t, env, traceID, projectID)
			for _, s := range got.Spans {
				if s.ProjectID != projectID {
					t.Errorf("%s: expected Project %d's Spans only, got %+v", sdk, projectID, s)
				}
				spans[i] = append(spans[i], s.SpanID+" "+s.Op+" "+s.Transaction)
			}
			if len(got.Spans) != 2 {
				t.Errorf("%s: expected the Transaction and its query in Project %d, got %v", sdk, projectID, spans[i])
			}
			if len(got.Issues) != 1 || got.Issues[0].ProjectID != projectID || len(got.Issues[0].Events) != 1 {
				t.Errorf("%s: expected the Event in an Issue of Project %d, got %+v", sdk, projectID, got.Issues)
			}
		}
		if !slices.Equal(spans[0], spans[1]) {
			t.Errorf("%s: expected the same Spans in both, got %v and %v", sdk, spans[0], spans[1])
		}
	}
}

func TestTheDocumentedLogTransformMovesTheTraceTagIntoTheTraceID(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	const trace = "6d2f0e4a9b3d4e6f8a7b6c5d4e3f2b10"
	program := readRepoFile(t, traceTagTransform)
	expectInTracesDocs(t, traceTagTransform)

	lines := []struct{ name, log, message, traceID string }{
		{"a Rails line tagged by the request middleware",
			`{"message": "[0f3c9a1e-req] [trace=` + trace + `] Completed 200 OK in 41ms", "level": "info"}`,
			"[0f3c9a1e-req] Completed 200 OK in 41ms", trace},
		{"lograge's key=value request line",
			`{"message": "method=GET path=/orders/42 controller=OrdersController action=show status=200 duration=41.20 trace=` + trace + `"}`,
			"method=GET path=/orders/42 controller=OrdersController action=show status=200 duration=41.20", trace},
		{"a Rails line in a Sidekiq job tagged by the job middleware",
			`{"message": "[trace=` + trace + `] Charging invoice 42", "level": "info", "service": "worker"}`,
			"Charging invoice 42", trace},
		{"Sidekiq's own job line, with the trace in its context",
			`{"message": "pid=7 tid=2kq class=InvoiceJob jid=b4a1 trace=` + trace + ` elapsed=0.31 INFO: done"}`,
			"pid=7 tid=2kq class=InvoiceJob jid=b4a1 elapsed=0.31 INFO: done", trace},
		{"Console's job line, as Vector ships it",
			`{"message": "[trace=` + trace + `] Performing OverviewJob (Job ID: 1) from SolidQueue(default)", "level": "INFO", "service": "jobs"}`,
			"Performing OverviewJob (Job ID: 1) from SolidQueue(default)", trace},
		{"a line without the tag",
			`{"message": "Listening on http://0.0.0.0:3001", "level": "INFO"}`,
			"Listening on http://0.0.0.0:3001", ""},
		{"a line whose trace id is already set, as a JSON request line's is",
			`{"message": "GET /acme/logs 200, 41.2 ms trace=` + strings.Repeat("a", 32) + `", "trace_id": "` + trace + `"}`,
			"GET /acme/logs 200, 41.2 ms trace=" + strings.Repeat("a", 32), trace},
	}
	logs := make([]string, len(lines))
	for i, line := range lines {
		logs[i] = line.log
	}

	got := previewProgram(t, env, program, logs...)
	if len(got.Errors) != 0 || got.Kept != len(lines) || len(got.Results) != len(lines) {
		t.Fatalf("Expected every line kept, got %+v", got)
	}
	for i, line := range lines {
		after := got.Results[i].After
		if after == nil || after.Message != line.message || after.TraceID != line.traceID || len(after.Attributes) != 0 {
			t.Errorf("%s: expected %q with trace id %q, got %+v", line.name, line.message, line.traceID, after)
		}
	}
}
