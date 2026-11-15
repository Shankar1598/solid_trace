package main_test

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"
)

// The Run page reads one Run's events: each event carries the details of
// its kind (an Execution's account and VM, a report's checks and Evidence
// paths) beside the Run's snapshot, and EventStore gives one Evidence file
// of an Agent session, for the page to show.

func TestTheRunPageReadsTheDetailsOfEachEvent(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newRunEventsOrganization(t, env)
	at := env.Clock.Now().Add(-time.Hour)
	run := runSnapshot{id: "run_details", state: "running", title: "Fix the cart total", executions: 1}

	started := runEvent(run, 1, at, "execution_started")
	started["run"].(map[string]any)["base_branch"] = "develop"
	started["execution"] = map[string]any{"id": "run_details-x1", "account": "team-2", "vm": "agent-vm-7"}
	started["agent"] = map[string]any{"session_id": runSession}
	done := run
	done.state = "done"
	report := runEvent(done, 2, at.Add(10*time.Minute), "report")
	report["run"].(map[string]any)["base_branch"] = "develop"
	checks := []any{
		map[string]any{"name": "unit", "command": "bun test", "exit_status": 0, "time": at.Format(time.RFC3339), "output_tail": "12 pass"},
		map[string]any{"name": "lint", "command": "bun lint", "exit_status": 1, "time": at.Format(time.RFC3339), "output_tail": "1 error"},
	}
	report["report"] = map[string]any{"outcome": "done", "summary": "Fixed the total.", "checks": checks,
		"evidence": []any{"evidence/cart.png", "evidence/report.md"}}
	report["agent"] = map[string]any{"session_id": runSession}
	expectStatus(t, sendRunEvents(t, env, o.dispatcherSecret, started, report), 200)
	env.ProcessLogs(t)

	status, answer := getRun(t, env, o.project, run.id)
	if status != 200 || len(answer.Events) != 2 {
		t.Fatalf("Expected the Run's 2 events, got %d %+v", status, answer)
	}
	if answer.Run.BaseBranch != "develop" || answer.Run.State != "done" {
		t.Fatalf("Expected the Run's base branch from its snapshot, got %+v", answer.Run)
	}
	first, last := answer.Events[0].Attributes, answer.Events[1].Attributes
	if first["execution.account"] != "team-2" || first["execution.vm"] != "agent-vm-7" || first["agent.session_id"] != runSession {
		t.Fatalf("Expected the Execution's account, VM and Agent session, got %+v", first)
	}
	if !reflect.DeepEqual(last["report.evidence"], []any{"evidence/cart.png", "evidence/report.md"}) {
		t.Fatalf("Expected the report's Evidence paths as a list, got %#v", last["report.evidence"])
	}
	got, ok := last["report.checks"].([]any)
	if !ok || len(got) != 2 {
		t.Fatalf("Expected the report's 2 checks as a list, got %#v", last["report.checks"])
	}
	lint, _ := got[1].(map[string]any)
	if lint["name"] != "lint" || lint["exit_status"] != float64(1) || lint["output_tail"] != "1 error" {
		t.Fatalf("Expected each check with its exit status and output tail, got %#v", got[1])
	}
}

func evidencePath(projectID int64, sessionID, path string) string {
	return fmt.Sprintf("/api/%d/agent-sessions/%s/evidence?path=%s", projectID, sessionID, url.QueryEscape(path))
}

func getEvidence(t *testing.T, env *TestEnv, path string) (int, string, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", path, nil)
	resp, err := env.QueryApp.Test(req, 10000)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(raw)
}

func TestTheEvidenceFileOfAnAgentSession(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	runSecret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
		scopes: []string{"sessions:write", "sessions:read"}, sessionIDs: []string{runSession}})
	png := "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR the image"
	firstHalf, secondHalf := "# Report\n\nFixed the total.\n", "## Cause report\n\nThe cart rounded each line.\n"
	for _, u := range []unitUpload{
		{path: "session.jsonl", body: lines("Fix the cart total")},
		{path: "evidence/cart.png", contentHash: "sha256:png", body: []byte("an old image")},
		{path: "evidence/cart.png", version: 2, contentHash: "sha256:png2", body: []byte(png)},
		{path: "evidence/report.md", contentHash: "sha256:md", body: []byte(firstHalf + secondHalf)},
	} {
		u.runID, u.project = "run_details", "shop"
		expectStatus(t, uploadUnit(t, env, runSecret, runSession, u), 200)
	}
	env.ProcessAgentFiles(t)

	t.Run("an Evidence file is its highest version, with the type of its name", func(t *testing.T) {
		status, kind, body := getEvidence(t, env, evidencePath(o.project, runSession, "evidence/cart.png"))
		if status != 200 || kind != "image/png" || body != png {
			t.Fatalf("Expected the second version of the image, got %d %q %q", status, kind, body)
		}
		status, kind, body = getEvidence(t, env, evidencePath(o.project, runSession, "evidence/report.md"))
		if status != 200 || kind != "text/markdown; charset=utf-8" || body != firstHalf+secondHalf {
			t.Fatalf("Expected evidence/report.md in full, got %d %q %q", status, kind, body)
		}
	})

	t.Run("a path that is not under evidence/ is refused, and one that the Agent session does not have is 404", func(t *testing.T) {
		for _, path := range []string{"session.jsonl", "evidence/../session.jsonl", "evidence/", "/evidence/cart.png"} {
			if status, _, body := getEvidence(t, env, evidencePath(o.project, runSession, path)); status != 400 {
				t.Errorf("Expected 400 for %q, got %d %s", path, status, body)
			}
		}
		if status, _, _ := getEvidence(t, env, evidencePath(o.project, runSession, "evidence/other.png")); status != 404 {
			t.Errorf("Expected 404 for a file that the Agent session does not have, got %d", status)
		}
		if status, _, _ := getEvidence(t, env, evidencePath(o.project, otherSession, "evidence/cart.png")); status != 404 {
			t.Errorf("Expected 404 for another Agent session, got %d", status)
		}
		if status, _, _ := getEvidence(t, env, evidencePath(o.project+1000, runSession, "evidence/cart.png")); status != 404 {
			t.Errorf("Expected 404 in another Project, got %d", status)
		}
	})
}
