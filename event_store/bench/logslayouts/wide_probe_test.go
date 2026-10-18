package logslayouts

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/duckdb/duckdb-go/v2"
)

// wideLayout promotes every (map, key) pair at ingest, in first-seen order,
// up to the Project limit; later keys stay in the maps.
type wideLayout struct {
	mapLayout
	cols  []PromotedKey
	index map[PromotedKey]int
}

const projectColumnLimit = 1000

func newWideLayout(g *Generator, logs int64) wideLayout {
	return newWideLayoutLimit(g, logs, projectColumnLimit)
}

func newWideLayoutLimit(g *Generator, logs int64, limit int) wideLayout {
	l := wideLayout{index: map[PromotedKey]int{}}
	for id := range logs {
		for _, a := range g.Log(id).Attrs {
			p := PromotedKey{mapFor(a.Value.Kind), a.Key}
			if p.Map == "attrs_json" || len(l.cols) == limit {
				continue
			}
			if _, ok := l.index[p]; !ok {
				l.index[p] = len(l.cols)
				l.cols = append(l.cols, p)
			}
		}
	}
	return l
}

func mapFor(k Kind) string {
	switch k {
	case KindInt, KindFloat:
		return "attrs_number"
	case KindBool:
		return "attrs_bool"
	case KindArray:
		return "attrs_json"
	}
	return "attrs_string"
}

func (l wideLayout) Name() string { return "wide" }
func (l wideLayout) attrColumns() string {
	cols := []string{mapLayout{}.attrColumns()}
	for _, p := range l.cols {
		typ := map[string]string{"attrs_string": "VARCHAR", "attrs_number": "DOUBLE", "attrs_bool": "BOOLEAN"}[p.Map]
		cols = append(cols, quoteIdent(p.Column())+" "+typ)
	}
	return strings.Join(cols, ", ")
}
func (l wideLayout) attr(m, k string) string {
	if _, ok := l.index[PromotedKey{m, k}]; ok {
		return quoteIdent(PromotedKey{m, k}.Column())
	}
	return fmt.Sprintf("%s[%s]", m, sqlString(k))
}
func (l wideLayout) StrAttr(k string) string { return l.attr("attrs_string", k) }
func (l wideLayout) NumAttr(k string) string { return l.attr("attrs_number", k) }
func (l wideLayout) NewWriter(ctx context.Context, db *sql.DB) (Writer, error) {
	return newAppenderWriter(ctx, db, nil, "", "logs", func(lg Log) []driver.Value { return wideRow(l, lg) })
}

func wideRow(l wideLayout, lg Log) []driver.Value {
	row := fixedValues(lg)
	maps := []duckdb.Map{{}, {}, {}, {}}
	vals := make([]driver.Value, len(l.cols))
	for _, a := range lg.Attrs {
		m := mapFor(a.Value.Kind)
		var v any
		switch a.Value.Kind {
		case KindString:
			v = a.Value.S
		case KindInt:
			v = float64(a.Value.I)
		case KindFloat:
			v = a.Value.F
		case KindBool:
			v = a.Value.B
		case KindArray:
			v = a.Value.A
		}
		if i, ok := l.index[PromotedKey{m, a.Key}]; ok {
			vals[i] = v
			continue
		}
		maps[slices.IndexFunc(mapColumns, func(c struct{ name, valueType string }) bool { return c.name == m })][a.Key] = v
	}
	for _, m := range maps {
		row = append(row, m)
	}
	return append(row, vals...)
}

// TestWideColumns measures promoting every key at ingest (up to 1000 per
// Project) against MAP-only, on the hot table, the hourly export and the
// Parquet footers. Under a minute; LOGSBENCH_PROBE=1 -v.
func TestWideColumns(t *testing.T) {
	if os.Getenv("LOGSBENCH_PROBE") == "" {
		t.Skip("set LOGSBENCH_PROBE=1 to run")
	}
	const logs = 30_000
	ctx := context.Background()
	start := time.Now()
	g := NewGenerator(1)
	dir := t.TempDir()
	wide := newWideLayout(g, logs)
	t.Logf("%d Logs carry more than %d (map, key) pairs; promoted the first %d seen", logs, projectColumnLimit, len(wide.cols))

	type table struct {
		l  Layout
		db *sql.DB
	}
	db, err := OpenDB(filepath.Join(dir, "logs.duckdb"), Settings{MemoryLimit: "2GB", Threads: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run := func(q string) time.Duration {
		t0 := time.Now()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%v\n%.300s", err, q)
		}
		return time.Since(t0)
	}
	write := func(l Layout, n int64) time.Duration {
		w, err := l.NewWriter(ctx, db)
		if err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		for id := int64(0); id < n; id += 5000 {
			if err := w.Write(ctx, Batch(g, id, 5000)); err != nil {
				t.Fatal(err)
			}
		}
		w.Close()
		return time.Since(t0)
	}

	// Appender into the wide table, alone.
	if err := CreateTable(ctx, db, wide); err != nil {
		t.Fatal(err)
	}
	d := write(wide, 20_000)
	t.Logf("appender, wide: %.0f Logs/s", 20_000/d.Seconds())
	run("DROP TABLE logs")

	// MAP-only table, then the wide table built from it in SQL.
	if err := CreateTable(ctx, db, mapLayout{}); err != nil {
		t.Fatal(err)
	}
	d = write(mapLayout{}, logs)
	t.Logf("appender, map: %.0f Logs/s", logs/d.Seconds())
	for range 2 {
		run("INSERT INTO logs SELECT * FROM logs")
	}
	run("CHECKPOINT")
	run(fmt.Sprintf("CREATE TABLE logs_wide (%s, %s)", fixedColumns, wide.attrColumns()))
	d = run("INSERT INTO logs_wide BY NAME SELECT " + exportColumns(mapLayout{}, wide.cols) + " FROM logs")
	t.Logf("SQL promotion of %d rows into %d columns: %.0f Logs/s", 4*logs, len(wide.cols), 4*logs/d.Seconds())
	t0 := time.Now()
	run("CHECKPOINT")
	t.Logf("checkpoint %v; database file %.1f MB (map + wide); %v so far", time.Since(t0).Round(time.Millisecond),
		float64(fileSize(filepath.Join(dir, "logs.duckdb")))/1e6, time.Since(start).Round(time.Millisecond))

	// Same answers, then scans, at 4×logs rows.
	timed := TimedParams(g, logs-1)
	vp := VerifyParams(g, logs-1)
	var base map[string]Result
	for _, tb := range []table{{mapLayout{}, db}, {wide, db}} {
		src := map[bool]string{true: "logs_wide", false: "logs"}[tb.l.Name() == "wide"]
		got := map[string]Result{}
		var out []string
		for _, q := range Queries() {
			if q.Name == "fetch_by_id" {
				continue
			}
			res, err := RunQuery(ctx, db, tb.l, q, src, vp)
			if err != nil {
				t.Fatalf("%s %s: %v", tb.l.Name(), q.Name, err)
			}
			got[q.Name] = res
			if !slices.Contains([]string{"common_attr_per_minute", "group_by_attr_top10", "rare_attr_eq", "count_by_service_per_minute"}, q.Name) {
				continue
			}
			var d time.Duration
			for range 2 {
				t1 := time.Now()
				if _, err := queryStrings(ctx, db, q.SQL(tb.l, src, timed), 0); err != nil {
					t.Fatal(err)
				}
				d = time.Since(t1)
			}
			out = append(out, fmt.Sprintf("%s %.0f ms", q.Name, ms(d)))
		}
		if base == nil {
			base = got
		} else if !reflect.DeepEqual(got, base) {
			t.Errorf("wide results differ from map")
		}
		t.Logf("%-4s at %d rows: %s", tb.l.Name(), 4*logs, strings.Join(out, ", "))
	}
	t.Logf("results agree: %v; %v so far", !t.Failed(), time.Since(start).Round(time.Millisecond))

	// Export Project 1 from the wide table: every column, or only the
	// columns with values (found by counting each one first).
	wdb := db
	all := filepath.Join(dir, "all.parquet")
	t0 = time.Now()
	if _, err := wdb.ExecContext(ctx, fmt.Sprintf("COPY (SELECT * FROM logs_wide WHERE project_id = 1 ORDER BY time, id) TO %s (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE %d)", sqlString(all), PromotionRowGroupSize)); err != nil {
		t.Fatal(err)
	}
	allMs := ms(time.Since(t0))
	var counts []string
	for _, c := range wide.cols {
		counts = append(counts, fmt.Sprintf("count(%s)", quoteIdent(c.Column())))
	}
	t0 = time.Now()
	row := make([]any, len(wide.cols))
	ptrs := make([]any, len(row))
	for i := range row {
		ptrs[i] = &row[i]
	}
	if err := wdb.QueryRowContext(ctx, "SELECT "+strings.Join(counts, ", ")+" FROM logs_wide WHERE project_id = 1").Scan(ptrs...); err != nil {
		t.Fatal(err)
	}
	countMs := ms(time.Since(t0))
	keep := []string{fixedNames}
	for _, c := range mapColumns {
		keep = append(keep, c.name)
	}
	for i, c := range wide.cols {
		if n, _ := row[i].(int64); n > 0 {
			keep = append(keep, quoteIdent(c.Column()))
		}
	}
	pruned := filepath.Join(dir, "pruned.parquet")
	t0 = time.Now()
	if _, err := wdb.ExecContext(ctx, fmt.Sprintf("COPY (SELECT %s FROM logs_wide WHERE project_id = 1 ORDER BY time, id) TO %s (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE %d)", strings.Join(keep, ", "), sqlString(pruned), PromotionRowGroupSize)); err != nil {
		t.Fatal(err)
	}
	t.Logf("export Project 1 (%.0fk rows): all %d columns %.0f ms, %.1f MB; counting non-empty columns %.0f ms; %d non-empty columns %.0f ms, %.1f MB",
		float64(4*logs)*0.7/1000, len(wide.cols), allMs, float64(fileSize(all))/1e6, countMs, len(keep)-1-len(mapColumns), ms(time.Since(t0)), float64(fileSize(pruned))/1e6)

	// A map-only file of the same Logs, for size.
	mapFile := filepath.Join(dir, "map.parquet")
	t0 = time.Now()
	db.ExecContext(ctx, fmt.Sprintf("COPY (SELECT * FROM logs WHERE project_id = 1 ORDER BY time, id) TO %s (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE %d)", sqlString(mapFile), PromotionRowGroupSize))
	t.Logf("map-only export of the same Logs: %.0f ms, %.1f MB", ms(time.Since(t0)), float64(fileSize(mapFile))/1e6)

	// Footers: 24 hour files of a day, read fresh each time.
	var files []string
	for h := range 24 {
		f := filepath.Join(dir, fmt.Sprintf("h%02d.parquet", h))
		if out, err := exec.Command("cp", pruned, f).CombinedOutput(); err != nil {
			t.Fatalf("cp: %v %s", err, out)
		}
		files = append(files, sqlString(f))
	}
	list := "[" + strings.Join(files, ", ") + "]"
	for _, c := range []struct{ name, q string }{
		{"merged schema of 24 files", "DESCRIBE SELECT * FROM read_parquet(" + list + ", union_by_name = true)"},
		{"null counts per column of 24 files", "SELECT path_in_schema, sum(stats_null_count), sum(num_values) FROM parquet_metadata(" + list + ") GROUP BY ALL"},
		{"common-key histogram over 24 files", queryNamed("common_attr_per_minute").SQL(wide, "read_parquet("+list+", union_by_name = true)", timed)},
	} {
		mem, err := OpenDB("", Settings{MemoryLimit: "2GB", Threads: 2})
		if err != nil {
			t.Fatal(err)
		}
		var ds []string
		for range 2 {
			t1 := time.Now()
			if _, err := queryStrings(ctx, mem, c.q, 0); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
			ds = append(ds, fmt.Sprintf("%.0f ms", ms(time.Since(t1))))
		}
		mem.Close()
		t.Logf("%s: first %s, second %s", c.name, ds[0], ds[1])
	}
	t.Logf("done in %v", time.Since(start).Round(time.Millisecond))
}

func queryNamed(name string) Query {
	for _, q := range Queries() {
		if q.Name == name {
			return q
		}
	}
	panic("no query " + name)
}

// TestWideColumnLimits times ingest and the hourly export with the first
// 100 and 250 keys promoted, against MAP-only. Under a minute.
func TestWideColumnLimits(t *testing.T) {
	if os.Getenv("LOGSBENCH_PROBE") == "" {
		t.Skip("set LOGSBENCH_PROBE=1 to run")
	}
	const logs = 20_000
	ctx := context.Background()
	start := time.Now()
	g := NewGenerator(1)
	db, err := OpenDB(filepath.Join(t.TempDir(), "logs.duckdb"), Settings{MemoryLimit: "2GB", Threads: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run := func(q string) time.Duration {
		t0 := time.Now()
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%v\n%.300s", err, q)
		}
		return time.Since(t0)
	}
	if err := CreateTable(ctx, db, mapLayout{}); err != nil {
		t.Fatal(err)
	}
	w, _ := mapLayout{}.NewWriter(ctx, db)
	t0 := time.Now()
	for id := int64(0); id < logs; id += 5000 {
		if err := w.Write(ctx, Batch(g, id, 5000)); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	t.Logf("map:  appender %.0f Logs/s", logs/time.Since(t0).Seconds())
	for range 2 {
		run("INSERT INTO logs SELECT * FROM logs")
	}
	export := func(table string) (time.Duration, int64) {
		f := filepath.Join(t.TempDir(), table+".parquet")
		d := run(fmt.Sprintf("COPY (SELECT * FROM %s WHERE project_id = 1 ORDER BY time, id) TO %s (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE %d)", table, sqlString(f), PromotionRowGroupSize))
		return d, fileSize(f)
	}
	d, size := export("logs")
	t.Logf("map:  export of Project 1 (%d rows) %.0f ms, %.2f MB", 4*logs*7/10, ms(d), float64(size)/1e6)
	for _, limit := range []int{100, 250} {
		wide := newWideLayoutLimit(g, logs, limit)
		table := fmt.Sprintf("logs_%d", limit)
		run(fmt.Sprintf("CREATE TABLE %s (%s, %s)", table, fixedColumns, wide.attrColumns()))
		ww := wideOn(ctx, t, db, wide, table)
		t0 := time.Now()
		for id := int64(0); id < logs/2; id += 5000 {
			if err := ww.Write(ctx, Batch(g, id, 5000)); err != nil {
				t.Fatal(err)
			}
		}
		ww.Close()
		appender := float64(logs/2) / time.Since(t0).Seconds()
		run(fmt.Sprintf("DELETE FROM %s", table))
		d := run(fmt.Sprintf("INSERT INTO %s BY NAME SELECT %s FROM logs", table, exportColumns(mapLayout{}, wide.cols)))
		ed, size := export(table)
		t.Logf("%d keys: appender %.0f Logs/s, SQL promotion %.0f Logs/s, export %.0f ms, %.2f MB", limit, appender, float64(4*logs)/d.Seconds(), ms(ed), float64(size)/1e6)
	}
	t.Logf("done in %v", time.Since(start).Round(time.Millisecond))
}

// wideOn is the wide layout's writer appending to table.
func wideOn(ctx context.Context, t *testing.T, db *sql.DB, l wideLayout, table string) Writer {
	w, err := newAppenderWriter(ctx, db, nil, "", table, func(lg Log) []driver.Value { return wideRow(l, lg) })
	if err != nil {
		t.Fatal(err)
	}
	return w
}
