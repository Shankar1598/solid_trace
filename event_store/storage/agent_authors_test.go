package storage

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// The join of a Run's message events to the person messages of its Agent
// session (spec, Who said each person message). It has no database: the
// seam is the Run's events and the person steps.

var authorsAt = time.Date(2026, 11, 14, 11, 0, 0, 0, time.UTC)

func authorsEvent(sec int, kind, execution, message, text, actorKind, actorName string) models.Log {
	s := map[string]string{"event.kind": kind, "execution.id": execution, "actor.kind": actorKind, "actor.name": actorName}
	if message != "" {
		s["message.id"] = message
		s["message.sha256"] = TextSHA256(text)
	}
	return models.Log{Time: authorsAt.Add(time.Duration(sec) * time.Second), Attributes: models.Attributes{String: s}}
}

func authorsPerson(sec int, line, source, text string) AgentTimelineStep {
	return AgentTimelineStep{ID: uuid.New(), Time: authorsAt.Add(time.Duration(sec) * time.Second), Role: "person", UUID: line, SourceUUID: source, Text: text}
}

func authorOf(authors map[string]AgentAuthor, step AgentTimelineStep) string {
	a := authors[step.ID.String()]
	return a.Kind + ":" + a.Name
}

func TestAgentAuthors(t *testing.T) {
	events := []models.Log{
		authorsEvent(0, "execution_started", "x1", "t1", "Fix the cart total", "factory", "the factory"),
		authorsEvent(9, "message_delivered", "x1", "m-mid", "Also check the footer", "sender", "CI bot"),
		authorsEvent(590, "execution_started", "x2", "", "", "person", "Ana"),
		authorsEvent(591, "message_delivered", "x2", "m-first", "Use the shop style", "person", "Ana"),
		// No uuid of these is in the record: the hash of the text decides.
		authorsEvent(605, "message_delivered", "x2", "w-1", "Continue", "schedule", "Nightly"),
		authorsEvent(606, "message_delivered", "x2", "w-2", "Continue", "tracker", "Bo"),
		authorsEvent(607, "message_written", "x2", "w-3", "Continue", "person", "Cy"),
		authorsEvent(1190, "execution_started", "x3", "", "", "person", "Ana"),
		authorsEvent(1191, "take_over", "x3", "", "", "person", "Cy"),
	}
	task := authorsPerson(0, "t1", "", "Fix the cart total")
	mid := authorsPerson(10, "q1", "m-mid", "Also check the footer")
	stray := authorsPerson(20, "s1", "", "A line that no event names")
	first := authorsPerson(600, "m-first", "", "Use the shop style")
	again1 := authorsPerson(610, "n1", "", "Continue")
	again2 := authorsPerson(620, "n2", "", "Continue")
	again3 := authorsPerson(630, "n3", "", "Continue")
	typed := authorsPerson(1200, "k1", "", "Show me the diff")

	authors := AgentAuthors(events, []AgentTimelineStep{task, mid, stray, first, again1, again2, again3, typed})
	for _, c := range []struct {
		what string
		step AgentTimelineStep
		want string
	}{
		{"the task", task, "factory:the factory"},
		{"a message during a tool call, by source_uuid", mid, "sender:CI bot"},
		{"the first message of an Execution", first, "person:Ana"},
		{"a line that no event names", stray, "not from a person:"},
		{"the first of two with the same text, by the hash", again1, "schedule:Nightly"},
		{"the second of two with the same text, by the hash", again2, "tracker:Bo"},
		{"a third with the text of a message that was only written", again3, "not from a person:"},
		{"a take-over Execution", typed, "person:Cy"},
	} {
		if got := authorOf(authors, c.step); got != c.want {
			t.Errorf("%s: expected %q, got %q", c.what, c.want, got)
		}
	}
}

func TestAgentAuthorsGivesTheAuthorOfAnEventThatArrivesLate(t *testing.T) {
	step := authorsPerson(10, "m-late", "", "One more thing")
	if got := authorOf(AgentAuthors(nil, []AgentTimelineStep{step}), step); got != "not from a person:" {
		t.Errorf("Expected no author before the event, got %q", got)
	}
	events := []models.Log{authorsEvent(0, "execution_started", "x1", "", "", "person", "Ana"),
		authorsEvent(9, "message_delivered", "x1", "m-late", "One more thing", "person", "Di")}
	if got := authorOf(AgentAuthors(events, []AgentTimelineStep{step}), step); got != "person:Di" {
		t.Errorf("Expected the author on the next read, got %q", got)
	}
}
