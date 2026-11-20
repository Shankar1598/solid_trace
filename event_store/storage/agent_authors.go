package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"time"

	"github.com/solidtrace/event_store/models"
)

// Who said each person message of a Run's Agent session. The join is at
// read time, from the Run's events, because an event can arrive after the
// parse and Logs are not rewritten. The Dispatcher gives each message that
// the wrapper writes to the harness one uuid, and names it in its events
// with these Attributes:
//
//   - message.id: the wrapper's uuid for the message, which is the uuid of
//     the .jsonl line of a message that starts a turn, and the
//     source_uuid of a queued_command attachment, for a message that the
//     agent got in the middle of a turn.
//   - message.sha256: the hex SHA-256 of the message's text.
//   - actor.kind, actor.id and actor.name: who said it. For a message of
//     a person, a sender, a schedule or a tracker user, the actor of the
//     message_accepted, message_written, message_delivered and
//     message_sent_again events. The execution_started event names the
//     Dispatcher's own message (the task, and the restore message) with
//     message.id and message.sha256 only: its author is always the
//     factory, and its actor stays the person who caused the Execution.
//
// A take_over event with an execution.id and an actor makes that person
// the author of each person message of that Execution that has no event.

// Kinds of an author that are not the Dispatcher's actor kinds.
const (
	// AuthorFactory is the actor kind of the Dispatcher's own messages.
	AuthorFactory = "factory"
	// AuthorFactoryName is the name of the factory as an author.
	AuthorFactoryName = "the factory"
	// AuthorNotFromAPerson is the author of a user line that no event
	// names: hook feedback, a subagent prompt.
	AuthorNotFromAPerson = "not from a person"
)

// Run event kinds that the join reads.
const (
	runEventExecutionStarted = "execution_started"
	runEventMessageDelivered = "message_delivered"
	runEventTakeOver         = "take_over"
)

// AgentAuthor is who said a person message.
type AgentAuthor struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// TextSHA256 is the hex SHA-256 of a message's text, as the Dispatcher
// writes it in message.sha256.
func TextSHA256(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// messageEvent is a run event that names a message.
type messageEvent struct {
	id, sha string
	author  AgentAuthor
	// execution is the Execution of the event ("" when it names none).
	execution string
	// delivered is true for an event that a fallback match can use: the
	// delivered event of a message, and the task in the started event.
	delivered bool
	// matched is true when a person message has this message's uuid.
	matched bool
}

// AgentAuthors gives the author of each person message of a Run's Agent
// session, by the message's step id. people are the steps with the role
// person, and events the Run's events, in the order of run.seq.
//
// A message is matched by its uuid to the event with the same message.id.
// A person message with no such uuid in the record is matched by the
// SHA-256 of its text to a delivered event of the same Execution, the
// first one that has no message matched by uuid, in order. A message that
// no event names is from the person who took the Execution over, when it
// was taken over, and else not from a person.
func AgentAuthors(events []models.Log, people []AgentTimelineStep) map[string]AgentAuthor {
	type execution struct {
		id      string
		started time.Time
		takenBy *AgentAuthor
	}
	var executions []*execution
	byExecution := map[string]*execution{}
	var named []*messageEvent
	byMessage := map[string]*messageEvent{}
	for _, log := range events {
		s := log.Attributes.String
		kind, executionID := s["event.kind"], s["execution.id"]
		author := AgentAuthor{Kind: s["actor.kind"], ID: s["actor.id"], Name: s["actor.name"]}
		switch kind {
		case runEventExecutionStarted:
			if _, ok := byExecution[executionID]; !ok && executionID != "" {
				x := &execution{id: executionID, started: log.Time}
				executions = append(executions, x)
				byExecution[executionID] = x
			}
		case runEventTakeOver:
			if x, ok := byExecution[executionID]; ok && author.Kind != "" {
				x.takenBy = &author
			}
		}
		id := s["message.id"]
		if kind == runEventExecutionStarted {
			// The message of a started event is the Dispatcher's own: the
			// task, or the restore message. The event's actor is the person
			// who caused the Execution, not who said it.
			author = AgentAuthor{Kind: AuthorFactory, Name: AuthorFactoryName}
		}
		if id == "" || author.Kind == "" {
			continue
		}
		m := byMessage[id]
		if m == nil {
			m = &messageEvent{id: id}
			byMessage[id] = m
			named = append(named, m)
		}
		m.author = author
		if sha := s["message.sha256"]; sha != "" {
			m.sha = sha
		}
		if executionID != "" {
			m.execution = executionID
		}
		if kind == runEventMessageDelivered || kind == runEventExecutionStarted {
			m.delivered = true
		}
	}
	slices.SortStableFunc(executions, func(a, b *execution) int { return a.started.Compare(b.started) })
	executionAt := func(t time.Time) *execution {
		var found *execution
		for _, x := range executions {
			if !x.started.After(t) {
				found = x
			}
		}
		return found
	}

	authors := make(map[string]AgentAuthor, len(people))
	var unmatched []AgentTimelineStep
	for _, p := range people {
		id := p.SourceUUID
		if id == "" {
			id = p.UUID
		}
		if m := byMessage[id]; m != nil && id != "" {
			m.matched = true
			authors[p.ID.String()] = m.author
			continue
		}
		unmatched = append(unmatched, p)
	}
	slices.SortStableFunc(unmatched, func(a, b AgentTimelineStep) int { return a.Time.Compare(b.Time) })
	for _, p := range unmatched {
		x := executionAt(p.Time)
		author := AgentAuthor{Kind: AuthorNotFromAPerson}
		if x != nil {
			sha := TextSHA256(p.Text)
			for _, m := range named {
				if m.delivered && !m.matched && m.execution == x.id && m.sha == sha {
					m.matched = true
					author = m.author
					break
				}
			}
			if author.Kind == AuthorNotFromAPerson && x.takenBy != nil {
				author = *x.takenBy
			}
		}
		authors[p.ID.String()] = author
	}
	return authors
}
