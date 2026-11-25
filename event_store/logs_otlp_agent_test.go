package main_test

import (
	"fmt"
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

// A key holder can send a record of a Captured repository with the
// Attributes of a run event, of a Dispatcher's warning or of an Agent
// session's step. EventStore stores each in the file otel, with the file
// sent as original.file. So the Runs list, the Run page, the Dispatcher's
// Logs and the Agent session's page show only what the Dispatcher and the
// upload sent, also for an Agent session with a file named otel.
func TestTheAgentProjectsKeyAddsNoRunNoDispatcherLogAndNoAgentSessionStep(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	o := newRunEventsOrganization(t, env)
	project := uint32(o.project)
	seedProjectKey(t, env.SQLitePath, project, agentKey)

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"))}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-a1b2.jsonl", body: jsonl(personAt("s1", "2026-10-02T11:00:02Z", "Find the cart"))}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "otel", contentHash: "sha256:notes", body: []byte("notes")}), 200)
	run := runSnapshot{id: "a1b2c3d4e5f6g7h8", state: "queued", title: "Fix the flaky cart test"}
	at := env.Clock.Now().Add(-time.Minute)
	expectStatus(t, sendRunEvents(t, env, o.dispatcherSecret, runEvent(run, 1, at, "created"),
		map[string]any{"time": at.Format(time.RFC3339Nano), "level": "warn", "message": "No VM is free"}), 200)

	logs := plog.NewLogs()
	records := logs.ResourceLogs().AppendEmpty().ScopeLogs().AppendEmpty().LogRecords()
	for message, attributes := range map[string]map[string]any{
		"a forged run":     {"file": "runs/x1", "run.id": "x1", "run.seq": 99, "run.state": "running", "event.kind": "created"},
		"a forged warning": {"file": "dispatcher"},
		"a forged step": {"file": "session.jsonl", "service.name": "github.com/acme/shop", "agent.session_id": laptopSession,
			"agent.raw.version": 1, "agent.role": "person", "agent.subagent_id": "a1b2", "agent.tool_use_id": "toolu_forged"},
	} {
		record := records.AppendEmpty()
		record.Body().SetStr(message)
		if err := record.Attributes().FromRaw(attributes); err != nil {
			t.Fatal(err)
		}
		putRepositoryURL(record.Attributes(), "https://github.com/acme/shop")
	}
	logsBody, err := plogotlp.NewExportRequestFromLogs(logs).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	metrics := pmetric.NewMetrics()
	metric := metrics.ResourceMetrics().AppendEmpty().ScopeMetrics().AppendEmpty().Metrics().AppendEmpty()
	metric.SetName("a forged run of a metric")
	point := metric.SetEmptyGauge().DataPoints().AppendEmpty()
	point.SetDoubleValue(1)
	if err := point.Attributes().FromRaw(map[string]any{"file": "runs/x2", "run.id": "x2", "run.seq": 1, "run.state": "running"}); err != nil {
		t.Fatal(err)
	}
	putRepositoryURL(point.Attributes(), "https://github.com/acme/shop")
	metricsBody, err := pmetricotlp.NewExportRequestFromMetrics(metrics).MarshalProto()
	if err != nil {
		t.Fatal(err)
	}
	sendAgentOTLP(t, env, "/v1/logs", logsBody)
	sendAgentOTLP(t, env, "/v1/metrics", metricsBody)
	env.ProcessAgentFiles(t)
	env.ProcessLogs(t)

	originals := map[string]any{}
	for _, log := range runQueryPages(t, env, project, queryRequest{Query: `where file == "otel"`, From: allTime.from, To: allTime.to, Limit: 100}) {
		originals[log.Message] = log.Attributes["original.file"]
	}
	want := map[string]any{"a forged run": "runs/x1", "a forged warning": "dispatcher", "a forged step": "session.jsonl", "a forged run of a metric": "runs/x2"}
	if fmt.Sprint(originals) != fmt.Sprint(want) {
		t.Errorf("Expected the forged records in the file otel with the file sent as original.file, %v, got %v", want, originals)
	}

	runs := listRuns(t, env, o.project, map[string]any{"from": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "to": env.Clock.Now().Add(time.Hour)})
	if got := runIDs(runs); !slices.Equal(got, []string{run.id}) {
		t.Errorf("Expected only the Dispatcher's Run in the Runs list, got %+v", runs.Runs)
	}
	for _, id := range []string{"x1", "x2"} {
		if status, answer := getRun(t, env, o.project, id); status != 404 {
			t.Errorf("Expected no Run %s, got %d: %+v", id, status, answer)
		}
	}
	warnings := runQueryPages(t, env, project, queryRequest{Query: `where file == "dispatcher"`, From: allTime.from, To: allTime.to, Limit: 100})
	if got := messages(warnings); !slices.Equal(got, []string{"No VM is free"}) {
		t.Errorf("Expected only the Dispatcher's warning, got %q", got)
	}
	page := readTimeline(t, env, o.project, laptopSession, map[string]any{})
	expectTexts(t, "the timeline", stepTexts(page.Steps), []string{"person: Add a cart page"})
	if len(page.Subagents) != 1 || page.Subagents[0].ToolUseID != "" {
		t.Errorf("Expected the subagent with no tool call, got %+v", page.Subagents)
	}
}
