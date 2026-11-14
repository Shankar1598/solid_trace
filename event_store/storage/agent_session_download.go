package storage

import (
	"context"
	"errors"
	"fmt"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
)

// The download of one Agent session directory reads agent_session_files in
// two queries. The first reads only the place and the length of each row
// (length(bytes) reads the size stream of the column, not the bytes), and
// gives the files with their sizes, which an archive needs before the
// bytes. The second reads the bytes of those rows in key order with small
// read blocks: the prototype measured 296 MB of ClickHouse memory for its
// largest Agent session (434 MB) with these settings, against 1.05 GB with
// the defaults (ADR 0020).

// AgentDownloadLogComment names the bytes query of a download in
// system.query_log.
const AgentDownloadLogComment = "agent session download"

// ErrAgentDownloadChanged is a download whose rows changed between its two
// queries, as when Retention drops a day in that time. The caller stops
// the archive, so that the client sees an error and asks again.
var ErrAgentDownloadChanged = errors.New("the Agent session's rows changed during the download")

// AgentOffsetRange is the offsets from From up to To, not including To.
type AgentOffsetRange struct {
	From, To uint64
}

// AgentDownloadFile is one file of an Agent session directory: the highest
// version of a path that agent_session_files holds.
type AgentDownloadFile struct {
	Path    string
	Version uint32
	// StartOffset is where the stored bytes start. It is more than 0 when
	// Retention dropped the start of the file: the file is partial.
	StartOffset uint64
	// EndOffset is the end of the file on the client's disk, as stored.
	EndOffset uint64
	// Size is the number of stored bytes of the file.
	Size uint64
	// Missing are the ranges after StartOffset that have no stored bytes:
	// the rest of each cut unit.
	Missing []AgentOffsetRange
	// rows are the offsets of the file's rows, in order.
	rows []uint64
}

// Partial reports whether the file has no start, because Retention
// dropped it.
func (f AgentDownloadFile) Partial() bool { return f.StartOffset > 0 }

// AgentDownloadFiles returns the files of an Agent session that
// agent_session_files holds, in path order: for each path its highest
// version, with its start, end, size and missing ranges. It reads no bytes.
func (ch *ClickHouse) AgentDownloadFiles(ctx context.Context, projectID uint32, sessionID uuid.UUID) ([]AgentDownloadFile, error) {
	rows, err := ch.conn.Query(ctx, `
		SELECT path, version, offset, end_offset, length(bytes)
		FROM agent_session_files
		WHERE project_id = ? AND session_id = ?
		ORDER BY path, version, offset`, projectID, sessionID)
	if err != nil {
		return nil, fmt.Errorf("read the files of agent session %s: %w", sessionID, err)
	}
	defer rows.Close()
	var files []AgentDownloadFile
	var at uint64 // the offset after the last row of the last file
	for rows.Next() {
		var path string
		var version uint32
		var offset, end, length uint64
		if err := rows.Scan(&path, &version, &offset, &end, &length); err != nil {
			return nil, fmt.Errorf("read the files of agent session %s: %w", sessionID, err)
		}
		last := len(files) - 1
		switch {
		case last < 0 || files[last].Path != path:
			files = append(files, AgentDownloadFile{Path: path, Version: version, StartOffset: offset})
			at = offset
		case version > files[last].Version:
			files[last] = AgentDownloadFile{Path: path, Version: version, StartOffset: offset}
			at = offset
		case offset < at:
			// A row stored two times, or one inside rows before it: the
			// upload rules do not let this happen. Its bytes are not read.
			continue
		}
		f := &files[len(files)-1]
		if offset > at {
			f.Missing = append(f.Missing, AgentOffsetRange{From: at, To: offset})
		}
		at = offset + length
		if end > at {
			// The rest of a cut unit, past its stored bytes.
			f.Missing = append(f.Missing, AgentOffsetRange{From: at, To: end})
			at = end
		}
		f.rows = append(f.rows, offset)
		f.Size += length
		f.EndOffset = at
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read the files of agent session %s: %w", sessionID, err)
	}
	return files, nil
}

// ReadAgentDownload reads the bytes of files, which AgentDownloadFiles
// gave, in key order, and calls write with the index of each row's file
// and the row's bytes. A row stored after AgentDownloadFiles read is not
// given. It returns ErrAgentDownloadChanged when a row of files is no
// longer there.
func (ch *ClickHouse) ReadAgentDownload(ctx context.Context, projectID uint32, sessionID uuid.UUID, files []AgentDownloadFile, write func(file int, bytes []byte) error) error {
	if len(files) == 0 {
		return nil
	}
	paths := make([]string, len(files))
	versions := make([]uint32, len(files))
	for i, f := range files {
		paths[i], versions[i] = f.Path, f.Version
	}
	// When the download stops before the last row, rows.Close reads all
	// the rest of the query. The cancel before it stops the query in
	// ClickHouse first.
	ctx, cancel := context.WithCancel(ctx)
	ctx = clickhouse.Context(ctx, clickhouse.WithSettings(clickhouse.Settings{
		"max_block_size":              16,
		"read_in_order_use_buffering": 0,
		"log_comment":                 AgentDownloadLogComment,
	}))
	rows, err := ch.conn.Query(ctx, `
		SELECT path, version, offset, bytes
		FROM agent_session_files
		WHERE project_id = ? AND session_id = ? AND has(?, path) AND version = transform(path, ?, ?, toUInt32(0))
		ORDER BY path, version, offset`, projectID, sessionID, paths, paths, versions)
	if err != nil {
		cancel()
		return fmt.Errorf("read the bytes of agent session %s: %w", sessionID, err)
	}
	defer func() {
		cancel()
		rows.Close()
	}()
	file, row := 0, 0
	for rows.Next() {
		var path, data string
		var version uint32
		var offset uint64
		if err := rows.Scan(&path, &version, &offset, &data); err != nil {
			return fmt.Errorf("read the bytes of agent session %s: %w", sessionID, err)
		}
		for file < len(files) && files[file].Path < path {
			if row != len(files[file].rows) {
				return ErrAgentDownloadChanged
			}
			file, row = file+1, 0
		}
		if file == len(files) || files[file].Path != path || version != files[file].Version {
			// The query asks only for the files' paths and versions.
			return fmt.Errorf("read the bytes of agent session %s: a row of %s version %d was not asked for", sessionID, path, version)
		}
		planned := files[file].rows
		switch {
		case row < len(planned) && offset == planned[row]:
			row++
			if err := write(file, []byte(data)); err != nil {
				return err
			}
		case row < len(planned) && offset > planned[row]:
			// A row of the plan is gone.
			return ErrAgentDownloadChanged
		default:
			// A row that AgentDownloadFiles left out (stored two times) or
			// that was stored after it read.
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read the bytes of agent session %s: %w", sessionID, err)
	}
	for ; file < len(files); file, row = file+1, 0 {
		if row != len(files[file].rows) {
			return ErrAgentDownloadChanged
		}
	}
	return nil
}
