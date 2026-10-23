package storage

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

// DuckDB's bin cache keeps, in memory, the rows of a chart's time bins whose
// Logs are all in Log files, so that a chart run again, as a Dashboard
// refreshes, reads only the bins it lacks and the span the hot tables and
// late Logs still hold. ClickHouse's tables have their own bin cache
// (logs_clickhouse_table.go), which shares its parts.
//
// A bin's rows are worked out from its Logs alone (logquery.Query.Bins), and
// are keyed by the Project, the Query, the bins' size, the bin's start and
// the manifest generations of the files of the hours it covers. Any change to
// those files, a late file, a re-export, Retention or a Project's deletion,
// changes the generations, so a bin from before is never taken again; it
// leaves the cache as the least recently used. A bin with a Log in a hot
// table or in logs_late is worked out on each run, and not kept.

// UseBinCache keeps up to maxBytes of bins' rows in the bin cache, which
// starts empty; 0 turns it off. Without it, there is no cache.
func (s *LogStore) UseBinCache(maxBytes int64) {
	if maxBytes <= 0 {
		s.bins.Store(nil)
		return
	}
	s.bins.Store(newBinCache(maxBytes))
}

// BinCacheStats returns how many bins runs have taken from the bin cache,
// and how many they worked out because it lacked them, since UseBinCache.
func (s *LogStore) BinCacheStats() (hits, misses int64) {
	cache := s.bins.Load()
	if cache == nil {
		return 0, 0
	}
	return cache.hits.Load(), cache.misses.Load()
}

// scanBinnedTable is ScanLogTable for a table made bin by bin, with the bin
// cache: each bin wholly in [from, to) with no Log in a hot table or
// logs_late comes from the cache if it has it. The rest of the range is
// worked out in one query, and its whole bins are kept in the cache. The rows
// are those the query over the whole range would give.
func (r *logRead) scanBinnedTable(scan LogTableScan, cache *binCache, from, to time.Time) ([][]any, error) {
	size := scan.Bins.Size
	first := binStart(from, size)
	if first.Before(from) {
		first = first.Add(size)
	}
	if to.Sub(from)/size > maxCachedBins {
		return r.queryTable(scan, []timeSpan{{from, to}})
	}
	live, err := r.binsWithLiveLogs(from, to, size)
	if err != nil {
		return nil, err
	}
	var bins []timeBin
	for start := first; !start.Add(size).After(to); start = start.Add(size) {
		if live[start.UnixMicro()] {
			continue
		}
		bin := timeBin{start: start, end: start.Add(size), key: r.binKey(scan.Bins, start, size)}
		bin.rows, bin.hit = cache.get(bin.key)
		bins = append(bins, bin)
	}

	// What the cache lacks is worked out in one query, in spans between
	// the bins it had.
	var spans []timeSpan
	at := from
	for _, bin := range bins {
		if !bin.hit {
			continue
		}
		if at.Before(bin.start) {
			spans = append(spans, timeSpan{at, bin.start})
		}
		at = bin.end
	}
	if at.Before(to) {
		spans = append(spans, timeSpan{at, to})
	}
	var rows [][]any
	if len(spans) > 0 {
		if rows, err = r.queryTable(scan, spans); err != nil {
			return nil, err
		}
	}

	// A bin is kept with all its rows: when the query stopped at its limit,
	// only those of the bins before its last row's.
	byStart := map[int64][][]any{}
	for _, row := range rows {
		start, err := rowBin(row)
		if err != nil {
			return nil, err
		}
		byStart[start] = append(byStart[start], row)
	}
	complete := func(time.Time) bool { return true }
	if len(rows) >= scan.Bins.Limit && len(rows) > 0 {
		last, _ := rowBin(rows[len(rows)-1])
		complete = func(start time.Time) bool { return start.UnixMicro() < last }
	}
	hits, misses := 0, 0
	for _, bin := range bins {
		if bin.hit {
			hits++
			continue
		}
		misses++
		if complete(bin.start) {
			cache.put(bin.key, byStart[bin.start.UnixMicro()])
		}
	}
	cache.hits.Add(int64(hits))
	cache.misses.Add(int64(misses))
	logger.L.Debug("Bin cache", "project_id", r.projectID, "hits", hits, "misses", misses, "live_spans", len(spans))

	// The rows in order of their bins, as the query sorts its rows by bin
	// first: the cached bins' between the query's.
	table := make([][]any, 0, min(len(rows)+hits, scan.Bins.Limit))
	next := 0
	for _, bin := range bins {
		if !bin.hit {
			continue
		}
		for next < len(rows) && mustRowBin(rows[next]) < bin.start.UnixMicro() {
			table = append(table, rows[next])
			next++
		}
		table = append(table, bin.rows...)
	}
	table = append(table, rows[next:]...)
	return table[:min(len(table), scan.Bins.Limit)], nil
}

// queryTable runs the table's query over the Logs of spans.
func (r *logRead) queryTable(scan LogTableScan, spans []timeSpan) ([][]any, error) {
	rows, err := queryRead(r.ctx, r.conn, scan.Query(r.sourcesIn(spans, scan.Select, scan.Where)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTable(rows, -1)
}

// binsWithLiveLogs returns the starts, in microseconds since 1970, of the
// bins of [from, to) that hold a Log in the Project's hot table or late Logs.
func (r *logRead) binsWithLiveLogs(from, to time.Time, size time.Duration) (map[int64]bool, error) {
	sources := []string{fmt.Sprintf("SELECT time FROM %s.%s WHERE project_id = %d AND %s",
		logsCatalog, lateTable, r.projectID, timeRange(from, to))}
	if r.hot {
		sources = append(sources, fmt.Sprintf("SELECT time FROM %s.%s WHERE %s", logsCatalog, hotTable(r.projectID), timeRange(from, to)))
	}
	rows, err := queryRead(r.ctx, r.conn, fmt.Sprintf(
		"SELECT DISTINCT epoch_us(time_bucket(to_microseconds(%d), time, TIMESTAMP '1970-01-01')) FROM (%s)",
		size.Microseconds(), strings.Join(sources, " UNION ALL ")))
	if err != nil {
		return nil, fmt.Errorf("list bins with live logs: %w", err)
	}
	defer rows.Close()
	live := map[int64]bool{}
	for rows.Next() {
		var start int64
		if err := rows.Scan(&start); err != nil {
			return nil, err
		}
		live[start] = true
	}
	return live, rows.Err()
}

// binKey is the cache key of the bin of size starting at start: the
// Project, the Query, the size, the start, and the generations of the files
// of the hours the bin covers, which no other files ever have.
func (r *logRead) binKey(bins *TableBins, start time.Time, size time.Duration) string {
	var key strings.Builder
	fmt.Fprintf(&key, "%d/%s/%d/%d/", r.projectID, bins.Query, size.Microseconds(), start.UnixMicro())
	for hour := start.Truncate(time.Hour); hour.Before(start.Add(size)); hour = hour.Add(time.Hour) {
		for _, generation := range r.generations[hour] {
			key.WriteString(strconv.FormatInt(generation, 10))
			key.WriteByte(',')
		}
	}
	return key.String()
}
