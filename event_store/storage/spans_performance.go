package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

// Performance reads: a Project's Transactions by name, and one Transaction's
// breakdown. They read segments for counts and durations, each weighted by
// 1 / its sample rate so that a 1% sample stands for the requests it was
// drawn from, and Spans for where the time went.

// errorStatus is a segment's status that makes it an error: any the SDK sent
// but "ok". A segment sent with none isn't one.
const errorStatus = "status NOT IN ('', 'ok')"

// TransactionsScope is a Project's Transactions over [From, To), in one
// environment, or every one when Environment is "".
type TransactionsScope struct {
	ProjectID   uint32
	From, To    time.Time
	Environment string
}

// TransactionStats are a Transaction name's segments over a period.
type TransactionStats struct {
	// Sampled is how many were stored; Requests how many requests they
	// stand for, each counted 1 / its sample rate.
	Sampled  uint64
	Requests float64
	// Errors are the requests that failed, weighted as Requests are.
	Errors float64
	// DurationUs is the requests' time, weighted as Requests are.
	DurationUs float64
	// P50Us, P95Us and P99Us are percentiles of the sampled durations,
	// unweighted.
	P50Us, P95Us, P99Us float64
}

// TransactionTotals are a Transaction name's numbers over the range and the
// period of the same length before it.
type TransactionTotals struct {
	Name              string
	Current, Previous TransactionStats
}

// maxTransactionNames bounds the names one read answers, the most time
// consuming first.
const maxTransactionNames = 1000

// Transactions reads a Project's Transaction names with segments in the
// scope's range, the most time consuming first, each with its numbers over
// the range and over the period of the same length before it. It also
// answers the environments of the range's segments in every environment.
func (ch *ClickHouse) Transactions(ctx context.Context, scope TransactionsScope) ([]TransactionTotals, []string, error) {
	ctx = TagRead(ctx, scope.ProjectID, ReadTransactions)
	previous := scope.From.Add(-scope.To.Sub(scope.From))
	where, args := segmentsWhere(scope.ProjectID, "", scope.Environment)
	args = append(args, clickhouse.Named("previous", previous), clickhouse.Named("from", scope.From),
		clickhouse.Named("to", scope.To))
	rows, err := ch.conn.Query(ctx, fmt.Sprintf(`
		SELECT transaction,
			countIf(current), sumIf(weight, current), sumIf(weight, current AND failed),
			sumIf(duration_us * weight, current), quantilesIf(0.5, 0.95, 0.99)(duration_us, current),
			countIf(NOT current), sumIf(weight, NOT current), sumIf(weight, NOT current AND failed),
			sumIf(duration_us * weight, NOT current), quantilesIf(0.5, 0.95, 0.99)(duration_us, NOT current)
		FROM (
			SELECT transaction, duration_us, 1 / sample_rate AS weight, %s AS failed,
				start >= {from:DateTime64(6, 'UTC')} AS current
			FROM spans
			WHERE %s AND start >= {previous:DateTime64(6, 'UTC')} AND start < {to:DateTime64(6, 'UTC')}
		)
		GROUP BY transaction
		HAVING countIf(current) > 0
		ORDER BY sumIf(duration_us * weight, current) DESC, transaction
		LIMIT %d`, errorStatus, where, maxTransactionNames), args...)
	if err != nil {
		return nil, nil, fmt.Errorf("read transactions: %w", err)
	}
	var totals []TransactionTotals
	for rows.Next() {
		var (
			t                TransactionTotals
			current, earlier []float64
		)
		c, p := &t.Current, &t.Previous
		if err := rows.Scan(&t.Name, &c.Sampled, &c.Requests, &c.Errors, &c.DurationUs, &current,
			&p.Sampled, &p.Requests, &p.Errors, &p.DurationUs, &earlier); err != nil {
			rows.Close()
			return nil, nil, err
		}
		c.setPercentiles(current)
		p.setPercentiles(earlier)
		totals = append(totals, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	where, args = segmentsWhere(scope.ProjectID, "", "")
	args = append(args, clickhouse.Named("from", scope.From), clickhouse.Named("to", scope.To))
	var environments []string
	if err := ch.conn.QueryRow(ctx, `
		SELECT arraySort(groupUniqArray(environment)) FROM spans
		WHERE `+where+` AND environment != ''
			AND start >= {from:DateTime64(6, 'UTC')} AND start < {to:DateTime64(6, 'UTC')}`, args...,
	).Scan(&environments); err != nil {
		return nil, nil, fmt.Errorf("read transactions' environments: %w", err)
	}
	return totals, environments, nil
}

func (s *TransactionStats) setPercentiles(p []float64) {
	if s.Sampled > 0 && len(p) == 3 {
		s.P50Us, s.P95Us, s.P99Us = p[0], p[1], p[2]
	}
}

// segmentsWhere is the condition on a Project's segments, of one
// Transaction name unless name is "", in one environment unless
// environment is "".
func segmentsWhere(projectID uint32, name, environment string) (string, []any) {
	where := "project_id = {project:UInt32} AND is_segment"
	args := []any{clickhouse.Named("project", projectID)}
	if name != "" {
		where += " AND transaction = {transaction:String}"
		args = append(args, clickhouse.Named("transaction", name))
	}
	if environment != "" {
		where += " AND environment = {environment:String}"
		args = append(args, clickhouse.Named("environment", environment))
	}
	return where, args
}

// TransactionScope is one Transaction name's requests over [From, To), in
// one environment, or every one when Environment is "".
type TransactionScope struct {
	ProjectID   uint32
	Name        string
	From, To    time.Time
	Environment string
	// Bucket is the width of the release series' points.
	Bucket time.Duration
}

// TransactionBreakdown is where one Transaction's time went.
type TransactionBreakdown struct {
	// Totals are its segments' numbers.
	Totals TransactionStats
	// Releases are its durations by its newest releases, the first seen
	// first.
	Releases []ReleaseDurations
	// Groups are its Spans' self time by group, the most first. A group
	// has one op.
	Groups []GroupSelfTime
	// Repeats are the groups that repeat within a request, the most
	// repeated first.
	Repeats []RepeatedGroup
	// Slowest are its slowest requests, the slowest first.
	Slowest []SlowRequest
}

// ReleaseDurations are a release's sampled durations over the range, and in
// each bucket of it that has any.
type ReleaseDurations struct {
	Release      string
	Sampled      uint64
	P50Us, P95Us float64
	Points       []ReleasePoint
}

type ReleasePoint struct {
	Time         time.Time
	Sampled      uint64
	P50Us, P95Us float64
}

// GroupSelfTime is the self time of a group's Spans, weighted as requests
// are, with one of its descriptions as sent.
type GroupSelfTime struct {
	Group, Op, Description string
	SelfTimeUs             float64
}

// RepeatedGroup is a group that some request ran more than once: the median
// and the most times a request with it ran it, and how many sampled requests
// ran it.
type RepeatedGroup struct {
	Group, Op, Description string
	Median                 float64
	Max                    uint32
	Requests               uint64
}

// SlowRequest is one sampled request of a Transaction.
type SlowRequest struct {
	TraceID     string
	Start       time.Time
	DurationUs  uint64
	Status      string
	Release     string
	Environment string
}

// Most of each list a breakdown answers.
const (
	maxRepeatedGroups  = 20
	maxSlowestRequests = 10
	maxReleases        = 20
)

// TransactionBreakdown reads one Transaction's numbers, its durations by
// release, its self time by group, its repeated groups and its slowest
// requests.
func (ch *ClickHouse) TransactionBreakdown(ctx context.Context, scope TransactionScope) (TransactionBreakdown, error) {
	ctx = TagRead(ctx, scope.ProjectID, ReadTransaction)
	var b TransactionBreakdown
	segments, args := segmentsWhere(scope.ProjectID, scope.Name, scope.Environment)
	args = append(args, clickhouse.Named("from", scope.From), clickhouse.Named("to", scope.To))
	inRange := " AND start >= {from:DateTime64(6, 'UTC')} AND start < {to:DateTime64(6, 'UTC')}"
	segments += inRange
	// Every row of the Transaction's requests, segments and Spans.
	spans := "project_id = {project:UInt32} AND transaction = {transaction:String}" + inRange
	if scope.Environment != "" {
		spans += " AND environment = {environment:String}"
	}
	bucket := max(int64(scope.Bucket.Seconds()), 1)

	// Its totals, each release's, and each release's by bucket.
	rows, err := ch.conn.Query(ctx, fmt.Sprintf(`
		SELECT grouping(release), release, count(), sum(1 / sample_rate), sum(1 / sample_rate * (%s)),
			sum(duration_us / sample_rate), quantiles(0.5, 0.95, 0.99)(duration_us)
		FROM spans WHERE %s
		GROUP BY GROUPING SETS ((), (release))
		ORDER BY min(start), release`, errorStatus, segments), args...)
	if err != nil {
		return b, fmt.Errorf("read a transaction's releases: %w", err)
	}
	releases := map[string]int{}
	for rows.Next() {
		var (
			grouping    uint64
			release     string
			s           TransactionStats
			percentiles []float64
		)
		if err := rows.Scan(&grouping, &release, &s.Sampled, &s.Requests, &s.Errors,
			&s.DurationUs, &percentiles); err != nil {
			rows.Close()
			return b, err
		}
		s.setPercentiles(percentiles)
		if grouping == 1 { // the totals, grouped by no release
			b.Totals = s
		} else {
			releases[release] = len(b.Releases)
			b.Releases = append(b.Releases, ReleaseDurations{Release: release, Sampled: s.Sampled, P50Us: s.P50Us, P95Us: s.P95Us})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return b, err
	}
	// The newest releases, still the first seen first.
	if over := len(b.Releases) - maxReleases; over > 0 {
		b.Releases = b.Releases[over:]
		clear(releases)
		for i, r := range b.Releases {
			releases[r.Release] = i
		}
	}
	rows, err = ch.conn.Query(ctx, fmt.Sprintf(`
		SELECT release, toStartOfInterval(start, INTERVAL %d SECOND) AS bucket, count(), quantiles(0.5, 0.95)(duration_us)
		FROM spans WHERE %s
		GROUP BY release, bucket ORDER BY bucket, release`, bucket, segments), args...)
	if err != nil {
		return b, fmt.Errorf("read a transaction's releases by time: %w", err)
	}
	for rows.Next() {
		var (
			p           ReleasePoint
			release     string
			percentiles []float64
		)
		if err := rows.Scan(&release, &p.Time, &p.Sampled, &percentiles); err != nil {
			rows.Close()
			return b, err
		}
		i, ok := releases[release]
		if !ok || i >= len(b.Releases) {
			continue
		}
		p.Time = p.Time.UTC()
		p.P50Us, p.P95Us = percentiles[0], percentiles[1]
		b.Releases[i].Points = append(b.Releases[i].Points, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return b, err
	}

	// Self time by group, its segment's included.
	rows, err = ch.conn.Query(ctx, `
		SELECT group_hash, any(op), any(description), sum(self_time_us / sample_rate) AS self
		FROM spans WHERE `+spans+`
		GROUP BY group_hash ORDER BY self DESC, group_hash`, args...)
	if err != nil {
		return b, fmt.Errorf("read a transaction's self time: %w", err)
	}
	for rows.Next() {
		var g GroupSelfTime
		if err := rows.Scan(&g.Group, &g.Op, &g.Description, &g.SelfTimeUs); err != nil {
			rows.Close()
			return b, err
		}
		b.Groups = append(b.Groups, g)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return b, err
	}

	// Repeats are per request: each of a request's Spans in a group has the
	// group's count in it.
	rows, err = ch.conn.Query(ctx, fmt.Sprintf(`
		SELECT group_hash, any(op), any(description), quantile(0.5)(repeats), max(repeats), count()
		FROM (
			SELECT trace_id, segment_id, group_hash, any(op) AS op, any(description) AS description, any(repeats) AS repeats
			FROM spans WHERE %s AND NOT is_segment
			GROUP BY trace_id, segment_id, group_hash
		)
		GROUP BY group_hash HAVING max(repeats) > 1
		ORDER BY max(repeats) DESC, quantile(0.5)(repeats) DESC, group_hash
		LIMIT %d`, spans, maxRepeatedGroups), args...)
	if err != nil {
		return b, fmt.Errorf("read a transaction's repeats: %w", err)
	}
	for rows.Next() {
		var r RepeatedGroup
		if err := rows.Scan(&r.Group, &r.Op, &r.Description, &r.Median, &r.Max, &r.Requests); err != nil {
			rows.Close()
			return b, err
		}
		b.Repeats = append(b.Repeats, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return b, err
	}

	rows, err = ch.conn.Query(ctx, fmt.Sprintf(`
		SELECT trace_id, start, duration_us, status, release, environment
		FROM spans WHERE %s
		ORDER BY duration_us DESC, start DESC LIMIT %d`, segments, maxSlowestRequests), args...)
	if err != nil {
		return b, fmt.Errorf("read a transaction's slowest requests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r SlowRequest
		if err := rows.Scan(&r.TraceID, &r.Start, &r.DurationUs, &r.Status, &r.Release, &r.Environment); err != nil {
			return b, err
		}
		r.Start = r.Start.UTC()
		b.Slowest = append(b.Slowest, r)
	}
	return b, rows.Err()
}
