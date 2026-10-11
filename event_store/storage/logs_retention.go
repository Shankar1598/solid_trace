package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

// LogProjects lists every Project that has anything of its Logs: a hot table,
// late Logs, files, promoted sets or popularity counts. Its Log processing
// cursor alone doesn't count.
func (s *LogStore) LogProjects() ([]uint32, error) {
	rows, err := s.db.Query(`
		SELECT CAST(substr(table_name, length('logs_hot_') + 1) AS UINTEGER)
		FROM duckdb_tables() WHERE database_name = ? AND schema_name = 'main' AND table_name LIKE 'logs\_hot\_%' ESCAPE '\'
		UNION SELECT project_id FROM logs.logs_late
		UNION SELECT project_id FROM logs.log_promoted_sets
		UNION SELECT project_id FROM logs.log_key_counts
		UNION SELECT project_id FROM logs.log_files`,
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
	return s.deleteLogs(projectID, func(ctx context.Context, conn *sql.Conn) error {
		before := dayString(day)
		for _, table := range []string{"log_promoted_sets", "log_key_counts"} {
			if _, err := conn.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s.%s WHERE project_id = ? AND day < CAST(? AS DATE)", logsCatalog, table),
				projectID, before); err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
		}
		if err := tombstoneLogFiles(ctx, conn, deletedForRetention,
			"project_id = ? AND day < CAST(? AS DATE)", projectID, before); err != nil {
			return err
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
	}, func() {
		s.promoted.deleteBefore(projectID, day)
	})
}

// deleteLogs runs change in a transaction on logs.duckdb, which deletes Logs
// and leaves tombstones of the files whose rows it deletes from the manifest,
// then deletes the bytes of tombstoned files (deleteTombstonedFiles). The
// commit and publish, which updates what is held in memory, happen with
// other writes locked out and no read beginning, as an export's commit and
// renames do, so a read has pinned every file whose row it saw before the
// bytes are deleted.
//
// The bytes of a file a read has pinned stay until a later pass deletes
// them. Only the Project's own pinned files return ErrLogFilesInUse, which
// comes only once the change has committed: another Project's wait for that
// Project's next pass, or the disk limit's.
func (s *LogStore) deleteLogs(projectID uint32, change func(context.Context, *sql.Conn) error, publish func()) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if err := s.commitDeletion(change, publish); err != nil {
		return err
	}
	pinned, err := s.deleteTombstonedFiles()
	if err != nil {
		return err
	}
	return filesInUse(slices.DeleteFunc(pinned, func(file logFile) bool { return file.projectID != projectID }))
}

// commitDeletion runs change in a transaction and commits it, then runs
// publish, both under publishMu.
func (s *LogStore) commitDeletion(change func(context.Context, *sql.Conn) error, publish func()) error {
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
	publish()
	return nil
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

// DeleteProjectLogs deletes every Log of a Project: its hot table, its late
// Logs, its files, its promoted sets and its popularity counts. Its Log
// processing cursor stays, so that a Log batch it already stored is never
// stored again.
func (s *LogStore) DeleteProjectLogs(projectID uint32) error {
	err := s.deleteLogs(projectID, func(ctx context.Context, conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s.%s", logsCatalog, hotTable(projectID))); err != nil {
			return fmt.Errorf("drop hot table: %w", err)
		}
		for _, table := range []string{lateTable, "log_promoted_sets", "log_key_counts"} {
			if _, err := conn.ExecContext(ctx,
				fmt.Sprintf("DELETE FROM %s.%s WHERE project_id = ?", logsCatalog, table), projectID); err != nil {
				return fmt.Errorf("delete %s: %w", table, err)
			}
		}
		return tombstoneLogFiles(ctx, conn, deletedWithProject, "project_id = ?", projectID)
	}, func() {
		s.promoted.deleteProject(projectID)
	})
	// Files a read has pinned don't hold up the checkpoint: the deletion
	// has committed, and the Project is no longer listed for another pass.
	if err != nil && !errors.Is(err, ErrLogFilesInUse) {
		return err
	}
	if _, err := s.db.Exec("CHECKPOINT " + logsCatalog); err != nil {
		logger.L.Warn("Checkpoint of logs DuckDB after deleting a Project's Logs failed", "error", err)
	}
	return err
}
