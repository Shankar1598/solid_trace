package logslayouts

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestScaledQueries times the full-scan queries at two sizes, so a straight
// line through them separates per-query overhead from per-row cost before
// projecting to a day: per-file Parquet with the 5% / 40-key promotion rule,
// one Project's hour file repeated, and the hot table, doubled in place.
// Duplicated rows are fine for timing, not for results. It must finish in
// under a minute; run with LOGSBENCH_PROBE=1 -v.
func TestScaledQueries(t *testing.T) {
	if os.Getenv("LOGSBENCH_PROBE") == "" {
		t.Skip("set LOGSBENCH_PROBE=1 to run")
	}
	const logs = 100_000
	ctx := context.Background()
	start := time.Now()
	g := NewGenerator(1)
	l := mapLayout{}
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "logs.duckdb"), Settings{MemoryLimit: "2GB", Threads: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := CreateTable(ctx, db, l); err != nil {
		t.Fatal(err)
	}
	w, err := l.NewWriter(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for id := int64(0); id < logs; id += 5000 {
		if err := w.Write(ctx, Batch(g, id, 5000)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	p := TimedParams(g, logs-1)
	t.Logf("wrote %d Logs in %v", logs, time.Since(start).Round(time.Millisecond))

	// One Project-1 file with the decided promotion rule, then a file of 16
	// copies of it (about 1.1M rows), read 2 and 8 times.
	files, _, err := ExportProjectHours(ctx, db, l, filepath.Join(dir, "pq"), Base, Base.Add(time.Hour), PromotedPerFile, PromotedMinShare, "data")
	if err != nil {
		t.Fatal(err)
	}
	var one ExportedFile
	for _, f := range files {
		if f.Project == p.Project {
			one = f
		}
	}
	t.Logf("Project 1 file: %d rows, %d promoted keys", one.Rows, len(one.Promoted))
	big := filepath.Join(dir, "big.parquet")
	if _, err := db.ExecContext(ctx, fmt.Sprintf("COPY (SELECT * FROM read_parquet([%s]) ORDER BY time, id) TO %s (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE %d)",
		strings.TrimSuffix(strings.Repeat(sqlString(one.Path)+", ", 16), ", "), sqlString(big), PromotionRowGroupSize)); err != nil {
		t.Fatal(err)
	}
	bigRows := one.Rows * 16

	names := []string{"count_by_service_per_minute", "common_attr_per_minute", "group_by_attr_top10", "rare_attr_eq"}
	var qs []Query
	for _, q := range Queries() {
		if slices.Contains(names, q.Name) {
			qs = append(qs, q)
		}
	}
	// timeAll returns the second of two runs of each query, in ms.
	timeAll := func(db *sql.DB, l Layout, src string) []float64 {
		var out []float64
		for _, q := range qs {
			var d time.Duration
			for range 2 {
				t0 := time.Now()
				if _, err := queryStrings(ctx, db, q.SQL(l, src, p), 0); err != nil {
					t.Fatalf("%s: %v", q.Name, err)
				}
				d = time.Since(t0)
			}
			out = append(out, ms(d))
		}
		return out
	}
	type point struct {
		label   string
		rows    int64
		threads int
		ms      []float64
	}
	var pq, hot []point

	for _, threads := range []int{2, 1} {
		mem, err := OpenDB("", Settings{MemoryLimit: "2GB", Threads: threads})
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []int{2, 8} {
			if threads == 1 && k == 2 {
				continue
			}
			copies := make([]ExportedFile, k)
			for i := range copies {
				copies[i] = ExportedFile{Path: big, Project: p.Project, Promoted: one.Promoted}
			}
			pl := newPromotedLayout(FormPerFile, copies, p.Project)
			pq = append(pq, point{fmt.Sprintf("%d files", k), bigRows * int64(k), threads, timeAll(mem, pl, PromotedSource(FormPerFile, copies, p.Project))})
		}
		mem.Close()
	}
	t.Logf("Parquet timed after %v", time.Since(start).Round(time.Millisecond))

	// Hot table: 4× and 16× the written Logs, by doubling in place.
	rows := int64(logs)
	for _, target := range []int64{4 * logs, 16 * logs} {
		for rows < target {
			if _, err := db.ExecContext(ctx, "INSERT INTO logs SELECT * FROM logs"); err != nil {
				t.Fatal(err)
			}
			rows *= 2
		}
		db.ExecContext(ctx, "CHECKPOINT")
		hot = append(hot, point{fmt.Sprintf("%d rows", rows), rows, 2, timeAll(db, l, "logs")})
	}
	db.ExecContext(ctx, "SET threads = 1")
	hot = append(hot, point{fmt.Sprintf("%d rows", rows), rows, 1, timeAll(db, l, "logs")})
	t.Logf("hot table timed after %v", time.Since(start).Round(time.Millisecond))

	report := func(title string, pts []point, dayRows float64) {
		t.Logf("%s (ms):", title)
		for _, pt := range pts {
			t.Logf("    %-14s threads %d: %v", pt.label, pt.threads, pt.ms)
		}
		a, b := pts[0], pts[1]
		t.Logf("    projection for %.0fM rows: per query, ms/M rows at 2 threads, fixed ms, 1→2 speedup, projected s at 4 vCPU (2→4 scaling as 1→2):", dayRows/1e6)
		for i, q := range qs {
			perRow := (b.ms[i] - a.ms[i]) / float64(b.rows-a.rows)
			fixed := a.ms[i] - perRow*float64(a.rows)
			speedup := pts[2].ms[i] / b.ms[i]
			t.Logf("    %-28s %7.1f  %7.1f  %4.2f  %6.2f", q.Name, perRow*1e6, fixed, speedup, (fixed+perRow*dayRows)/max(speedup, 1)/1000)
		}
	}
	// A day for a Project with the instance's whole volume: 22 hours in
	// Parquet and the 2 hours the hot table holds.
	report("Parquet, per file, N = 40 / 5%", pq, 22*float64(HourLogs))
	report("hot table", hot, 2*float64(HourLogs))
	t.Logf("done in %v", time.Since(start).Round(time.Millisecond))
}

// TestPromotedKeyCount logs how many keys the decided rule promotes per file.
func TestPromotedKeyCount(t *testing.T) {
	if os.Getenv("LOGSBENCH_PROBE") == "" {
		t.Skip("set LOGSBENCH_PROBE=1 to run")
	}
	db := writeTestLogs(t, mapLayout{}, NewGenerator(1))
	start := time.Now()
	files, st, err := ExportProjectHours(context.Background(), db, mapLayout{}, t.TempDir(), Base, Base.Add(time.Hour), PromotedPerFile, PromotedMinShare, "data")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		t.Logf("Project %d: %d rows, %d promoted keys", f.Project, f.Rows, len(f.Promoted))
	}
	t.Logf("top keys %.0f ms, copy %.0f ms, total %v", st.TopKeysMs, st.CopyMs, time.Since(start).Round(time.Millisecond))
}
