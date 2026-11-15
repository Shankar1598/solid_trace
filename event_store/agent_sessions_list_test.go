package main_test

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"
)

// The Agent sessions list and totals (spec, The agent ingest API: the
// read endpoints for Console): the Agent sessions of the Agent Project
// that the filters select, latest activity first, and the totals of the
// tokens and the estimated cost of those Agent sessions.

const (
	apiSession = "5a8c1e3f-7b2d-4e6a-9c0f-1d3b5e7a9c2e"
	oldSession = "9e1f3a5c-2b4d-4f6a-8c0e-3d5b7f9a1c4e"
)

type listedAgentSession struct {
	ID              string   `json:"id"`
	First           string   `json:"first"`
	Last            string   `json:"last"`
	UserID          int64    `json:"user_id"`
	Harness         string   `json:"harness"`
	Repository      string   `json:"repository"`
	Folder          string   `json:"folder"`
	RunID           string   `json:"run_id"`
	Project         string   `json:"project"`
	ParentSessionID string   `json:"parent_session_id"`
	Model           string   `json:"model"`
	Input           uint64   `json:"input_tokens"`
	Output          uint64   `json:"output_tokens"`
	CacheRead       uint64   `json:"cache_read_tokens"`
	CacheWrite      uint64   `json:"cache_write_tokens"`
	CostUSD         *float64 `json:"cost_usd"`
	PersonMessages  uint64   `json:"person_messages"`
	Turns           uint64   `json:"turns"`
	ToolCalls       uint64   `json:"tool_calls"`
	Subagents       int      `json:"subagents"`
	Flags           []string `json:"flags"`
}

type agentSessionsAnswer struct {
	AgentSessions []listedAgentSession `json:"agent_sessions"`
	Totals        struct {
		AgentSessions int      `json:"agent_sessions"`
		Input         uint64   `json:"input_tokens"`
		Output        uint64   `json:"output_tokens"`
		CacheRead     uint64   `json:"cache_read_tokens"`
		CacheWrite    uint64   `json:"cache_write_tokens"`
		CostUSD       *float64 `json:"cost_usd"`
		NoCost        int      `json:"no_cost"`
	} `json:"totals"`
	More         bool     `json:"more"`
	Repositories []string `json:"repositories"`
}

func listAgentSessions(t *testing.T, env *TestEnv, projectID int64, body map[string]any) agentSessionsAnswer {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/agent-sessions", projectID), body)
	if status != 200 {
		t.Fatalf("Expected 200 for %v, got %d: %s", body, status, raw)
	}
	var answer agentSessionsAnswer
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode %s: %v", raw, err)
	}
	return answer
}

func listedIDs(answer agentSessionsAnswer) []string {
	ids := []string{}
	for _, s := range answer.AgentSessions {
		ids = append(ids, s.ID)
	}
	return ids
}

// agentSessionsFixture is an Agent Project with four Agent sessions: two
// of one person in github.com/acme/shop (one of them in September), one
// of another person in github.com/acme/api, and a Run's in shop.
type agentSessionsFixture struct {
	agentOrganization
}

func newAgentSessionsFixture(t *testing.T, env *TestEnv) agentSessionsFixture {
	t.Helper()
	rows := newConsoleRows(t, env)
	o := agentOrganization{rows: rows, organization: rows.organization("Acme")}
	o.project = rows.agentProject(o.organization, "github.com/acme/shop", "github.com/acme/api")
	o.user, o.otherUser = rows.user(), rows.user()
	rows.member(o.organization, o.user)
	rows.member(o.organization, o.otherUser)
	write := []string{"sessions:write"}
	o.secret, _ = rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: write})
	o.otherSecret, _ = rows.apiToken(apiTokenRow{user: o.otherUser, organization: o.organization, scopes: write})
	runSecret, _ := rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: write, sessionIDs: []string{runSession}})

	env.Clock.Set(time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	expectStatus(t, uploadUnit(t, env, o.secret, oldSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("o1", "2026-09-20T10:00:00Z", "An old task"),
		replyAt("o2", "2026-09-20T10:00:05Z", "msg_o", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
	)}), 200)
	env.ProcessAgentFiles(t)

	env.Clock.Set(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`),
		toolResultAt("u2", "2026-10-02T11:00:06Z", "toolu_1"),
	)}), 200)
	expectStatus(t, uploadUnit(t, env, o.otherSecret, apiSession, unitUpload{path: "session.jsonl", repository: "github.com/acme/api", body: jsonl(
		personAt("b1", "2026-10-03T09:00:00Z", "Fix the API"),
		replyAt("b2", "2026-10-03T09:00:05Z", "msg_2", "claude-sonnet-5-5", `{"type":"text","text":"Done."}`),
	)}), 200)
	expectStatus(t, uploadUnit(t, env, runSecret, runSession, unitUpload{path: "session.jsonl", runID: "run_42", project: "shop", body: jsonl(
		personAt("r1", "2026-10-03T10:00:00Z", "Fix issue 42"),
	)}), 200)
	env.ProcessAgentFiles(t)
	return agentSessionsFixture{o}
}

var octoberRange = map[string]any{"from": "2026-10-01T00:00:00Z", "to": "2026-10-04T00:00:00Z"}

func withFilters(filters map[string]any) map[string]any {
	body := map[string]any{}
	for name, value := range octoberRange {
		body[name] = value
	}
	for name, value := range filters {
		body[name] = value
	}
	return body
}

func TestTheAgentSessionsListGivesTheAgentSessionsOfTheRangeLatestFirstWithTheirTotals(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	f := newAgentSessionsFixture(t, env)

	answer := listAgentSessions(t, env, f.project, octoberRange)
	if got, want := listedIDs(answer), []string{runSession, apiSession, laptopSession}; !slices.Equal(got, want) {
		t.Fatalf("Expected the Agent sessions of October, latest activity first, %v, got %v", want, got)
	}
	cart := answer.AgentSessions[2]
	if cart.First != "2026-10-02T11:00:00Z" || cart.Last != "2026-10-02T11:00:06Z" || cart.UserID != f.user ||
		cart.Repository != "github.com/acme/shop" || cart.Model != "claude-opus-5-5" || cart.Harness != "claude-code" ||
		cart.Input != 10 || cart.Output != 50 || cart.CacheRead != 1000 || cart.CacheWrite != 100 ||
		cart.PersonMessages != 1 || cart.ToolCalls != 1 || cart.CostUSD == nil || math.Abs(*cart.CostUSD-0.00174) > 1e-12 {
		t.Errorf("Unexpected row of the cart Agent session %+v", cart)
	}
	run := answer.AgentSessions[0]
	if run.RunID != "run_42" || run.Project != "shop" || run.CostUSD != nil || run.Flags == nil {
		t.Errorf("Expected the Run's row with no cost and a list of flags, got %+v", run)
	}

	totals := answer.Totals
	if totals.AgentSessions != 3 || totals.Input != 20 || totals.Output != 100 || totals.CacheRead != 2000 ||
		totals.CacheWrite != 200 || totals.NoCost != 1 || answer.More {
		t.Errorf("Expected the totals of the three Agent sessions, one with no cost, got %+v", answer)
	}
	if totals.CostUSD == nil || math.Abs(*totals.CostUSD-(0.00174+0.00087)) > 1e-12 {
		t.Errorf("Expected the sum of the two costs, got %v", totals.CostUSD)
	}
	if want := []string{"github.com/acme/api", "github.com/acme/shop"}; !slices.Equal(answer.Repositories, want) {
		t.Errorf("Expected the repositories of the range %v, got %v", want, answer.Repositories)
	}

	september := listAgentSessions(t, env, f.project, map[string]any{"from": "2026-09-19T00:00:00Z", "to": "2026-09-21T00:00:00Z"})
	if got := listedIDs(september); !slices.Equal(got, []string{oldSession}) {
		t.Errorf("Expected only the September Agent session, got %v", got)
	}
	// An Agent session is in a range when it has activity in it.
	hour := listAgentSessions(t, env, f.project, map[string]any{"from": "2026-10-03T09:30:00Z", "to": "2026-10-03T10:30:00Z"})
	if got := listedIDs(hour); !slices.Equal(got, []string{runSession}) {
		t.Errorf("Expected only the Run's Agent session at 10:00, got %v", got)
	}
}

func TestTheAgentSessionsFiltersSelectThePersonTheRepositoryTheProjectAndTheRun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	f := newAgentSessionsFixture(t, env)

	for _, c := range []struct {
		name    string
		filters map[string]any
		want    []string
	}{
		{"a person", map[string]any{"user_id": f.user}, []string{runSession, laptopSession}},
		{"another person", map[string]any{"user_id": f.otherUser}, []string{apiSession}},
		{"a repository", map[string]any{"repository": "github.com/acme/api"}, []string{apiSession}},
		// The Project (Runs) filter selects the Agent sessions of Runs only.
		{"a Project of Runs", map[string]any{"project": "shop"}, []string{runSession}},
		{"another Project", map[string]any{"project": "blog"}, []string{}},
		{"the Agent sessions of Runs", map[string]any{"run": "run"}, []string{runSession}},
		{"the Agent sessions of no Run", map[string]any{"run": "none"}, []string{apiSession, laptopSession}},
		{"a person and a repository", map[string]any{"user_id": f.user, "repository": "github.com/acme/api"}, []string{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			answer := listAgentSessions(t, env, f.project, withFilters(c.filters))
			if got := listedIDs(answer); !slices.Equal(got, c.want) {
				t.Errorf("Expected %v, got %v", c.want, got)
			}
			if answer.Totals.AgentSessions != len(c.want) {
				t.Errorf("Expected the totals of the %d rows, got %+v", len(c.want), answer.Totals)
			}
		})
	}

	t.Run("the totals of the person's rows", func(t *testing.T) {
		totals := listAgentSessions(t, env, f.project, withFilters(map[string]any{"user_id": f.otherUser})).Totals
		if totals.Input != 10 || totals.CostUSD == nil || math.Abs(*totals.CostUSD-0.00087) > 1e-12 || totals.NoCost != 0 {
			t.Errorf("Expected the tokens and the cost of the api Agent session, got %+v", totals)
		}
	})

	t.Run("no cost is not a cost of 0", func(t *testing.T) {
		totals := listAgentSessions(t, env, f.project, withFilters(map[string]any{"run": "run"})).Totals
		if totals.CostUSD != nil || totals.NoCost != 1 {
			t.Errorf("Expected no cost total, got %+v", totals)
		}
	})

	t.Run("a limit gives the latest rows and the totals of all", func(t *testing.T) {
		answer := listAgentSessions(t, env, f.project, withFilters(map[string]any{"limit": 1}))
		if got := listedIDs(answer); !slices.Equal(got, []string{runSession}) || !answer.More || answer.Totals.AgentSessions != 3 {
			t.Errorf("Expected the latest row, more, and the totals of the three, got %v %+v", got, answer)
		}
	})

	t.Run("another Project has none of them", func(t *testing.T) {
		if got := listedIDs(listAgentSessions(t, env, f.project+1000, octoberRange)); len(got) != 0 {
			t.Errorf("Expected no Agent sessions, got %v", got)
		}
	})
}

func TestTheAgentSessionsListRefusesABadRequest(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	for _, body := range []map[string]any{
		{},
		{"from": "2026-10-04T00:00:00Z", "to": "2026-10-01T00:00:00Z"},
		withFilters(map[string]any{"run": "maybe"}),
		withFilters(map[string]any{"limit": -1}),
	} {
		if status, raw := postQueryApp(t, env, "/api/123/agent-sessions", body); status != 400 {
			t.Errorf("Expected 400 for %v, got %d: %s", body, status, raw)
		}
	}
}
