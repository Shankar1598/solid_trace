package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// ErrLogFilesInUse is returned by a deletion of Log files that a read is
// still reading. Those files stay, and the deletion's next pass takes them.
var ErrLogFilesInUse = errors.New("log files are being read")

// logRead is a read of a Project's Logs of a time range as they were at one
// moment: a DuckDB transaction on a connection of its own, and the Log files
// the manifest listed then, pinned until the read ends.
//
// The transaction reads the manifest while no export or deletion can
// publish, so each Log is in exactly one of the hot tables and the files,
// and the files are pinned before a deletion can take them. After that, a
// read holds up nothing: an export commits and renames while it runs, and
// the transaction still sees DuckDB as it was; a deletion leaves the files a
// read has pinned for its next pass.
type logRead struct {
	store     *LogStore
	ctx       context.Context
	conn      *sql.Conn
	projectID uint32
	// hot says whether the Project had a hot table.
	hot bool
	// hours are the hours of the range that may hold the Project's Logs,
	// newest first, and files the paths of their files.
	hours []time.Time
	files map[time.Time][]string
	// cursor is the Project's Log processing cursor, for a read of the Logs
	// ingested after a batch.
	cursor uuid.UUID
	// pinned are the keys of the files.
	pinned []string
	// began says the transaction is open; discard, that the connection
	// must not go back to the pool.
	began, discard bool
}

// beginRead begins a read of a Project's Logs of [from, to). Its queries stop
// when ctx ends. The caller must end it.
func (s *LogStore) beginRead(ctx context.Context, projectID uint32, from, to time.Time) (*logRead, error) {
	return s.startRead(ctx, projectID, func(r *logRead) (map[time.Time][]logFile, error) {
		files, err := listLogFiles(r.ctx, r.conn, r.projectID, from, to)
		if err != nil {
			return nil, err
		}
		r.hours, err = r.hoursWithLogs(from, to, files)
		return files, err
	})
}

// beginIngestedRead begins a read of a Project's Logs ingested after the Log
// batch after: it reads the Project's Log processing cursor, and lists the
// files that hold a later batch. Its hours are those of the files, and those
// between the oldest and the newest Log of a later batch in the hot table.
// It reads nothing more when no batch after after is up to the cursor. The
// caller must end it.
func (s *LogStore) beginIngestedRead(ctx context.Context, projectID uint32, after uuid.UUID) (*logRead, error) {
	return s.startRead(ctx, projectID, func(r *logRead) (map[time.Time][]logFile, error) {
		var err error
		if r.cursor, err = logProcessingCursor(r.ctx, r.conn, r.projectID); err != nil {
			return nil, err
		}
		if !batchesBetween(after, r.cursor) {
			return nil, nil
		}
		files, err := listLogFilesAfterBatch(r.ctx, r.conn, r.projectID, after)
		if err != nil {
			return nil, err
		}
		hours := map[time.Time]bool{}
		for hour := range files {
			hours[hour] = true
		}
		if r.hot {
			var oldest, newest sql.NullTime
			err := r.conn.QueryRowContext(r.ctx, fmt.Sprintf("SELECT min(time), max(time) FROM %s.%s WHERE %s",
				logsCatalog, hotTable(r.projectID), batchWindow(after, r.cursor))).Scan(&oldest, &newest)
			if err != nil {
				return nil, err
			}
			if oldest.Valid {
				for hour := oldest.Time.UTC().Truncate(time.Hour); !hour.After(newest.Time.UTC()); hour = hour.Add(time.Hour) {
					hours[hour] = true
				}
			}
		}
		r.hours = newestFirst(hours)
		return files, nil
	})
}

// startRead begins a read of a Project's Logs, with the files list returns
// and the hours it sets. Its queries stop when ctx ends. The caller must end
// it.
func (s *LogStore) startRead(ctx context.Context, projectID uint32, list func(*logRead) (map[time.Time][]logFile, error)) (*logRead, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	r := &logRead{store: s, ctx: ctx, conn: conn, projectID: projectID, files: map[time.Time][]string{}}
	if err := r.begin(list); err != nil {
		r.end()
		return nil, err
	}
	return r, nil
}

func (r *logRead) begin(list func(*logRead) (map[time.Time][]logFile, error)) error {
	s := r.store
	s.publishMu.RLock()
	defer s.publishMu.RUnlock()

	if _, err := r.conn.ExecContext(r.ctx, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	r.began = true
	// Project deletion drops the hot table, and a later Log makes it again.
	var err error
	if r.hot, err = hotTableExists(r.ctx, r.conn, r.projectID); err != nil {
		return err
	}
	// DuckDB fixes a transaction's view of logs.duckdb when it first reads
	// it, so the manifest, and any Log processing cursor, are read here,
	// under the lock, with the hot tables.
	files, err := list(r)
	if err != nil {
		return err
	}
	for hour, hourFiles := range files {
		for _, file := range hourFiles {
			r.files[hour] = append(r.files[hour], s.files.path(file))
			r.pinned = append(r.pinned, file.key())
		}
	}
	s.pin(r.pinned)
	return nil
}

// end ends the read: it closes the transaction, so that whatever SQL wrote in
// it commits, and unpins its files.
func (r *logRead) end() {
	if r.began {
		if _, err := r.conn.ExecContext(context.Background(), "COMMIT"); err != nil {
			r.discard = true
		}
	}
	if r.discard {
		discardConn(r.conn)
	}
	r.conn.Close()
	r.store.unpin(r.pinned)
}

// hoursWithLogs returns, newest first, the hours of [from, to) that may hold
// the Project's Logs: those with files, and those between the oldest and the
// newest Log not yet exported.
func (r *logRead) hoursWithLogs(from, to time.Time, files map[time.Time][]logFile) ([]time.Time, error) {
	hours := map[time.Time]bool{}
	for hour := range files {
		hours[hour] = true
	}

	sources := []string{fmt.Sprintf("SELECT time FROM %s.%s WHERE project_id = %d AND %s",
		logsCatalog, lateTable, r.projectID, timeRange(from, to))}
	if r.hot {
		sources = append(sources, fmt.Sprintf("SELECT time FROM %s.%s WHERE %s",
			logsCatalog, hotTable(r.projectID), timeRange(from, to)))
	}
	var oldest, newest sql.NullTime
	err := r.conn.QueryRowContext(r.ctx, fmt.Sprintf("SELECT min(time), max(time) FROM (%s)", strings.Join(sources, " UNION ALL "))).
		Scan(&oldest, &newest)
	if err != nil {
		return nil, err
	}
	if oldest.Valid {
		for hour := oldest.Time.UTC().Truncate(time.Hour); !hour.After(newest.Time.UTC()); hour = hour.Add(time.Hour) {
			hours[hour] = true
		}
	}

	return newestFirst(hours), nil
}

// newestFirst lists a set of hours, newest first.
func newestFirst(hours map[time.Time]bool) []time.Time {
	sorted := make([]time.Time, 0, len(hours))
	for hour := range hours {
		sorted = append(sorted, hour)
	}
	slices.SortFunc(sorted, func(a, b time.Time) int { return b.Compare(a) })
	return sorted
}

// queryLogs runs a query whose rows are Logs as a hot table holds them, with
// any promoted columns.
func (r *logRead) queryLogs(query string) ([]models.Log, error) {
	rows, err := r.conn.QueryContext(r.ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var logs []models.Log
	for rows.Next() {
		log, err := scanHotRow(rows, columns)
		if err != nil {
			return nil, err
		}
		logs = append(logs, log)
	}
	return logs, rows.Err()
}

// pin marks Log files, by their keys, as being read, so that no deletion
// takes them.
func (s *LogStore) pin(keys []string) {
	s.pinMu.Lock()
	defer s.pinMu.Unlock()
	for _, key := range keys {
		s.pins[key]++
	}
}

// unpin undoes pin.
func (s *LogStore) unpin(keys []string) {
	s.pinMu.Lock()
	defer s.pinMu.Unlock()
	for _, key := range keys {
		if s.pins[key]--; s.pins[key] <= 0 {
			delete(s.pins, key)
		}
	}
}

// pinned reports whether a read has pinned the Log file with this key.
func (s *LogStore) pinned(key string) bool {
	s.pinMu.Lock()
	defer s.pinMu.Unlock()
	return s.pins[key] > 0
}
