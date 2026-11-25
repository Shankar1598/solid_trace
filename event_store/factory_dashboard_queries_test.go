package main_test

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

// The four Charts of Console's starter Factory Dashboard. Console keeps
// their Queries in config/factory_dashboard_queries.yml; the tests run them
// here, on closed run events, so that a Query that EventStore cannot run, or
// that counts a repeated closed event twice, fails.

type dashboardChart struct {
	Title string `yaml:"title"`
	Query string `yaml:"query"`
}

func dashboardCharts(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../console/config/factory_dashboard_queries.yml")
	if err != nil {
		t.Fatalf("Read the Charts' Queries: %v", err)
	}
	var file struct {
		Charts []dashboardChart `yaml:"charts"`
	}
	if err := yaml.Unmarshal(raw, &file); err != nil {
		t.Fatalf("Decode the Charts' Queries: %v", err)
	}
	if len(file.Charts) != 4 {
		t.Fatalf("Expected 4 Charts, got %d", len(file.Charts))
	}
	queries := map[string]string{}
	for _, chart := range file.Charts {
		queries[chart.Title] = chart.Query
	}
	return queries
}

// closedRunEvent is the closed event of a Run in the file given, as
// EventStore stores it.
func closedRunEvent(t *testing.T, file, runID, project, reason string, tokens, toPR, toOutcome, interventions int) string {
	t.Helper()
	return logJSON(t, "Run "+runID+": closed", languageAt(10, 10), map[string]any{
		"file": file, "event.kind": "closed", "event.id": runID + "-9", "run.id": runID, "run.seq": 9, "run.close_reason": reason,
		"factory.project": project, "totals.tokens.input": tokens / 2, "totals.tokens.output": tokens / 2,
		"totals.tokens.cache_read": 0, "totals.tokens.cache_write": 0, "totals.seconds_to_pr_opened": toPR,
		"totals.seconds_from_pr_opened_to_outcome": toOutcome, "totals.interventions.total": interventions,
	})
}

// chartValues runs a Chart's Query for the day of the Logs, with the Project
// filter set to projects, and returns the number of each row.
func chartValues(t *testing.T, env *TestEnv, query string, projects ...string) []float64 {
	t.Helper()
	req := queryRequest{Query: query, From: languageAt(0, 0), To: languageAt(0, 0).AddDate(0, 0, 1)}
	if len(projects) > 0 {
		req.Parameters = map[string][]string{"project": projects}
	}
	status, raw := postQueryApp(t, env, "/api/123/logs/query", req)
	if status != 200 {
		t.Fatalf("Query %q expected 200, got %d: %s", query, status, raw)
	}
	var answer struct {
		Table struct {
			Columns []struct {
				Name       string  `json:"name"`
				BinSeconds float64 `json:"bin_seconds"`
			} `json:"columns"`
			Rows [][]any `json:"rows"`
		} `json:"table"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode the table %s: %v", raw, err)
	}
	columns := answer.Table.Columns
	if len(columns) != 2 || columns[0].Name != "time" || columns[0].BinSeconds == 0 {
		t.Fatalf("Query %q expected a time bin and one number, got %+v", query, columns)
	}
	var values []float64
	for _, row := range answer.Table.Rows {
		if value, ok := row[1].(float64); ok {
			values = append(values, value)
		}
	}
	return values
}

func assertChartValue(t *testing.T, title string, got []float64, want float64) {
	t.Helper()
	if len(got) != 1 || math.Abs(got[0]-want) > 0.01 {
		t.Errorf("%s: expected one bin with %.2f, got %v", title, want, got)
	}
}

func TestTheFactoryDashboardChartsRunAndCountEachRunOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	charts := dashboardCharts(t)

	env.Clock.Set(languageAt(10, 29))
	// Run a: merged, 100 tokens, 2 h, 3 interventions. Its closed event comes
	// twice, as the Dispatcher's sender can send it. Run b: merged, 300 tokens,
	// 1 h, 4 interventions. Run c: declined, 7 interventions, of the other Project.
	// A closed event that the Agent Project's key sent is in the file otel,
	// and no Chart counts it.
	a := closedRunEvent(t, "runs/run_a", "run_a", "shop", "pr_merged", 100, 3600, 3600, 3)
	b := closedRunEvent(t, "runs/run_b", "run_b", "shop", "pr_merged", 300, 3600, 0, 4)
	c := closedRunEvent(t, "runs/run_c", "run_c", "api", "pr_declined", 50, 600, 600, 7)
	forged := closedRunEvent(t, "otel", "run_f", "shop", "pr_declined", 1000000, 1, 1, 100)
	postAndProcess(t, env, a, a, b, c, forged)

	assertChartValue(t, "tokens", chartValues(t, env, charts["Tokens for each merged PR"]), 200)
	assertChartValue(t, "cycle time", chartValues(t, env, charts["Cycle time of merged PRs"]), 1.5)
	assertChartValue(t, "acceptance", chartValues(t, env, charts["Acceptance rate"]), 200.0/3)
	assertChartValue(t, "interventions", chartValues(t, env, charts["Interventions for each Run"]), 14.0/3)

	// The Project filter picks the Runs of its Projects.
	assertChartValue(t, "acceptance of shop", chartValues(t, env, charts["Acceptance rate"], "shop"), 100)
	assertChartValue(t, "interventions of api", chartValues(t, env, charts["Interventions for each Run"], "api"), 7)
	assertChartValue(t, "interventions of shop and api", chartValues(t, env, charts["Interventions for each Run"], "shop", "api"), 14.0/3)
}
