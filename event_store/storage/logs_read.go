package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/solidtrace/event_store/models"
)

// ErrLogFilesInUse is returned by a deletion of Log files that a read is
// still reading. Those files stay, and the deletion's next pass takes them.
var ErrLogFilesInUse = errors.New("log files are being read")

// logRead is a read of a Project's Logs of a time range as they were at one
// moment: a DuckDB transaction on a connection of its own, and the Log files
// there were then, pinned until the read ends.
//
// The transaction and the file list are taken together while no export or
// deletion can publish, so each Log is in exactly one of them. After that, a
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
	// newest first, and files their files.
	hours []time.Time
	files map[time.Time][]string
	// began says the transaction is open; discard, that the connection
	// must not go back to the pool.
	began, discard bool
}

// beginRead begins a read of a Project's Logs of [from, to). Its queries stop
// when ctx ends. The caller must end it.
func (s *LogStore) beginRead(ctx context.Context, projectID uint32, from, to time.Time) (*logRead, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	r := &logRead{store: s, ctx: ctx, conn: conn, projectID: projectID, files: map[time.Time][]string{}}
	if err := r.begin(from, to); err != nil {
		r.end()
		return nil, err
	}
	return r, nil
}

func (r *logRead) begin(from, to time.Time) error {
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
	if r.hours, err = r.hoursWithLogs(from, to); err != nil {
		return err
	}
	var pinned []string
	for _, hour := range r.hours {
		files, err := s.files.Hour(r.projectID, hour)
		if err != nil {
			return err
		}
		r.files[hour] = files
		pinned = append(pinned, files...)
	}
	s.pin(pinned)
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
	for _, files := range r.files {
		r.store.unpin(files)
	}
}

// hoursWithLogs returns, newest first, the hours of [from, to) that may hold
// the Project's Logs: those with files, and those between the oldest and the
// newest Log not yet exported.
func (r *logRead) hoursWithLogs(from, to time.Time) ([]time.Time, error) {
	files := r.store.files
	hours := map[time.Time]bool{}
	first, last := from.Truncate(time.Hour), to.Add(-time.Microsecond).Truncate(time.Hour)

	days, err := os.ReadDir(files.ProjectDir(r.projectID))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, day := range days {
		d, ok := strings.CutPrefix(day.Name(), "day=")
		if !ok {
			continue
		}
		date, err := time.Parse(time.DateOnly, d)
		if err != nil || date.Before(LogDay(first)) || date.After(last) {
			continue
		}
		hourDirs, err := os.ReadDir(files.DayDir(r.projectID, date))
		if err != nil {
			return nil, err
		}
		for _, hourDir := range hourDirs {
			var h int
			if _, err := fmt.Sscanf(hourDir.Name(), "hour=%02d", &h); err != nil {
				continue
			}
			hour := date.Add(time.Duration(h) * time.Hour)
			if !hour.Before(first) && !hour.After(last) {
				hours[hour] = true
			}
		}
	}

	sources := []string{fmt.Sprintf("SELECT time FROM %s.%s WHERE project_id = %d AND %s",
		logsCatalog, lateTable, r.projectID, timeRange(from, to))}
	if r.hot {
		sources = append(sources, fmt.Sprintf("SELECT time FROM %s.%s WHERE %s",
			logsCatalog, hotTable(r.projectID), timeRange(from, to)))
	}
	var oldest, newest sql.NullTime
	err = r.conn.QueryRowContext(r.ctx, fmt.Sprintf("SELECT min(time), max(time) FROM (%s)", strings.Join(sources, " UNION ALL "))).
		Scan(&oldest, &newest)
	if err != nil {
		return nil, err
	}
	if oldest.Valid {
		for hour := oldest.Time.UTC().Truncate(time.Hour); !hour.After(newest.Time.UTC()); hour = hour.Add(time.Hour) {
			hours[hour] = true
		}
	}

	sorted := make([]time.Time, 0, len(hours))
	for hour := range hours {
		sorted = append(sorted, hour)
	}
	slices.SortFunc(sorted, func(a, b time.Time) int { return b.Compare(a) })
	return sorted, nil
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

// pin marks Log files as being read, so that no deletion takes them.
func (s *LogStore) pin(files []string) {
	s.pinMu.Lock()
	defer s.pinMu.Unlock()
	for _, file := range files {
		s.pins[file]++
	}
}

// unpin undoes pin.
func (s *LogStore) unpin(files []string) {
	s.pinMu.Lock()
	defer s.pinMu.Unlock()
	for _, file := range files {
		if s.pins[file]--; s.pins[file] <= 0 {
			delete(s.pins, file)
		}
	}
}

// pinnedIn reports whether a read has pinned a Log file inside dir.
func (s *LogStore) pinnedIn(dir string) bool {
	s.pinMu.Lock()
	defer s.pinMu.Unlock()
	prefix := filepath.Clean(dir) + string(filepath.Separator)
	for file := range s.pins {
		if strings.HasPrefix(file, prefix) {
			return true
		}
	}
	return false
}

// removeUnread deletes dir and everything in it, unless a read has pinned a
// file in it: then it deletes nothing and returns ErrLogFilesInUse. The
// caller holds publishMu, so no read pins a file meanwhile.
func (s *LogStore) removeUnread(dir string) error {
	if s.pinnedIn(dir) {
		return fmt.Errorf("delete %s: %w", dir, ErrLogFilesInUse)
	}
	return os.RemoveAll(dir)
}
