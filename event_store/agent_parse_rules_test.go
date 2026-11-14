package main_test

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The parse rules (spec, Storage): the tokens of a reply once, its
// estimated cost at list prices, and who sent each message.

// reply is one assistant line of the reply msgID, with the usage that
// Claude Code repeats on each line of the reply.
func reply(uuid, msgID, model, block, usage string) string {
	return `{"type":"assistant","uuid":"` + uuid + `","timestamp":"2026-10-02T11:00:05Z","message":{"id":"` + msgID +
		`","role":"assistant","model":"` + model + `","content":[` + block + `],"usage":` + usage + `}}`
}

func number(a map[string]any, name string) (float64, bool) {
	v, ok := a[name].(float64)
	return v, ok
}

func expectCost(t *testing.T, what string, a map[string]any, want float64) {
	t.Helper()
	cost, ok := number(a, "agent.cost_usd")
	if !ok || math.Abs(cost-want) > 1e-12 {
		t.Errorf("%s: expected the estimated cost %v USD, got %v (%v)", what, want, a["agent.cost_usd"], ok)
	}
}

var tokenAttributes = []string{"agent.tokens.input", "agent.tokens.output", "agent.tokens.cache_read", "agent.tokens.cache_write", "agent.cost_usd"}

func TestTheTokensOfAReplyOfSeveralLinesAreOnItsLastLineOnly(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	usage := `{"input_tokens":10,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":50,"cache_creation":{"ephemeral_5m_input_tokens":100,"ephemeral_1h_input_tokens":0}}`
	lines := []string{
		reply("a1", "msg_r", "claude-opus-5-5", `{"type":"thinking","thinking":"Look at the cart."}`, usage),
		reply("a2", "msg_r", "claude-opus-5-5", `{"type":"text","text":"I read the cart."}`, usage),
		reply("a3", "msg_r", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`, usage),
		`{"type":"user","uuid":"u2","timestamp":"2026-10-02T11:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"package cart"}]}}`,
	}
	// The reply goes over two units, which one parse reads.
	first := jsonl(lines[:2]...)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: jsonl(lines[2:]...)}), 200)

	logs := sessionLogs(t, env, o.project, laptopSession)
	if len(logs) != len(lines) {
		t.Fatalf("Expected %d Logs, got %d: %+v", len(lines), len(logs), logs)
	}
	for i, log := range logs {
		if i == 2 {
			continue
		}
		for _, name := range tokenAttributes {
			if _, ok := log.Attributes[name]; ok {
				t.Errorf("Log %d: expected no %s, as it is not the last line of its reply, got %+v", i, name, log.Attributes)
			}
		}
	}
	last := logs[2].Attributes
	for name, want := range map[string]float64{"agent.tokens.input": 10, "agent.tokens.output": 50, "agent.tokens.cache_read": 1000, "agent.tokens.cache_write": 100} {
		if got, ok := number(last, name); !ok || got != want {
			t.Errorf("The last line of the reply: expected %s %v, got %v", name, want, last[name])
		}
	}
	// Claude Opus 5.5: $4 input, $20 output, $0.20 cache read and $5 a
	// 5-minute cache write, for each million tokens.
	expectCost(t, "The last line of the reply", last, 10*4e-6+50*20e-6+1000*0.2e-6+100*5e-6)
}

func TestAReplyThatTheParseReadsInTwoRangesHasItsTokensOnce(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	usage := func(output, cacheWrite int) string {
		return fmt.Sprintf(`{"input_tokens":10,"cache_creation_input_tokens":%d,"cache_read_input_tokens":1000,"output_tokens":%d}`, cacheWrite, output)
	}
	// The wrapper uploads every few seconds, and the parse runs after each
	// upload, between two lines of one reply. The second reply's last line
	// has more output and cache-write tokens than its first line.
	ranges := [][]string{
		{reply("a1", "msg_r", "claude-opus-5-5", `{"type":"text","text":"I read the cart."}`, usage(50, 0))},
		{
			reply("a2", "msg_r", "claude-opus-5-5", `{"type":"tool_use","id":"toolu_1","name":"Read","input":{}}`, usage(50, 0)),
			`{"type":"user","uuid":"u1","timestamp":"2026-10-02T11:00:06Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"package cart"}]}}`,
			reply("a3", "msg_s", "claude-opus-5-5", `{"type":"text","text":"The cart"}`, usage(20, 100)),
		},
		{reply("a4", "msg_s", "claude-opus-5-5", `{"type":"text","text":"is fine."}`, usage(30, 300))},
	}
	offset := 0
	var logs []listedLog
	for _, lines := range ranges {
		body := jsonl(lines...)
		expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: offset, body: body}), 200)
		offset += len(body)
		logs = sessionLogs(t, env, o.project, laptopSession)
	}
	if len(logs) != 5 {
		t.Fatalf("Expected 5 Logs, got %+v", logs)
	}
	sums := map[string]map[string]float64{}
	withTokens := 0
	for _, log := range logs {
		id, _ := log.Attributes["agent.message_id"].(string)
		if _, ok := log.Attributes["agent.tokens.output"]; ok {
			withTokens++
		}
		if sums[id] == nil {
			sums[id] = map[string]float64{}
		}
		for _, name := range tokenAttributes {
			v, _ := number(log.Attributes, name)
			sums[id][name] += v
		}
	}
	// msg_r has its tokens once, on its first line; msg_s on both lines,
	// 20 and then 10 more output tokens, 100 and then 200 more cache-write
	// tokens.
	if withTokens != 3 {
		t.Errorf("Expected 3 Logs with tokens, got %d: %+v", withTokens, logs)
	}
	for id, last := range map[string]struct{ output, cacheWrite float64 }{"msg_r": {50, 0}, "msg_s": {30, 300}} {
		output := last.output
		want := map[string]float64{"agent.tokens.input": 10, "agent.tokens.output": output, "agent.tokens.cache_read": 1000, "agent.tokens.cache_write": last.cacheWrite}
		for name, v := range want {
			if sums[id][name] != v {
				t.Errorf("%s: expected %v %s in all, got %v", id, v, name, sums[id][name])
			}
		}
		// Claude Opus 5.5: $4 input, $20 output, $0.20 cache read and $5 a
		// 5-minute cache write.
		if cost := 10*4e-6 + output*20e-6 + 1000*0.2e-6 + last.cacheWrite*5e-6; math.Abs(sums[id]["agent.cost_usd"]-cost) > 1e-12 {
			t.Errorf("%s: expected the cost %v USD in all, got %v", id, cost, sums[id]["agent.cost_usd"])
		}
	}
}

func TestAModelWithNoPriceHasItsTokensAndNoCost(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	usage := `{"input_tokens":1000,"cache_creation_input_tokens":0,"cache_read_input_tokens":0,"output_tokens":100}`
	lines := []string{
		reply("a1", "msg_1", "claude-unknown-9", `{"type":"text","text":"Hello"}`, usage),
		// A model id with a date has the price of its model.
		reply("a2", "msg_2", "claude-haiku-4-5-20251001", `{"type":"text","text":"Hello"}`, usage),
	}
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(lines...)}), 200)

	logs := sessionLogs(t, env, o.project, laptopSession)
	if len(logs) != 2 {
		t.Fatalf("Expected 2 Logs, got %+v", logs)
	}
	unknown := logs[0].Attributes
	if got, _ := number(unknown, "agent.tokens.input"); got != 1000 {
		t.Errorf("Expected the tokens of a model with no price, got %+v", unknown)
	}
	if cost, ok := unknown["agent.cost_usd"]; ok {
		t.Errorf("Expected no cost for a model with no price, never 0, got %v", cost)
	}
	// Claude Haiku 4.5: $1 input and $5 output for each million tokens.
	expectCost(t, "A dated Claude Haiku 4.5", logs[1].Attributes, 1000*1e-6+100*5e-6)
}

func TestASyntheticReplyHasNoTokensAndNoCost(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	// Claude Code writes a synthetic reply itself, as when a request
	// fails. Its usage is not a request's.
	usage := `{"input_tokens":1000,"cache_creation_input_tokens":100,"cache_read_input_tokens":1000,"output_tokens":100}`
	lines := []string{reply("a1", "msg_syn", "<synthetic>", `{"type":"text","text":"API Error"}`, usage)}
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(lines...)}), 200)

	logs := sessionLogs(t, env, o.project, laptopSession)
	if len(logs) != 1 {
		t.Fatalf("Expected 1 Log, got %+v", logs)
	}
	for _, name := range tokenAttributes {
		if v, ok := logs[0].Attributes[name]; ok {
			t.Errorf("Expected no %s on a synthetic reply, got %v", name, v)
		}
	}
}

func TestWhoSentEachUserLine(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)

	main := []string{
		`{"type":"user","uuid":"u1","timestamp":"2026-10-02T11:00:00Z","message":{"role":"user","content":"Add a cart page"}}`,
		`{"type":"user","uuid":"u2","timestamp":"2026-10-02T11:00:01Z","message":{"role":"user","content":[{"type":"text","text":"Use the shop style"}]}}`,
		`{"type":"user","uuid":"u3","isMeta":true,"timestamp":"2026-10-02T11:00:02Z","message":{"role":"user","content":"Stop hook feedback: run the report"}}`,
		// The shapes of real transcripts: a person's message with its origin,
		// the notice of a background task's end as a user line and as a
		// queued message, and a subagent's hand-back.
		`{"type":"user","uuid":"u4","timestamp":"2026-10-02T11:00:02.1Z","origin":{"kind":"human"},"promptSource":"typed","message":{"role":"user","content":"Run the tests"}}`,
		`{"type":"user","uuid":"u5","timestamp":"2026-10-02T11:00:02.2Z","origin":{"kind":"task-notification"},"promptSource":"system","message":{"role":"user","content":"<task-notification>\n<task-id>b1</task-id>\n<status>completed</status>\n</task-notification>"}}`,
		`{"type":"user","uuid":"u6","timestamp":"2026-10-02T11:00:02.3Z","message":{"role":"user","content":"<task-notification>\n<task-id>b2</task-id>\n</task-notification>"}}`,
		`{"type":"attachment","uuid":"q1","timestamp":"2026-10-02T11:00:02.4Z","attachment":{"type":"queued_command","prompt":"Also check the footer","source_uuid":"c1","commandMode":"prompt","origin":{"kind":"human"}}}`,
		`{"type":"attachment","uuid":"q2","timestamp":"2026-10-02T11:00:02.5Z","attachment":{"type":"queued_command","prompt":"<task-notification>\n<task-id>b3</task-id>\n</task-notification>","source_uuid":"c2","commandMode":"task-notification","origin":{"kind":"task-notification","producer":"session-task"}}}`,
		`{"type":"attachment","uuid":"q3","timestamp":"2026-10-02T11:00:02.6Z","attachment":{"type":"queued_command","prompt":"<task-notification>\n<task-id>b4</task-id>\n</task-notification>","source_uuid":"c3","commandMode":"task-notification"}}`,
		// A queued notice that only its commandMode marks.
		`{"type":"attachment","uuid":"q5","timestamp":"2026-10-02T11:00:02.65Z","attachment":{"type":"queued_command","prompt":"Background task b5 is done","source_uuid":"c5","commandMode":"task-notification"}}`,
		`{"type":"attachment","uuid":"q4","timestamp":"2026-10-02T11:00:02.7Z","attachment":{"type":"queued_command","prompt":"<agent-message from=\"a0\">The report</agent-message>","source_uuid":"c4","commandMode":"prompt","isMeta":true,"origin":{"kind":"peer","from":"a0"}}}`,
		// A sidechain line, as a subagent's prompt, in a file that is not
		// under subagents/.
		`{"type":"user","uuid":"u7","isSidechain":true,"timestamp":"2026-10-02T11:00:02.8Z","message":{"role":"user","content":"Find the cart code"}}`,
	}
	subagent := []string{
		`{"type":"user","uuid":"s1","isSidechain":true,"timestamp":"2026-10-02T11:00:03Z","message":{"role":"user","content":"Find the cart code"}}`,
		`{"type":"user","uuid":"s2","isSidechain":true,"timestamp":"2026-10-02T11:00:04Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"cart.go"}]}}`,
	}
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: jsonl(main...)}), 200)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "subagents/agent-a1.jsonl", body: jsonl(subagent...)}), 200)

	logs := sessionLogs(t, env, o.project, laptopSession)
	want := []string{"person", "person", "not from a person", "person", "not from a person", "not from a person",
		"person", "not from a person", "not from a person", "not from a person", "not from a person", "not from a person",
		"not from a person", "tool"}
	if len(logs) != len(want) {
		t.Fatalf("Expected %d Logs, got %+v", len(want), logs)
	}
	for i, log := range logs {
		if role, _ := log.Attributes["agent.role"].(string); role != want[i] {
			t.Errorf("Log %d (%q): expected the role %q, got %q", i, log.Message, want[i], role)
		}
	}
}

// The transcripts of the run model check (ticket 25 of the factory map),
// reduced to the fields that the parse reads, with the text of tool output
// and hook feedback removed.
func uploadFixture(t *testing.T, env *TestEnv, o agentOrganization, name, sessionID string) []listedLog {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "agent_transcripts", name))
	if err != nil {
		t.Fatal(err)
	}
	expectStatus(t, uploadUnit(t, env, o.secret, sessionID, unitUpload{path: sessionID + ".jsonl", body: body}), 200)
	return sessionLogs(t, env, o.project, sessionID)
}

func TestAMessageDeliveredInTheMiddleOfATurnIsAPersonsMessageAtItsDeliveryTime(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	logs := uploadFixture(t, env, o, "mid-turn-message.jsonl", "9d3b0d78-a7ce-4152-ba06-ce61683e5ef7")
	var found []listedLog
	for _, log := range logs {
		if log.Message == "Also reply with the word BANANA." {
			found = append(found, log)
		}
	}
	if len(found) != 1 {
		t.Fatalf("Expected one Log of the mid-turn message, got %+v", found)
	}
	a := found[0].Attributes
	if a["agent.role"] != "person" || a["agent.source_uuid"] != "cc188e10-1c84-4980-a478-81fe09b70958" {
		t.Errorf("Expected a person message with the source_uuid of the wrapper, got %+v", a)
	}
	// The time of the remove line, not the time that the message was written.
	if want := time.Date(2026, 11, 14, 13, 39, 39, 580_000_000, time.UTC); !found[0].Time.Equal(want) {
		t.Errorf("Expected the delivery time %s, got %s", want, found[0].Time)
	}
}

func TestTheTranscriptsOfTheRunModelCheckGiveTheirCounts(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC))
	o := newAgentOrganization(t, env)

	// The tokens and the cost come from each transcript's last cost-state
	// line, which Claude Code wrote.
	cases := []struct {
		name, session                        string
		person, notPerson, replies           int
		input, output, cacheRead, cacheWrite float64
		cost                                 float64
	}{
		{"mid-turn-message.jsonl", "9d3b0d78-a7ce-4152-ba06-ce61683e5ef7", 2, 1, 4, 8, 384, 66361, 7511, 0.0023586099999999997},
		{"three-turns.jsonl", "49a7965a-2a6a-4c88-9459-2d53d525ac50", 3, 0, 8, 16, 1910, 150770, 10451, 0.004554500000000001},
		{"stop-hook-feedback.jsonl", "037f3e87-5440-480f-acbd-4f56305dfcf6", 2, 2, 6, 12, 450, 102685, 8555, 0.00296405},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			logs := uploadFixture(t, env, o, c.name, c.session)
			person, notPerson, replies := 0, 0, 0
			sums := map[string]float64{}
			for _, log := range logs {
				switch log.Attributes["agent.role"] {
				case "person":
					person++
				case "not from a person":
					notPerson++
				}
				if _, ok := log.Attributes["agent.tokens.output"]; ok {
					replies++
				}
				for _, name := range tokenAttributes {
					v, _ := number(log.Attributes, name)
					sums[name] += v
				}
			}
			if person != c.person || notPerson != c.notPerson || replies != c.replies {
				t.Errorf("Expected %d person messages, %d not from a person and %d replies, got %d, %d and %d",
					c.person, c.notPerson, c.replies, person, notPerson, replies)
			}
			for name, want := range map[string]float64{
				"agent.tokens.input": c.input, "agent.tokens.output": c.output,
				"agent.tokens.cache_read": c.cacheRead, "agent.tokens.cache_write": c.cacheWrite,
			} {
				if sums[name] != want {
					t.Errorf("Expected %v %s, got %v", want, name, sums[name])
				}
			}
			if math.Abs(sums["agent.cost_usd"]-c.cost) > 1e-9 {
				t.Errorf("Expected the cost %v USD of the cost-state line, got %v", c.cost, sums["agent.cost_usd"])
			}
		})
	}
}
