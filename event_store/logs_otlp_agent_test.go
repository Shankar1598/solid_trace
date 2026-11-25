package main_test

import (
	"io"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

// The Agent Project's key stores only the OTLP records of a Captured
// repository, by each record's own vcs.repository.url.full. The others are
// counted as filtered, and the sender gets no rejection for them.

const agentKey = "agent_key"

// agentKeyProject is an Organization's Agent Project with the key agentKey,
// and entries as its Captured repositories.
func agentKeyProject(t *testing.T, env *TestEnv, entries ...string) uint32 {
	t.Helper()
	rows := newConsoleRows(t, env)
	project := rows.agentProject(rows.organization("Acme"), entries...)
	if len(entries) == 0 {
		rows.exec("UPDATE projects SET captured_repositories = '[]' WHERE id = ?", project)
	}
	seedProjectKey(t, env.SQLitePath, uint32(project), agentKey)
	return uint32(project)
}

// repositoryRecord is one OTLP record of a request, under a resource of its
// own: its own vcs.repository.url.full and its resource's, "" for none.
type repositoryRecord struct{ name, own, resource string }

// claudeCodeRecords are the records of one export from Claude Code: one of
// the Captured repository, one of another repository, one sent before
// Claude Code reads the remote, and one with the repository only on the
// resource.
func claudeCodeRecords(prefix string) []repositoryRecord {
	return []repositoryRecord{
		{prefix + "captured", "https://github.com/acme/shop", ""},
		{prefix + "another repository", "https://github.com/example-owner/shop", ""},
		{prefix + "managed_settings_resolved", "", ""},
		{prefix + "repository on the resource", "", "https://github.com/acme/shop"},
	}
}

func putRepositoryURL(attributes pcommon.Map, url string) {
	if url != "" {
		attributes.PutStr("vcs.repository.url.full", url)
	}
}

func otlpLogRecords(t *testing.T, records []repositoryRecord) []byte {
	t.Helper()
	logs := plog.NewLogs()
	for _, r := range records {
		resourceLogs := logs.ResourceLogs().AppendEmpty()
		resourceLogs.Resource().Attributes().PutStr("service.name", "claude-code")
		putRepositoryURL(resourceLogs.Resource().Attributes(), r.resource)
		record := resourceLogs.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
		record.Body().SetStr(r.name)
		putRepositoryURL(record.Attributes(), r.own)
	}
	body, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func otlpMetricPoints(t *testing.T, records []repositoryRecord) []byte {
	t.Helper()
	metrics := pmetric.NewMetrics()
	for _, r := range records {
		resourceMetrics := metrics.ResourceMetrics().AppendEmpty()
		resourceMetrics.Resource().Attributes().PutStr("service.name", "claude-code")
		putRepositoryURL(resourceMetrics.Resource().Attributes(), r.resource)
		metric := resourceMetrics.ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
		metric.SetName(r.name)
		point := metric.SetEmptySum().DataPoints().AppendEmpty()
		point.SetDoubleValue(0.01)
		putRepositoryURL(point.Attributes(), r.own)
	}
	body, err := pmetricotlp.NewExportRequestFromMetrics(metrics).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// sendAgentOTLP posts a protobuf body to an OTLP route with the Agent
// Project's key, and expects 200 with no rejected record.
func sendAgentOTLP(t *testing.T, env *TestEnv, path string, body []byte) {
	t.Helper()
	resp := sendOTLP(t, env, path, "application/x-protobuf", body, map[string]string{"Authorization": "Bearer " + agentKey})
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("Expected 200 from %s, got %d: %s", path, resp.StatusCode, raw)
	}
	var rejected int64
	var message string
	if path == "/v1/logs" {
		response := plogotlp.NewExportResponse()
		if err := response.UnmarshalProto(raw); err != nil {
			t.Fatalf("Decode the export response %q: %v", raw, err)
		}
		rejected, message = response.PartialSuccess().RejectedLogRecords(), response.PartialSuccess().ErrorMessage()
	} else {
		response := pmetricotlp.NewExportResponse()
		if err := response.UnmarshalProto(raw); err != nil {
			t.Fatalf("Decode the export response %q: %v", raw, err)
		}
		rejected, message = response.PartialSuccess().RejectedDataPoints(), response.PartialSuccess().ErrorMessage()
	}
	if rejected != 0 || message != "" {
		t.Errorf("Expected no rejected record from %s, got %d: %q", path, rejected, message)
	}
}

// filteredOverview flushes the Ingest counts and answers the Overview of
// project for the clock's day.
func filteredOverview(t *testing.T, env *TestEnv, project uint32) overviewAnswer {
	t.Helper()
	day := env.Clock.Now()
	env.Clock.Advance(time.Second)
	env.FlushIngestCounts(t)
	return overview(t, env, day, day, project)
}

func TestTheAgentProjectsKeyStoresOnlyTheLogsAndMetricsOfACapturedRepository(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	project := agentKeyProject(t, env, "github.com/acme/*")

	sendAgentOTLP(t, env, "/v1/logs", otlpLogRecords(t, claudeCodeRecords("log: ")))
	sendAgentOTLP(t, env, "/v1/metrics", otlpMetricPoints(t, claudeCodeRecords("metric: ")))
	env.ProcessLogs(t)

	stored := latestLogs(t, env, project, 100)
	got := messages(stored)
	slices.Sort(got)
	if want := []string{"log: captured", "metric: captured"}; !slices.Equal(got, want) {
		t.Fatalf("Expected only the records of the Captured repository, %q, got %q", want, got)
	}
	for _, log := range stored {
		if log.Service != "claude-code" || log.Attributes["vcs.repository.url.full"] != "https://github.com/acme/shop" {
			t.Errorf("Expected a Log of claude-code with its repository, got %+v", log)
		}
	}

	answer := filteredOverview(t, env, project)
	if answer.Band.Received.Logs != 8 || answer.Band.Received.Requests != 2 || answer.Band.FilteredLogs != 6 || answer.Band.UnreadableLogs != 0 {
		t.Errorf("Expected 8 Logs received in 2 requests, 6 filtered and none unreadable, got %+v, %d filtered, %d unreadable",
			answer.Band.Received, answer.Band.FilteredLogs, answer.Band.UnreadableLogs)
	}
	if len(answer.Projects) != 1 || answer.Projects[0].FilteredLogs != 6 {
		t.Errorf("Expected the Agent Project's 6 filtered Logs, got %+v", answer.Projects)
	}
}

func TestTheAgentProjectsKeyStoresNothingWithNoCapturedRepository(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	project := agentKeyProject(t, env)

	sendAgentOTLP(t, env, "/v1/logs", otlpLogRecords(t, claudeCodeRecords("")))
	sendAgentOTLP(t, env, "/v1/metrics", otlpMetricPoints(t, claudeCodeRecords("")))
	env.ProcessLogs(t)

	if stored := latestLogs(t, env, project, 100); len(stored) != 0 {
		t.Fatalf("Expected no Log, got %q", messages(stored))
	}
	if answer := filteredOverview(t, env, project); answer.Band.Received.Logs != 8 || answer.Band.FilteredLogs != 8 {
		t.Errorf("Expected 8 Logs received and filtered, got %+v and %d filtered", answer.Band.Received, answer.Band.FilteredLogs)
	}
}

// Run events come in with the Dispatcher's API token, not the key, so the
// filter does not apply to them, though they have no repository URL.
func TestRunEventsAreStoredBesideTheAgentProjectsKeyAndNotFiltered(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newRunEventsOrganization(t, env)
	project := uint32(o.project)
	seedProjectKey(t, env.SQLitePath, project, agentKey)
	at := env.Clock.Now().Add(-time.Minute)
	run := runSnapshot{id: "a1b2c3d4e5f6g7h8", state: "queued", title: "Fix the flaky cart test"}

	answer := sendRunEvents(t, env, o.dispatcherSecret, runEvent(run, 1, at, "created"), runEvent(run, 2, at.Add(time.Second), "queued"),
		map[string]any{"time": at.Format(time.RFC3339Nano), "level": "warn", "message": "No VM is free"})
	expectStatus(t, answer, 200)
	if answer.body["accepted"] != float64(3) {
		t.Fatalf("Expected 3 accepted, got %s", answer.raw)
	}
	sendAgentOTLP(t, env, "/v1/logs", otlpLogRecords(t, claudeCodeRecords("")))
	env.ProcessLogs(t)

	events := runQueryPages(t, env, project, queryRequest{Query: `where file == "runs/a1b2c3d4e5f6g7h8"`, From: allTime.from, To: allTime.to, Limit: 100})
	warnings := runQueryPages(t, env, project, queryRequest{Query: `where file == "dispatcher"`, From: allTime.from, To: allTime.to, Limit: 100})
	if len(events) != 2 || len(warnings) != 1 {
		t.Fatalf("Expected the Run's 2 events and the Dispatcher's warning, got %+v and %+v", events, warnings)
	}
	if answer := filteredOverview(t, env, project); answer.Band.Received.Logs != 7 || answer.Band.FilteredLogs != 3 {
		t.Errorf("Expected 7 Logs received and only the key's 3 filtered, got %+v and %d filtered", answer.Band.Received, answer.Band.FilteredLogs)
	}
}
