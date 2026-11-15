package storage

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
)

// AgentRawLineMaxBytes is the most bytes of a raw line that 'Show full'
// gives. A longer line is given cut at this length.
const AgentRawLineMaxBytes = 16 << 20

// AgentRawLine is one line of an Agent session's file in the raw layer,
// as the agent ingest API stored it: redacted, without its newline.
type AgentRawLine struct {
	Line string
	// Cut is true when the raw layer does not hold all of the line: the
	// rest of a cut unit, or the bytes past AgentRawLineMaxBytes.
	Cut bool
}

// ReadAgentRawLine reads the line that starts at offset in version of the
// Agent session's file at path, from agent_session_files. found is false
// when the raw layer holds no bytes at that offset.
func (s *ClickHouseLogs) ReadAgentRawLine(ctx context.Context, projectID uint32, sessionID uuid.UUID, path string, version uint32, offset uint64) (line AgentRawLine, found bool, err error) {
	// The rows from the one that holds offset, in key order, a few at a
	// time: the read stops at the first newline.
	ctx, cancel := context.WithCancel(clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_block_size":              4,
		"read_in_order_use_buffering": 0,
	})))
	query := `SELECT offset, end_offset, bytes FROM agent_session_files
		WHERE project_id = ? AND session_id = ? AND path = ? AND version = ? AND end_offset > ?
		ORDER BY offset`
	began := time.Now()
	defer func() { recordStatement(ctx, query, began, err) }()
	rows, err := s.conn.Query(ctx, query, projectID, sessionID, path, version, offset)
	if err != nil {
		cancel()
		return AgentRawLine{}, false, fmt.Errorf("read the raw line: %w", err)
	}
	defer func() {
		cancel()
		rows.Close()
	}()
	var out []byte
	at := offset // the offset of the next byte of the line
	// unitEnd is the end on disk of the unit that holds at. It is past
	// the stored bytes when the unit is cut.
	var unitEnd uint64
	for rows.Next() {
		var rowOffset, rowEnd uint64
		var data string
		if err := rows.Scan(&rowOffset, &rowEnd, &data); err != nil {
			return AgentRawLine{}, false, fmt.Errorf("read the raw line: %w", err)
		}
		end := rowOffset + uint64(len(data))
		if end <= at {
			// A row stored two times, or the rest of a cut unit before at.
			continue
		}
		if rowOffset > at {
			// A gap: the rest of a cut unit is not stored.
			return AgentRawLine{Line: string(out), Cut: true}, found, nil
		}
		found = true
		part := []byte(data[at-rowOffset:])
		if newline := bytes.IndexByte(part, '\n'); newline >= 0 {
			part = part[:newline]
			out = append(out, part...)
			if len(out) > AgentRawLineMaxBytes {
				return AgentRawLine{Line: string(out[:AgentRawLineMaxBytes]), Cut: true}, true, nil
			}
			return AgentRawLine{Line: string(out)}, true, nil
		}
		out = append(out, part...)
		if len(out) > AgentRawLineMaxBytes {
			return AgentRawLine{Line: string(out[:AgentRawLineMaxBytes]), Cut: true}, true, nil
		}
		at = end
		unitEnd = rowEnd
	}
	if err := rows.Err(); err != nil {
		return AgentRawLine{}, false, fmt.Errorf("read the raw line: %w", err)
	}
	if at < unitEnd {
		// The line runs into the rest of a cut unit at the end of the
		// file, which is not stored.
		return AgentRawLine{Line: string(out), Cut: true}, found, nil
	}
	// The file ends with no newline after the line: the line as written so
	// far.
	return AgentRawLine{Line: string(out)}, found, nil
}
