package main_test

import (
	"context"
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
	Title           string   `json:"title"`
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
	Projects     []string `json:"projects"`
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
	return withRange(octoberRange, filters)
}

func withRange(timeRange, filters map[string]any) map[string]any {
	body := map[string]any{}
	for name, value := range timeRange {
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
	// The Projects of the range's Runs: an engineer's own Agent session has none.
	if want := []string{"shop"}; !slices.Equal(answer.Projects, want) {
		t.Errorf("Expected the Projects of the range's Runs %v, got %v", want, answer.Projects)
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

	t.Run("a limit of all the rows gives no more", func(t *testing.T) {
		answer := listAgentSessions(t, env, f.project, withFilters(map[string]any{"limit": 3}))
		if got := listedIDs(answer); len(got) != 3 || answer.More {
			t.Errorf("Expected the three rows and no more, got %v %+v", got, answer)
		}
		answer = listAgentSessions(t, env, f.project, withFilters(map[string]any{"limit": 2}))
		if got := listedIDs(answer); len(got) != 2 || !answer.More {
			t.Errorf("Expected two rows and more, got %v %+v", got, answer)
		}
	})

	t.Run("another Project has none of them", func(t *testing.T) {
		if got := listedIDs(listAgentSessions(t, env, f.project+1000, octoberRange)); len(got) != 0 {
			t.Errorf("Expected no Agent sessions, got %v", got)
		}
	})
}

// The 2-second rule (spec, Storage): if a sessions-page query takes more
// than 2 seconds, an agent_messages table is necessary. The fixture is a
// month of ten engineers, each with 12 Agent sessions a day of a main
// transcript and two subagents. It is written as rows of agent_sessions,
// as the parse writes them, so that it is fast to make.
func TestTheAgentSessionsListOfAMonthOfTenEngineersAnswersInLessThanTwoSeconds(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	const (
		engineers = 10
		sessions  = engineers * 12 * 30
		projectID = 123
	)
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	err := env.ClickHouse.Conn().Exec(context.Background(), `INSERT INTO agent_sessions
		SELECT ? AS project_id, toDate(at) AS day, reinterpretAsUUID(MD5(toString(s))) AS session_id,
			if(n % 3 = 0, 'session.jsonl', concat('subagents/agent-', toString(n % 3), '.jsonl')) AS path, 1 AS version,
			at AS first_time, at + toIntervalMinute(40) AS last_time, s % ? + 1 AS user_id, 'claude-code' AS harness,
			concat('github.com/acme/repository-', toString(s % 5)) AS repository, '' AS folder,
			if(s % 7 = 0, concat('run_', toString(s)), '') AS run_id, if(s % 7 = 0, 'shop', '') AS factory_project,
			'' AS parent_session_id, 2000 AS input_tokens, 30000 AS output_tokens, 4000000 AS cache_read_tokens,
			200000 AS cache_write_tokens, 3.5 AS price_cost_usd, 80 AS replies, 0 AS unpriced_replies,
			tuple(toUInt64(900000), 'claude-opus-5-5') AS last_reply, tuple(toUInt64(0), toFloat64(0)) AS last_cost_state,
			if(n % 3 = 0, 12, 0) AS person_messages, if(n % 3 = 0, 10, 0) AS turns, 150 AS tool_calls,
			1 AS has_start, 0 AS flags,
			if(n % 3 = 0, tuple(toUInt8(1), toUInt64(800000), concat('Session ', toString(s))), tuple(toUInt8(0), toUInt64(0), '')) AS title
		FROM (SELECT number AS n, intDiv(number, 3) AS s,
			toDateTime64(?, 6, 'UTC') + toIntervalSecond(intDiv(intDiv(number, 3) * 2592000, ?)) AS at
			FROM numbers(?))`, projectID, engineers, start, sessions, sessions*3)
	if err != nil {
		t.Fatalf("Insert the month of Agent sessions: %v", err)
	}

	month := map[string]any{"from": start.Format(time.RFC3339), "to": start.AddDate(0, 0, 30).Format(time.RFC3339)}
	for _, c := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"the whole month", month, sessions},
		{"one engineer", withRange(month, map[string]any{"user_id": 3}), sessions / engineers},
		{"one repository of no Run", withRange(month, map[string]any{"repository": "github.com/acme/repository-2", "run": "none"}), 0},
	} {
		began := time.Now()
		answer := listAgentSessions(t, env, projectID, c.body)
		took := time.Since(began)
		if c.want != 0 && answer.Totals.AgentSessions != c.want {
			t.Errorf("%s: expected %d Agent sessions, got %d", c.name, c.want, answer.Totals.AgentSessions)
		}
		if took > 2*time.Second {
			t.Errorf("%s: expected an answer in less than 2 seconds, it took %s", c.name, took)
		}
		t.Logf("%s: %d Agent sessions in %s", c.name, answer.Totals.AgentSessions, took)
	}
}

func TestTheAgentSessionsListRefusesABadRequest(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()

	for _, body := range []map[string]any{
		{},
		{"from": "2026-10-04T00:00:00Z", "to": "2026-10-01T00:00:00Z"},
		withFilters(map[string]any{"run": "maybe"}),
		withFilters(map[string]any{"limit": -1}),
		withFilters(map[string]any{"limit": 501}),
	} {
		if status, raw := postQueryApp(t, env, "/api/123/agent-sessions", body); status != 400 {
			t.Errorf("Expected 400 for %v, got %d: %s", body, status, raw)
		}
	}
}

// A line's time can be up to one hour after the time when the server
// stored it. An Agent session stored just before midnight, with its lines
// just after midnight, has its row on the day before them: a range that
// starts after midnight must still list it.
func TestTheAgentSessionsListGivesAnAgentSessionStoredOnTheDayBeforeItsLines(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	env.Clock.Set(time.Date(2026, 10, 2, 23, 30, 0, 0, time.UTC))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-03T00:20:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-03T00:20:05Z", "msg_1", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
	)}), 200)
	env.ProcessAgentFiles(t)

	answer := listAgentSessions(t, env, o.project, map[string]any{"from": "2026-10-03T00:10:00Z", "to": "2026-10-03T01:00:00Z"})
	if got := listedIDs(answer); !slices.Equal(got, []string{laptopSession}) {
		t.Errorf("Expected the Agent session stored on the day before its lines, got %v", got)
	}
}

// An old Agent session that uploads late, with a side file as a tool
// result: the side file has no lines, so only its stored time is known. The
// Agent session's times and its place in a range are the times of its
// lines, never the upload time.
func TestAnOldAgentSessionUploadedLateKeepsItsOwnTimesWithASideFile(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	env.Clock.Set(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
	)}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "tool-results/toolu_01.txt",
		contentHash: "sha256:aa", body: []byte("the output")}), 200)
	env.ProcessAgentFiles(t)

	october := listAgentSessions(t, env, o.project, map[string]any{"from": "2026-10-01T00:00:00Z", "to": "2026-10-04T00:00:00Z"})
	if len(october.AgentSessions) != 1 || october.AgentSessions[0].Last != "2026-10-02T11:00:05Z" ||
		october.AgentSessions[0].First != "2026-10-02T11:00:00Z" {
		t.Errorf("Expected the times of the lines, got %+v", october.AgentSessions)
	}
	upload := listAgentSessions(t, env, o.project, map[string]any{"from": "2026-10-19T00:00:00Z", "to": "2026-10-21T00:00:00Z"})
	if got := listedIDs(upload); len(got) != 0 {
		t.Errorf("Expected no Agent session in the range of the upload day, got %v", got)
	}
}
