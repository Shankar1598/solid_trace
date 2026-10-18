package logslayouts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// CyclesConfig runs the hot table as ADR 0004 describes, scaled down: Logs
// are ingested without pause, and when hour h starts the hour before the
// previous one (h-2) is exported to per-Project Parquet and deleted, so the
// table holds the current and previous hour. An "hour" is HourLogs Logs of
// logical time.
type CyclesConfig struct {
	Dir      string // deleted afterwards
	Seed     uint64
	Settings Settings
	HourLogs int64
	Cycles   int // exports to run
	// Factor is the ingest rate as a multiple of the target; 0 is unthrottled.
	Factor    int
	FlushLogs int // Logs per flush when unthrottled
	Promote   int
	MinShare  float64 // a promoted key's minimum share of a file's Logs
	// Checkpoint runs CHECKPOINT after each delete, so freed blocks can be
	// reused at once; otherwise only automatic checkpoints run.
	Checkpoint bool
	Logf       func(format string, args ...any)
}

type CyclesReport struct {
	Engine     string
	Machine    string
	Started    time.Time
	Settings   Settings
	HourLogs   int64
	HourMB     float64 // generated (uncompressed) MB per hour
	Factor     int
	Promote    int
	Checkpoint bool
	Cycles     []Cycle
	Errors     []string `json:",omitempty"`
}

// Cycle is one export of an hour, measured after its delete (and checkpoint).
type Cycle struct {
	Hour         int
	ExportedRows int64
	Files        int
	TopKeysMs    float64
	CopyMs       float64
	DeleteMs     float64
	CheckpointMs float64 `json:",omitempty"`
	TableRows    int64
	DBBytes      int64
	WALBytes     int64
	TotalBlocks  int64
	FreeBlocks   int64
	// During is ingest from the start of the export to the end of the delete
	// (and checkpoint); Between is the rest of the cycle.
	During, Between FlushStats
}

type FlushStats struct {
	Flushes        int
	Logs           int64
	P50Ms, P99Ms   float64
	MaxMs          float64
	MaxBacklogLogs int64
}

type flushSample struct {
	start, end time.Time
	logs       int
	backlog    int64
}

func RunCycles(ctx context.Context, cfg CyclesConfig) (*CyclesReport, error) {
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}
	defer os.RemoveAll(cfg.Dir)
	logf := func(format string, args ...any) {
		if cfg.Logf != nil {
			cfg.Logf(format, args...)
		}
	}
	g := NewGenerator(cfg.Seed)
	rep := &CyclesReport{Machine: machine(), Started: time.Now().UTC(), Settings: cfg.Settings,
		HourLogs: cfg.HourLogs, Factor: cfg.Factor, Promote: cfg.Promote, Checkpoint: cfg.Checkpoint}
	for id := int64(0); id < cfg.HourLogs; id++ {
		rep.HourMB += float64(g.Log(id).Size) / 1e6
	}

	l := mapLayout{}
	dbPath := filepath.Join(cfg.Dir, "logs.duckdb")
	db, err := OpenDB(dbPath, cfg.Settings)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	if err := CreateTable(ctx, db, l); err != nil {
		return nil, err
	}
	rep.Engine, _ = engineInfo(ctx, db)
	w, err := l.NewWriter(ctx, db)
	if err != nil {
		return nil, err
	}

	var nextID atomic.Int64
	var mu sync.Mutex
	var samples []flushSample
	wctx, stopWriter := context.WithCancel(ctx)
	writerErr := make(chan error, 1)
	go func() {
		writerErr <- ingest(wctx, cfg, g, w, &nextID, func(s flushSample) {
			mu.Lock()
			samples = append(samples, s)
			mu.Unlock()
		})
	}()
	defer func() {
		stopWriter()
		<-writerErr
		w.Close()
	}()

	hourDur := time.Duration(cfg.HourLogs) * time.Second / LogsPerSecond
	hourStart := func(h int) time.Time { return Base.Add(time.Duration(h) * hourDur) }
	parquetDir := filepath.Join(cfg.Dir, "parquet")
	cycleStart := time.Now()
	for h := 2; len(rep.Cycles) < cfg.Cycles; h++ {
		// Wait for hour h to start.
		for nextID.Load() < int64(h)*cfg.HourLogs {
			select {
			case err := <-writerErr:
				return rep, fmt.Errorf("writer: %w", err)
			case <-time.After(20 * time.Millisecond):
			}
		}
		c := Cycle{Hour: h - 2}
		t0 := time.Now()
		files, st, err := ExportProjectHours(ctx, db, l, parquetDir, hourStart(h-2), hourStart(h-1), cfg.Promote, cfg.MinShare, fmt.Sprintf("data-%d", h-2))
		if err != nil {
			return rep, fmt.Errorf("export hour %d: %w", h-2, err)
		}
		c.Files, c.TopKeysMs, c.CopyMs = len(files), st.TopKeysMs, st.CopyMs
		for _, f := range files {
			c.ExportedRows += f.Rows
		}
		td := time.Now()
		if _, err := db.ExecContext(ctx, "DELETE FROM logs WHERE time < "+timestamp(hourStart(h-1))); err != nil {
			return rep, fmt.Errorf("delete hour %d: %w", h-2, err)
		}
		c.DeleteMs = ms(time.Since(td))
		if cfg.Checkpoint {
			tc := time.Now()
			if _, err := db.ExecContext(ctx, "CHECKPOINT"); err != nil {
				msg := fmt.Sprintf("checkpoint after hour %d: %s", h-2, firstLine(err))
				logf("ERROR %s", msg)
				rep.Errors = append(rep.Errors, msg)
			}
			c.CheckpointMs = ms(time.Since(tc))
		}
		exportEnd := time.Now()

		db.QueryRowContext(ctx, "SELECT count(*) FROM logs").Scan(&c.TableRows)
		db.QueryRowContext(ctx, "SELECT total_blocks, free_blocks FROM pragma_database_size() WHERE database_name = 'logs'").Scan(&c.TotalBlocks, &c.FreeBlocks)
		c.DBBytes = fileSize(dbPath)
		c.WALBytes = fileSize(dbPath + ".wal")

		mu.Lock()
		c.During = flushStats(samples, t0, exportEnd)
		c.Between = flushStats(samples, cycleStart, t0)
		mu.Unlock()
		cycleStart = exportEnd
		logf("hour %d: exported %d rows in %d files (top keys %.0f ms, copy %.0f ms), delete %.0f ms, checkpoint %.0f ms; table %d rows, file %.1f MB + WAL %.1f MB, blocks %d (%d free); flush p99 %.0f / max %.0f ms during export, %.0f / %.0f ms between",
			c.Hour, c.ExportedRows, c.Files, c.TopKeysMs, c.CopyMs, c.DeleteMs, c.CheckpointMs, c.TableRows,
			float64(c.DBBytes)/1e6, float64(c.WALBytes)/1e6, c.TotalBlocks, c.FreeBlocks,
			c.During.P99Ms, c.During.MaxMs, c.Between.P99Ms, c.Between.MaxMs)
		rep.Cycles = append(rep.Cycles, c)
	}
	return rep, nil
}

// ingest writes Logs until ctx ends: at cfg.Factor × the target rate, one
// flush per second, or unthrottled in cfg.FlushLogs batches.
func ingest(ctx context.Context, cfg CyclesConfig, g *Generator, w Writer, nextID *atomic.Int64, record func(flushSample)) error {
	write := func(batch []Log, backlog int64) error {
		t0 := time.Now()
		if err := w.Write(ctx, batch); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		record(flushSample{start: t0, end: time.Now(), logs: len(batch), backlog: backlog})
		nextID.Add(int64(len(batch)))
		return nil
	}
	if cfg.Factor == 0 {
		for ctx.Err() == nil {
			if err := write(Batch(g, nextID.Load(), cfg.FlushLogs), 0); err != nil {
				return err
			}
		}
		return nil
	}
	perSecond := int64(LogsPerSecond * cfg.Factor)
	start := time.Now()
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		due := int64(float64(perSecond) * time.Since(start).Seconds())
		n := due - nextID.Load()
		if n <= 0 {
			continue
		}
		if err := write(Batch(g, nextID.Load(), int(n)), n-perSecond); err != nil {
			return err
		}
	}
}

// flushStats covers the flushes that overlap [from, to).
func flushStats(samples []flushSample, from, to time.Time) FlushStats {
	var st FlushStats
	var ds []time.Duration
	for _, s := range samples {
		if s.end.Before(from) || !s.start.Before(to) {
			continue
		}
		ds = append(ds, s.end.Sub(s.start))
		st.Logs += int64(s.logs)
		st.MaxBacklogLogs = max(st.MaxBacklogLogs, s.backlog)
	}
	st.Flushes = len(ds)
	st.P50Ms, st.P99Ms, st.MaxMs = percentiles(ds)
	return st
}

func fileSize(path string) int64 {
	fi, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// CyclesMarkdown renders a cycles report.
func CyclesMarkdown(r *CyclesReport) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	rate := "unthrottled"
	if r.Factor > 0 {
		rate = fmt.Sprintf("%d× target (%.1f MB/s)", r.Factor, float64(TargetBytesPerSec*r.Factor)/1e6)
	}
	w("Engine `%s` on %s. DuckDB memory_limit %s, threads %d. An hour is %d Logs (%.0f MB generated); ingest %s; N = %d promoted keys; checkpoint after delete: %v.\n\n",
		r.Engine, r.Machine, r.Settings.MemoryLimit, r.Settings.Threads, r.HourLogs, r.HourMB, rate, r.Promote, r.Checkpoint)
	w("| Hour | Rows exported | Files | Top keys ms | Copy ms | Delete ms | Checkpoint ms | Table rows | .duckdb MB | WAL MB | Blocks (free) | Flush p50 / p99 / max ms during export | Max backlog during | Flush p50 / p99 / max ms between |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, c := range r.Cycles {
		w("| %d | %d | %d | %.0f | %.0f | %.0f | %.0f | %d | %.1f | %.1f | %d (%d) | %.0f / %.0f / %.0f | %d | %.0f / %.0f / %.0f |\n",
			c.Hour, c.ExportedRows, c.Files, c.TopKeysMs, c.CopyMs, c.DeleteMs, c.CheckpointMs, c.TableRows,
			float64(c.DBBytes)/1e6, float64(c.WALBytes)/1e6, c.TotalBlocks, c.FreeBlocks,
			c.During.P50Ms, c.During.P99Ms, c.During.MaxMs, c.During.MaxBacklogLogs,
			c.Between.P50Ms, c.Between.P99Ms, c.Between.MaxMs)
	}
	if len(r.Errors) > 0 {
		w("\nErrors:\n\n- %s\n", strings.Join(r.Errors, "\n- "))
	}
	return b.String()
}
