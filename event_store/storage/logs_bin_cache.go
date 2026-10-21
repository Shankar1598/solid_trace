package storage

import (
	"container/list"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/solidtrace/event_store/pkg/logger"
)

// The bin cache keeps, in memory, the rows of a chart's time bins whose Logs
// are all in Log files, so that a chart run again, as a Dashboard refreshes,
// reads only the bins it lacks and the span the hot tables and late Logs
// still hold.
//
// A bin's rows are worked out from its Logs alone (logquery.Query.Bins), and
// are keyed by the Project, the Query, the bins' size, the bin's start and
// the manifest generations of the files of the hours it covers. Any change to
// those files, a late file, a re-export, Retention or a Project's deletion,
// changes the generations, so a bin from before is never taken again; it
// leaves the cache as the least recently used. A bin with a Log in a hot
// table or in logs_late is worked out on each run, and not kept.

// maxCachedBins is the most bins a run takes from the cache or adds to it; a
// run with more bins in its range runs as if there were no cache.
const maxCachedBins = 20_000

// TableBins says that a table's rows are made bin by bin, ordered by bin
// first, which lets a scan take bins from the bin cache.
type TableBins struct {
	// Size is the bins' size. Each bin starts a whole number of sizes
	// after 1970, and its start is the first column of its rows.
	Size time.Duration
	// Query identifies the table's Query: two with the same Query make the
	// same rows from the same Logs.
	Query string
	// Limit is the most rows the table has, where its query stops too.
	Limit int
}

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

// binCache is the bin cache: bins' rows by key, the least recently used
// dropped first once they take more than maxBytes.
type binCache struct {
	mu       sync.Mutex
	maxBytes int64
	bytes    int64
	// recent holds *binEntry, the most recently used first.
	recent  *list.List
	entries map[string]*list.Element

	hits, misses atomic.Int64
}

type binEntry struct {
	key   string
	rows  [][]any
	bytes int64
}

func newBinCache(maxBytes int64) *binCache {
	return &binCache{maxBytes: maxBytes, recent: list.New(), entries: map[string]*list.Element{}}
}

func (c *binCache) get(key string) ([][]any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	element, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.recent.MoveToFront(element)
	return element.Value.(*binEntry).rows, true
}

// put keeps a bin's rows, which no one may change after.
func (c *binCache) put(key string, rows [][]any) {
	entry := &binEntry{key: key, rows: rows, bytes: binEntryBytes(key, rows)}
	if entry.bytes > c.maxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if element, ok := c.entries[key]; ok {
		c.bytes -= element.Value.(*binEntry).bytes
		c.recent.Remove(element)
	}
	c.entries[key] = c.recent.PushFront(entry)
	c.bytes += entry.bytes
	for c.bytes > c.maxBytes {
		oldest := c.recent.Back()
		evicted := oldest.Value.(*binEntry)
		c.recent.Remove(oldest)
		delete(c.entries, evicted.key)
		c.bytes -= evicted.bytes
	}
}

// binEntryBytes guesses the memory a bin's entry takes.
func binEntryBytes(key string, rows [][]any) int64 {
	n := int64(160 + len(key))
	for _, row := range rows {
		n += 24 + 16*int64(len(row))
		for _, v := range row {
			n += valueBytes(v)
		}
	}
	return n
}

func valueBytes(v any) int64 {
	switch v := v.(type) {
	case nil:
		return 0
	case string:
		return int64(len(v))
	case []byte:
		return int64(len(v))
	case time.Time:
		return 24
	case map[string]any:
		n := int64(48)
		for key, part := range v {
			n += 32 + int64(len(key)) + valueBytes(part)
		}
		return n
	}
	return 8
}

// timeBin is one bin of a run's range that may come from the cache: key is
// its key, and rows its rows when hit says the cache had them.
type timeBin struct {
	start, end time.Time
	key        string
	rows       [][]any
	hit        bool
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

// binStart is the start of the bin of size that t is in.
func binStart(t time.Time, size time.Duration) time.Time {
	us, step := t.UnixMicro(), size.Microseconds()
	start := us / step * step
	if start > us {
		start -= step
	}
	return time.UnixMicro(start).UTC()
}

// rowBin is the start of a row's bin, its first column, in microseconds
// since 1970.
func rowBin(row []any) (int64, error) {
	if len(row) > 0 {
		if start, ok := row[0].(time.Time); ok {
			return start.UnixMicro(), nil
		}
	}
	return 0, fmt.Errorf("a binned table's row starts with %v, not its bin's time", row)
}

// mustRowBin is rowBin of a row rowBin has read before.
func mustRowBin(row []any) int64 {
	start, _ := rowBin(row)
	return start
}
