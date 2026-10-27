package main_test

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// sentTransaction is a captured Transaction sent again as another request:
// under its own trace id, at another time, its times stretched by scale, at
// another sample rate, release or environment, with another status or none,
// and without some of its Spans.
type sentTransaction struct {
	fixture string
	traceID string
	at      time.Time
	scale   float64
	// rate is the envelope's sample rate; "" keeps the fixture's, "none"
	// sends none.
	rate        string
	release     string
	environment string
	// status is the segment's; "" keeps the fixture's, "none" sends none.
	status string
	drop   []string // span ids left out
}

// envelope is the fixture's envelope, changed as the Transaction says.
func (s sentTransaction) envelope(t *testing.T) string {
	t.Helper()
	lines := strings.SplitN(transactionFixture(t, s.fixture), "\n", 3)
	var header, payload map[string]any
	decode := func(raw string, into *map[string]any) {
		d := json.NewDecoder(strings.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(into); err != nil {
			t.Fatalf("Decode %s: %v", s.fixture, err)
		}
	}
	decode(lines[0], &header)
	decode(lines[2], &payload)

	dsc := header["trace"].(map[string]any)
	dsc["trace_id"] = s.traceID
	switch s.rate {
	case "":
	case "none":
		delete(dsc, "sample_rate")
	default:
		dsc["sample_rate"] = s.rate
	}

	scale := s.scale
	if scale == 0 {
		scale = 1
	}
	start := seconds(t, payload["start_timestamp"])
	at := float64(s.at.UnixMicro()) / 1e6
	shift := func(v any) float64 { return at + (seconds(t, v)-start)*scale }
	payload["start_timestamp"], payload["timestamp"] = shift(payload["start_timestamp"]), shift(payload["timestamp"])
	if s.release != "" {
		payload["release"] = s.release
	}
	if s.environment != "" {
		payload["environment"] = s.environment
	}
	trace := payload["contexts"].(map[string]any)["trace"].(map[string]any)
	trace["trace_id"] = s.traceID
	switch s.status {
	case "":
	case "none":
		delete(trace, "status")
	default:
		trace["status"] = s.status
	}
	var spans []any
	for _, raw := range payload["spans"].([]any) {
		span := raw.(map[string]any)
		if slices.Contains(s.drop, span["span_id"].(string)) {
			continue
		}
		span["trace_id"] = s.traceID
		span["start_timestamp"], span["timestamp"] = shift(span["start_timestamp"]), shift(span["timestamp"])
		spans = append(spans, span)
	}
	payload["spans"] = spans

	h, _ := json.Marshal(header)
	p, _ := json.Marshal(payload)
	return string(h) + "\n" + `{"type":"transaction","content_type":"application/json"}` + "\n" + string(p) + "\n"
}

func seconds(t *testing.T, v any) float64 {
	t.Helper()
	f, err := strconv.ParseFloat(fmt.Sprint(v), 64)
	if err != nil {
		t.Fatalf("Expected seconds, got %v", v)
	}
	return f
}

func traceIDOf(n int) string { return fmt.Sprintf("7e3f0e4a9b3d4e6f8a7b6c5d4e3f%04x", n) }

// The Transactions the Performance tests send to Project 123, around
// testNow, 12:00. The range read is 11:00 to 12:00, so its previous period is
// 10:00 to 11:00. The Rails request takes 120 ms as captured.
var performanceTransactions = []sentTransaction{
	// In the range, in production: two at 25%, each standing for 4
	// requests, and one at 5%, standing for 20.
	{fixture: "sentry-ruby-6.3-rails", traceID: traceIDOf(1), at: at(11, 10)},
	{fixture: "sentry-ruby-6.3-rails", traceID: traceIDOf(2), at: at(11, 20), scale: 2, status: "internal_error",
		drop: []string{"a000000000000012"}},
	// sentry-ruby sends no status when none was set: it isn't an error.
	{fixture: "sentry-ruby-5.28-rails", traceID: traceIDOf(3), at: at(11, 30), scale: 0.5, rate: "0.05",
		release: "api@1.5.0", status: "none", drop: []string{"a000000000000012"}},
	{fixture: "sentry-ruby-6.3-url", traceID: traceIDOf(4), at: at(11, 40)},
	// In staging, with no sample rate: it stands for itself.
	{fixture: "sentry-ruby-6.3-rails", traceID: traceIDOf(7), at: at(11, 50), environment: "staging", rate: "none"},
	// In the previous period, and before it.
	{fixture: "sentry-ruby-6.3-rails", traceID: traceIDOf(5), at: at(10, 30)},
	{fixture: "sentry-ruby-6.3-rails", traceID: traceIDOf(6), at: at(9, 30)},
}

func at(hour, minute int) time.Time { return time.Date(2026, 10, 2, hour, minute, 0, 0, time.UTC) }

func sendPerformanceTransactions(t *testing.T, env *TestEnv) {
	t.Helper()
	for _, s := range performanceTransactions {
		postEnvelopeWithKey(t, env, "test_public_key", s.envelope(t))
	}
	env.ProcessSpans(t)
}

type transactionStats struct {
	Sampled        int     `json:"sampled"`
	Throughput     float64 `json:"throughput"`
	P50Us          int64   `json:"p50_us"`
	P95Us          int64   `json:"p95_us"`
	P99Us          int64   `json:"p99_us"`
	ErrorRate      float64 `json:"error_rate"`
	TimeConsumedUs float64 `json:"time_consumed_us"`
}

func (s transactionStats) String() string {
	return fmt.Sprintf("%d sampled, %.6f/s, p50 %dus p95 %dus p99 %dus, errors %.4f, consumed %.0fus",
		s.Sampled, s.Throughput, s.P50Us, s.P95Us, s.P99Us, s.ErrorRate, s.TimeConsumedUs)
}

type transactionRow struct {
	Name string `json:"name"`
	transactionStats
	Previous *transactionStats `json:"previous"`
}

func (r transactionRow) row() string {
	previous := "none before"
	if r.Previous != nil {
		previous = "before: " + r.Previous.String()
	}
	return fmt.Sprintf("%s: %s; %s", r.Name, r.transactionStats, previous)
}

type transactionsAnswer struct {
	Environments []string         `json:"environments"`
	Transactions []transactionRow `json:"transactions"`
}

func readTransactions(t *testing.T, env *TestEnv, projectID uint32, from, to time.Time, environment string) transactionsAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/transactions", projectID),
		map[string]any{"from": from, "to": to, "environment": environment})
	if status != 200 {
		t.Fatalf("Transactions expected 200, got %d: %s", status, raw)
	}
	var answer transactionsAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode transactions %q: %v", raw, err)
	}
	return answer
}

func TestTransactionsAreWeightedBySampleRateAndComparedWithThePreviousPeriod(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	sendPerformanceTransactions(t, env)

	got := readTransactions(t, env, 123, at(11, 0), at(12, 0), "production")
	// OrdersController#show stands for 4 + 4 + 20 = 28 requests in the
	// hour, of which the 4 that failed are errors. They took 120, 240 and
	// 60 ms, so it consumed 4×120 + 4×240 + 20×60 = 2,640 ms. Its
	// percentiles are of the three sampled, unweighted. The Rack request
	// stands for 4 that failed in 10 ms.
	want := []string{
		"OrdersController#show: 3 sampled, 0.007778/s, p50 120000us p95 228000us p99 237600us, errors 0.1429, consumed 2640000us; " +
			"before: 1 sampled, 0.001111/s, p50 120000us p95 120000us p99 120000us, errors 0.0000, consumed 480000us",
		"<unparameterised>: 1 sampled, 0.001111/s, p50 10000us p95 10000us p99 10000us, errors 1.0000, consumed 40000us; none before",
	}
	var rows []string
	for _, r := range got.Transactions {
		rows = append(rows, r.row())
	}
	if !slices.Equal(rows, want) {
		t.Errorf("Expected production's Transactions by time consumed\n  %s\ngot\n  %s",
			strings.Join(want, "\n  "), strings.Join(rows, "\n  "))
	}
	if !slices.Equal(got.Environments, []string{"production", "staging"}) {
		t.Errorf("Expected the range's environments, got %v", got.Environments)
	}

	// Every environment's: staging's request stands for itself.
	all := readTransactions(t, env, 123, at(11, 0), at(12, 0), "")
	if len(all.Transactions) != 2 || all.Transactions[0].Sampled != 4 ||
		math.Abs(all.Transactions[0].Throughput-29.0/3600) > 1e-9 {
		t.Errorf("Expected OrdersController#show's 4 sampled standing for 29 requests in every environment, got %v", all.Transactions)
	}
	if got := readTransactions(t, env, 456, at(11, 0), at(12, 0), ""); len(got.Transactions) != 0 {
		t.Errorf("Expected no Transactions of another Project, got %v", got.Transactions)
	}
}

type transactionAnswer struct {
	Name string `json:"name"`
	transactionStats
	BucketSeconds int `json:"bucket_seconds"`
	Releases      []struct {
		Release string `json:"release"`
		Sampled int    `json:"sampled"`
		P50Us   int64  `json:"p50_us"`
		P95Us   int64  `json:"p95_us"`
		Points  []struct {
			Time    time.Time `json:"time"`
			Sampled int       `json:"sampled"`
			P50Us   int64     `json:"p50_us"`
			P95Us   int64     `json:"p95_us"`
		} `json:"points"`
	} `json:"releases"`
	Ops []struct {
		Op            string  `json:"op"`
		Share         float64 `json:"share"`
		AvgSelfTimeUs float64 `json:"avg_self_time_us"`
	} `json:"ops"`
	Groups []struct {
		Group         string  `json:"group"`
		Op            string  `json:"op"`
		Description   string  `json:"description"`
		Normalised    string  `json:"normalised"`
		Share         float64 `json:"share"`
		AvgSelfTimeUs float64 `json:"avg_self_time_us"`
	} `json:"groups"`
	Repeats []struct {
		Op         string  `json:"op"`
		Normalised string  `json:"normalised"`
		Median     float64 `json:"median"`
		Max        int     `json:"max"`
		Sampled    int     `json:"sampled"`
	} `json:"repeats"`
	Slowest []struct {
		TraceID     string    `json:"trace_id"`
		Start       time.Time `json:"start"`
		DurationUs  int64     `json:"duration_us"`
		Status      string    `json:"status"`
		Release     string    `json:"release"`
		Environment string    `json:"environment"`
	} `json:"slowest"`
}

func readTransaction(t *testing.T, env *TestEnv, projectID uint32, name string, from, to time.Time, environment string) transactionAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/transaction", projectID),
		map[string]any{"name": name, "from": from, "to": to, "environment": environment})
	if status != 200 {
		t.Fatalf("Transaction expected 200, got %d: %s", status, raw)
	}
	var answer transactionAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode transaction %q: %v", raw, err)
	}
	return answer
}

func TestATransactionsTimeIsBrokenDownByOpGroupAndReleaseWithItsRepeatsAndSlowestTraces(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	sendPerformanceTransactions(t, env)

	got := readTransaction(t, env, 123, "OrdersController#show", at(11, 0), at(12, 0), "production")
	if got.Name != "OrdersController#show" || got.String() != (transactionStats{
		Sampled: 3, Throughput: 28.0 / 3600, P50Us: 120000, P95Us: 228000, P99Us: 237600, ErrorRate: 1.0 / 7, TimeConsumedUs: 2640000,
	}).String() {
		t.Errorf("Expected the Transaction's numbers as Performance lists them, got %s: %s", got.Name, got.transactionStats)
	}

	// Each request's self time by op, in ms, as captured: the segment's 35,
	// the queries' 25, the view's 30 and the HTTP call's 30. The two
	// requests without the third line item spent its 5 ms in the segment,
	// and took twice and half as long. Weighted 4, 4 and 20, the segment
	// spent 4×35 + 4×80 + 20×20 = 860 ms of the 2,640, the queries
	// 4×25 + 4×40 + 20×10 = 460, the view and the call 4×30 + 4×60 + 20×15
	// = 660 each. Each is over 28 requests on average.
	var ops []string
	for _, o := range got.Ops {
		ops = append(ops, fmt.Sprintf("%s %.4f %.0fus", o.Op, o.Share, o.AvgSelfTimeUs))
	}
	wantOps := []string{"http.server 0.3258 30714us", "http.client 0.2500 23571us", "template.render 0.2500 23571us",
		"db.sql.active_record 0.1742 16429us"}
	if !slices.Equal(ops, wantOps) {
		t.Errorf("Expected self time by op\n  %s\ngot\n  %s", strings.Join(wantOps, "\n  "), strings.Join(ops, "\n  "))
	}

	// By group, the queries apart: the line items' 4×15 + 4×20 + 20×5 = 240
	// and the order's 4×10 + 4×20 + 20×5 = 220.
	var groups []string
	for _, g := range got.Groups {
		groups = append(groups, fmt.Sprintf("%s %s %.4f %.0fus", g.Op, g.Normalised, g.Share, g.AvgSelfTimeUs))
		if g.Group == "" || g.Description == "" {
			t.Errorf("Expected each group's hash and a description of it, got %+v", g)
		}
	}
	wantGroups := []string{
		"http.server OrdersController#show 0.3258 30714us",
		"http.client GET https://rates.example.com/rates 0.2500 23571us",
		"template.render orders/show.html.erb 0.2500 23571us",
		`db.sql.active_record SELECT "line_items".* FROM "line_items" WHERE "line_items"."id" = ? 0.0909 8571us`,
		`db.sql.active_record SELECT "orders".* FROM "orders" WHERE "orders"."id" = ? LIMIT ? 0.0833 7857us`,
	}
	if !slices.Equal(groups, wantGroups) {
		t.Errorf("Expected self time by group\n  %s\ngot\n  %s", strings.Join(wantGroups, "\n  "), strings.Join(groups, "\n  "))
	}

	// The line items ran 3, 2 and 2 times.
	var repeats []string
	for _, r := range got.Repeats {
		repeats = append(repeats, fmt.Sprintf("%s %s median %g max %d in %d", r.Op, r.Normalised, r.Median, r.Max, r.Sampled))
	}
	wantRepeats := []string{`db.sql.active_record SELECT "line_items".* FROM "line_items" WHERE "line_items"."id" = ? median 2 max 3 in 3`}
	if !slices.Equal(repeats, wantRepeats) {
		t.Errorf("Expected the repeated line items, %v, got %v", wantRepeats, repeats)
	}

	// p50 and p95 by release, over the range and a minute at a time.
	var releases []string
	for _, r := range got.Releases {
		line := fmt.Sprintf("%s %d p50 %d p95 %d:", r.Release, r.Sampled, r.P50Us, r.P95Us)
		for _, p := range r.Points {
			line += fmt.Sprintf(" %s %d %d/%d", p.Time.Format("15:04"), p.Sampled, p.P50Us, p.P95Us)
		}
		releases = append(releases, line)
	}
	wantReleases := []string{
		"api@1.4.0 2 p50 180000 p95 234000: 11:10 1 120000/120000 11:20 1 240000/240000",
		"api@1.5.0 1 p50 60000 p95 60000: 11:30 1 60000/60000",
	}
	if got.BucketSeconds != 60 || !slices.Equal(releases, wantReleases) {
		t.Errorf("Expected releases a minute at a time\n  %s\ngot every %ds\n  %s", strings.Join(wantReleases, "\n  "),
			got.BucketSeconds, strings.Join(releases, "\n  "))
	}

	var slowest []string
	for _, s := range got.Slowest {
		slowest = append(slowest, fmt.Sprintf("%s %s %d %q %s %s", s.TraceID, s.Start.Format("15:04"), s.DurationUs,
			s.Status, s.Release, s.Environment))
	}
	wantSlowest := []string{
		traceIDOf(2) + ` 11:20 240000 "internal_error" api@1.4.0 production`,
		traceIDOf(1) + ` 11:10 120000 "ok" api@1.4.0 production`,
		traceIDOf(3) + ` 11:30 60000 "" api@1.5.0 production`,
	}
	if !slices.Equal(slowest, wantSlowest) {
		t.Errorf("Expected the slowest traces\n  %s\ngot\n  %s", strings.Join(wantSlowest, "\n  "), strings.Join(slowest, "\n  "))
	}

	// Another name, environment or Project has none of these.
	if got := readTransaction(t, env, 123, "OrdersController#show", at(11, 0), at(12, 0), "staging"); got.Sampled != 1 || len(got.Repeats) != 1 {
		t.Errorf("Expected staging's one request, got %s", got.transactionStats)
	}
	if got := readTransaction(t, env, 456, "OrdersController#show", at(11, 0), at(12, 0), ""); got.Sampled != 0 ||
		len(got.Ops)+len(got.Groups)+len(got.Releases)+len(got.Slowest) != 0 {
		t.Errorf("Expected nothing of another Project, got %+v", got)
	}
}
