package logslayouts

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testLogs = 30_000

func writeTestLogs(t *testing.T, l Layout, g *Generator) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := OpenDB(filepath.Join(t.TempDir(), l.Name()+".duckdb"), Settings{MemoryLimit: "2GB", Threads: 2, Extra: l.Setup()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := CreateTable(ctx, db, l); err != nil {
		t.Fatal(err)
	}
	w, err := l.NewWriter(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for start := int64(0); start < testLogs; start += 5000 {
		if err := w.Write(ctx, Batch(g, start, 5000)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return db
}

func runAll(t *testing.T, db *sql.DB, l Layout, src string, p Params) map[string]Result {
	t.Helper()
	out := map[string]Result{}
	for _, q := range Queries() {
		res, err := RunQuery(context.Background(), db, l, q, src, p)
		if err != nil {
			t.Fatalf("%s: %v\n%s", q.Name, err, q.SQL(l, src, p))
		}
		out[q.Name] = res
	}
	return out
}

func TestLayoutsAgreeWithTheGenerator(t *testing.T) {
	g := NewGenerator(1)
	p := VerifyParams(g, testLogs/2)
	var baseline map[string]Result
	for _, l := range Layouts() {
		t.Run(l.Name(), func(t *testing.T) {
			db := writeTestLogs(t, l, g)
			got := runAll(t, db, l, "logs", p)
			if baseline == nil {
				baseline = got
				checkAgainstGenerator(t, g, p, got)
				return
			}
			for name, want := range baseline {
				if !reflect.DeepEqual(got[name], want) {
					t.Errorf("%s differs from the %s baseline:\n got  %v\n want %v", name, Layouts()[0].Name(), got[name], want)
				}
			}
		})
	}
}

func TestParquetAgreesWithTheHotTable(t *testing.T) {
	g := NewGenerator(1)
	p := VerifyParams(g, testLogs/2)
	for _, l := range Layouts() {
		t.Run(l.Name(), func(t *testing.T) {
			db := writeTestLogs(t, l, g)
			dir := t.TempDir()
			files, err := ExportHourlyParquet(context.Background(), db, l, dir, 4096)
			if err != nil {
				t.Fatal(err)
			}
			if len(files) == 0 {
				t.Fatal("no Parquet files written")
			}
			hot := runAll(t, db, l, "logs", p)
			cold := runAll(t, db, l, ParquetSource(dir), p)
			if !reflect.DeepEqual(hot, cold) {
				t.Errorf("Parquet results differ from the hot table:\n hot  %v\n cold %v", hot, cold)
			}
		})
	}
}

// checkAgainstGenerator recomputes each query in Go from the generated Logs.
func checkAgainstGenerator(t *testing.T, g *Generator, p Params, got map[string]Result) {
	t.Helper()
	var logs []Log
	for id := int64(0); id <= p.MaxID; id++ {
		if l := g.Log(id); l.ProjectID == p.Project && !l.Time.Before(p.Since) && !l.Time.After(p.Until) {
			logs = append(logs, l)
		}
	}
	filters := map[string]func(Log) bool{
		"errors_last_15m": func(l Log) bool {
			return (l.Level == "error" || l.Level == "fatal") && l.Time.After(p.Until.Add(-15*time.Minute))
		},
		"common_attr_eq": func(l Log) bool { return l.AttrsMap()[CommonKey] == CommonValue },
		"rare_attr_eq":   func(l Log) bool { return l.AttrsMap()[RareKey] == RareValue },
		"mixed_numeric_range": func(l Log) bool {
			v, ok := l.AttrsMap()[MixedKey].(float64)
			return ok && v >= MixedLo && v <= MixedHi
		},
		"message_substring": func(l Log) bool { return strings.Contains(l.Message, Needle) },
	}
	for name, keep := range filters {
		var count, xor int64
		var top []string
		for i := len(logs) - 1; i >= 0; i-- {
			if keep(logs[i]) {
				count++
				xor ^= logs[i].ID
				if len(top) < 100 {
					top = append(top, strconv.FormatInt(logs[i].ID, 10))
				}
			}
		}
		if count == 0 {
			t.Errorf("%s: the generator gives no matches in %d Logs; the check is vacuous", name, len(logs))
		}
		if want := [][]string{{strconv.FormatInt(count, 10), strconv.FormatInt(xor, 10)}}; !reflect.DeepEqual(got[name].Check, want) {
			t.Errorf("%s: count/xor %v, want %v", name, got[name].Check, want)
		}
		var ids []string
		for _, row := range got[name].Rows {
			ids = append(ids, row[0])
		}
		if !slices.Equal(ids, top) {
			t.Errorf("%s: top ids %v, want %v", name, ids, top)
		}
	}

	perMinute := map[string]int{}
	versions := map[string]int{}
	for _, l := range logs {
		perMinute[l.Service+" "+l.Time.Truncate(time.Minute).Format(time.RFC3339Nano)]++
		if v, ok := l.AttrsMap()[GroupKey].(string); ok {
			versions[v]++
		}
	}
	var wantHist []string
	for k, n := range perMinute {
		wantHist = append(wantHist, fmt.Sprintf("%s %d", k, n))
	}
	var gotHist []string
	for _, row := range got["count_by_service_per_minute"].Rows {
		gotHist = append(gotHist, strings.Join(row, " "))
	}
	sort.Strings(wantHist)
	sort.Strings(gotHist)
	if !slices.Equal(gotHist, wantHist) {
		t.Errorf("count_by_service_per_minute: %v, want %v", gotHist, wantHist)
	}

	type kv struct {
		k string
		n int
	}
	var top []kv
	for k, n := range versions {
		top = append(top, kv{k, n})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].n > top[j].n || top[i].n == top[j].n && top[i].k < top[j].k })
	var wantTop [][]string
	for _, e := range top[:10] {
		wantTop = append(wantTop, []string{e.k, strconv.Itoa(e.n)})
	}
	if !reflect.DeepEqual(got["group_by_attr_top10"].Rows, wantTop) {
		t.Errorf("group_by_attr_top10: %v, want %v", got["group_by_attr_top10"].Rows, wantTop)
	}

	fetched := got["fetch_by_id"].Rows
	if len(fetched) != 1 {
		t.Fatalf("fetch_by_id returned %d rows", len(fetched))
	}
	l := g.Log(p.FetchID)
	attrs, _ := json.Marshal(l.AttrsMap())
	want := []string{strconv.FormatInt(l.ID, 10), strconv.Itoa(int(l.ProjectID)), l.Time.Format(time.RFC3339Nano),
		l.Level, l.Message, l.Service, l.Environment, nullOr(l.TraceID), nullOr(l.SpanID), string(attrs)}
	if !slices.Equal(fetched[0], want) {
		t.Errorf("fetch_by_id:\n got  %v\n want %v", fetched[0], want)
	}
}

func nullOr(s string) string {
	if s == "" {
		return "NULL"
	}
	return s
}
