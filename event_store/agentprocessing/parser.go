package agentprocessing

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// LogStore is ClickHouse's Logs as the parse uses them.
// *storage.ClickHouseLogs is the production adapter.
type LogStore interface {
	InsertLogBatches(projectID uint32, insert storage.LogInsert) error
}

// Retention is a Project's Log Retention, which normalise needs, as Log
// ingest reads it. *storage.ConsoleSettings is the production adapter.
type Retention interface {
	LogRetentionDays(projectID uint32) (int, error)
}

// Parser is the parse: one worker, never in the ingest request, that makes
// each line of the .jsonl units that Agent file processing has stored into
// one Log of the Agent Project (ADR 0020). It reads a Project's Agent file
// batches after its own cursor, up to Agent file processing's cursor, and
// makes each Log exactly once:
//  1. it records in Pebble the range of batches it is about to parse;
//  2. it inserts their Logs, and what they add to the totals of each
//     Agent session (agent_sessions), with a dedup token made from the
//     range;
//  3. it moves its cursor past the range and deletes the batches.
//
// After a crash or a failed insert, the recorded range is parsed and
// inserted again with the same token, and ClickHouse drops what it already
// has.
type Parser struct {
	*loop
	batches   *storage.PebbleWriter
	logs      LogStore
	retention Retention
}

// NewParser constructs the parse.
func NewParser(batches *storage.PebbleWriter, logs LogStore, retention Retention) *Parser {
	p := &Parser{batches: batches, logs: logs, retention: retention}
	p.loop = newLoop("The parse of Agent sessions", p.parseUnits)
	return p
}

// Prepare runs once, before the worker starts. It deletes the batches that
// a crash left in Pebble after they were parsed, and marks every Project
// with batches as pending.
func (p *Parser) Prepare() error {
	projects, err := p.batches.ProjectsWithAgentFileBatches()
	if err != nil {
		return fmt.Errorf("list projects with agent file batches: %w", err)
	}
	for _, projectID := range projects {
		cursor, err := p.batches.AgentParseCursor(projectID)
		if err != nil {
			return fmt.Errorf("read the parse cursor of project %d: %w", projectID, err)
		}
		if cursor != uuid.Nil {
			if err := p.batches.DeleteAgentFileBatchesUpTo(projectID, cursor); err != nil {
				return fmt.Errorf("delete parsed agent file batches of project %d: %w", projectID, err)
			}
		}
		p.Notify(projectID)
	}
	return nil
}

// parseUnits parses the units of a Project's batches after its cursor that
// Agent file processing has stored, up to about maxBytesPerInsert: or,
// while the range it last recorded is past the cursor, that range again.
func (p *Parser) parseUnits(projectID uint32) (more bool, err error) {
	cursor, err := p.batches.AgentParseCursor(projectID)
	if err != nil {
		return false, fmt.Errorf("read the parse cursor: %w", err)
	}
	recorded, err := p.batches.RecordedAgentParseInsert(projectID)
	if err != nil {
		return false, fmt.Errorf("read the recorded range of the parse: %w", err)
	}
	stored, err := p.batches.AgentFileCursor(projectID)
	if err != nil {
		return false, fmt.Errorf("read agent file processing cursor: %w", err)
	}
	if stored == uuid.Nil {
		return false, nil
	}
	batches, err := p.batches.AgentFileBatchesAfter(projectID, cursor, maxBytesPerInsert)
	if err != nil {
		return false, fmt.Errorf("read agent file batches: %w", err)
	}
	batches, again := nextRange(upTo(batches, stored), cursor, recorded)
	if len(batches) == 0 {
		return false, nil
	}
	last := batches[len(batches)-1].ID
	if !again {
		if err := p.batches.RecordAgentParseInsert(projectID, last); err != nil {
			return false, fmt.Errorf("record the range of the parse: %w", err)
		}
	}

	retentionDays, err := p.retention.LogRetentionDays(projectID)
	if err != nil {
		return false, fmt.Errorf("read retention: %w", err)
	}
	units := make([]storage.AgentFileUnit, len(batches))
	for i, batch := range batches {
		units[i] = batch.Unit
	}
	encoded, err := p.batches.AgentParseState(projectID)
	if err != nil {
		return false, fmt.Errorf("read the parse state: %w", err)
	}
	state, err := decodeParseState(encoded)
	if err != nil {
		return false, err
	}
	logsOfUnits, sessionDays, reviewRecords, state := parseUnits(units, retentionDays, state)
	encoded, err = state.encode()
	if err != nil {
		return false, err
	}
	var parsed []storage.LogBatch
	for i, logs := range logsOfUnits {
		if len(logs) > 0 {
			parsed = append(parsed, storage.LogBatch{ID: batches[i].ID, Logs: logs})
		}
	}
	insert := storage.LogInsert{After: cursor, UpTo: last, Batches: parsed, Again: again, Range: "agent",
		AgentSessions: sessionDays, ReviewRecords: reviewRecords}
	if err := p.logs.InsertLogBatches(projectID, insert); err != nil {
		return false, fmt.Errorf("insert the Logs of agent sessions: %w", err)
	}
	if err := p.batches.SetAgentParseCursor(projectID, last, encoded); err != nil {
		return false, fmt.Errorf("move the parse cursor: %w", err)
	}
	if err := p.batches.DeleteAgentFileBatchesUpTo(projectID, last); err != nil {
		logger.L.Warn("Deleting parsed agent file batches failed", "project_id", projectID, "error", err)
	}
	return true, nil
}
