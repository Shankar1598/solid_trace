package logslayouts

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDefaultVariantParquet records how this engine handles VARIANT Parquet
// shredded by default (every key seen becomes a column). It is slow, so it
// runs only with LOGSBENCH_PROBE=1; rerun it on DuckDB 2.0.0.
func TestDefaultVariantParquet(t *testing.T) {
	if os.Getenv("LOGSBENCH_PROBE") == "" {
		t.Skip("set LOGSBENCH_PROBE=1 to run")
	}
	g := NewGenerator(1)
	l := variantLayout{}
	ctx := context.Background()
	db := writeTestLogs(t, l, g)
	dir := t.TempDir()
	for _, rg := range []int{122880, 4096} {
		path := filepath.Join(dir, fmt.Sprintf("rg%d.parquet", rg))
		start := time.Now()
		_, err := db.ExecContext(ctx, fmt.Sprintf("COPY logs TO %s (FORMAT parquet, ROW_GROUP_SIZE %d)", sqlString(path), rg))
		var cols int
		db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM parquet_schema(%s)", sqlString(path))).Scan(&cols)
		t.Logf("COPY %d logs, ROW_GROUP_SIZE %d: %v, %d schema columns, err=%q", testLogs, rg, time.Since(start).Round(time.Millisecond), cols, firstLine(err))
	}
	path := filepath.Join(dir, "rg4096.parquet")
	for _, q := range []string{
		"SELECT count(*) FROM read_parquet(%s) WHERE variant_extract_string(attrs, 'http.method') = 'POST'",
		"SELECT count(*) FROM read_parquet(%s) WHERE variant_extract(attrs, 'http.method')::VARCHAR = 'POST'",
	} {
		fresh, err := OpenDB("", Settings{MemoryLimit: "2GB", Threads: 2})
		if err != nil {
			t.Fatal(err)
		}
		var n int
		start := time.Now()
		err = fresh.QueryRowContext(ctx, fmt.Sprintf(q, sqlString(path))).Scan(&n)
		t.Logf("%s: %d rows in %v, err=%q", q[45:], n, time.Since(start).Round(time.Millisecond), firstLine(err))
		fresh.Close()
	}
}
