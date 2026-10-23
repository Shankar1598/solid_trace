package logquery

import (
	"fmt"
	"time"
)

// Histogram is the Query that counts the Logs that pass q's leading filters
// per bucket of time, of bucket's size from the epoch, and per Level:
// q's head, then summarize count() by bin(time, bucket), level. Its table's
// columns are time, level (NULL for Logs with no Level) and count_. Any
// stages after q's leading filters are left out, so the histogram counts
// the Logs a table is worked out from.
func (q *Query) Histogram(bucket time.Duration) *Query {
	text := fmt.Sprintf("summarize count() by bin(time, %dms), level", bucket.Milliseconds())
	h, err := Compile(text, Bounds{From: q.from, To: q.to}, nil)
	if err != nil {
		panic("logquery: histogram: " + err.Error())
	}
	h.head = q.head
	h.from, h.to = q.from, q.to
	return h
}

// Filters reports whether the Query has leading filters, where or search
// stages: a histogram counts only the Logs that pass them.
func (q *Query) Filters() bool {
	return len(q.head) > 0
}

// AutoBucket is the bucket size a histogram of a time range has when none is
// asked for: as bin_auto picks, at most about 100 buckets.
func AutoBucket(span time.Duration) time.Duration {
	return autoBin(span)
}
