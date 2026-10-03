package storage

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

// LogProjects lists every Project that has anything of its Logs in
// logs.duckdb or on disk: a hot table, late Logs, files, promoted sets or
// popularity counts. Its Log processing cursor alone doesn't count.
func (s *LogStore) LogProjects() ([]uint32, error) {
	rows, err := s.db.Query(`
		SELECT CAST(substr(table_name, length('logs_hot_') + 1) AS UINTEGER)
		FROM duckdb_tables() WHERE database_name = ? AND schema_name = 'main' AND table_name LIKE 'logs\_hot\_%' ESCAPE '\'
		UNION SELECT project_id FROM logs.logs_late
		UNION SELECT project_id FROM logs.log_promoted_sets
		UNION SELECT project_id FROM logs.log_key_counts`,
		logsCatalog)
	if err != nil {
		return nil, fmt.Errorf("list projects with logs: %w", err)
	}
	defer rows.Close()
	projects := make(map[uint32]bool)
	for rows.Next() {
		var projectID uint32
		if err := rows.Scan(&projectID); err != nil {
			return nil, err
		}
		projects[projectID] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	dirs, err := s.files.projectDirs()
	if err != nil {
		return nil, err
	}
	for projectID := range dirs {
		projects[projectID] = true
	}
	list := make([]uint32, 0, len(projects))
	for projectID := range projects {
		list = append(list, projectID)
	}
	slices.Sort(list)
	return list, nil
}

// DeleteLogDaysBefore deletes a Project's Logs of every UTC day before that of
// day, wherever they are, with those days' promoted sets and popularity
// counts.
func (s *LogStore) DeleteLogDaysBefore(projectID uint32, day time.Time) error {
	day = LogDay(day)
	return s.deleteLogs(func(ctx context.Context, conn *sql.Conn) error {
		before := dayString(day)
		for _, table := range []string{"log_promoted_sets", "log_key_counts"} {
			if _, err := conn.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s.%s WHERE project_id = ? AND day < CAST(? AS DATE)", logsCatalog, table),
				projectID, before); err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
		}
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s.%s WHERE project_id = ? AND time < CAST(? AS TIMESTAMP)", logsCatalog, lateTable),
			projectID, timestampString(day)); err != nil {
			return fmt.Errorf("delete late logs: %w", err)
		}
		// Only when the hourly export has been failing for a day can the
		// hot table hold Logs this old.
		hot, err := hotTableExists(ctx, conn, projectID)
		if err != nil || !hot {
			return err
		}
		if _, err := conn.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s.%s WHERE time < CAST(? AS TIMESTAMP)", logsCatalog, hotTable(projectID)),
			timestampString(day)); err != nil {
			return fmt.Errorf("delete hot logs: %w", err)
		}
		return nil
	}, func() error {
		days, err := s.files.dayDirs(projectID)
		if err != nil {
			return err
		}
		for dirDay, dir := range days {
			if dirDay.Before(day) {
				if err := os.RemoveAll(dir); err != nil {
					return err
				}
			}
		}
		s.promoted.deleteBefore(projectID, day)
		return nil
	})
}

// deleteLogs runs change in a transaction on logs.duckdb and, once it
// commits, deletes files with publish. Both happen with the writes and
// readers locked out, as an export's commit and renames do, so a reader
// never sees a file vanish under it.
//
// If publish fails after the commit, the files it didn't delete stay until
// the next pass deletes them.
func (s *LogStore) deleteLogs(change func(context.Context, *sql.Conn) error, publish func() error) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	if err := change(ctx, conn); err != nil {
		if _, rollbackErr := conn.ExecContext(ctx, "ROLLBACK"); rollbackErr != nil {
			logger.L.Error("DuckDB rollback failed", "error", rollbackErr)
			discardConn(conn)
		}
		return err
	}
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		discardConn(conn)
		return err
	}
	return publish()
}

// hotTableExists reports whether a Project has a hot table, inside the
// connection's transaction.
func hotTableExists(ctx context.Context, conn *sql.Conn, projectID uint32) (bool, error) {
	var exists bool
	err := conn.QueryRowContext(ctx,
		"SELECT count(*) > 0 FROM duckdb_tables() WHERE database_name = ? AND schema_name = 'main' AND table_name = ?",
		logsCatalog, hotTable(projectID)).Scan(&exists)
	return exists, err
}

// projectDirs returns the folder of each Project that has one.
func (f LogFiles) projectDirs() (map[uint32]string, error) {
	dirs, err := filepath.Glob(filepath.Join(f.Dir, "project=*"))
	if err != nil {
		return nil, err
	}
	projects := make(map[uint32]string, len(dirs))
	for _, dir := range dirs {
		id, err := strconv.ParseUint(strings.TrimPrefix(filepath.Base(dir), "project="), 10, 32)
		if err != nil {
			continue // not a folder the export writes
		}
		projects[uint32(id)] = dir
	}
	return projects, nil
}

// dayDirs returns a Project's day folders by their UTC day.
func (f LogFiles) dayDirs(projectID uint32) (map[time.Time]string, error) {
	dirs, err := filepath.Glob(filepath.Join(f.ProjectDir(projectID), "day=*"))
	if err != nil {
		return nil, err
	}
	days := make(map[time.Time]string, len(dirs))
	for _, dir := range dirs {
		day, err := time.Parse(time.DateOnly, strings.TrimPrefix(filepath.Base(dir), "day="))
		if err != nil {
			continue // not a folder the export writes
		}
		days[day] = dir
	}
	return days, nil
}

// DeleteProjectLogs deletes every Log of a Project: its hot table, its late
// Logs, its files, its promoted sets and its popularity counts. Its Log
// processing cursor stays, so that a Log batch it already stored is never
// stored again.
func (s *LogStore) DeleteProjectLogs(projectID uint32) error {
	err := s.deleteLogs(func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", logsCatalog, hotTable(projectID))); err != nil {
			return fmt.Errorf("drop hot table: %w", err)
		}
		for _, table := range []string{lateTable, "log_promoted_sets", "log_key_counts"} {
			if _, err := conn.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s.%s WHERE project_id = ?", logsCatalog, table), projectID); err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
		}
		return nil
	}, func() error {
		s.promoted.deleteProject(projectID)
		return os.RemoveAll(s.files.ProjectDir(projectID))
	})
	if err != nil {
		return err
	}
	if _, err := s.db.Exec("CHECKPOINT " + logsCatalog); err != nil {
		logger.L.Warn("Checkpoint of logs DuckDB after deleting a Project's Logs failed", "error", err)
	}
	return nil
}
