package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// The Transactions below are envelopes captured from sentry-ruby 5.28 and
// 6.3 and from sentry-go 0.47, with fixed ids and times, by the programs in
// testdata/transactions:
//   - sentry-ruby-*-rails: a Rails request at a 25% sample rate, with an N+1
//     of three line items and a view that calls a service over HTTP, in the
//     traces 5c1f…2a10 (5.28) and 6d2f…2b10 (6.3);
//   - sentry-ruby-*-url: a Rack request named by its raw URL;
//   - sentry-go-0.47: the service answering that call in the 6.3 trace, at a
//     5% sample rate.
const (
	ruby528Trace = "5c1f0e4a9b3d4e6f8a7b6c5d4e3f2a10"
	ruby63Trace  = "6d2f0e4a9b3d4e6f8a7b6c5d4e3f2b10"
	ruby63URL    = "6d2f0e4a9b3d4e6f8a7b6c5d4e3f2b11"
)

// transactionFixture reads a captured envelope.
func transactionFixture(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "transactions", name+".envelope"))
	if err != nil {
		t.Fatalf("Reading the fixture %s failed: %v", name, err)
	}
	return string(raw)
}

// postEnvelopeWithKey sends an envelope with a Project's key, as a Sentry SDK
// does, and expects 200.
func postEnvelopeWithKey(t *testing.T, env *TestEnv, key, envelope string) {
	t.Helper()
	req, _ := http.NewRequest("POST", "/api/1/envelope", bytes.NewBufferString(envelope))
	req.Header.Set("Content-Type", "application/x-sentry-envelope")
	req.Header.Set("X-Sentry-Auth", "Sentry sentry_version=7, sentry_key="+key)
	resp, err := env.App.Test(req, 2000)
	if err != nil {
		t.Fatalf("POST envelope failed: %v", err)
	}
	if resp.StatusCode != 200 {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected 200, got %d: %s", resp.StatusCode, raw)
	}
}

// traceSpan is a Span as the trace route answers it.
type traceSpan struct {
	ProjectID    uint32            `json:"project_id"`
	SpanID       string            `json:"span_id"`
	ParentSpanID string            `json:"parent_span_id"`
	SegmentID    string            `json:"segment_id"`
	IsSegment    bool              `json:"is_segment"`
	Transaction  string            `json:"transaction"`
	Op           string            `json:"op"`
	Description  string            `json:"description"`
	Group        string            `json:"group"`
	Repeats      int               `json:"repeats"`
	Status       string            `json:"status"`
	Start        time.Time         `json:"start"`
	DurationUs   int64             `json:"duration_us"`
	SelfTimeUs   int64             `json:"self_time_us"`
	SampleRate   float64           `json:"sample_rate"`
	Environment  string            `json:"environment"`
	Release      string            `json:"release"`
	ServerName   string            `json:"server_name"`
	Attributes   map[string]string `json:"attributes"`
}

// row is a Span's place in its trace and its timing, as one line.
func (s traceSpan) row() string {
	return fmt.Sprintf("%d %s<-%s %s %q %s/%s %dus self %dus %q x%d",
		s.ProjectID, s.SpanID, s.ParentSpanID, s.Op, s.Description, s.SegmentID, s.Transaction,
		s.DurationUs, s.SelfTimeUs, s.Status, s.Repeats)
}

// traceAnswer is the trace route's answer.
type traceAnswer struct {
	TraceID string      `json:"trace_id"`
	Spans   []traceSpan `json:"spans"`
	// The trace's Logs, read from LogsFrom to LogsTo, and its Events by
	// Issue.
	Logs     []traceLog   `json:"logs"`
	MoreLogs bool         `json:"more_logs"`
	LogsFrom *time.Time   `json:"logs_from"`
	LogsTo   *time.Time   `json:"logs_to"`
	Issues   []traceIssue `json:"issues"`
}

// traceLog is a Log as the trace route answers it, with its Project.
type traceLog struct {
	ProjectID uint32    `json:"project_id"`
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Message   string    `json:"message"`
	TraceID   string    `json:"trace_id"`
}

// traceIssue is an Issue's Events in a trace, by the Issue's fingerprint, as
// the trace route answers them.
type traceIssue struct {
	ProjectID          uint32       `json:"project_id"`
	IssueFingerprintID int64        `json:"issue_fingerprint_id"`
	Events             []traceEvent `json:"events"`
}

type traceEvent struct {
	UUID      string    `json:"uuid"`
	Timestamp time.Time `json:"timestamp"`
}

// readTrace asks the trace route for a trace across Projects.
func readTrace(t *testing.T, env *TestEnv, traceID string, projectIDs ...uint32) traceAnswer {
	t.Helper()
	return readTraceAround(t, env, traceID, nil, projectIDs...)
}

// readTraceAround asks the trace route for a trace across Projects, with a
// time hint unless hint is nil.
func readTraceAround(t *testing.T, env *TestEnv, traceID string, hint *time.Time, projectIDs ...uint32) traceAnswer {
	t.Helper()
	body := map[string]any{"project_ids": projectIDs, "trace_id": traceID}
	if hint != nil {
		body["time"] = hint.Format(time.RFC3339Nano)
	}
	status, raw := postQueryApp(t, env, "/trace", body)
	if status != 200 {
		t.Fatalf("Trace %s expected 200, got %d: %s", traceID, status, raw)
	}
	var answer traceAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode trace %q: %v", raw, err)
	}
	return answer
}

func rows(spans []traceSpan) []string {
	rows := make([]string, len(spans))
	for i, s := range spans {
		rows[i] = s.row()
	}
	return rows
}

// railsRows are the Rails request's Spans in Project 123, in start order:
// the segment's self time is its 120 ms less its children's 10, 15 and
// 60 ms, and the view's is its 60 ms less the HTTP call's 30.
var railsRows = []string{
	`123 a000000000000001<- http.server "OrdersController#show" a000000000000001/OrdersController#show 120000us self 35000us "ok" x1`,
	`123 a000000000000002<-a000000000000001 db.sql.active_record "SELECT \"orders\".* FROM \"orders\" WHERE \"orders\".\"id\" = 42 LIMIT 1" a000000000000001/OrdersController#show 10000us self 10000us "" x1`,
	`123 a000000000000010<-a000000000000001 db.sql.active_record "SELECT \"line_items\".* FROM \"line_items\" WHERE \"line_items\".\"id\" = 1" a000000000000001/OrdersController#show 5000us self 5000us "" x3`,
	`123 a000000000000011<-a000000000000001 db.sql.active_record "SELECT \"line_items\".* FROM \"line_items\" WHERE \"line_items\".\"id\" = 2" a000000000000001/OrdersController#show 5000us self 5000us "" x3`,
	`123 a000000000000012<-a000000000000001 db.sql.active_record "SELECT \"line_items\".* FROM \"line_items\" WHERE \"line_items\".\"id\" = 3" a000000000000001/OrdersController#show 5000us self 5000us "" x3`,
	`123 a000000000000003<-a000000000000001 template.render "orders/show.html.erb" a000000000000001/OrdersController#show 60000us self 30000us "" x1`,
	`123 a000000000000004<-a000000000000003 http.client "GET https://rates.example.com/rates" a000000000000001/OrdersController#show 30000us self 30000us "ok" x1`,
}

// goRows are the service's Spans in Project 124, which answered the Rails
// request's HTTP call: its segment's self time is its 26 ms less 4 and 15.
var goRows = []string{
	`124 c000000000000001<-a000000000000004 http.server "GET /rates" c000000000000001/GET /rates 26000us self 7000us "ok" x1`,
	`124 c000000000000002<-c000000000000001 cache.get "rates:EUR" c000000000000001/GET /rates 4000us self 4000us "ok" x1`,
	`124 c000000000000003<-c000000000000001 db.sql.query "SELECT rate FROM rates WHERE currency = 'EUR' AND day = '2026-10-02'" c000000000000001/GET /rates 15000us self 15000us "ok" x1`,
}

func TestTransactionsFromEachSDKAreAnsweredAsATraceAcrossItsProjects(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 124, "go_public_key")

	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-5.28-rails"))
	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-6.3-rails"))
	postEnvelopeWithKey(t, env, "go_public_key", transactionFixture(t, "sentry-go-0.47"))
	env.ProcessSpans(t)

	for _, traceID := range []string{ruby528Trace, ruby63Trace} {
		got := readTrace(t, env, traceID, 123)
		if got.TraceID != traceID {
			t.Errorf("Expected the trace id %s, got %s", traceID, got.TraceID)
		}
		if !slices.Equal(rows(got.Spans), railsRows) {
			t.Errorf("Expected trace %s's Spans in Project 123\n  %s\ngot\n  %s", traceID,
				strings.Join(railsRows, "\n  "), strings.Join(rows(got.Spans), "\n  "))
		}
		for _, s := range got.Spans {
			if s.SampleRate != 0.25 || s.Environment != "production" || s.Release != "api@1.4.0" || s.ServerName != "web-1" {
				t.Errorf("Expected %s from the Rails request at 0.25 in production, api@1.4.0 on web-1, got %+v", s.SpanID, s)
			}
		}
		groups := map[string]string{}
		for _, s := range got.Spans {
			groups[s.SpanID] = s.Group
		}
		if g := groups["a000000000000010"]; g == "" || g != groups["a000000000000011"] || g != groups["a000000000000012"] || g == groups["a000000000000002"] {
			t.Errorf("Expected the line items' queries in one group apart from the order's, got %v", groups)
		}
		segment, call := got.Spans[0], got.Spans[6]
		if segment.Attributes["customer"] != "acme" || segment.Attributes["http.response.status_code"] != "200" {
			t.Errorf("Expected the segment's tags and data as Attributes, got %v", segment.Attributes)
		}
		if call.Attributes["http.response.status_code"] != "200" {
			t.Errorf("Expected the HTTP call's data as Attributes, got %v", call.Attributes)
		}
		if !segment.Start.Equal(time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)) {
			t.Errorf("Expected the segment to start at 11:00, got %v", segment.Start)
		}
	}

	// The 6.3 trace crosses into Project 124 where the HTTP call went.
	both := readTrace(t, env, ruby63Trace, 123, 124)
	if want := append(slices.Clone(railsRows), goRows...); !slices.Equal(rows(both.Spans), want) {
		t.Errorf("Expected the trace across Projects 123 and 124\n  %s\ngot\n  %s",
			strings.Join(want, "\n  "), strings.Join(rows(both.Spans), "\n  "))
	}
	for _, s := range both.Spans[len(railsRows):] {
		if s.SampleRate != 0.05 || s.Release != "go-api@2.0.0" || s.ServerName != "go-1" {
			t.Errorf("Expected %s from the service at 0.05, go-api@2.0.0 on go-1, got %+v", s.SpanID, s)
		}
	}
	if got := both.Spans[len(railsRows)].Attributes["currency"]; got != "EUR" {
		t.Errorf("Expected the service's segment tagged currency EUR, got %q", got)
	}

	// Only the given Projects' Spans are answered.
	if got := readTrace(t, env, ruby63Trace, 124); !slices.Equal(rows(got.Spans), goRows) {
		t.Errorf("Expected only Project 124's Spans, got\n  %s", strings.Join(rows(got.Spans), "\n  "))
	}
	if got := readTrace(t, env, ruby63Trace, 456); len(got.Spans) != 0 {
		t.Errorf("Expected no Spans of another Project's trace, got %v", rows(got.Spans))
	}
}

func TestATransactionNamedByItsRawURLIsUnparameterisedAndARetryStoresItOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// ClickHouse stores the first insert but its answer is lost: Span
	// processing inserts the same range again.
	env.FlakySpans.failAfterInsert.Store(1)
	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-5.28-url"))
	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-6.3-url"))
	env.ProcessSpans(t)
	if got := env.FlakySpans.attempts.Load(); got < 2 {
		t.Fatalf("Expected the insert tried again after its answer was lost, got %d attempts", got)
	}

	for _, traceID := range []string{"5c1f0e4a9b3d4e6f8a7b6c5d4e3f2a11", ruby63URL} {
		got := readTrace(t, env, traceID, 123).Spans
		want := []string{`123 b000000000000001<- http.server "/orders/42" b000000000000001/<unparameterised> 10000us self 10000us "internal_error" x1`}
		if !slices.Equal(rows(got), want) {
			t.Errorf("Expected trace %s stored once as Unparameterised, %v, got %v", traceID, want, rows(got))
			continue
		}
		if raw := got[0].Attributes["transaction.raw"]; raw != "/orders/42" {
			t.Errorf("Expected the raw name kept as an Attribute, got %q", raw)
		}
	}
}

func TestATransactionMakesNoIssueWhileItsEnvelopesEventAndLogsAreStored(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-6.3-rails"))
	env.Process(t)
	var issues int
	sqliteQuery(t, env, &issues, "SELECT count(*) FROM issues WHERE project_id = 123")
	if issues != 0 {
		t.Fatalf("Expected a Transaction to make no Issue, got %d", issues)
	}

	// The 5.28 Transaction's envelope, with an event item and a log item.
	envelope := strings.TrimRight(transactionFixture(t, "sentry-ruby-5.28-rails"), "\n") +
		sentryItem(`{"type":"event"}`, `{"event_id":"9b7d475493954601ab7f3b599b2578cb","message":"Payment declined","environment":"production"}`) +
		sentryItem(logItemHeader(1), `{"items":[{"timestamp":1790938800.05,"trace_id":"`+ruby528Trace+`","level":"warn","body":"Slow rates"}]}`)
	postEnvelopeWithKey(t, env, "test_public_key", envelope)
	env.Process(t)
	env.ProcessLogs(t)
	env.ProcessSpans(t)

	sqliteQuery(t, env, &issues, "SELECT count(*) FROM issues WHERE project_id = 123")
	if issues != 1 {
		t.Errorf("Expected the envelope's Event to make one Issue, got %d", issues)
	}
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"Slow rates"}) {
		t.Errorf("Expected the envelope's Log stored, got %v", got)
	}
	for _, traceID := range []string{ruby528Trace, ruby63Trace} {
		if got := readTrace(t, env, traceID, 123).Spans; len(got) != len(railsRows) {
			t.Errorf("Expected trace %s's %d Spans stored, got %v", traceID, len(railsRows), rows(got))
		}
	}
}

func TestSpansPastTraceRetentionAndADeletedProjectsSpansAreDropped(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	// Project 123 keeps its Spans a day, 125 the default 14 days, and 124
	// is deleted.
	seedProject(t, env.SQLitePath, 123, 0)
	consoleExec(t, env, "UPDATE projects SET trace_retention_days = 1 WHERE id = 123")
	seedProject(t, env.SQLitePath, 124, 0)
	seedProject(t, env.SQLitePath, 125, 0)
	seedProjectKey(t, env.SQLitePath, 124, "go_public_key")
	seedProjectKey(t, env.SQLitePath, 125, "kept_public_key")
	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-6.3-rails"))
	postEnvelopeWithKey(t, env, "go_public_key", transactionFixture(t, "sentry-go-0.47"))
	postEnvelopeWithKey(t, env, "kept_public_key", transactionFixture(t, "sentry-ruby-5.28-rails"))
	postAndProcess(t, env, logJSON(t, "kept for 30 days", testNow, nil))
	env.ProcessSpans(t)
	consoleExec(t, env, "DELETE FROM projects WHERE id = 124")

	// Past midnight on 4 October, a day back is wholly past 2 October.
	env.Clock.Set(time.Date(2026, 10, 4, 0, 30, 0, 0, time.UTC))
	env.DeleteLogsPastRetention(t)

	if got := readTrace(t, env, ruby63Trace, 123, 124).Spans; len(got) != 0 {
		t.Errorf("Expected Project 123's Spans past a day and Project 124's dropped, got %v", rows(got))
	}
	if got := readTrace(t, env, ruby528Trace, 125).Spans; len(got) != len(railsRows) {
		t.Errorf("Expected Project 125's Spans kept for 14 days, got %v", rows(got))
	}
	if got := messages(latestLogs(t, env, 123, 10)); !slices.Equal(got, []string{"kept for 30 days"}) {
		t.Errorf("Expected Project 123's Logs kept for their own Retention, got %v", got)
	}
}
