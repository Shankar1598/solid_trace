package main_test

import (
	"math"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The totals call of the agent ingest API: the Dispatcher's closing event
// of a Run carries the sum of the agent_sessions rows of the Run's Agent
// sessions. It reads them with a child token that has sessions:read for
// the Run's Agent session ids, and the call sums only ids of that list.

// takeOverSession is the Agent session after a /clear in a take-over:
// the interactive harness writes its transcript, and no output stream.
const takeOverSession = "5a8c1e2f-3b4d-4e6f-9a0b-1c2d3e4f5a6b"

func agentSessionTotalsPath(ids ...string) string {
	return "/api/agent/v1/agent-session-totals?agent_session_ids=" + url.QueryEscape(strings.Join(ids, ","))
}

// sessionTotalsAnswer calls the totals call with secret for ids.
func sessionTotalsAnswer(t *testing.T, env *TestEnv, secret string, ids ...string) agentAnswer {
	t.Helper()
	return callAgentAPI(t, env.App, agentSessionTotalsPath(ids...), map[string]string{"Authorization": "Bearer " + secret})
}

func numberIn(t *testing.T, answer agentAnswer, name string) float64 {
	t.Helper()
	n, ok := answer.body[name].(float64)
	if !ok {
		t.Fatalf("Expected a number %s in %s", name, answer.raw)
	}
	return n
}

func TestAgentSessionTotalsOfARun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	expiry := env.Clock.Now().Add(3 * time.Hour)
	child := func(scopes []string, ids ...string) string {
		_, parent := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: dispatcherScopes,
			expiresAt: &expiry})
		secret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: scopes,
			sessionIDs: ids, expiresAt: &expiry, parent: parent})
		return secret
	}
	upload := child([]string{"sessions:write"}, runSession, takeOverSession, otherSession)
	read := []string{"sessions:read"}

	// The headless Execution's Agent session: two replies of claude-opus-5-5.
	headless := jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Fix the cart"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`),
		toolResultAt("u2", "2026-10-02T11:00:06Z", "toolu_1"),
		replyAt("a2", "2026-10-02T11:00:09Z", "msg_2", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
	)
	// The take-over's Agent session: one reply of claude-sonnet-5-5.
	interactive := jsonl(
		personAt("u3", "2026-10-02T11:30:00Z", "Now the checkout"),
		replyAt("a3", "2026-10-02T11:30:05Z", "msg_3", "claude-sonnet-5-5", `{"type":"text","text":"Done too."}`),
	)
	run := func(body []byte) unitUpload {
		return unitUpload{path: "session.jsonl", runID: "run-0193", project: "shop", body: body}
	}
	expectStatus(t, uploadUnit(t, env, upload, runSession, run(headless)), 200)
	expectStatus(t, uploadUnit(t, env, upload, takeOverSession, run(interactive)), 200)
	// Another Run's Agent session.
	expectStatus(t, uploadUnit(t, env, upload, otherSession, unitUpload{path: "session.jsonl", runID: "run-0200",
		project: "shop", body: jsonl(replyAt("b1", "2026-10-02T11:40:00Z", "msg_9", "claude-opus-5-5", `{"type":"text","text":"x"}`))}), 200)

	t.Run("before the parse, the answer says that the totals are not complete", func(t *testing.T) {
		answer := sessionTotalsAnswer(t, env, child(read, runSession, takeOverSession), runSession, takeOverSession)
		expectStatus(t, answer, 200)
		if answer.body["complete"] != false {
			t.Errorf("Expected complete false before the parse, got %s", answer.raw)
		}
	})

	t.Run("the sum of the Run's agent_sessions rows, the take-over's among them", func(t *testing.T) {
		totals := agentSessionTotals(t, env, o.project)
		answer := sessionTotalsAnswer(t, env, child(read, runSession, takeOverSession), runSession, takeOverSession)
		expectStatus(t, answer, 200)
		h, i := totals[runSession], totals[takeOverSession]
		// Three requests of oneRequest: 10 input, 50 output, 1000 cache
		// read and 100 cache write tokens each.
		want := map[string]float64{"agent_sessions": 2, "input_tokens": 30, "output_tokens": 150,
			"cache_read_tokens": 3000, "cache_write_tokens": 300, "no_cost": 0}
		for name, n := range want {
			if got := numberIn(t, answer, name); got != n {
				t.Errorf("Expected %s %v, got %v", name, n, got)
			}
		}
		if h.Input+i.Input != 30 || h.CacheRead+i.CacheRead != 3000 {
			t.Errorf("The agent_sessions rows do not hold the tokens of the replies: %+v %+v", h, i)
		}
		if cost := numberIn(t, answer, "cost_usd"); math.Abs(cost-(0.00174+0.00174+0.00087)) > 1e-12 ||
			math.Abs(cost-(*h.CostUSD+*i.CostUSD)) > 1e-12 {
			t.Errorf("Expected the cost of the two Agent sessions, got %v", cost)
		}
		if answer.body["complete"] != true {
			t.Errorf("Expected complete true after the parse, got %s", answer.raw)
		}
	})

	t.Run("only the asked ids count, and an id with no upload adds nothing", func(t *testing.T) {
		answer := sessionTotalsAnswer(t, env, child(read, runSession, laptopSession), runSession, laptopSession)
		expectStatus(t, answer, 200)
		if numberIn(t, answer, "agent_sessions") != 1 || numberIn(t, answer, "input_tokens") != 20 ||
			answer.body["complete"] != true {
			t.Errorf("Expected the headless Agent session alone, got %s", answer.raw)
		}
	})

	t.Run("an id that is not in the token's list is refused", func(t *testing.T) {
		secret := child(read, runSession, takeOverSession)
		for _, ids := range [][]string{{otherSession}, {runSession, otherSession}} {
			answer := sessionTotalsAnswer(t, env, secret, ids...)
			expectStatus(t, answer, 403)
			if msg, _ := answer.body["error"].(string); !strings.Contains(msg, otherSession) {
				t.Errorf("Expected a body that names %s, got %s", otherSession, answer.raw)
			}
		}
	})

	t.Run("a token with no sessions:read, or with no list, is refused", func(t *testing.T) {
		expectStatus(t, sessionTotalsAnswer(t, env, upload, runSession), 403)
		noList, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
			scopes: []string{"sessions:read"}})
		expectStatus(t, sessionTotalsAnswer(t, env, noList, runSession), 403)
		expectStatus(t, sessionTotalsAnswer(t, env, o.secret, runSession), 403)
		expectStatus(t, sessionTotalsAnswer(t, env, "st_unknown", runSession), 401)
	})

	t.Run("a call with no ids, or with an id that is not a UUID, is refused", func(t *testing.T) {
		secret := child(read, runSession)
		expectStatus(t, callAgentAPI(t, env.App, "/api/agent/v1/agent-session-totals",
			map[string]string{"Authorization": "Bearer " + secret}), 400)
		expectStatus(t, sessionTotalsAnswer(t, env, secret, "not-a-uuid"), 400)
	})
}
