package storage

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/logger"
)

// A token limited to a Run's Agent sessions writes for any person (a
// different person can continue a Run), but only to an Agent session of
// that Run. The Dispatcher adds to a Run each Agent session id that the
// wrapper finds in the project directory, and the agent can write any
// UUID there.
func TestARunsTokenWritesOnlyAnAgentSessionOfItsRun(t *testing.T) {
	logger.Init()
	writer, err := NewPebbleWriter(&config.Config{PebblePath: filepath.Join(t.TempDir(), "pebble")})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	const project = 1
	unit := func(session uuid.UUID, user int64, runID, line string, offset uint64) AgentFileUnit {
		s := AgentSession{UserID: user, Harness: "claude-code", Repository: "github.com/acme/shop", RunID: runID}
		if runID != "" {
			s.Project = "shop"
		}
		return AgentFileUnit{SessionID: session, Path: session.String() + ".jsonl", Version: 1, Offset: offset,
			Bytes: []byte(line + "\n"), Session: s}
	}
	laptop, run := uuid.New(), uuid.New()
	if _, _, err := writer.WriteAgentFileUnit(project, AgentUpload{Unit: unit(laptop, 7, "", "mine", 0)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writer.WriteAgentFileUnit(project, AgentUpload{Unit: unit(run, 9, "run-0193", "the task", 0),
		AnyPerson: true}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		what    string
		session uuid.UUID
		runID   string
	}{
		{"a person's laptop Agent session", laptop, "run-0193"},
		{"an Agent session of another Run", run, "run-0200"},
	} {
		_, stored, err := writer.WriteAgentFileUnit(project, AgentUpload{Unit: unit(tc.session, 9, tc.runID, "foreign", 5),
			AnyPerson: true})
		if !errors.Is(err, ErrAgentSessionOfAnotherRun) || stored {
			t.Errorf("%s: a Run's token wrote to it (stored %v, %v)", tc.what, stored, err)
		}
	}

	// The same Run goes on, for another person too.
	if _, stored, err := writer.WriteAgentFileUnit(project, AgentUpload{Unit: unit(run, 11, "run-0193", "next", 9),
		AnyPerson: true}); err != nil || !stored {
		t.Errorf("the Run's own Agent session: stored %v, %v", stored, err)
	}
}
