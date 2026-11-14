package storage

import (
	"bytes"
	"fmt"

	"github.com/google/uuid"
)

// agentFilesDedupWindow is how many of the last inserts' blocks the raw
// table of Agent sessions remembers, as logsDedupWindow is for Logs.
const agentFilesDedupWindow = 10000

// agentSessionFilesMigration makes agent_session_files, the raw layer of
// every Agent session (ADR 0020): one row for each piece of at most
// agentFileRowBytes of a unit that the agent ingest API stored. day is the
// UTC day on which the server stored the unit, so each line is kept for the
// same number of days after it was written. The version's band, 10000 and
// up, is the factory effort's; its hundreds are the ticket (06).
//
// flags are the unit's AgentFlag bits (cut, trimmed). The last row of a
// cut unit ends at the unit's end on disk, past its bytes.
//
// Past the spec's shape, each row also has who and where the Agent session
// is (the person, the harness, the repository, the folder, and for a Run
// its id and Project), so that the parse of the rows into Logs needs no
// other store. These repeat along the sort key and cost almost nothing
// compressed.
var agentSessionFilesMigration = clickHouseMigration{version: 10601, statements: []string{
	`CREATE TABLE IF NOT EXISTS agent_session_files (
		project_id UInt32,
		day Date,
		stored_at DateTime64(6, 'UTC'),
		session_id UUID,
		path String,
		version UInt32,
		offset UInt64,
		end_offset UInt64,
		content_hash String,
		bytes String CODEC(ZSTD(1)),
		flags UInt8,
		user_id UInt64,
		harness LowCardinality(String),
		repository LowCardinality(String),
		folder LowCardinality(String),
		run_id LowCardinality(String),
		factory_project LowCardinality(String)
	) ENGINE = MergeTree
	PARTITION BY (project_id, day)
	ORDER BY (project_id, session_id, path, version, offset)
	SETTINGS storage_policy = '` + ClickHouseStoragePolicy + `', non_replicated_deduplication_window = ` + fmt.Sprint(agentFilesDedupWindow),
}}

// AgentDays are the raw layer of the Agent sessions of each Agent Project,
// kept for its Retention. Its parts move to the bucket by the time that the
// server stored them.
var AgentDays = DayFamily{Name: "Agent sessions", Tables: []string{"agent_session_files"}, TimeColumn: "stored_at"}

// agentFileRowBytes is the most bytes of one row of agent_session_files: a
// unit larger than this is stored as several rows.
const agentFileRowBytes = 1 << 20

// AgentFileInsert is the units of a range of a Project's Agent file
// batches, the batches after After up to UpTo, which name the insert to
// ClickHouse.
type AgentFileInsert struct {
	After, UpTo uuid.UUID
	Units       []AgentFileUnit
}

func (i AgentFileInsert) token(projectID uint32) string {
	return fmt.Sprintf("agent-files-%d-%s-%s", projectID, i.After, i.UpTo)
}

// InsertAgentFiles inserts a range's units into agent_session_files with
// the range's dedup token: a range inserted again, whole or in part, is
// stored once.
func (ch *ClickHouse) InsertAgentFiles(projectID uint32, insert AgentFileInsert) error {
	if len(insert.Units) == 0 {
		return nil
	}
	block, err := ch.conn.PrepareBatch(insertContext(insert.token(projectID)), `INSERT INTO agent_session_files (project_id, day,
		stored_at, session_id, path, version, offset, end_offset, content_hash, bytes, flags, user_id, harness, repository,
		folder, run_id, factory_project)`)
	if err != nil {
		return fmt.Errorf("insert agent session files: %w", err)
	}
	defer block.Abort()
	for _, u := range insert.Units {
		stored := u.StoredAt.UTC()
		rows := agentFileRows(u.Offset, u.Bytes)
		for i, row := range rows {
			end := row.offset + uint64(len(row.bytes))
			if i == len(rows)-1 {
				// The unit's end on disk: past the bytes for a cut unit.
				end = u.End()
			}
			if err := block.Append(projectID, LogDay(stored), stored, u.SessionID, u.Path, u.Version, row.offset,
				end, u.ContentHash, string(row.bytes), u.Flags, uint64(u.Session.UserID),
				u.Session.Harness, u.Session.Repository, u.Session.Folder, u.Session.RunID, u.Session.Project); err != nil {
				return fmt.Errorf("insert agent session files: %w", err)
			}
		}
	}
	if err := block.Send(); err != nil {
		return fmt.Errorf("insert agent session files: %w", err)
	}
	return nil
}

type agentFileRow struct {
	offset uint64
	bytes  []byte
}

// agentFileRows cuts a unit that starts at offset into rows of at most
// agentFileRowBytes. Each row ends after a newline where the row holds
// one, so that a row of a .jsonl file holds whole lines unless one line is
// longer than a row.
func agentFileRows(offset uint64, unit []byte) []agentFileRow {
	var rows []agentFileRow
	for len(unit) > 0 {
		n := len(unit)
		if n > agentFileRowBytes {
			n = agentFileRowBytes
			if newline := bytes.LastIndexByte(unit[:n], '\n'); newline >= 0 {
				n = newline + 1
			}
		}
		rows = append(rows, agentFileRow{offset: offset, bytes: unit[:n]})
		offset += uint64(n)
		unit = unit[n:]
	}
	return rows
}
