package main_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

// Who said each person message of a Run's Agent session: the read endpoint
// of the Agent session page joins the Run's message events to the person
// messages at read time (spec, Who said each person message).

type timelineAuthor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

type authoredStep struct {
	timelineStep
	Author *timelineAuthor `json:"author"`
}

func textSHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// messageRunEvent is a run event of the kind that names a message, as the
// Dispatcher sends it: the message's uuid and the hash of its text, and the
// actor that said it.
func messageRunEvent(run runSnapshot, seq int, at time.Time, kind, executionID, messageID, text, actorKind, actorID, actorName string) map[string]any {
	event := runEvent(run, seq, at, kind)
	event["execution"] = map[string]any{"id": executionID}
	event["actor"] = map[string]any{"kind": actorKind, "id": actorID, "name": actorName}
	if messageID != "" {
		event["message.id"] = messageID
		event["message.sha256"] = textSHA256(text)
	}
	return event
}

func readAuthors(t *testing.T, env *TestEnv, o runEventsOrganization) map[string]*timelineAuthor {
	t.Helper()
	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/agent-sessions/%s", o.project, runSession), map[string]any{})
	if status != 200 {
		t.Fatalf("Expected 200, got %d: %s", status, raw)
	}
	var answer struct {
		Steps []authoredStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode %s: %v", raw, err)
	}
	authors := map[string]*timelineAuthor{}
	for _, s := range answer.Steps {
		if s.Role == "person" && s.Author == nil {
			t.Errorf("Expected an author on each person message, got none on %q", s.Text)
		}
		if s.Role != "person" && s.Author != nil {
			t.Errorf("Expected no author on a step that is not a person message, got %+v", s.Author)
		}
		if s.Author != nil {
			authors[s.Text] = s.Author
		}
	}
	return authors
}

func expectAuthor(t *testing.T, authors map[string]*timelineAuthor, text, kind, name string) {
	t.Helper()
	a := authors[text]
	if a == nil || a.Kind != kind || a.Name != name {
		t.Errorf("Expected %q from %s %q, got %+v", text, kind, name, a)
	}
}

func TestTheAgentSessionOfARunSaysWhoSaidEachPersonMessage(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC))
	o := newRunEventsOrganization(t, env)
	runSecret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
		scopes: []string{"sessions:write", "sessions:read"}, sessionIDs: []string{runSession}})
	run := runSnapshot{id: "run_authors", state: "running", title: "Fix the cart total", executions: 1}
	at := time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC)
	const x1, x2, x3 = "run_authors-x1", "run_authors-x2", "run_authors-x3"

	task := "Fix the cart total"
	person := func(uuid string, sec int, text string) string {
		return personAt(uuid, at.Add(time.Duration(sec)*time.Second).Format(time.RFC3339), text)
	}
	queued := func(uuid, source string, sec int, text string) string {
		return fmt.Sprintf(`{"type":"attachment","uuid":%q,"timestamp":%q,"attachment":{"type":"queued_command","prompt":%q,"source_uuid":%q,"commandMode":"prompt"}}`,
			uuid, at.Add(time.Duration(sec)*time.Second).Format(time.RFC3339), text, source)
	}
	expectStatus(t, uploadUnit(t, env, runSecret, runSession, unitUpload{path: "session.jsonl", runID: run.id, project: "checkout", folder: "web", body: jsonl(
		// Execution 1: the task, a message during a tool call, hook feedback,
		// and a message that no uuid names.
		person("t1", 0, task),
		replyAt("a1", at.Add(5*time.Second).Format(time.RFC3339), "msg_1", "claude-opus-5-5", `{"type":"text","text":"On it."}`),
		queued("q1", "m-mid", 10, "Also check the footer"),
		person("stray", 20, "A line that no event names"),
		// Execution 2: its first message, and two messages with the same text
		// that the fallback matches in order.
		person("m-first", 600, "Use the shop style"),
		person("no-uuid-1", 610, "Continue"),
		person("no-uuid-2", 620, "Check the totals"),
		// Execution 3, a take-over: what the person typed has no event.
		person("typed-1", 1200, "Show me the diff"),
		// A message whose event arrives after the first read.
		person("m-late", 1210, "One more thing"),
	)}), 200)
	env.ProcessAgentFiles(t)

	started := func(seq int, when time.Time, execution string) map[string]any {
		e := messageRunEvent(run, seq, when, "execution_started", execution, "", "", "person", "7", "Ana")
		return e
	}
	// The started event names the task, and its actor is the person who started the Run.
	e1 := messageRunEvent(run, 1, at, "execution_started", x1, "t1", task, "person", "7", "Ana")
	e2 := messageRunEvent(run, 2, at.Add(9*time.Second), "message_delivered", x1, "m-mid", "Also check the footer", "sender", "ci", "CI bot")
	e3 := started(3, at.Add(590*time.Second), x2)
	e4 := messageRunEvent(run, 4, at.Add(591*time.Second), "message_delivered", x2, "m-first", "Use the shop style", "person", "7", "Ana")
	// The uuid of these two is not in the record: Seam 4 found no uuid.
	e5 := messageRunEvent(run, 5, at.Add(605*time.Second), "message_delivered", x2, "w-1", "Continue", "schedule", "s1", "Nightly")
	e6 := messageRunEvent(run, 6, at.Add(606*time.Second), "message_delivered", x2, "w-2", "Check the totals", "tracker", "u9", "Bo")
	e7 := started(7, at.Add(1190*time.Second), x3)
	e8 := messageRunEvent(run, 8, at.Add(1191*time.Second), "take_over", x3, "", "", "person", "11", "Cy")
	expectStatus(t, sendRunEvents(t, env, o.dispatcherSecret, e1, e2, e3, e4, e5, e6, e7, e8), 200)
	env.ProcessLogs(t)

	authors := readAuthors(t, env, o)
	t.Run("the task shows the factory", func(t *testing.T) { expectAuthor(t, authors, task, "factory", "the factory") })
	t.Run("a message during a tool call shows the actor of its event", func(t *testing.T) {
		expectAuthor(t, authors, "Also check the footer", "sender", "CI bot")
	})
	t.Run("the first message of an Execution shows the actor of its event", func(t *testing.T) {
		expectAuthor(t, authors, "Use the shop style", "person", "Ana")
	})
	t.Run("a user line with no event shows not from a person", func(t *testing.T) {
		expectAuthor(t, authors, "A line that no event names", "not from a person", "")
	})
	t.Run("with no uuid in the record, the hash of the text gives the author", func(t *testing.T) {
		expectAuthor(t, authors, "Continue", "schedule", "Nightly")
		expectAuthor(t, authors, "Check the totals", "tracker", "Bo")
	})
	t.Run("in a take-over Execution each person message is from the person who took over", func(t *testing.T) {
		expectAuthor(t, authors, "Show me the diff", "person", "Cy")
	})

	// An event that arrives after the parse gives the author on the next read.
	t.Run("an event after the parse still gives the author on the next read", func(t *testing.T) {
		late := messageRunEvent(run, 9, at.Add(1205*time.Second), "message_delivered", x3, "m-late", "One more thing", "person", "12", "Di")
		expectStatus(t, sendRunEvents(t, env, o.dispatcherSecret, late), 200)
		env.ProcessLogs(t)
		expectAuthor(t, readAuthors(t, env, o), "One more thing", "person", "Di")
	})
}

func TestTheFallbackMatchesTheHashOfTheTextInOrderInsideTheExecution(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	env.Clock.Set(time.Date(2026, 11, 14, 14, 0, 0, 0, time.UTC))
	o := newRunEventsOrganization(t, env)
	runSecret, _ := o.rows.apiToken(apiTokenRow{user: o.user, organization: o.organization,
		scopes: []string{"sessions:write", "sessions:read"}, sessionIDs: []string{runSession}})
	run := runSnapshot{id: "run_fallback", state: "running", title: "Fix the cart total", executions: 1}
	at := time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC)
	stamp := func(sec int) string { return at.Add(time.Duration(sec) * time.Second).Format(time.RFC3339) }
	expectStatus(t, uploadUnit(t, env, runSecret, runSession, unitUpload{path: "session.jsonl", runID: run.id, project: "checkout", folder: "web", body: jsonl(
		personAt("p1", stamp(0), "Fix the cart total"),
		personAt("p2", stamp(60), "Continue"),
		personAt("p3", stamp(120), "Continue"),
		personAt("p4", stamp(180), "Continue"),
	)}), 200)
	env.ProcessAgentFiles(t)

	events := []map[string]any{
		messageRunEvent(run, 1, at, "execution_started", "run_fallback-x1", "w-task", "Fix the cart total", "person", "7", "Ana"),
		messageRunEvent(run, 2, at.Add(59*time.Second), "message_delivered", "run_fallback-x1", "w-1", "Continue", "person", "7", "Ana"),
		messageRunEvent(run, 3, at.Add(119*time.Second), "message_delivered", "run_fallback-x1", "w-2", "Continue", "person", "8", "Bo"),
		// Written but never delivered: it has no proof, so the fallback does
		// not use it.
		messageRunEvent(run, 4, at.Add(170*time.Second), "message_written", "run_fallback-x1", "w-3", "Continue", "person", "9", "Cy"),
	}
	expectStatus(t, sendRunEvents(t, env, o.dispatcherSecret, events...), 200)
	env.ProcessLogs(t)

	status, raw := postQueryApp(t, env, fmt.Sprintf("/api/%d/agent-sessions/%s", o.project, runSession), map[string]any{})
	if status != 200 {
		t.Fatalf("Expected 200, got %d: %s", status, raw)
	}
	var answer struct {
		Steps []authoredStep `json:"steps"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		t.Fatalf("Decode %s: %v", raw, err)
	}
	var got []string
	for _, s := range answer.Steps {
		got = append(got, s.Author.Kind+":"+s.Author.Name)
	}
	expectTexts(t, "the authors in order", got, []string{"factory:the factory", "person:Ana", "person:Bo", "not from a person:"})
}
