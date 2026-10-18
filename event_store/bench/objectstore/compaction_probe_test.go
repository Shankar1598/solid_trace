package objectstore

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestCompactionProbeLocal measures what daily compaction would save on local
// disk (logs v2 ticket 20): it merges a Project's hour files into one file
// per day, as spec step 7 would, and times a 12-chart Dashboard's kinds of
// query over both layouts, each on a fresh DuckDB (cold), then again (warm).
// The OS page cache stays warm either way. Point it at a Project's export
// directory, such as the dev stack's:
//
//	COMPACTION_PROBE_DIR=../storage/duckdb/development/logs_parquet/project=1 \
//	go test -count=1 -v -run TestCompactionProbeLocal ./bench/objectstore/
func TestCompactionProbeLocal(t *testing.T) {
	dir := os.Getenv("COMPACTION_PROBE_DIR")
	if dir == "" {
		t.Skip("set COMPACTION_PROBE_DIR to a Project's logs_parquet directory to run")
	}
	hours, err := filepath.Glob(filepath.Join(dir, "day=*", "hour=*", "*.parquet"))
	if err != nil || len(hours) == 0 {
		t.Fatalf("no hour files under %s: %v", dir, err)
	}
	days, err := filepath.Glob(filepath.Join(dir, "day=*"))
	if err != nil {
		t.Fatal(err)
	}

	open := func() *sql.DB {
		db, err := sql.Open("duckdb", "?autoinstall_known_extensions=false&autoload_known_extensions=false")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}

	merged := t.TempDir()
	db := open()
	began := time.Now()
	var dayFiles []string
	for _, day := range days {
		out := filepath.Join(merged, filepath.Base(day)+".parquet")
		mustExec(t, db, fmt.Sprintf(`COPY (SELECT * FROM read_parquet('%s', union_by_name = true, hive_partitioning = false) ORDER BY time)
			TO '%s' (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE 122880)`, filepath.Join(day, "hour=*", "*.parquet"), out))
		dayFiles = append(dayFiles, out)
	}
	t.Logf("merged %d hour files into %d day files in %v", len(hours), len(dayFiles), time.Since(began).Round(time.Millisecond))

	list := func(files []string) string {
		quoted := make([]string, len(files))
		for i, f := range files {
			quoted[i] = "'" + f + "'"
		}
		return fmt.Sprintf("read_parquet([%s], union_by_name = true, hive_partitioning = false)", strings.Join(quoted, ", "))
	}
	describe := func(files []string) string {
		var groups, rows int64
		if err := db.QueryRow(fmt.Sprintf("SELECT count(DISTINCT (file_name, row_group_id)) FROM parquet_metadata([%s])",
			strings.TrimSuffix(strings.TrimPrefix(list(files), "read_parquet(["), "], union_by_name = true, hive_partitioning = false)"))).Scan(&groups); err != nil {
			t.Fatal(err)
		}
		if err := db.QueryRow("SELECT count(*) FROM " + list(files)).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		var bytes int64
		for _, f := range files {
			if info, err := os.Stat(f); err == nil {
				bytes += info.Size()
			}
		}
		return fmt.Sprintf("%d files, %d row groups, %d rows, %.0f MB", len(files), groups, rows, float64(bytes)/1e6)
	}

	// The charts' shapes, as EventStore's Query compiles them (readable SQL):
	// an Attribute is read from the four typed maps.
	attr := func(key string) string {
		return fmt.Sprintf("{'s': attrs_string['%[1]s'], 'n': attrs_number['%[1]s'], 'b': attrs_bool['%[1]s'], 'j': attrs_json['%[1]s']}", key)
	}
	queries := []struct{ name, sql string }{
		{"count()", "SELECT count(*) FROM %s"},
		{"count() by bin(1h), level", "SELECT time_bucket(INTERVAL '1 hour', time) AS bin, level, count(*) FROM %s GROUP BY ALL"},
		{"percentile(duration_ms, 95) by bin(1h), service", "SELECT time_bucket(INTERVAL '1 hour', time) AS bin, service, approx_quantile(attrs_number['duration_ms'], 0.95) FROM %s GROUP BY ALL"},
		{"dcount(user.id)", "SELECT approx_count_distinct(" + attr("user.id") + ") FROM %s"},
		{"top 10 http.path", "SELECT " + attr("http.path") + " AS path, count(*) AS n FROM %s WHERE attrs_string['http.path'] IS NOT NULL GROUP BY 1 ORDER BY n DESC LIMIT 10"},
	}

	for _, layout := range []struct {
		name  string
		files []string
	}{{"hour files", hours}, {"day files", dayFiles}} {
		t.Logf("%s: %s", layout.name, describe(layout.files))
		for _, q := range queries {
			fresh := open()
			sql := fmt.Sprintf(q.sql, list(layout.files))
			cold, _ := runChart(t, fresh, sql)
			warm, _ := runChart(t, fresh, sql)
			t.Logf("  %-48s cold %6v  warm %6v", q.name, cold.Round(time.Millisecond), warm.Round(time.Millisecond))
		}
	}
}
