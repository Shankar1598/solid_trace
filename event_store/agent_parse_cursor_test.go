package main_test

import (
	"testing"
)

// The parse reads the batches that Agent file processing has inserted into
// the raw layer, and no further: a Log's raw line is then always there.
func TestTheParseStopsAtTheCursorOfAgentFileProcessing(t *testing.T) {
	env := setupTestEnv(t)
	defer env.Cleanup()
	o := newAgentOrganization(t, env)
	project := uint32(o.project)

	first := jsonl(transcript[:2]...)
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", body: first}), 200)
	if err := env.AgentFileProcessor.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}
	// A unit that is stored in Pebble and not yet inserted.
	expectStatus(t, uploadUnit(t, env, o.secret, laptopSession, unitUpload{path: "session.jsonl", offset: len(first), body: jsonl(transcript[2])}), 200)
	if err := env.AgentParser.ProcessUntilCaughtUp(); err != nil {
		t.Fatal(err)
	}

	inserted, err := env.PebbleWriter.AgentFileCursor(project)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := env.PebbleWriter.AgentParseCursor(project)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != inserted {
		t.Errorf("Expected the parse cursor %s at the cursor of Agent file processing %s", parsed, inserted)
	}
	if logs := sessionLogs(t, env, o.project, laptopSession); len(logs) != 3 {
		t.Errorf("Expected the 3 lines parsed once Agent file processing has caught up, got %d Logs", len(logs))
	}
}
