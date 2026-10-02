package logslayouts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Config struct {
	Layout   Layout
	Dir      string // this layout's working files; deleted afterwards
	Seed     uint64
	Settings Settings
	// Unthrottled writes as fast as possible; Target writes at the target
	// rate, the second half with queries running; Burst at BurstFactor×.
	Unthrottled, Target, Burst time.Duration
	BurstFactor                int
	FlushLogs                  int // Logs per flush when unthrottled
	RowGroupSizes              []int
	// Promote lists the N to try for per-Project hourly Parquet with each
	// file's top N keys promoted to columns (N > 0 for the MAP layout only).
	Promote []int
	// VerifyMaxID caps the Logs the cross-layout check covers, so layouts that
	// wrote different amounts are compared on the same Logs.
	VerifyMaxID  int64
	QueryTimeout time.Duration
	// SetBudget bounds each query set (hot, prefetch, Parquet async on/off,
	// verify): queries left when it runs out are skipped, so an obviously
	// slow layout does not run for long.
	SetBudget time.Duration
	// CheckpointTimeout bounds the final checkpoint; past it the layout stops.
	CheckpointTimeout time.Duration
	Keep              bool
	Logf              func(format string, args ...any)
}

type Report struct {
	Layout              string
	Engine              string
	Machine             string
	Started             time.Time
	Settings            Settings
	EngineSettings      map[string]string
	GeneratorLogsPerSec float64
	Phases              []Phase
	Rows                int64
	Bytes               int64
	CheckpointMs        float64
	DBFileBytes         int64
	Shredding           []RowGroupShred `json:",omitempty"`
	HotQueries          []QueryTiming
	// HotQueriesPrefetch repeats the cold hot-table queries with
	// storage_block_prefetch forced on, when the engine accepts that.
	HotPrefetchSetting string
	HotQueriesPrefetch []QueryTiming `json:",omitempty"`
	VerifyMaxID        int64
	Verify             map[string]Result
	Parquet            []ParquetRun
	// ProjectRows is how many Logs the queried Project (1) has.
	ProjectRows int64
	Promotion   []PromotionRun `json:",omitempty"`
	Errors      []string       `json:",omitempty"`
}

// PromotionRun is per-Project hourly Parquet with each file's top N keys
// promoted to columns.
type PromotionRun struct {
	N                 int
	TopKeysMs, CopyMs float64
	Files             int
	Bytes             int64
	Forms             []FormRun
}

type FormRun struct {
	Form    string
	Queries []QueryTiming
	// Threads1 repeats Queries with threads = 1, to estimate scaling.
	Threads1 []QueryTiming `json:",omitempty"`
	Verify   map[string]Result
}

type Phase struct {
	Name            string
	Seconds         float64
	TargetBytesPerS float64 // 0 when unthrottled
	Logs, Bytes     int64
	LogsPerSec      float64
	MBPerSec        float64
	Flushes         int
	FlushP50Ms      float64
	FlushP99Ms      float64
	FlushMaxMs      float64
	MaxBacklogLogs  int64
	KeptUp          bool
	PeakRSSMB       float64
	CPUPercent      float64 // 100 = one core
	DuckDBMemoryMB  float64
	QueriesRun      int
	QueryP50Ms      float64 `json:",omitempty"`
	QueryMaxMs      float64 `json:",omitempty"`
	QueryErrors     int     `json:",omitempty"`
}

type RowGroupShred struct {
	RowGroup int64
	Rows     int64
	Shredded bool
}

type QueryTiming struct {
	Name   string
	ColdMs float64
	WarmMs float64 // median of three
	Rows   int
	Error  string `json:",omitempty"`
	// Skipped is set when the query set ran out of its time budget first.
	Skipped bool `json:",omitempty"`
}

type ParquetRun struct {
	RowGroupSize int
	ExportMs     float64
	Files        int
	Bytes        int64
	RowGroups    int64
	Columns      int64 // leaf columns in the Parquet schema
	AsyncOn      []QueryTiming
	AsyncOnRSSMB float64
	AsyncOff     []QueryTiming
	// AsyncOffRSSMB is the peak RSS while the read_ahead_depth = 0 queries ran.
	AsyncOffRSSMB float64
	Verify        map[string]Result
}

// sampler tracks peak RSS between resets.
type sampler struct {
	peak atomic.Int64
	stop chan struct{}
	done chan struct{}
}

func startSampler() *sampler {
	s := &sampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(s.done)
		t := time.NewTicker(200 * time.Millisecond)
		defer t.Stop()
		for {
			s.observe()
			select {
			case <-s.stop:
				return
			case <-t.C:
			}
		}
	}()
	return s
}

func (s *sampler) observe() {
	r := rssBytes()
	for {
		p := s.peak.Load()
		if r <= p || s.peak.CompareAndSwap(p, r) {
			return
		}
	}
}

// reset returns the peak since the last reset, in MB.
func (s *sampler) reset() float64 {
	s.observe()
	return float64(s.peak.Swap(rssBytes())) / 1e6
}

func (s *sampler) close() { close(s.stop); <-s.done }

type runner struct {
	cfg    Config
	g      *Generator
	db     *sql.DB
	w      Writer
	s      *sampler
	nextID int64
	report *Report
}

func (r *runner) logf(format string, args ...any) {
	if r.cfg.Logf != nil {
		r.cfg.Logf(format, args...)
	}
}

func (r *runner) errorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	r.logf("ERROR %s", msg)
	r.report.Errors = append(r.report.Errors, msg)
}

func Run(ctx context.Context, cfg Config) (*Report, error) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}
	cfg.Settings.Extra = append(slices.Clone(cfg.Settings.Extra), cfg.Layout.Setup()...)
	if !cfg.Keep {
		defer os.RemoveAll(cfg.Dir)
	}
	r := &runner{cfg: cfg, g: NewGenerator(cfg.Seed), s: startSampler(), report: &Report{
		Layout:   cfg.Layout.Name(),
		Machine:  machine(),
		Started:  time.Now().UTC(),
		Settings: cfg.Settings,
	}}
	defer r.s.close()
	rep := r.report

	rep.GeneratorLogsPerSec = generatorRate(r.g, 3*time.Second)
	r.logf("generator alone: %.0f Logs/s", rep.GeneratorLogsPerSec)

	if err := r.open(); err != nil {
		return nil, err
	}
	if err := CreateTable(ctx, r.db, cfg.Layout); err != nil {
		return nil, err
	}
	rep.Engine, rep.EngineSettings = engineInfo(ctx, r.db)
	var err error
	if r.w, err = cfg.Layout.NewWriter(ctx, r.db); err != nil {
		return nil, err
	}

	for _, ph := range []struct {
		name    string
		d       time.Duration
		factor  int
		queries bool
	}{
		{"unthrottled", cfg.Unthrottled, 0, false},
		{"target", cfg.Target / 2, 1, false},
		{"target_with_queries", cfg.Target / 2, 1, true},
		{fmt.Sprintf("burst_%dx", cfg.BurstFactor), cfg.Burst, cfg.BurstFactor, false},
	} {
		p, err := r.phase(ctx, ph.name, ph.d, ph.factor, ph.queries)
		if err != nil {
			return rep, fmt.Errorf("phase %s: %w", ph.name, err)
		}
		r.logf("%s: %d Logs, %.0f Logs/s, %.2f MB/s, flush p99 %.0f ms, max backlog %d, kept up %v, peak RSS %.0f MB, CPU %.0f%%",
			p.Name, p.Logs, p.LogsPerSec, p.MBPerSec, p.FlushP99Ms, p.MaxBacklogLogs, p.KeptUp, p.PeakRSSMB, p.CPUPercent)
		rep.Phases = append(rep.Phases, p)
	}
	if err := r.w.Close(); err != nil {
		return rep, err
	}
	rep.Rows = r.nextID
	lastID := r.nextID - 1

	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, cfg.CheckpointTimeout)
	_, err = r.db.ExecContext(cctx, "CHECKPOINT")
	cancel()
	if err != nil {
		// Too slow (or failed) to measure further: stop this layout, without
		// a second checkpoint on close.
		r.errorf("final checkpoint stopped after %v: %s; layout not measured further", time.Since(start).Round(time.Millisecond), firstLine(err))
		r.db.Exec("PRAGMA disable_checkpoint_on_shutdown")
		r.db.Close()
		return rep, nil
	}
	rep.CheckpointMs = ms(time.Since(start))
	if strings.HasPrefix(cfg.Layout.Name(), "variant") {
		rep.Shredding, err = variantShredding(ctx, r.db)
		if err != nil {
			r.errorf("shredding report: %v", err)
		}
	}
	r.db.Close()
	rep.DBFileBytes = dirBytes(cfg.Dir)
	r.logf("checkpoint %.0f ms; %d rows, %.1f MB on disk (%.0f B/Log)", rep.CheckpointMs, rep.Rows,
		float64(rep.DBFileBytes)/1e6, float64(rep.DBFileBytes)/float64(rep.Rows))

	timed := TimedParams(r.g, lastID)
	hot := func() (*sql.DB, error) { return OpenDB(r.dbPath(), cfg.Settings) }
	rep.HotQueries = r.timeQueries(ctx, hot, cfg.Layout, "logs", timed, cfg.Dir, "", 3)
	if setting := prefetchSetting(ctx, hot); setting != "" {
		rep.HotPrefetchSetting = setting
		rep.HotQueriesPrefetch = r.timeQueries(ctx, hot, cfg.Layout, "logs", timed, cfg.Dir, "SET GLOBAL storage_block_prefetch = '"+setting+"'", 0)
	}

	rep.VerifyMaxID = min(cfg.VerifyMaxID, lastID)
	verify := VerifyParams(r.g, rep.VerifyMaxID)
	if rep.Verify, err = r.verify(ctx, hot, cfg.Layout, "logs", verify); err != nil {
		r.errorf("verify hot table: %v", err)
	}

	for _, rg := range cfg.RowGroupSizes {
		pr, err := r.parquet(ctx, rg, timed, verify)
		if err != nil {
			r.errorf("parquet row group %d: %v", rg, err)
		}
		rep.Parquet = append(rep.Parquet, pr)
	}

	maxN := slices.Max(append([]int{0}, cfg.Promote...))
	for _, n := range cfg.Promote {
		if _, isMap := cfg.Layout.(mapLayout); n > 0 && !isMap {
			continue
		}
		pr, err := r.promotion(ctx, n, n == maxN, lastID, timed, verify)
		if err != nil {
			r.errorf("promotion N=%d: %v", n, err)
		}
		rep.Promotion = append(rep.Promotion, pr)
	}
	return rep, nil
}

// promotion exports per-Project hourly Parquet with n keys promoted and times
// the query set on Project 1's files in each query form. Threads = 1 is timed
// too for N = 0 and, when scaling is set, for the per-file form.
func (r *runner) promotion(ctx context.Context, n int, scaling bool, lastID int64, timed, verify Params) (PromotionRun, error) {
	pr := PromotionRun{N: n}
	dir := filepath.Join(r.cfg.Dir, fmt.Sprintf("promote-%d", n))
	defer os.RemoveAll(dir)

	db, err := OpenDB(r.dbPath(), r.cfg.Settings)
	if err != nil {
		return pr, err
	}
	files, st, err := ExportProjectHours(ctx, db, r.cfg.Layout, dir, Base, r.g.Log(lastID).Time.Add(time.Microsecond), n, 0, "data")
	db.Close()
	if err != nil {
		return pr, err
	}
	pr.TopKeysMs, pr.CopyMs = st.TopKeysMs, st.CopyMs
	pr.Files = len(files)
	pr.Bytes = dirBytes(dir)
	r.report.ProjectRows = 0
	for _, f := range files {
		if f.Project == timed.Project {
			r.report.ProjectRows += f.Rows
		}
	}
	r.logf("promote N=%d: top keys %.0f ms, copy %.0f ms, %d files, %.1f MB", n, pr.TopKeysMs, pr.CopyMs, pr.Files, float64(pr.Bytes)/1e6)

	forms := []string{FormMap}
	if n > 0 {
		forms = []string{FormCoalesce, FormPerFile}
	}
	inMemory := func(s Settings) func() (*sql.DB, error) {
		return func() (*sql.DB, error) { return OpenDB("", s) }
	}
	one := r.cfg.Settings
	one.Threads = 1
	var errs []error
	for _, form := range forms {
		fr := FormRun{Form: form}
		var l Layout = r.cfg.Layout
		if _, isMap := l.(mapLayout); isMap {
			l = newPromotedLayout(form, files, timed.Project)
		}
		src := PromotedSource(form, files, timed.Project)
		r.logf(" %s:", form)
		fr.Queries = r.timeQueries(ctx, inMemory(r.cfg.Settings), l, src, timed, dir, "", 3)
		if n == 0 || scaling && form == FormPerFile {
			r.logf(" %s, threads = 1:", form)
			fr.Threads1 = r.timeQueries(ctx, inMemory(one), l, src, timed, dir, "", 3)
		}
		if fr.Verify, err = r.verify(ctx, inMemory(r.cfg.Settings), l, src, verify); err != nil {
			errs = append(errs, fmt.Errorf("verify %s: %w", form, err))
		}
		pr.Forms = append(pr.Forms, fr)
	}
	return pr, errors.Join(errs...)
}

func (r *runner) dbPath() string { return filepath.Join(r.cfg.Dir, r.cfg.Layout.Name()+".duckdb") }

func (r *runner) open() error {
	var err error
	r.db, err = OpenDB(r.dbPath(), r.cfg.Settings)
	return err
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func machine() string {
	host, _ := os.Hostname()
	mem := "?"
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		mem = strings.Fields(strings.SplitN(string(b), "\n", 2)[0])[1] + " kB"
	}
	return fmt.Sprintf("%s, %d CPU, %s RAM, %s/%s", host, runtime.NumCPU(), mem, runtime.GOOS, runtime.GOARCH)
}

func generatorRate(g *Generator, d time.Duration) float64 {
	start := time.Now()
	var n int64
	for time.Since(start) < d {
		g.Log(10_000_000 + n)
		n++
	}
	return float64(n) / time.Since(start).Seconds()
}

func engineInfo(ctx context.Context, db *sql.DB) (string, map[string]string) {
	var version string
	db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
	settings := map[string]string{}
	rows, err := db.QueryContext(ctx, `SELECT name, value FROM duckdb_settings() WHERE name IN
		('memory_limit', 'threads', 'async_threads', 'read_ahead_depth', 'storage_block_prefetch',
		 'variant_minimum_shredding_size', 'disable_parquet_prefetching', 'prefetch_all_parquet_files')`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var k, v string
			rows.Scan(&k, &v)
			settings[k] = v
		}
	}
	return version, settings
}

// prefetchSetting finds a storage_block_prefetch value that prefetches local
// blocks too, or "" if the engine accepts none of the candidates.
func prefetchSetting(ctx context.Context, open func() (*sql.DB, error)) string {
	db, err := open()
	if err != nil {
		return ""
	}
	defer db.Close()
	for _, v := range []string{"ALWAYS", "ALWAYS_PREFETCH", "ALL", "LOCAL_AND_REMOTE"} {
		if _, err := db.ExecContext(ctx, "SET GLOBAL storage_block_prefetch = '"+v+"'"); err == nil {
			return v
		}
	}
	return ""
}

// phase writes for d. factor 0 is unthrottled; otherwise it writes
// factor × the target rate, one flush per second.
func (r *runner) phase(ctx context.Context, name string, d time.Duration, factor int, queries bool) (Phase, error) {
	p := Phase{Name: name, TargetBytesPerS: float64(TargetBytesPerSec * factor)}
	r.s.reset()
	cpu0 := cpuSeconds()
	start := time.Now()
	deadline := start.Add(d)
	var flushes []time.Duration

	var qwg sync.WaitGroup
	var qlat []time.Duration
	qctx, stopQueries := context.WithCancel(ctx)
	if queries {
		qwg.Add(1)
		go func() {
			defer qwg.Done()
			for qctx.Err() == nil {
				for _, q := range Queries() {
					params := TimedParams(r.g, max(0, r.nextIDAtomic()-1))
					t0 := time.Now()
					_, err := queryStrings(qctx, r.db, q.SQL(r.cfg.Layout, "logs", params), 0)
					if qctx.Err() != nil {
						return
					}
					p.QueriesRun++
					if err != nil {
						p.QueryErrors++
						r.logf("query %s during ingest: %v", q.Name, firstLine(err))
						continue
					}
					qlat = append(qlat, time.Since(t0))
				}
			}
		}()
	}

	write := func(batch []Log) error {
		t0 := time.Now()
		if err := r.w.Write(ctx, batch); err != nil {
			return err
		}
		flushes = append(flushes, time.Since(t0))
		for _, l := range batch {
			p.Bytes += int64(l.Size)
		}
		p.Logs += int64(len(batch))
		atomic.AddInt64(&r.nextID, int64(len(batch)))
		return nil
	}

	var err error
	if factor == 0 {
		err = r.unthrottled(ctx, deadline, write)
	} else {
		rate := float64(TargetBytesPerSec * factor)
		perSecond := int64(LogsPerSecond * factor)
		tick := time.NewTicker(time.Second)
		for err == nil && time.Now().Before(deadline) {
			<-tick.C
			due := int64(rate * time.Since(start).Seconds())
			var batch []Log
			var size int64
			for p.Bytes+size < due {
				l := r.g.Log(r.nextIDAtomic() + int64(len(batch)))
				batch = append(batch, l)
				size += int64(l.Size)
			}
			// Each tick writes everything due, so a batch beyond one
			// second's worth is backlog left by slow earlier flushes.
			p.MaxBacklogLogs = max(p.MaxBacklogLogs, int64(len(batch))-perSecond)
			if len(batch) > 0 {
				err = write(batch)
			}
		}
		tick.Stop()
	}
	stopQueries()
	qwg.Wait()
	if err != nil {
		return p, err
	}

	elapsed := time.Since(start)
	p.Seconds = elapsed.Seconds()
	p.LogsPerSec = float64(p.Logs) / p.Seconds
	p.MBPerSec = float64(p.Bytes) / p.Seconds / 1e6
	p.Flushes = len(flushes)
	p.FlushP50Ms, p.FlushP99Ms, p.FlushMaxMs = percentiles(flushes)
	if factor != 0 {
		// Kept up: never more than a second behind, and flushes fit the tick.
		p.KeptUp = p.MaxBacklogLogs <= int64(LogsPerSecond*factor) && p.FlushP99Ms < 1000
	}
	if len(qlat) > 0 {
		p.QueryP50Ms, _, p.QueryMaxMs = percentiles(qlat)
	}
	p.PeakRSSMB = r.s.reset()
	p.CPUPercent = (cpuSeconds() - cpu0) / p.Seconds * 100
	var mem sql.NullInt64
	r.db.QueryRowContext(ctx, "SELECT sum(memory_usage_bytes) FROM duckdb_memory()").Scan(&mem)
	p.DuckDBMemoryMB = float64(mem.Int64) / 1e6
	return p, nil
}

func (r *runner) nextIDAtomic() int64 { return atomic.LoadInt64(&r.nextID) }

// unthrottled generates batches on a second goroutine so generation and
// writing overlap, as decoding and writing would in EventStore.
func (r *runner) unthrottled(ctx context.Context, deadline time.Time, write func([]Log) error) error {
	batches := make(chan []Log, 2)
	gctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	// Wait for the generator so its CPU does not count against the next phase.
	defer func() { cancel(); <-done }()
	go func() {
		defer close(done)
		defer close(batches)
		next := r.nextIDAtomic()
		for gctx.Err() == nil {
			b := Batch(r.g, next, r.cfg.FlushLogs)
			next += int64(len(b))
			select {
			case batches <- b:
			case <-gctx.Done():
				return
			}
		}
	}()
	for b := range batches {
		if err := write(b); err != nil {
			return err
		}
		if !time.Now().Before(deadline) {
			return nil
		}
	}
	return ctx.Err()
}

func percentiles(ds []time.Duration) (p50, p99, mx float64) {
	if len(ds) == 0 {
		return 0, 0, 0
	}
	s := slices.Clone(ds)
	slices.Sort(s)
	at := func(q float64) float64 { return ms(s[min(len(s)-1, int(q*float64(len(s))))]) }
	return at(0.5), at(0.99), ms(s[len(s)-1])
}

func variantShredding(ctx context.Context, db *sql.DB) ([]RowGroupShred, error) {
	// attrs is column 9; a shredded VARIANT row group has a [9, 2, ...] subtree.
	rows, err := db.QueryContext(ctx, `SELECT row_group_id,
			sum(count) FILTER (WHERE column_name = 'id' AND column_path = '[0]'),
			bool_or(column_path LIKE '[9, 2%')
		FROM pragma_storage_info('logs') GROUP BY row_group_id ORDER BY row_group_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RowGroupShred
	for rows.Next() {
		var s RowGroupShred
		var n sql.NullInt64
		if err := rows.Scan(&s.RowGroup, &n, &s.Shredded); err != nil {
			return nil, err
		}
		s.Rows = n.Int64
		out = append(out, s)
	}
	return out, rows.Err()
}

// slowQuery is a cold time past which only one warm run is timed.
const slowQuery = 2 * time.Second

// timeQueries runs each query cold (fresh DuckDB, files evicted from the page
// cache) and then warm: the median of warmRuns, or one run if cold was slow.
func (r *runner) timeQueries(ctx context.Context, open func() (*sql.DB, error), l Layout, src string, p Params, cacheDir, setup string, warmRuns int) []QueryTiming {
	var out []QueryTiming
	setStart := time.Now()
	for _, q := range Queries() {
		qt := QueryTiming{Name: q.Name}
		// The first query always runs, so every set has at least one timing.
		if len(out) > 0 && time.Since(setStart) > r.cfg.SetBudget {
			qt.Skipped = true
			r.logf("  %-28s skipped: query set over its %v budget", q.Name, r.cfg.SetBudget)
			out = append(out, qt)
			continue
		}
		err := func() error {
			dropPageCache(cacheDir)
			db, err := open()
			if err != nil {
				return err
			}
			defer db.Close()
			if setup != "" {
				if _, err := db.ExecContext(ctx, setup); err != nil {
					return err
				}
			}
			var warm []time.Duration
			runs := warmRuns
			for i := 0; i <= runs; i++ {
				qctx, cancel := context.WithTimeout(ctx, r.cfg.QueryTimeout)
				t0 := time.Now()
				rows, err := queryStrings(qctx, db, q.SQL(l, src, p), 0)
				d := time.Since(t0)
				cancel()
				if err != nil {
					return err
				}
				qt.Rows = len(rows)
				if i == 0 {
					qt.ColdMs = ms(d)
					if d > slowQuery {
						runs = min(runs, 1)
					}
				} else {
					warm = append(warm, d)
				}
			}
			if len(warm) > 0 {
				slices.Sort(warm)
				qt.WarmMs = ms(warm[len(warm)/2])
			}
			return nil
		}()
		if err != nil {
			qt.Error = firstLine(err)
			r.errorf("%s on %s: %s", q.Name, src, qt.Error)
		}
		r.logf("  %-28s cold %8.1f ms  warm %8.1f ms  rows %d", q.Name, qt.ColdMs, qt.WarmMs, qt.Rows)
		out = append(out, qt)
	}
	return out
}

func (r *runner) verify(ctx context.Context, open func() (*sql.DB, error), l Layout, src string, p Params) (map[string]Result, error) {
	db, err := open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	out := map[string]Result{}
	var errs []error
	setStart := time.Now()
	for _, q := range Queries() {
		if len(out) > 0 && time.Since(setStart) > r.cfg.SetBudget {
			errs = append(errs, fmt.Errorf("%s: skipped, verify over its %v budget", q.Name, r.cfg.SetBudget))
			continue
		}
		qctx, cancel := context.WithTimeout(ctx, r.cfg.QueryTimeout)
		res, err := RunQuery(qctx, db, l, q, src, p)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %s", q.Name, firstLine(err)))
			continue
		}
		out[q.Name] = res
	}
	return out, errors.Join(errs...)
}

func (r *runner) parquet(ctx context.Context, rowGroupSize int, timed, verify Params) (ParquetRun, error) {
	pr := ParquetRun{RowGroupSize: rowGroupSize}
	dir := filepath.Join(r.cfg.Dir, fmt.Sprintf("parquet-rg%d", rowGroupSize))
	defer os.RemoveAll(dir)

	db, err := OpenDB(r.dbPath(), r.cfg.Settings)
	if err != nil {
		return pr, err
	}
	start := time.Now()
	files, err := ExportHourlyParquet(ctx, db, r.cfg.Layout, dir, rowGroupSize)
	pr.ExportMs = ms(time.Since(start))
	db.Close()
	if err != nil {
		return pr, err
	}
	pr.Files = len(files)
	pr.Bytes = dirBytes(dir)
	src := ParquetSource(dir)
	inMemory := func() (*sql.DB, error) { return OpenDB("", r.cfg.Settings) }
	if mdb, err := inMemory(); err == nil {
		mdb.QueryRowContext(ctx, fmt.Sprintf("SELECT count(DISTINCT (file_name, row_group_id)), count(DISTINCT path_in_schema) FROM parquet_metadata(%s)",
			sqlString(filepath.Join(dir, "*.parquet")))).Scan(&pr.RowGroups, &pr.Columns)
		mdb.Close()
	}
	r.logf("parquet rg %d: export %.0f ms, %d files, %.1f MB, %d row groups, %d columns",
		rowGroupSize, pr.ExportMs, pr.Files, float64(pr.Bytes)/1e6, pr.RowGroups, pr.Columns)

	r.logf(" async on (read_ahead_depth default):")
	r.s.reset()
	pr.AsyncOn = r.timeQueries(ctx, inMemory, r.cfg.Layout, src, timed, dir, "", 3)
	pr.AsyncOnRSSMB = r.s.reset()
	r.logf(" async off (read_ahead_depth = 0):")
	pr.AsyncOff = r.timeQueries(ctx, inMemory, r.cfg.Layout, src, timed, dir, "SET GLOBAL read_ahead_depth = 0", 3)
	pr.AsyncOffRSSMB = r.s.reset()
	pr.Verify, err = r.verify(ctx, inMemory, r.cfg.Layout, src, verify)
	return pr, err
}

// firstLine drops DuckDB's multi-line detail (stack traces, hints).
func firstLine(err error) string {
	if err == nil {
		return ""
	}
	line, _, _ := strings.Cut(err.Error(), "\n")
	return line
}
