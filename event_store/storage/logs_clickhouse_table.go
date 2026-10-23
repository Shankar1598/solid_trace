package storage

import (
	"container/list"
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/solidtrace/event_store/pkg/chsql"
	"github.com/solidtrace/event_store/pkg/logger"
)

// TableScan asks for a table worked out from a Project's Logs in a time
// range.
type TableScan struct {
	ProjectID uint32
	// From and To are the time range, [From, To).
	From, To time.Time
	// Where, if set, returns the SQL condition a Log must meet. An empty
	// Filter keeps every Log.
	Where func(ClickHouseColumns) Filter
	// Select returns the select list the Logs are read with.
	Select func(ClickHouseColumns) string
	// Query returns the query that works the table out from logs, the
	// query that reads the Logs.
	Query func(logs string) string
	// Bins, if set, says the table is made bin by bin, so that bins of
	// hours that have ended can come from the bin cache.
	Bins *TableBins
	// Now is the time the scan runs at: the hours before its hour have
	// ended.
	Now time.Time
}

// ScanLogTable runs a query over a Project's Logs in a time range and
// returns its rows. Each value is as ClickHouse gives it, with NULL as nil:
// a string, a number, a bool, a time, or a tuple's values as a []any.
//
// With Bins and the bin cache on, it runs over only the bins the cache
// lacks, and keeps those of hours that have ended (scanBinnedTable).
// Cancelling ctx stops it.
func (s *ClickHouseLogs) ScanLogTable(ctx context.Context, scan TableScan) ([][]any, error) {
	from, to := storableRange(scan.From, scan.To)
	if !to.After(from) {
		to = from
	}
	ctx = s.old.withBucketKey(ctx)
	if cache := s.bins.Load(); cache != nil && scan.Bins != nil && from.Before(to) {
		return s.scanBinnedTable(ctx, scan, cache, from, to)
	}
	return s.queryTable(ctx, scan.Query(s.tableSource(scan, []timeSpan{{from, to}})))
}

// timeSpan is the time range [from, to).
type timeSpan struct {
	from, to time.Time
}

// tableSource is the query that reads a table's Logs of spans, which are in
// time order and apart: those in logs, and those in the old Log files.
func (s *ClickHouseLogs) tableSource(scan TableScan, spans []timeSpan) string {
	conditions := []string{fmt.Sprintf("project_id = %d", scan.ProjectID), timeSpans(spans)}
	source := fmt.Sprintf("SELECT %s FROM logs %s", scan.Select(ClickHouseColumns{}), whereClause(conditions, filterOf(scan.Where)))
	return source + s.old.tableSources(scan, spans)
}

// timeSpans is the condition that time is in one of spans.
func timeSpans(spans []timeSpan) string {
	inSpans := make([]string, len(spans))
	for i, span := range spans {
		inSpans[i] = fmt.Sprintf("(time >= %s AND time < %s)", chsql.Time(span.from), chsql.Time(span.to))
	}
	return "(" + strings.Join(inSpans, " OR ") + ")"
}

// queryTable runs a query and reads its rows, as ScanLogTable returns them,
// recording it in ctx's ReadStatements.
func (s *ClickHouseLogs) queryTable(ctx context.Context, query string) (table [][]any, err error) {
	began := time.Now()
	defer func() { recordStatement(ctx, query, began, err) }()
	rows, err := s.conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	types := rows.ColumnTypes()
	table = [][]any{}
	for rows.Next() {
		dest := make([]any, len(types))
		for i, t := range types {
			dest[i] = reflect.New(t.ScanType()).Interface()
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		row := make([]any, len(dest))
		for i, d := range dest {
			row[i] = plainValue(reflect.ValueOf(d).Elem())
		}
		table = append(table, row)
	}
	return table, rows.Err()
}

// plainValue is a value clickhouse-go scanned, with pointers followed and
// NULL as nil, in a tuple's values too.
func plainValue(v reflect.Value) any {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Slice && v.Type().Elem().Kind() == reflect.Interface {
		values := make([]any, v.Len())
		for i := range values {
			values[i] = plainValue(v.Index(i))
		}
		return values
	}
	return v.Interface()
}

// The bin cache keeps, in memory, the rows of a chart's time bins of hours
// that have ended, so that a chart run again, as a Dashboard refreshes,
// reads only the bins it lacks and the hours still going.
//
// A bin's rows are worked out from its Logs alone (logquery.Query.Bins), and
// are keyed by the Project, the Query, the bins' size, the bin's start and
// the generations of the hours it covers. A (Project, hour)'s generation
// changes once Log processing has inserted Logs of that hour, or its day's
// Logs are deleted, so a bin from before is never taken again; it leaves the cache as the least recently
// used. Generations are in memory too, so a restart empties both together.
//
// A bin of an hour that hasn't ended isn't kept: Logs still arrive in it, and
// would change its generation at once.

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
func (s *ClickHouseLogs) UseBinCache(maxBytes int64) {
	if maxBytes <= 0 {
		s.bins.Store(nil)
		return
	}
	s.bins.Store(newBinCache(maxBytes))
}

// BinCacheStats returns how many bins runs have taken from the bin cache,
// and how many they worked out because it lacked them, since UseBinCache.
func (s *ClickHouseLogs) BinCacheStats() (hits, misses int64) {
	cache := s.bins.Load()
	if cache == nil {
		return 0, 0
	}
	return cache.hits.Load(), cache.misses.Load()
}

// projectHour is an hour of a Project's Logs.
type projectHour struct {
	projectID uint32
	hour      int64 // Unix seconds
}

// generations are the bin cache's generations of (Project, hour)s: 0 until
// Logs are inserted into the hour, then changed on each insert, and when
// Retention, Project deletion or the disk limit deletes the hour's day.
type generations struct {
	mu   sync.Mutex
	byID map[projectHour]uint64
}

// bump changes the generation of each hour of a Project that the batches'
// Logs are in.
func (g *generations) bump(projectID uint32, batches []LogBatch) {
	hours := map[projectHour]bool{}
	for _, batch := range batches {
		for _, log := range batch.Logs {
			hours[projectHour{projectID, log.Time.Truncate(time.Hour).Unix()}] = true
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.byID == nil {
		g.byID = map[projectHour]uint64{}
	}
	for hour := range hours {
		g.byID[hour]++
	}
}

// bumpDay changes the generation of each hour of a Project's UTC day, as
// when its Logs are deleted.
func (g *generations) bumpDay(projectID uint32, day time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.byID == nil {
		g.byID = map[projectHour]uint64{}
	}
	for hour := LogDay(day); hour.Before(LogDay(day).AddDate(0, 0, 1)); hour = hour.Add(time.Hour) {
		g.byID[projectHour{projectID, hour.Unix()}]++
	}
}

// key writes the generations of a Project's hours that [start, end) covers.
func (g *generations) key(key *strings.Builder, projectID uint32, start, end time.Time) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for hour := start.Truncate(time.Hour); hour.Before(end); hour = hour.Add(time.Hour) {
		key.WriteString(strconv.FormatUint(g.byID[projectHour{projectID, hour.Unix()}], 10))
		key.WriteByte(',')
	}
}

// scanBinnedTable is ScanLogTable for a table made bin by bin, with the bin
// cache: each bin wholly in [from, to) whose hours have ended comes from the
// cache if it has it. The rest of the range is worked out in one query, and
// its bins of hours that have ended are kept in the cache. The rows are those
// the query over the whole range would give.
func (s *ClickHouseLogs) scanBinnedTable(ctx context.Context, scan TableScan, cache *binCache, from, to time.Time) ([][]any, error) {
	size := scan.Bins.Size
	whole := []timeSpan{{from, to}}
	first := binStart(from, size)
	if first.Before(from) {
		first = first.Add(size)
	}
	if to.Sub(from)/size > maxCachedBins {
		return s.queryTable(ctx, scan.Query(s.tableSource(scan, whole)))
	}
	// The keys are read before the query runs: a bin worked out from Logs
	// inserted after its key was read is kept under a key no run reads
	// again.
	var bins []timeBin
	for start := first; !start.Add(size).After(to); start = start.Add(size) {
		end := start.Add(size)
		if lastHour := end.Add(-time.Microsecond).Truncate(time.Hour); lastHour.Add(time.Hour).After(scan.Now) {
			break
		}
		var key strings.Builder
		fmt.Fprintf(&key, "%d/%s/%d/%d/", scan.ProjectID, scan.Bins.Query, size.Microseconds(), start.UnixMicro())
		s.generations.key(&key, scan.ProjectID, start, end)
		bin := timeBin{start: start, end: end, key: key.String()}
		bin.rows, bin.hit = cache.get(bin.key)
		bins = append(bins, bin)
	}
	if len(bins) == 0 {
		// No hour of the range has ended, as in the explorer's last hour.
		return s.queryTable(ctx, scan.Query(s.tableSource(scan, whole)))
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
		var err error
		if rows, err = s.queryTable(ctx, scan.Query(s.tableSource(scan, spans))); err != nil {
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
	logger.L.Debug("Bin cache", "project_id", scan.ProjectID, "hits", hits, "misses", misses, "spans", len(spans))

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
	case []any:
		n := int64(24)
		for _, part := range v {
			n += 16 + valueBytes(part)
		}
		return n
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
