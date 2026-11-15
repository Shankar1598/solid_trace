package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"
)

// The Dispatcher sends run events to the agent ingest API, and EventStore
// stores them as Logs in the Organization's Agent Project. The Runs list
// and the Run page read them back.

const runEventsPath = "/api/agent/v1/run-events"

// runEventsOrganization is an Organization with Agent sessions on and the
// Dispatcher's token of its Admin.
type runEventsOrganization struct {
	agentOrganization
	dispatcherSecret string
}

func newRunEventsOrganization(t *testing.T, env *TestEnv) runEventsOrganization {
	t.Helper()
	o := runEventsOrganization{agentOrganization: newAgentOrganization(t, env)}
	o.dispatcherSecret, _ = o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
		scopes: []string{"run_events:write", "issues:comment", "tokens:create"}})
	return o
}

// sendRunEvents posts a batch of run events as the Dispatcher's sender
// loop does.
func sendRunEvents(t *testing.T, env *TestEnv, secret string, events ...map[string]any) agentAnswer {
	t.Helper()
	raw, _ := json.Marshal(events)
	req, _ := http.NewRequest("POST", runEventsPath, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	resp, err := env.App.Test(req, 10000)
	if err != nil {
		t.Fatalf("POST %s: %v", runEventsPath, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	answer := agentAnswer{status: resp.StatusCode, raw: string(body)}
	_ = json.Unmarshal(body, &answer.body)
	return answer
}

// runSnapshot is the Run's list fields that each of its events carries.
type runSnapshot struct {
	id, state, reason, title, prURL, prOutcome string
	executions                                 int
}

// runEvent is one run event as the Dispatcher sends it: a Log in the JSON
// Log format, with the Run's snapshot.
func runEvent(run runSnapshot, seq int, at time.Time, kind string) map[string]any {
	return map[string]any{
		"time":    at.UTC().Format(time.RFC3339Nano),
		"level":   "info",
		"message": fmt.Sprintf("Run %s: %s", run.id, kind),
		"run": map[string]any{
			"id": run.id, "seq": seq, "state": run.state, "reason": run.reason, "trigger": "cli",
			"title": run.title, "executions": run.executions,
			"pr": map[string]any{"url": run.prURL, "outcome": run.prOutcome},
		},
		"event":           map[string]any{"id": fmt.Sprintf("%s-%d", run.id, seq), "kind": kind},
		"execution":       map[string]any{"id": fmt.Sprintf("%s-x%d", run.id, run.executions)},
		"factory.project": "checkout",
		"repository":      "bitbucket.org/acme/shop",
		"folder":          "web",
		"actor":           map[string]any{"kind": "person", "id": "7", "name": "Ana"},
	}
}

type runRow struct {
	ID         string    `json:"id"`
	Seq        int       `json:"seq"`
	Time       time.Time `json:"time"`
	Kind       string    `json:"kind"`
	State      string    `json:"state"`
	Reason     string    `json:"reason"`
	Project    string    `json:"project"`
	Repository string    `json:"repository"`
	Folder     string    `json:"folder"`
	Trigger    string    `json:"trigger"`
	Title      string    `json:"title"`
	Actor      struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"actor"`
	PR struct {
		URL     string `json:"url"`
		Outcome string `json:"outcome"`
	} `json:"pr"`
	Executions int  `json:"executions"`
	Active     bool `json:"active"`
	Closed     bool `json:"closed"`
}

type runsAnswer struct {
	Runs []runRow `json:"runs"`
	More bool     `json:"more"`
}

func listRuns(t *testing.T, env *TestEnv, projectID int64, body map[string]any) runsAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/runs", projectID), body)
	if status != 200 {
		t.Fatalf("Expected 200 for %v, got %d: %s", body, status, raw)
	}
	var answer runsAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode %s: %v", raw, err)
	}
	return answer
}

func runIDs(answer runsAnswer) []string {
	ids := []string{}
	for _, r := range answer.Runs {
		ids = append(ids, r.ID)
	}
	return ids
}

type runAnswer struct {
	Run    runRow      `json:"run"`
	Events []listedLog `json:"events"`
}

func getRun(t *testing.T, env *TestEnv, projectID int64, runID string) (int, runAnswer) {
	t.Helper()
	status, raw := getQuery(t, env, fmt.Sprintf("/api/%d/runs/%s", projectID, runID))
	var answer runAnswer
	if status == 200 {
		if err := json.Unmarshal(raw, &answer); err != nil {
			t.Fatalf("Decode %s: %v", raw, err)
		}
	}
	return status, answer
}

func eventSeqs(answer runAnswer) []float64 {
	seqs := []float64{}
	for _, e := range answer.Events {
		seq, _ := e.Attributes["run.seq"].(float64)
		seqs = append(seqs, seq)
	}
	return seqs
}

func TestRunEventsAreLogsInTheAgentProject(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newRunEventsOrganization(t, env)
	at := env.Clock.Now().Add(-time.Hour)
	run := runSnapshot{id: "a1b2c3d4e5f6g7h8", state: "queued", title: "Fix the flaky cart test"}

	t.Run("a batch from the Dispatcher's token is stored, and a Query on the Agent Project returns each event", func(t *testing.T) {
		running := run
		running.state, running.executions = "running", 1
		warning := map[string]any{"time": at.Format(time.RFC3339Nano), "level": "warn",
			"message": "No VM is free for Run a1b2c3d4e5f6g7h8 after 10 minutes"}
		answer := sendRunEvents(t, env, o.dispatcherSecret,
			runEvent(run, 1, at, "created"), runEvent(run, 2, at.Add(time.Second), "queued"),
			runEvent(running, 3, at.Add(2*time.Second), "execution_started"), warning)
		expectStatus(t, answer, 200)
		if answer.body["accepted"] != float64(4) || answer.body["dropped"] != float64(0) {
			t.Fatalf("Expected 4 accepted and none dropped, got %s", answer.raw)
		}
		env.ProcessLogs(t)

		events := runQueryPages(t, env, uint32(o.project), queryRequest{Query: `where file == "runs/a1b2c3d4e5f6g7h8"`,
			From: allTime.from, To: allTime.to, Limit: 100})
		if len(events) != 3 {
			t.Fatalf("Expected the Run's 3 events, got %+v", events)
		}
		last := events[0]
		if last.Service != "bitbucket.org/acme/shop" || last.Attributes["event.kind"] != "execution_started" ||
			last.Attributes["run.seq"] != float64(3) || last.Attributes["run.state"] != "running" ||
			last.Attributes["actor.name"] != "Ana" || last.Attributes["factory.project"] != "checkout" ||
			!last.Time.Equal(at.Add(2*time.Second)) {
			t.Fatalf("Expected the Dispatcher's event with its time, service and Attributes, got %+v", last)
		}

		warnings := runQueryPages(t, env, uint32(o.project), queryRequest{Query: `where file == "dispatcher"`,
			From: allTime.from, To: allTime.to, Limit: 100})
		if len(warnings) != 1 || warnings[0].Level != "warn" || warnings[0].Message != warning["message"] {
			t.Fatalf("Expected the Dispatcher's warning Log with file dispatcher, got %+v", warnings)
		}
	})

	t.Run("a token without run_events:write gets 403, and no token gets 401", func(t *testing.T) {
		answer := sendRunEvents(t, env, o.secret, runEvent(run, 9, at, "queued"))
		expectStatus(t, answer, 403)
		if answer.body["scope"] != "run_events:write" {
			t.Fatalf("Expected the 403 to name run_events:write, got %s", answer.raw)
		}
		expectStatus(t, sendRunEvents(t, env, "", runEvent(run, 9, at, "queued")), 401)
	})

	t.Run("the Dispatcher's token uploads no Agent session", func(t *testing.T) {
		expectStatus(t, uploadUnit(t, env, o.dispatcherSecret, laptopSession, unitUpload{path: "session.jsonl", body: lines("Hi")}), 403)
	})

	t.Run("a run event with no run.seq, or a Run id that is not a plain name, is dropped", func(t *testing.T) {
		noSeq := runEvent(run, 1, at, "queued")
		delete(noSeq["run"].(map[string]any), "seq")
		badID := runEvent(runSnapshot{id: "../a1"}, 1, at, "queued")
		answer := sendRunEvents(t, env, o.dispatcherSecret, noSeq, badID)
		expectStatus(t, answer, 200)
		if answer.body["accepted"] != float64(0) || answer.body["dropped"] != float64(2) {
			t.Fatalf("Expected both dropped, got %s", answer.raw)
		}
	})

	t.Run("an Organization with Agent sessions off gets 403", func(t *testing.T) {
		off := o.rows.organization("No agents yet")
		o.rows.member(off, o.user)
		secret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: off, scopes: []string{"run_events:write"}})
		expectStatus(t, sendRunEvents(t, env, secret, runEvent(run, 1, at, "created")), 403)
	})
}

func TestRunsListAndOneRunFromRunEvents(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newRunEventsOrganization(t, env)
	now := env.Clock.Now()
	day := 24 * time.Hour

	running := runSnapshot{id: "running0000000001", state: "running", executions: 1}
	done := runSnapshot{id: "done0000000000001", state: "done", executions: 1}
	openPR := runSnapshot{id: "openpr00000000001", state: "done", executions: 1, prURL: "https://bitbucket.org/acme/shop/pull-requests/4"}
	closed := runSnapshot{id: "closed00000000001", state: "done", executions: 1, prURL: "https://bitbucket.org/acme/shop/pull-requests/3", prOutcome: "merged"}
	waiting := runSnapshot{id: "waiting0000000001", state: "waiting_on_person", reason: "needs a person", executions: 1}
	old := runSnapshot{id: "old00000000000001", state: "failed", executions: 1}

	queued := func(r runSnapshot) runSnapshot {
		r.state, r.reason, r.executions, r.prURL, r.prOutcome = "queued", "", 0, "", ""
		return r
	}
	batch := []map[string]any{
		runEvent(queued(running), 1, now.Add(-5*day), "queued"), runEvent(running, 2, now.Add(-5*day+time.Minute), "execution_started"),
		runEvent(queued(done), 1, now.Add(-4*day), "queued"), runEvent(done, 2, now.Add(-3*time.Hour), "report"),
		runEvent(queued(openPR), 1, now.Add(-10*day), "queued"), runEvent(openPR, 2, now.Add(-9*day), "pr_opened"),
		runEvent(queued(closed), 1, now.Add(-6*day), "queued"), runEvent(closed, 2, now.Add(-2*time.Hour), "pr_outcome"),
		runEvent(closed, 3, now.Add(-2*time.Hour+time.Second), "closed"),
		runEvent(waiting, 2, now.Add(-1*time.Hour), "report"), runEvent(queued(waiting), 1, now.Add(-2*day), "queued"),
		runEvent(old, 1, now.Add(-29*day), "failed"),
	}
	expectStatus(t, sendRunEvents(t, env, o.dispatcherSecret, batch...), 200)
	// The sender loop sends a batch again when it did not get the answer.
	expectStatus(t, sendRunEvents(t, env, o.dispatcherSecret, batch...), 200)
	env.ProcessLogs(t)
	// 31 days pass: the old Run's event leaves the default range.
	env.Clock.Set(now.Add(2 * day))
	now = env.Clock.Now()

	t.Run("the Runs list gives the last event of each Run in 30 days, Active Runs first", func(t *testing.T) {
		answer := listRuns(t, env, o.project, map[string]any{})
		want := []string{waiting.id, running.id, openPR.id, closed.id, done.id}
		if got := runIDs(answer); !slices.Equal(got, want) {
			t.Fatalf("Expected %v, got %v", want, got)
		}
		w := answer.Runs[0]
		if w.State != "waiting_on_person" || w.Reason != "needs a person" || w.Seq != 2 || w.Kind != "report" ||
			w.Project != "checkout" || w.Repository != "bitbucket.org/acme/shop" || w.Folder != "web" ||
			w.Trigger != "cli" || w.Actor.Name != "Ana" || w.Actor.Kind != "person" || !w.Active || w.Closed {
			t.Fatalf("Expected the waiting Run's last event, got %+v", w)
		}
		c := answer.Runs[3]
		if c.Active || !c.Closed || c.PR.Outcome != "merged" || c.Kind != "closed" {
			t.Fatalf("Expected the closed Run to be closed and not active, got %+v", c)
		}
		if p := answer.Runs[2]; !p.Active || p.PR.URL == "" {
			t.Fatalf("Expected a done Run with an open PR to be active, got %+v", p)
		}
		if d := answer.Runs[4]; d.Active || d.Closed {
			t.Fatalf("Expected a done Run with no PR to be neither active nor closed, got %+v", d)
		}
	})

	t.Run("a longer range is the reader's choice", func(t *testing.T) {
		answer := listRuns(t, env, o.project, map[string]any{"from": now.Add(-40 * day), "to": now})
		if got := runIDs(answer); len(got) != 6 || got[5] != old.id {
			t.Fatalf("Expected the old Run last in 40 days, got %v", got)
		}
	})

	t.Run("the filters and the limit", func(t *testing.T) {
		if got := runIDs(listRuns(t, env, o.project, map[string]any{"state": "waiting_on_person"})); !slices.Equal(got, []string{waiting.id}) {
			t.Fatalf("Expected the waiting Run only, got %v", got)
		}
		if got := runIDs(listRuns(t, env, o.project, map[string]any{"project": "search"})); len(got) != 0 {
			t.Fatalf("Expected no Run of another Project, got %v", got)
		}
		answer := listRuns(t, env, o.project, map[string]any{"limit": 2})
		if got := runIDs(answer); !slices.Equal(got, []string{waiting.id, running.id}) || !answer.More {
			t.Fatalf("Expected two Runs and more, got %v %v", got, answer.More)
		}
	})

	t.Run("one Run gives its events in run.seq order, each one time", func(t *testing.T) {
		status, answer := getRun(t, env, o.project, waiting.id)
		if status != 200 {
			t.Fatalf("Expected 200, got %d", status)
		}
		if got := eventSeqs(answer); !slices.Equal(got, []float64{1, 2}) {
			t.Fatalf("Expected events 1 and 2 one time each, got %v", got)
		}
		if answer.Run.ID != waiting.id || answer.Run.State != "waiting_on_person" || !answer.Run.Active {
			t.Fatalf("Expected the Run's last event as its row, got %+v", answer.Run)
		}
		_, answer = getRun(t, env, o.project, closed.id)
		if got := eventSeqs(answer); !slices.Equal(got, []float64{1, 2, 3}) {
			t.Fatalf("Expected events 1 to 3, got %v", got)
		}
		// One Run's page reads all its events, also those past 30 days.
		_, answer = getRun(t, env, o.project, old.id)
		if got := eventSeqs(answer); !slices.Equal(got, []float64{1}) {
			t.Fatalf("Expected the old Run's event, got %v", got)
		}
	})

	t.Run("a Run with no events is 404", func(t *testing.T) {
		if status, _ := getRun(t, env, o.project, "nosuchrun0000001"); status != 404 {
			t.Fatalf("Expected 404, got %d", status)
		}
	})
}

// An Agent session file at a path under runs/ is also a Log in the Agent
// Project, but it is not a run event: the Runs list does not show it.
func TestAnAgentSessionFileUnderRunsIsNotARun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newRunEventsOrganization(t, env)
	now := env.Clock.Now()

	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "runs/notes.jsonl", folder: "web", body: jsonl(transcript...)}), 200)
	run := runSnapshot{id: "real0000000000001", state: "running", executions: 1}
	expectStatus(t, sendRunEvents(t, env, o.dispatcherSecret, runEvent(run, 1, now.Add(-time.Hour), "execution_started")), 200)
	if logs := sessionLogs(t, env, o.project, laptopSession); len(logs) != len(transcript) {
		t.Fatalf("Expected %d Logs of the Agent session, got %d", len(transcript), len(logs))
	}
	env.ProcessLogs(t)

	answer := listRuns(t, env, o.project, map[string]any{"from": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "to": now.Add(time.Hour)})
	if got := runIDs(answer); !slices.Equal(got, []string{run.id}) {
		t.Fatalf("Expected only the real Run, got %+v", answer.Runs)
	}
}
