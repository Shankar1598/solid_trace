package main_test

import (
	"fmt"
	"slices"
	"testing"
	"time"
)

// A trace's Logs and Events share its trace id: a Log's trace_id, and an
// Event's contexts.trace.trace_id.

// unsampledTrace is a request whose SDK sampled no Transaction: only its
// Logs and its Event carry the id.
const unsampledTrace = "7e3f0e4a9b3d4e6f8a7b6c5d4e3f2c10"

// tracedLog is a Log of a trace at a time.
func tracedLog(t *testing.T, message, traceID string, at time.Time) string {
	t.Helper()
	return logJSON(t, message, at, map[string]any{"trace_id": traceID})
}

// tracedEvent is an Event of a trace at a time, its message its Issue.
func tracedEvent(message, traceID string, at time.Time) string {
	return fmt.Sprintf(`{"message":%q,"fingerprint":[%q],"timestamp":%q,"contexts":{"trace":{"trace_id":%q,"span_id":"a000000000000003"}}}`,
		message, message, at.Format(time.RFC3339Nano), traceID)
}

func logRows(logs []traceLog) []string {
	rows := make([]string, len(logs))
	for i, l := range logs {
		rows[i] = fmt.Sprintf("%d %s %s", l.ProjectID, l.Time.Format("15:04:05.000"), l.Message)
	}
	return rows
}

// issueRows are each Issue's Project and its Events' times, its fingerprint
// left out as the test can't know it.
func issueRows(issues []traceIssue) []string {
	rows := make([]string, len(issues))
	for i, issue := range issues {
		row := fmt.Sprint(issue.ProjectID)
		for _, e := range issue.Events {
			row += " " + e.Timestamp.Format("15:04:05.000")
		}
		rows[i] = row
	}
	return rows
}

func TestASampledTraceAnswersItsLogsAndEventsByIssueAcrossItsProjects(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	seedProjectKey(t, env.SQLitePath, 124, "go_public_key")
	seedProjectKey(t, env.SQLitePath, 125, "other_public_key")

	// The 6.3 Rails request, 11:00:00 to 11:00:00.120, called Project 124.
	postEnvelopeWithKey(t, env, "test_public_key", transactionFixture(t, "sentry-ruby-6.3-rails"))
	postEnvelopeWithKey(t, env, "go_public_key", transactionFixture(t, "sentry-go-0.47"))
	env.ProcessSpans(t)

	at := func(minute, second, ms int) time.Time {
		return time.Date(2026, 10, 2, 11, minute, second, ms*int(time.Millisecond), time.UTC)
	}
	postAndProcessTo(t, env, 123, "test_public_key",
		tracedLog(t, "Rendering order", ruby63Trace, at(0, 0, 50)),
		tracedLog(t, "Sent receipt", ruby63Trace, at(4, 0, 0)),
		tracedLog(t, "Much later", ruby63Trace, at(20, 0, 0)),
		tracedLog(t, "Another request", ruby528Trace, at(0, 0, 70)))
	postAndProcessTo(t, env, 124, "go_public_key", tracedLog(t, "Fetching rates", ruby63Trace, at(0, 0, 60)))
	postAndProcessTo(t, env, 125, "other_public_key", tracedLog(t, "Not asked for", ruby63Trace, at(0, 0, 80)))

	postEventTo(t, env, 123, "test_public_key", tracedEvent("Payment declined", ruby63Trace, at(0, 0, 100)))
	postEventTo(t, env, 123, "test_public_key", tracedEvent("Payment declined", ruby63Trace, at(0, 0, 110)))
	postEventTo(t, env, 124, "go_public_key", tracedEvent("Rates stale", ruby63Trace, at(0, 0, 70)))
	postEventTo(t, env, 123, "test_public_key", tracedEvent("Payment declined", ruby528Trace, at(0, 0, 90)))
	postEventTo(t, env, 125, "other_public_key", tracedEvent("Not asked for", ruby63Trace, at(0, 0, 90)))
	env.Process(t)

	got := readTrace(t, env, ruby63Trace, 123, 124)

	if len(got.Spans) != len(railsRows)+len(goRows) {
		t.Errorf("Expected the trace's %d Spans, got %v", len(railsRows)+len(goRows), rows(got.Spans))
	}
	// Its Projects' Logs of it within 5 minutes of its Spans, in time order.
	wantLogs := []string{"123 11:00:00.050 Rendering order", "124 11:00:00.060 Fetching rates", "123 11:04:00.000 Sent receipt"}
	if !slices.Equal(logRows(got.Logs), wantLogs) {
		t.Errorf("Expected the trace's Logs %v, got %v", wantLogs, logRows(got.Logs))
	}
	if got.MoreLogs {
		t.Error("Expected no more Logs than answered")
	}
	from, to := at(-5, 0, 0), at(5, 0, 120)
	if got.LogsFrom == nil || got.LogsTo == nil || !got.LogsFrom.Equal(from) || !got.LogsTo.Equal(to) {
		t.Errorf("Expected Logs read from %v to %v, got %v to %v", from, to, got.LogsFrom, got.LogsTo)
	}
	for _, l := range got.Logs {
		if l.ID == "" || l.TraceID != ruby63Trace {
			t.Errorf("Expected each Log with its id and trace id, got %+v", l)
		}
	}
	// Its Events, by Issue, the Issue with the earliest Event first.
	wantIssues := []string{"124 11:00:00.070", "123 11:00:00.100 11:00:00.110"}
	if !slices.Equal(issueRows(got.Issues), wantIssues) {
		t.Errorf("Expected the trace's Events by Issue %v, got %v", wantIssues, issueRows(got.Issues))
	}
	if len(got.Issues) == 2 {
		var payment int64
		sqliteQuery(t, env, &payment, `SELECT f.id FROM issue_fingerprints f JOIN issues i ON i.id = f.issue_id
			WHERE i.project_id = 123 AND i.title LIKE '%Payment declined%'`)
		if got.Issues[1].IssueFingerprintID != payment || got.Issues[1].Events[0].UUID == "" {
			t.Errorf("Expected Project 123's Events under the Issue's fingerprint %d with their ids, got %+v", payment, got.Issues[1])
		}
	}
}

func TestAnUnsampledTraceAnswersItsLogsAndEventsAroundItsHintOrItsEvent(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	at := func(minute, second int) time.Time { return time.Date(2026, 10, 2, 11, minute, second, 0, time.UTC) }
	postAndProcessTo(t, env, 123, "test_public_key",
		tracedLog(t, "Charging card", unsampledTrace, at(30, 0)),
		tracedLog(t, "Card declined", unsampledTrace, at(30, 2)),
		tracedLog(t, "An hour before", unsampledTrace, at(-30, 0)))
	postEventTo(t, env, 123, "test_public_key", tracedEvent("Payment declined", unsampledTrace, at(30, 1)))
	env.Process(t)

	want := []string{"123 11:30:00.000 Charging card", "123 11:30:02.000 Card declined"}

	// Around a time hint, such as a Log's.
	hint := at(30, 2)
	got := readTraceAround(t, env, unsampledTrace, &hint, 123)
	if len(got.Spans) != 0 {
		t.Errorf("Expected no Spans of an unsampled trace, got %v", rows(got.Spans))
	}
	if !slices.Equal(logRows(got.Logs), want) {
		t.Errorf("Expected the Logs around the hint %v, got %v", want, logRows(got.Logs))
	}
	if !slices.Equal(issueRows(got.Issues), []string{"123 11:30:01.000"}) {
		t.Errorf("Expected the trace's Event, got %v", issueRows(got.Issues))
	}
	if from := at(25, 2); got.LogsFrom == nil || !got.LogsFrom.Equal(from) {
		t.Errorf("Expected Logs read from 5 minutes before the hint, %v, got %v", from, got.LogsFrom)
	}

	// Without a hint, around its Event, as when its id is pasted.
	got = readTrace(t, env, unsampledTrace, 123)
	if !slices.Equal(logRows(got.Logs), want) {
		t.Errorf("Expected the Logs around its Event %v, got %v", want, logRows(got.Logs))
	}
	if !slices.Equal(issueRows(got.Issues), []string{"123 11:30:01.000"}) {
		t.Errorf("Expected the trace's Event, got %v", issueRows(got.Issues))
	}

	// A trace of nothing answers nothing, and reads no Logs.
	got = readTrace(t, env, "0000000000000000000000000000abcd", 123)
	if len(got.Spans)+len(got.Logs)+len(got.Issues) != 0 || got.LogsFrom != nil {
		t.Errorf("Expected nothing of an unknown trace, got %+v", got)
	}
}
