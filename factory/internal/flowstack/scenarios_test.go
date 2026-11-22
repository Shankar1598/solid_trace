package main

import (
	"context"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/cli/dispatcherapi"
	"github.com/solidtrace/factory/internal/factorytest"
)

func TestMain(m *testing.M) {
	factorytest.MainIfAsked()
	os.Exit(m.Run())
}

func TestTheDemoRunsReachTheirStatesAndTheLongRunStaysRunningUntilStopped(t *testing.T) {
	env := factorytest.New(t, factorytest.Executions)
	env.Project("shop")
	ana := env.Admin()
	client := &dispatcherapi.Client{URL: env.URL(), HTTP: &http.Client{Timeout: 30 * time.Second},
		Tokens: staticToken(ana.Token), Version: "0.0.0-test"}
	s := stage{Client: client, Project: "shop", Agent: env.Runner.Agent, Dispatched: env.Runner.Dispatched,
		Say: func(format string, args ...any) { t.Logf(format, args...) }}

	list, err := scenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) < 6 {
		t.Fatalf("the demo has %d Runs, not at least 6", len(list))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	runs, err := s.Start(ctx, list)
	if err != nil {
		t.Fatalf("%v\nDispatcher log:\n%s", err, env.Logs())
	}

	get := func(i int) dispatcherapi.Run {
		run, err := client.Get(ctx, runs[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		return run
	}
	// Done with a PR, two checks that passed and one Evidence file.
	first := get(0)
	if first.PRURL == "" || len(first.Flags) != 0 {
		t.Errorf("the first Run: PR %q, flags %q", first.PRURL, first.Flags)
	}
	reports := reportEvents(env.RunEvents(first.ID))
	if len(reports) != 1 {
		t.Fatalf("the first Run has %d report events", len(reports))
	}
	checks, _ := reports[0]["report.checks"].([]any)
	if len(checks) != 2 {
		t.Errorf("the first Run has %d checks, not 2: %v", len(checks), checks)
	}
	if evidence := evidencePaths(reports[0]); !slices.Contains(evidence, "evidence/cart-total.txt") {
		t.Errorf("the first Run's Evidence: %q", evidence)
	}
	// Done, no Evidence.
	if second := get(1); !slices.Equal(second.Flags, []string{dispatcherapi.FlagNoEvidence}) || second.PRURL == "" {
		t.Errorf("the second Run: PR %q, flags %q", second.PRURL, second.Flags)
	}
	// The question.
	if events := env.RunEvents(runs[2].ID); !strings.Contains(strings.Join(reportQuestions(events), "\n"), "currency") {
		t.Errorf("the third Run's report has no question: %v", events)
	}
	// The stopped Run is stopped; the long Run still works.
	if stopped := get(4); stopped.State != dispatcherapi.StateStopped {
		t.Errorf("the stopped Run is %s", stopped.State)
	}
	if long := get(5); long.State != dispatcherapi.StateRunning {
		t.Errorf("the long Run is %s", long.State)
	}

	// The demo merges the first Run's PR on the fake Forge, so that the Run
	// closes and the starter Dashboard has a closed event to draw.
	if err := s.MergeFirstPR(ctx, env, runs[0]); err != nil {
		t.Fatalf("%v\nDispatcher log:\n%s", err, env.Logs())
	}
	var closed map[string]any
	for _, ev := range env.RunEvents(runs[0].ID) {
		if ev["event.kind"] == "closed" {
			closed = ev
		}
	}
	if closed == nil || closed["run.close_reason"] != "pr_merged" {
		t.Errorf("the first Run has no closed event with the reason pr_merged: %v", closed)
	}

	// The demo's end stops the long Run, so that it does not stay running.
	s.StopRunning(ctx, runs)
	if long := get(5); long.State != dispatcherapi.StateStopped {
		t.Errorf("after the end of the demo, the long Run is %s", long.State)
	}
}

func reportEvents(events []map[string]any) []map[string]any {
	var reports []map[string]any
	for _, ev := range events {
		if ev["event.kind"] == "report" {
			reports = append(reports, ev)
		}
	}
	return reports
}

func evidencePaths(report map[string]any) []string {
	list, _ := report["report.evidence"].([]any)
	var paths []string
	for _, p := range list {
		paths = append(paths, p.(string))
	}
	return paths
}

func reportQuestions(events []map[string]any) []string {
	var questions []string
	for _, ev := range reportEvents(events) {
		if q, ok := ev["report.question"].(string); ok {
			questions = append(questions, q)
		}
	}
	return questions
}
