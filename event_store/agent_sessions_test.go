package main_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// The totals of each Agent session (spec, Storage): agent_sessions has
// one row for each (Agent session, day of activity), which a read sums by
// Agent session id.

const forkSession = "e2a4c6d8-1b3f-4a5c-8e7d-9f0a1b2c3d4e"

// The usage of one request: 10 input, 50 output, 1000 cache read and 100
// cache write (5 minutes) tokens. At list prices, claude-opus-5-5 costs
// (10*4 + 50*20 + 1000*0.20 + 100*4*1.25) / 1e6 = 0.00174 USD and
// claude-sonnet-5-5 (10*2 + 50*10 + 1000*0.10 + 100*2*1.25) / 1e6 =
// 0.00087 USD.
const oneRequest = `{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":50,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":0}}`

func personAt(uuid, at, text string) string {
	return `{"type":"user","uuid":"` + uuid + `","timestamp":"` + at + `","message":{"role":"user","content":"` + text + `"}}`
}

func replyAt(uuid, at, msgID, model, block string) string {
	return `{"type":"assistant","uuid":"` + uuid + `","timestamp":"` + at + `","message":{"id":"` + msgID +
		`","role":"assistant","model":"` + model + `","content":[` + block + `],"usage":` + oneRequest + `}}`
}

// replyWithEffortAt is replyAt with the effort of the reply at the top
// level of the line, where Claude Code writes it next to perTurnEffort.
func replyWithEffortAt(uuid, at, msgID, model, effort, block string) string {
	return strings.Replace(replyAt(uuid, at, msgID, model, block), `{"type":"assistant",`,
		`{"type":"assistant","effort":"`+effort+`","perTurnEffort":"`+effort+`",`, 1)
}

func toolResultAt(uuid, at, toolUseID string) string {
	return `{"type":"user","uuid":"` + uuid + `","timestamp":"` + at + `","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + toolUseID + `","content":"ok"}]}}`
}

// costState is Claude Code's cost-state line, which has no timestamp.
func costState(total float64) string {
	return fmt.Sprintf(`{"type":"cost-state","sessionId":"x","totalCostUSD":%v}`, total)
}

// agentSessionTotals are the totals of the Agent Project's Agent sessions,
// by id, once the parse has caught up.
func agentSessionTotals(t *testing.T, env *TestEnv, projectID int64) map[string]storage.AgentSessionTotals {
	t.Helper()
	env.ProcessAgentFiles(t)
	all, err := env.ClickHouseLogs.AgentSessionTotals(context.Background(), uint32(projectID))
	if err != nil {
		t.Fatalf("Read the Agent session totals: %v", err)
	}
	totals := map[string]storage.AgentSessionTotals{}
	for _, s := range all {
		totals[s.SessionID.String()] = s
	}
	return totals
}

func sessionTotalsOf(t *testing.T, env *TestEnv, projectID int64, sessionID string) storage.AgentSessionTotals {
	t.Helper()
	s, ok := agentSessionTotals(t, env, projectID)[sessionID]
	if !ok {
		t.Fatalf("Expected totals of the Agent session %s", sessionID)
	}
	return s
}

func expectSessionCost(t *testing.T, what string, s storage.AgentSessionTotals, want float64) {
	t.Helper()
	if s.CostUSD == nil || math.Abs(*s.CostUSD-want) > 1e-12 {
		got := "no cost"
		if s.CostUSD != nil {
			got = fmt.Sprint(*s.CostUSD)
		}
		t.Errorf("%s: expected the cost %v USD, got %s", what, want, got)
	}
}

type sessionCounts struct {
	input, output, cacheRead, cacheWrite uint64
	person, turns, toolCalls             uint64
	subagents                            int
}

func countsOf(s storage.AgentSessionTotals) sessionCounts {
	return sessionCounts{s.Input, s.Output, s.CacheRead, s.CacheWrite, s.PersonMessages, s.Turns, s.ToolCalls, s.Subagents}
}

func TestAnAgentSessionOverTwoDaysSumsItsTokensCostAndCounts(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	env.Clock.Set(time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC))
	first := jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`),
		toolResultAt("u2", "2026-10-02T11:00:06Z", "toolu_1"),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	env.ProcessAgentFiles(t)

	// The next day the person resumes the Agent session: the same id, and
	// a subagent works for it.
	env.Clock.Set(time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC))
	second := jsonl(
		personAt("u3", "2026-10-03T09:00:00Z", "Now the checkout"),
		replyAt("a2", "2026-10-03T09:00:05Z", "msg_2", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: second}), 200)
	subagent := jsonl(
		personAt("s1", "2026-10-03T09:00:10Z", "Find the checkout code"),
		// A server-side tool call, such as a web search, is a tool call too.
		replyAt("s2", "2026-10-03T09:00:20Z", "msg_3", "claude-sonnet-5-5", `{"type":"server_tool_use","id":"srvtoolu_2","name":"web_search","input":{}}`),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-a1.jsonl", body: subagent}), 200)

	totals := agentSessionTotals(t, env, o.project)
	if len(totals) != 1 {
		t.Fatalf("Expected one Agent session, got %+v", totals)
	}
	s := totals[laptopSession]
	want := sessionCounts{input: 30, output: 150, cacheRead: 3000, cacheWrite: 300, person: 2, turns: 2, toolCalls: 2, subagents: 1}
	if got := countsOf(s); got != want {
		t.Errorf("Expected %+v, got %+v", want, got)
	}
	expectSessionCost(t, "the price-table sum", s, 0.00174+0.00174+0.00087)
	if !s.First.Equal(time.Date(2026, 10, 2, 11, 0, 0, 0, time.UTC)) || !s.Last.Equal(time.Date(2026, 10, 3, 9, 0, 20, 0, time.UTC)) {
		t.Errorf("Expected the times of the first and the last line, got %s and %s", s.First, s.Last)
	}
	// The subagent's model is not the model of the Agent session.
	if s.Model != "claude-opus-5-5" || s.Harness != "claude-code" || s.Repository != "github.com/acme/shop" ||
		s.UserID != o.user || s.RunID != "" || s.ParentSessionID != "" || len(s.Flags) != 0 {
		t.Errorf("Expected the model of the main transcript's last reply, who and where, and no flags, got %+v", s)
	}
}

// The effort of an Agent session is the effort of the latest reply in its
// main transcript, as its model is. A later reply with no effort, as from
// a version of Claude Code before effort, gives none.
func TestTheEffortOfAnAgentSessionIsTheEffortOfTheLatestReplyOfItsMainTranscript(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	first := jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyWithEffortAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", "high", `{"type":"text","text":"Done."}`),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	if s := sessionTotalsOf(t, env, o.project, laptopSession); s.Effort != "high" || s.Model != "claude-opus-5-5" {
		t.Errorf("Expected the effort and the model of the one reply, got %q and %q", s.Effort, s.Model)
	}

	second := jsonl(replyWithEffortAt("a2", "2026-10-02T11:01:00Z", "msg_2", "claude-opus-5-5", "xhigh", `{"type":"text","text":"More."}`))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: second}), 200)
	// A subagent's reply is not a reply of the main transcript.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-a1.jsonl", body: jsonl(
		replyWithEffortAt("s1", "2026-10-02T11:02:00Z", "msg_3", "claude-sonnet-5-5", "low", `{"type":"text","text":"Found it."}`))}), 200)
	if s := sessionTotalsOf(t, env, o.project, laptopSession); s.Effort != "xhigh" {
		t.Errorf("Expected the effort of the main transcript's latest reply, got %q", s.Effort)
	}

	third := jsonl(replyAt("a3", "2026-10-02T11:03:00Z", "msg_4", "claude-opus-5-5", `{"type":"text","text":"Last."}`))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first) + len(second), body: third}), 200)
	if s := sessionTotalsOf(t, env, o.project, laptopSession); s.Effort != "" || s.Model != "claude-opus-5-5" {
		t.Errorf("Expected no effort after a latest reply with none, and its model, got %q and %q", s.Effort, s.Model)
	}
}

// An install from before effort gets the effort column at its next start.
// An Agent session parsed before it has no effort, until new lines of it
// are parsed: the parse does not parse it again.
func TestAnInstallFromBeforeEffortGetsTheEffortColumnAtItsNextStart(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	first := jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyWithEffortAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", "high", `{"type":"text","text":"Done."}`),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	env.ProcessAgentFiles(t)

	// The database as an install from before effort has it: no effort
	// column, and no record of the step that adds it.
	ctx := context.Background()
	for _, statement := range []string{
		`ALTER TABLE agent_sessions DROP COLUMN last_reply_effort`,
		`DELETE FROM schema_migrations WHERE version = 11203`,
	} {
		if err := env.ClickHouse.Conn().Exec(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	env.Reopen(t)

	if s := sessionTotalsOf(t, env, o.project, laptopSession); s.Effort != "" || s.Model != "claude-opus-5-5" {
		t.Errorf("Expected no effort for the Agent session parsed before the start, and its model, got %q and %q", s.Effort, s.Model)
	}
	second := jsonl(replyWithEffortAt("a2", "2026-10-02T11:01:00Z", "msg_2", "claude-opus-5-5", "max", `{"type":"text","text":"More."}`))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: second}), 200)
	if s := sessionTotalsOf(t, env, o.project, laptopSession); s.Effort != "max" {
		t.Errorf("Expected the effort of a reply parsed after the start, got %q", s.Effort)
	}
}

func TestTheTotalsOfARunsAgentSessionCarryTheRun(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	runSecret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization, scopes: []string{"sessions:write"}, sessionIDs: []string{runSession}})

	expectStatus(t, uploadUnit(t, env, runSecret, runSession, unitUpload{path: "session.jsonl", folder: "apps/web", runID: "run_42", project: "shop", body: jsonl(personAt("u1", "2026-10-02T11:00:00Z", "Fix the bug"))}), 200)
	s := sessionTotalsOf(t, env, o.project, runSession)
	if s.RunID != "run_42" || s.Project != "shop" || s.Folder != "apps/web" {
		t.Errorf("Expected the Run, its Project and the folder, got %+v", s)
	}
	// A main transcript with no reply and no cost-state line has no cost,
	// never 0.
	if s.CostUSD != nil {
		t.Errorf("Expected no cost without a reply or a cost-state line, got %v", *s.CostUSD)
	}
}

func TestTheCostIsTheLastCostStateWhenItIsLaterThanTheLastReply(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	first := jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
		costState(0.5),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	expectSessionCost(t, "after the cost-state line", sessionTotalsOf(t, env, o.project, laptopSession), 0.5)

	// A reply after the cost-state line: the price-table sum.
	second := jsonl(replyAt("a2", "2026-10-02T11:01:00Z", "msg_2", "claude-opus-5-5", `{"type":"text","text":"More."}`))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: second}), 200)
	expectSessionCost(t, "after a later reply", sessionTotalsOf(t, env, o.project, laptopSession), 0.00174*2)

	// A model with no price gives no cost, never 0.
	expectStatus(t, uploadUnit(t, env, o.secret, otherSession, unitUpload{path: "session.jsonl", body: jsonl(
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-unknown-1", `{"type":"text","text":"Done."}`))}), 200)
	s := sessionTotalsOf(t, env, o.project, otherSession)
	if s.CostUSD != nil || s.Output != 50 {
		t.Errorf("Expected the tokens and no cost, got %+v", s)
	}
}

func TestTheTranscriptsOfTheRunModelCheckGiveTheirSessionTotals(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	// The numbers are each transcript's last cost-state line, which Claude
	// Code wrote, and its person messages.
	cases := []struct {
		name, session string
		want          sessionCounts
		cost          float64
	}{
		{"mid-turn-message.jsonl", "9d3b0d78-a7ce-4152-ba06-ce61683e5ef7", sessionCounts{input: 8, output: 384, cacheRead: 66361, cacheWrite: 7511, person: 2, turns: 1}, 0.0023586099999999997},
		{"three-turns.jsonl", "49a7965a-2a6a-4c88-9459-2d53d525ac50", sessionCounts{input: 16, output: 1910, cacheRead: 150770, cacheWrite: 10451, person: 3, turns: 3}, 0.004554500000000001},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			uploadFixture(t, env, o, c.name, c.session)
			s := sessionTotalsOf(t, env, o.project, c.session)
			got := countsOf(s)
			got.toolCalls, got.subagents = 0, 0
			if got != c.want {
				t.Errorf("Expected %+v, got %+v", c.want, got)
			}
			expectSessionCost(t, "the cost-state total", s, c.cost)
		})
	}
}

func TestAForkWithARecordedParentIsANewAgentSessionWithItsParent(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	body := jsonl(personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: body}), 200)
	// A resume appends to the same Agent session.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(body), body: jsonl(personAt("u2", "2026-10-02T12:00:00Z", "Go on"))}), 200)
	// A fork or a /clear is a new id, which the uploader links to its parent.
	expectStatus(t, uploadUnit(t, env, o.secret, forkSession, unitUpload{path: "session.jsonl", parent: laptopSession, body: body}), 200)

	totals := agentSessionTotals(t, env, o.project)
	if len(totals) != 2 {
		t.Fatalf("Expected two Agent sessions, got %+v", totals)
	}
	if s := totals[laptopSession]; s.PersonMessages != 2 || s.ParentSessionID != "" {
		t.Errorf("Expected the resumed Agent session with both messages and no parent, got %+v", s)
	}
	if s := totals[forkSession]; s.PersonMessages != 1 || s.ParentSessionID != laptopSession {
		t.Errorf("Expected the fork with its parent %s, got %+v", laptopSession, s)
	}
	_, files := offsetsOf(t, env, o.secret, forkSession)
	if files.AgentSession == nil || files.AgentSession.ParentSessionID != laptopSession {
		t.Errorf("Expected the offsets call to give the parent, got %+v", files.AgentSession)
	}

	for _, parent := range []string{"not-a-uuid", forkSession} {
		answer := uploadUnit(t, env, o.secret, forkSession, unitUpload{path: "session.jsonl", offset: len(body), parent: parent, body: body})
		expectStatus(t, answer, 400)
	}
}

func TestTheFlagsCutAndTrimmedShowOnTheTotals(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	// A tool result over 32 MiB is stored cut.
	const mib = 1 << 20
	big := bytes.Repeat([]byte("r"), 32*mib)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "tool-results/big.txt", contentHash: "sha256:big", size: 40 * mib, body: big}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(personAt("u1", "2026-10-02T11:00:00Z", "Run it"))}), 200)

	// A transcript that is shorter than before is a new version.
	long := jsonl(personAt("u1", "2026-10-02T11:00:00Z", "One"), personAt("u2", "2026-10-02T11:00:01Z", "Two"))
	expectStatus(t, uploadUnit(t, env, o.secret, otherSession, unitUpload{path: "session.jsonl", body: long}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, otherSession, unitUpload{path: "session.jsonl", version: 2, body: jsonl(personAt("u3", "2026-10-02T11:00:02Z", "Three"))}), 200)

	totals := agentSessionTotals(t, env, o.project)
	if got := totals[laptopSession].Flags; !slices.Equal(got, []string{"cut"}) {
		t.Errorf("Expected the flag cut, got %v", got)
	}
	if got := totals[otherSession].Flags; !slices.Equal(got, []string{"trimmed"}) {
		t.Errorf("Expected the flag trimmed, got %v", got)
	}
}

func TestTheParseStoppedBetweenTheInsertAndTheCursorDoesNotChangeTheTotals(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	first := jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	if err := env.AgentFileProcessor.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}

	// The parse inserts the Logs and the totals, and EventStore stops
	// before it moves its cursor: the range is parsed again at the start.
	crashed := make(chan struct{})
	env.FlakyLogs.failAfterInsert(connectionLost, func() {
		env.FlakyLogs.goDown()
		close(crashed)
	})
	done := make(chan error, 1)
	go func() { done <- env.AgentParser.ProcessUntilCaughtUp() }()
	select {
	case <-crashed:
	case <-time.After(10 * time.Second):
		t.Fatalf("Expected the parse to insert the range")
	}
	env.Restart(t)
	<-done
	env.FlakyLogs.recover()

	s := sessionTotalsOf(t, env, o.project, laptopSession)
	want := sessionCounts{input: 10, output: 50, cacheRead: 1000, cacheWrite: 100, person: 1, turns: 1, toolCalls: 1}
	if got := countsOf(s); got != want {
		t.Errorf("Expected %+v once, got %+v", want, got)
	}
	expectSessionCost(t, "once", s, 0.00174)
}

func TestRetentionDropsTheTotalsOfTheSameDaysAsTheRawTable(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	consoleExec(t, env, "UPDATE projects SET log_retention_days = 2 WHERE id = ?", o.project)
	// With no Project left, the cleanup deletes nothing.
	o.rows.project(o.organization)

	at := func(day int) time.Time { return time.Date(2026, 10, day, 12, 0, 0, 0, time.UTC) }
	offset := 0
	for _, day := range []int{1, 3, 4} {
		env.Clock.Set(at(day))
		body := jsonl(personAt(fmt.Sprintf("u%d", day), at(day).Format(time.RFC3339), fmt.Sprintf("October %d", day)))
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: offset, body: body}), 200)
		offset += len(body)
		env.ProcessAgentFiles(t)
	}
	if s := sessionTotalsOf(t, env, o.project, laptopSession); s.PersonMessages != 3 || len(s.Flags) != 0 {
		t.Fatalf("Expected three person messages and no flags at first, got %+v", s)
	}

	// Retention of 2 days on October 6th keeps October 4th: the start of
	// the file is gone, so the Agent session is partial.
	env.Clock.Set(at(6))
	env.DeleteLogsPastRetention(t)
	s := sessionTotalsOf(t, env, o.project, laptopSession)
	if s.PersonMessages != 1 || !s.First.Equal(at(4)) || !slices.Equal(s.Flags, []string{"partial"}) {
		t.Errorf("Expected the totals of October 4th, flagged partial, got %+v", s)
	}

	consoleExec(t, env, "DELETE FROM projects WHERE id = ?", o.project)
	env.DeleteLogsPastRetention(t)
	if totals := agentSessionTotals(t, env, o.project); len(totals) != 0 {
		t.Errorf("Expected no totals after the Agent Project's deletion, got %+v", totals)
	}
}

func TestTheCostStateIsLaterThanTheRepliesBeforeItInTheFile(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	// The replies are 1 ms out of order, as Claude Code writes them: the
	// cost-state line after them is still later than the last reply.
	body := jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05.110Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`),
		toolResultAt("u2", "2026-10-02T11:00:05.109Z", "toolu_1"),
		costState(0.5),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: body}), 200)
	expectSessionCost(t, "the cost-state after a reply that is out of order", sessionTotalsOf(t, env, o.project, laptopSession), 0.5)
}

func TestACostStateThatStartsAUnitIsNotLaterThanTheRepliesAfterIt(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	env.Clock.Set(time.Date(2026, 10, 2, 11, 0, 10, 0, time.UTC))
	first := jsonl(
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	env.ProcessAgentFiles(t)

	// An upload cut before the cost-state line, and the server stores the
	// next units late: the cost-state line has the stored time, which is
	// after the time of the reply that comes after it in the file.
	env.Clock.Set(time.Date(2026, 10, 2, 11, 10, 0, 0, time.UTC))
	second := jsonl(costState(0.5))
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: second}), 200)
	third := jsonl(
		personAt("u2", "2026-10-02T11:01:00Z", "Go on"),
		replyAt("a2", "2026-10-02T11:01:05Z", "msg_2", "claude-opus-5-5", `{"type":"text","text":"More."}`),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first) + len(second), body: third}), 200)
	expectSessionCost(t, "a reply after the cost-state line", sessionTotalsOf(t, env, o.project, laptopSession), 0.00174*2)
}

func TestTheTotalsOfATrimmedTranscriptCountItsNewVersionOnly(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	start := []string{
		personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"),
		replyAt("a1", "2026-10-02T11:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`),
	}
	long := jsonl(append(start, personAt("u2", "2026-10-02T11:01:00Z", "Go on"))...)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: long}), 200)
	env.ProcessAgentFiles(t)
	// The transcript is trimmed: version 2 repeats its start.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", version: 2, body: jsonl(start...)}), 200)

	s := sessionTotalsOf(t, env, o.project, laptopSession)
	want := sessionCounts{input: 10, output: 50, cacheRead: 1000, cacheWrite: 100, person: 1, turns: 1, toolCalls: 1}
	if got := countsOf(s); got != want {
		t.Errorf("Expected the counts of version 2 alone, %+v, got %+v", want, got)
	}
	expectSessionCost(t, "version 2 alone", s, 0.00174)
	if !slices.Equal(s.Flags, []string{"trimmed"}) {
		t.Errorf("Expected the flag trimmed, got %v", s.Flags)
	}
}

func TestAParentSentOnALaterUploadIsKept(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	body := jsonl(personAt("u1", "2026-10-02T11:00:00Z", "Add a cart page"))
	expectStatus(t, uploadUnit(t, env, o.secret, forkSession, unitUpload{path: "session.jsonl", body: body}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, forkSession, unitUpload{path: "session.jsonl", offset: len(body), parent: laptopSession, body: jsonl(personAt("u2", "2026-10-02T11:01:00Z", "Go on"))}), 200)

	if s := sessionTotalsOf(t, env, o.project, forkSession); s.ParentSessionID != laptopSession {
		t.Errorf("Expected the parent %s on the totals, got %+v", laptopSession, s)
	}
	_, files := offsetsOf(t, env, o.secret, forkSession)
	if files.AgentSession == nil || files.AgentSession.ParentSessionID != laptopSession {
		t.Errorf("Expected the offsets call to give the parent, got %+v", files.AgentSession)
	}
}

func TestLineTimesFarFromTheStoredTimeGiveTheStoredTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	stored := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	env.Clock.Set(stored)
	// A laptop with a wrong clock, or a broken transcript.
	body := jsonl(
		personAt("u1", "0001-01-01T00:00:00Z", "Add a cart page"),
		replyAt("a1", "1960-01-01T00:00:05Z", "msg_1", "claude-opus-5-5", `{"type":"text","text":"Done."}`),
		personAt("u2", "2999-01-01T00:00:00Z", "Go on"),
	)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: body}), 200)
	s := sessionTotalsOf(t, env, o.project, laptopSession)
	if !s.First.Equal(stored) || !s.Last.Equal(stored) {
		t.Errorf("Expected the stored time %s as first and last, got %s and %s", stored, s.First, s.Last)
	}
	expectSessionCost(t, "a reply with an old time", s, 0.00174)
}
