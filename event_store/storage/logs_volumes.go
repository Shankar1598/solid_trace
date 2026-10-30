package storage

import (
	"context"
	"fmt"
	"time"
)

// CountedLogs counts the Logs of each Project of projectIDs whose own time
// is in [from, to), from the histogram's counts by minute, so from and to
// should be whole minutes. It never reads logs. A Project with none is left
// out.
func (s *ClickHouseLogs) CountedLogs(ctx context.Context, projectIDs []uint32, from, to time.Time) (map[uint32]int64, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT project_id, toInt64(sum(logs))
		FROM log_minute_counts
		WHERE project_id IN ? AND minute >= ? AND minute < ?
		GROUP BY project_id`, projectIDs, from, to)
	if err != nil {
		return nil, fmt.Errorf("count Logs by minute: %w", err)
	}
	defer rows.Close()
	counts := map[uint32]int64{}
	for rows.Next() {
		var projectID uint32
		var logs int64
		if err := rows.Scan(&projectID, &logs); err != nil {
			return nil, err
		}
		counts[projectID] = logs
	}
	return counts, rows.Err()
}
