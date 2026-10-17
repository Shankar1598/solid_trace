package storage

import (
	"context"
	"fmt"
	"time"
)

// AttributeCount is how many of a Project's Logs carried an Attribute with
// values of one type, over some days. Type is "string", "number" or "bool",
// as log_key_counts names it.
type AttributeCount struct {
	Name string        `json:"name"`
	Type AttributeType `json:"type"`
	Logs int64         `json:"logs"`
}

// AttributeCounts returns the Attributes of a Project's Logs over the UTC
// days that [from, to) touches, the days of from and of to's last instant
// and those between: at most limit, most Logs first, then by name and type,
// and whether there were more. An empty range counts from's day.
//
// The counts are the popularity counts Log processing keeps in
// logs.log_key_counts, summed over the days. So they leave out JSON
// Attributes and late Logs, which are never counted, and a Project's day
// with more Attributes than keyCountsKept keeps only its top ones, with
// approximate counts.
func (s *LogStore) AttributeCounts(ctx context.Context, projectID uint32, from, to time.Time, limit int) ([]AttributeCount, bool, error) {
	first, last := LogDay(from), LogDay(from)
	if to.After(from) {
		last = LogDay(to.Add(-time.Nanosecond))
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT key, type, CAST(sum(logs) AS BIGINT) AS logs FROM logs.log_key_counts
		WHERE project_id = ? AND day BETWEEN CAST(? AS DATE) AND CAST(? AS DATE)
		GROUP BY key, type
		ORDER BY logs DESC, key, type
		LIMIT ?`,
		projectID, dayString(first), dayString(last), limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("count Attributes: %w", err)
	}
	defer rows.Close()
	counts := []AttributeCount{}
	for rows.Next() {
		var count AttributeCount
		if err := rows.Scan(&count.Name, &count.Type, &count.Logs); err != nil {
			return nil, false, err
		}
		counts = append(counts, count)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	if len(counts) > limit {
		return counts[:limit], true, nil
	}
	return counts, false, nil
}
