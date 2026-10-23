// Package objectstore is a throwaway probe for Logs v2 Phase 5 step 2: can
// the static duckdb-go binary LOAD httpfs, and how fast are count-by-bin
// charts over hour files in an S3-compatible bucket? Results are in
// .scratch/logs-v2/research/object-store-probe.md.
//
// Every test skips unless OBJECTSTORE_PROBE=1 and a bucket is given:
//
//	OBJECTSTORE_PROBE=1 SOLIDTRACE_TEST_S3_ENDPOINT=http://127.0.0.1:9000 \
//	SOLIDTRACE_TEST_S3_BUCKET=solidtrace-dev \
//	SOLIDTRACE_OBJECT_STORE_KEY_ID=… SOLIDTRACE_OBJECT_STORE_SECRET=… \
//	go test -count=1 -v -run 'TestProbe1' ./bench/objectstore/
//
// Run the tests one at a time, in order; each takes under a minute. The
// generated files are kept in ~/.cache/solid-trace/probe-objectstore between
// runs; TestProbe9Clean deletes them and the bucket's copies.
package objectstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/bench/logslayouts"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	// probeProject is the one Project the probe's Logs belong to.
	probeProject = 1
	// logsPerHour keeps 168 hour files under about 300 MB.
	logsPerHour = 16_000
	probeDays   = 7
	// prefix is where the probe's files go in the bucket.
	prefix = "probe-objectstore/"
)

// day0 is the first day of the probe's Logs.
var day0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

type probeEnv struct {
	endpoint, bucket, key, secret string
	// dir holds the generated files; extDir the httpfs extension.
	dir, extDir string
}

func envOrSkip(t *testing.T) probeEnv {
	t.Helper()
	if os.Getenv("OBJECTSTORE_PROBE") == "" {
		t.Skip("set OBJECTSTORE_PROBE=1 to run")
	}
	e := probeEnv{
		endpoint: os.Getenv("SOLIDTRACE_TEST_S3_ENDPOINT"),
		bucket:   os.Getenv("SOLIDTRACE_TEST_S3_BUCKET"),
		key:      os.Getenv("SOLIDTRACE_OBJECT_STORE_KEY_ID"),
		secret:   os.Getenv("SOLIDTRACE_OBJECT_STORE_SECRET"),
	}
	if e.endpoint == "" || e.bucket == "" || e.key == "" || e.secret == "" {
		t.Fatal("set SOLIDTRACE_TEST_S3_ENDPOINT, SOLIDTRACE_TEST_S3_BUCKET, SOLIDTRACE_OBJECT_STORE_KEY_ID and SOLIDTRACE_OBJECT_STORE_SECRET")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	e.dir = filepath.Join(home, ".cache/solid-trace/probe-objectstore")
	e.extDir = filepath.Join(home, ".cache/solid-trace/duckdb_extensions")
	if v := os.Getenv("SOLIDTRACE_DUCKDB_EXTENSION_DIRECTORY"); v != "" {
		e.extDir = v
	}
	return e
}

// filesDir is where the export writes the hour files.
func (e probeEnv) filesDir() string { return filepath.Join(e.dir, "logs_parquet") }

// hourFile is the path of an hour's file below the files directory.
func hourFile(hour time.Time) string {
	return fmt.Sprintf("project=%d/day=%s/hour=%02d/data.parquet", probeProject, hour.Format("2006-01-02"), hour.Hour())
}

func (e probeEnv) s3Client() *s3.Client {
	return s3.New(s3.Options{
		Region:                     "us-east-1",
		BaseEndpoint:               aws.String(e.endpoint),
		UsePathStyle:               true,
		Credentials:                credentials.NewStaticCredentialsProvider(e.key, e.secret, ""),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
}

// openDuckDB opens a DuckDB with httpfs's extension directory set and
// extension autoinstall and autoload off, as EventStore would, then sets
// each global setting in settings ("name=value&...").
func (e probeEnv) openDuckDB(t *testing.T, settings string) *sql.DB {
	t.Helper()
	dsn := "?autoinstall_known_extensions=false&autoload_known_extensions=false&extension_directory=" + e.extDir
	db, err := sql.Open("duckdb", filepath.Join(t.TempDir(), "probe.duckdb")+dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, setting := range strings.Split(settings, "&") {
		if name, value, ok := strings.Cut(setting, "="); ok {
			mustExec(t, db, fmt.Sprintf("SET GLOBAL %s = '%s'", name, value))
		}
	}
	return db
}

// secretSQL is the CREATE SECRET EventStore would run for endpoint.
func (e probeEnv) secretSQL(endpoint string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(endpoint, "http://"), "https://")
	return fmt.Sprintf(`CREATE OR REPLACE SECRET solidtrace_bucket (
		TYPE s3, KEY_ID '%s', SECRET '%s', ENDPOINT '%s', URL_STYLE 'path',
		USE_SSL %v, REGION 'us-east-1', SCOPE 's3://%s/')`,
		e.key, e.secret, host, strings.HasPrefix(endpoint, "https://"), e.bucket)
}

func mustExec(t *testing.T, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, q string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), q); err != nil {
		t.Fatalf("%v\n%s", err, q)
	}
}

// TestProbe1Generate writes a day of hour files through the export path,
// from the logs bench's realistic Logs, then copies it to six more days with
// the same layout, and uploads the 168 files to the bucket.
func TestProbe1Generate(t *testing.T) {
	e := envOrSkip(t)
	logger.Init()
	if _, err := os.Stat(filepath.Join(e.filesDir(), hourFile(day0.Add(probeDays*24*time.Hour-time.Hour)))); err == nil {
		t.Log("files already generated")
	} else {
		generate(t, e)
	}
	upload(t, e)
}

func generate(t *testing.T, e probeEnv) {
	if err := os.RemoveAll(e.dir); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	events, err := storage.NewDuckDBWriter(filepath.Join(e.dir, "events.duckdb"), "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	store, err := storage.NewLogStore(events, filepath.Join(e.dir, "logs.duckdb"), e.filesDir())
	if err != nil {
		t.Fatal(err)
	}
	g := logslayouts.NewGenerator(1)
	id := int64(0)
	for h := range 24 {
		hour := day0.Add(time.Duration(h) * time.Hour)
		for batch := range logsPerHour / 2000 {
			logs := make([]models.Log, 2000)
			for i := range logs {
				n := batch*2000 + i
				logs[i] = probeLog(g.Log(id), hour.Add(time.Duration(n)*time.Hour/logsPerHour))
				id++
			}
			if err := store.AppendLogBatches(probeProject, []storage.LogBatch{{ID: uuid.Must(uuid.NewV7()), Logs: logs}}, hour.Add(59*time.Minute)); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.ExportLogs(hour.Add(2*time.Hour + time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("exported 24 hours of %d Logs in %v", logsPerHour, time.Since(began))

	// Six more days: the same files with their times moved on, written as
	// the export writes them.
	db := e.openDuckDB(t, "")
	for d := 1; d < probeDays; d++ {
		for h := range 24 {
			src := filepath.Join(e.filesDir(), hourFile(day0.Add(time.Duration(h)*time.Hour)))
			hour := day0.Add(time.Duration(d*24+h) * time.Hour)
			dst := filepath.Join(e.filesDir(), hourFile(hour))
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			mustExec(t, db, fmt.Sprintf(`COPY (
				SELECT * REPLACE (time + INTERVAL %d DAY AS time) FROM read_parquet('%s') ORDER BY time, id
			) TO '%s' (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE 30720)`, d, src, dst))
		}
	}
	var files int
	var bytes int64
	filepath.WalkDir(e.filesDir(), func(path string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(path, ".parquet") {
			info, _ := d.Info()
			files++
			bytes += info.Size()
		}
		return nil
	})
	t.Logf("generated %d files, %.1f MB, in %v", files, float64(bytes)/1e6, time.Since(began))
	var cols int
	if err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM parquet_schema('%s') WHERE num_children IS NULL",
		filepath.Join(e.filesDir(), hourFile(day0)))).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	var meta int64
	if err := db.QueryRow(fmt.Sprintf("SELECT max(footer_size) FROM parquet_file_metadata('%s')",
		filepath.Join(e.filesDir(), hourFile(day0)))).Scan(&meta); err != nil {
		t.Logf("footer size: %v", err)
	}
	t.Logf("an hour file has %d leaf columns and a %d B footer", cols, meta)
}

// probeLog is a bench Log as a models.Log at time at.
func probeLog(l logslayouts.Log, at time.Time) models.Log {
	log := models.Log{
		ID: uuid.New(), Time: at, Level: l.Level, Message: l.Message, Service: l.Service,
		Environment: l.Environment, TraceID: l.TraceID, SpanID: l.SpanID,
	}
	a := &log.Attributes
	for _, attr := range l.Attrs {
		switch v := attr.Value; v.Kind {
		case logslayouts.KindString:
			if a.String == nil {
				a.String = map[string]string{}
			}
			a.String[attr.Key] = v.S
		case logslayouts.KindInt, logslayouts.KindFloat:
			if a.Number == nil {
				a.Number = map[string]float64{}
			}
			a.Number[attr.Key] = v.JSON().(float64)
		case logslayouts.KindBool:
			if a.Bool == nil {
				a.Bool = map[string]bool{}
			}
			a.Bool[attr.Key] = v.B
		case logslayouts.KindArray:
			if a.JSON == nil {
				a.JSON = map[string]string{}
			}
			j, _ := json.Marshal(v.A)
			a.JSON[attr.Key] = string(j)
		}
	}
	return log
}

func upload(t *testing.T, e probeEnv) {
	client := e.s3Client()
	began := time.Now()
	n := 0
	err := filepath.WalkDir(e.filesDir(), func(path string, d fs.DirEntry, err error) error {
		if err != nil || !strings.HasSuffix(path, "data.parquet") {
			return err
		}
		rel, _ := filepath.Rel(e.filesDir(), path)
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = client.PutObject(context.Background(), &s3.PutObjectInput{
			Bucket: aws.String(e.bucket), Key: aws.String(prefix + rel), Body: f,
		})
		n++
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uploaded %d files in %v", n, time.Since(began))
}

// TestProbe2LoadAndSecret checks httpfs LOADs from the fixed extension
// directory with autoinstall and autoload off, and that a secret made on one
// pooled connection is used by the others.
func TestProbe2LoadAndSecret(t *testing.T) {
	e := envOrSkip(t)
	ctx := context.Background()
	db := e.openDuckDB(t, "")
	file := fmt.Sprintf("s3://%s/%s%s", e.bucket, prefix, hourFile(day0))

	var n int64
	err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM read_parquet('%s')", file)).Scan(&n)
	t.Logf("before LOAD, reading s3:// fails: %v", err)

	// Hold three connections at once, so each is a different one.
	conns := make([]*sql.Conn, 3)
	for i := range conns {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		conns[i] = c
	}
	mustExec(t, conns[0], "LOAD httpfs")
	var path string
	if err := conns[1].QueryRowContext(ctx, "SELECT install_path FROM duckdb_extensions() WHERE extension_name = 'httpfs' AND loaded").Scan(&path); err != nil {
		t.Fatalf("httpfs isn't loaded on another connection: %v", err)
	}
	t.Logf("httpfs LOADed on one connection is loaded on another, from %s", path)

	err = conns[1].QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM read_parquet('%s')", file)).Scan(&n)
	t.Logf("with no secret, reading fails: %v", err)

	mustExec(t, conns[0], e.secretSQL(e.endpoint))
	for i, c := range conns {
		if err := c.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM read_parquet('%s')", file)).Scan(&n); err != nil {
			t.Fatalf("connection %d can't read with the secret: %v", i, err)
		}
		var secrets string
		c.QueryRowContext(ctx, "SELECT string_agg(name || ' ' || type || ' ' || persistent || ' ' || storage, ', ') FROM duckdb_secrets()").Scan(&secrets)
		t.Logf("connection %d reads %d Logs; its secrets: %s", i, n, secrets)
	}
	// A connection opened after the secret was made.
	c, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM read_parquet('%s')", file)).Scan(&n); err != nil {
		t.Fatalf("a new connection can't read with the secret: %v", err)
	}
	t.Logf("a connection opened after the secret reads %d Logs", n)

	// A file outside the secret's scope gets no credentials.
	err = c.QueryRowContext(ctx, "SELECT count(*) FROM read_parquet('s3://another-bucket/x.parquet')").Scan(&n)
	t.Logf("another bucket: %v", err)
}

// TestProbe3MixedRead reads half a day's hours from local files and half
// from the bucket in one read_parquet list.
func TestProbe3MixedRead(t *testing.T) {
	e := envOrSkip(t)
	db := e.openDuckDB(t, "")
	mustExec(t, db, "LOAD httpfs")
	mustExec(t, db, e.secretSQL(e.endpoint))
	var local, mixed []string
	for h := range 24 {
		rel := hourFile(day0.Add(time.Duration(h) * time.Hour))
		path := filepath.Join(e.filesDir(), rel)
		local = append(local, "'"+path+"'")
		if h%2 == 0 {
			mixed = append(mixed, "'"+path+"'")
		} else {
			mixed = append(mixed, fmt.Sprintf("'s3://%s/%s%s'", e.bucket, prefix, rel))
		}
	}
	q := "SELECT count(*), count(DISTINCT id), min(time), max(time) FROM read_parquet([%s], union_by_name = true, hive_partitioning = false)"
	var want, got [4]any
	if err := db.QueryRow(fmt.Sprintf(q, strings.Join(local, ", "))).Scan(&want[0], &want[1], &want[2], &want[3]); err != nil {
		t.Fatal(err)
	}
	began := time.Now()
	if err := db.QueryRow(fmt.Sprintf(q, strings.Join(mixed, ", "))).Scan(&got[0], &got[1], &got[2], &got[3]); err != nil {
		t.Fatal(err)
	}
	t.Logf("local: %v; mixed local and s3://: %v, in %v", want, got, time.Since(began))
	if want != got {
		t.Errorf("mixed read differs")
	}
}

// chartSQL is a count-by-bin chart over hours [from, from+hours), read a day
// at a time as LogStore reads files, from local paths or the bucket.
func (e probeEnv) chartSQL(hours int, bin string, remote bool) string {
	var days []string
	from := day0
	to := day0.Add(time.Duration(hours) * time.Hour)
	for d := from; d.Before(to); d = d.Add(24 * time.Hour) {
		var files []string
		for h := range 24 {
			hour := d.Add(time.Duration(h) * time.Hour)
			if !hour.Before(to) {
				break
			}
			if remote {
				files = append(files, fmt.Sprintf("'s3://%s/%s%s'", e.bucket, prefix, hourFile(hour)))
			} else {
				files = append(files, "'"+filepath.Join(e.filesDir(), hourFile(hour))+"'")
			}
		}
		days = append(days, fmt.Sprintf(
			"SELECT time FROM (SELECT * FROM read_parquet([%s], union_by_name = true, hive_partitioning = false)) WHERE time >= TIMESTAMP '%s' AND time < TIMESTAMP '%s'",
			strings.Join(files, ", "), d.Format(time.DateTime), earlier(to, d.Add(24*time.Hour)).Format(time.DateTime)))
	}
	return fmt.Sprintf("SELECT time_bucket(INTERVAL '%s', time) AS bin, count(*) FROM (%s) GROUP BY bin ORDER BY bin",
		bin, strings.Join(days, "\nUNION ALL BY NAME\n"))
}

// earlier returns the earlier of two times.
func earlier(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

type chartCase struct {
	name  string
	hours int
	bin   string
}

var charts = []chartCase{{"24 hours", 24, "5 minutes"}, {"7 days", probeDays * 24, "1 hour"}}

// runChart runs a chart and returns its time and row count.
func runChart(t *testing.T, db *sql.DB, q string) (time.Duration, int) {
	t.Helper()
	began := time.Now()
	rows, err := db.Query(q)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	return time.Since(began), n
}

// cacheSettings are DuckDB 1.5.6's caches for remote Parquet, all on or all
// off.
func cacheSettings(on bool) string {
	return fmt.Sprintf("parquet_metadata_cache=%[1]v&enable_http_metadata_cache=%[1]v&enable_external_file_cache=%[1]v", on)
}

// TestProbe4LocalCharts times the charts over local files: the baseline.
func TestProbe4LocalCharts(t *testing.T) {
	e := envOrSkip(t)
	for _, caches := range []bool{false, true} {
		db := e.openDuckDB(t, cacheSettings(caches))
		for _, c := range charts {
			q := e.chartSQL(c.hours, c.bin, false)
			cold, n := runChart(t, db, q)
			warm, _ := runChart(t, db, q)
			t.Logf("local, caches %v, %s: %d bins, cold %v, warm %v", caches, c.name, n, cold.Round(time.Millisecond), warm.Round(time.Millisecond))
		}
	}
}

// TestProbe5RemoteCharts times the charts over the bucket's files, through a
// proxy that counts requests and can add a round trip's latency to each.
func TestProbe5RemoteCharts(t *testing.T) {
	e := envOrSkip(t)
	latency := envDuration("OBJECTSTORE_PROBE_LATENCY", 0)
	connect := envDuration("OBJECTSTORE_PROBE_CONNECT", 0)
	p := startProxy(t, e.endpoint, latency, connect)
	threads := os.Getenv("OBJECTSTORE_PROBE_THREADS")
	cacheModes := []bool{false, true}
	if os.Getenv("OBJECTSTORE_PROBE_CACHES_ON") != "" {
		cacheModes = []bool{true}
	}
	for _, caches := range cacheModes {
		settings := cacheSettings(caches)
		if threads != "" {
			settings += "&threads=" + threads
		}
		db := e.openDuckDB(t, settings)
		mustExec(t, db, "LOAD httpfs")
		// httpfs's own settings, which exist only once it is loaded.
		for _, setting := range strings.Split(os.Getenv("OBJECTSTORE_PROBE_SETTINGS"), "&") {
			if name, value, ok := strings.Cut(setting, "="); ok {
				mustExec(t, db, fmt.Sprintf("SET GLOBAL %s = '%s'", name, value))
			}
		}
		mustExec(t, db, e.secretSQL(p.url))
		for _, c := range charts {
			if c.hours > 24 && os.Getenv("OBJECTSTORE_PROBE_SKIP_7D") != "" {
				continue
			}
			q := e.chartSQL(c.hours, c.bin, true)
			p.reset()
			cold, n := runChart(t, db, q)
			coldStats := p.stats()
			p.reset()
			warm, _ := runChart(t, db, q)
			t.Logf("remote (latency %v, connect %v, threads %s, %s), caches %v, %s: %d bins\n    cold %v: %s\n    warm %v: %s",
				latency, connect, cmpOr(threads, "default"), os.Getenv("OBJECTSTORE_PROBE_SETTINGS"), caches, c.name, n,
				cold.Round(time.Millisecond), coldStats, warm.Round(time.Millisecond), p.stats())
		}
		rows, err := db.Query("SELECT tag, memory_usage_bytes FROM duckdb_memory() WHERE memory_usage_bytes > 0 ORDER BY tag")
		if err != nil {
			t.Fatal(err)
		}
		var memory []string
		for rows.Next() {
			var tag string
			var bytes int64
			rows.Scan(&tag, &bytes)
			memory = append(memory, fmt.Sprintf("%s %.1f MB", tag, float64(bytes)/1e6))
		}
		rows.Close()
		t.Logf("memory after the warm runs, caches %v: %s", caches, strings.Join(memory, ", "))
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func envDuration(name string, def time.Duration) time.Duration {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		panic(err)
	}
	return d
}

// TestProbe6RowGroups rewrites the first day's files with 16 row groups
// each, uploads them under another prefix and counts the requests a cold
// 24-hour chart makes: busy Projects' hour files have hundreds of row groups.
func TestProbe6RowGroups(t *testing.T) {
	e := envOrSkip(t)
	latency := envDuration("OBJECTSTORE_PROBE_LATENCY", 0)
	p := startProxy(t, e.endpoint, latency, 0)
	db := e.openDuckDB(t, cacheSettings(true)+"&threads=16")
	mustExec(t, db, "LOAD httpfs")
	mustExec(t, db, "SET GLOBAL httpfs_connection_caching = true")
	mustExec(t, db, e.secretSQL(p.url))
	client := e.s3Client()
	var files []string
	for h := range 24 {
		rel := hourFile(day0.Add(time.Duration(h) * time.Hour))
		local := filepath.Join(t.TempDir(), "rg.parquet")
		mustExec(t, db, fmt.Sprintf(`COPY (SELECT * FROM read_parquet('%s') ORDER BY time, id)
			TO '%s' (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE 1000)`, filepath.Join(e.filesDir(), rel), local))
		f, err := os.Open(local)
		if err != nil {
			t.Fatal(err)
		}
		key := "probe-objectstore/rg/" + rel
		_, err = client.PutObject(context.Background(), &s3.PutObjectInput{Bucket: aws.String(e.bucket), Key: aws.String(key), Body: f})
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, fmt.Sprintf("'s3://%s/%s'", e.bucket, key))
	}
	var groups int
	if err := db.QueryRow(fmt.Sprintf("SELECT count(DISTINCT (file_name, row_group_id)) FROM parquet_metadata([%s])", strings.Join(files, ", "))).Scan(&groups); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db = e.openDuckDB(t, cacheSettings(true)+"&threads=16")
	mustExec(t, db, "LOAD httpfs")
	mustExec(t, db, "SET GLOBAL httpfs_connection_caching = true")
	mustExec(t, db, e.secretSQL(p.url))
	q := fmt.Sprintf("SELECT time_bucket(INTERVAL '5 minutes', time) AS bin, count(*) FROM read_parquet([%s], union_by_name = true, hive_partitioning = false) GROUP BY bin", strings.Join(files, ", "))
	p.reset()
	took, n := runChart(t, db, q)
	t.Logf("24 files, %d row groups, latency %v: %d bins, cold %v: %s", groups, latency, n, took.Round(time.Millisecond), p.stats())
	p.reset()
	q = fmt.Sprintf("SELECT count(*) FROM read_parquet([%s], union_by_name = true, hive_partitioning = false) WHERE level = 'error'", strings.Join(files, ", "))
	took, n = runChart(t, db, q)
	t.Logf("the same files, count where level = 'error': cold %v: %s", took.Round(time.Millisecond), p.stats())
}

// TestProbe9Clean deletes the generated files and the bucket's copies.
func TestProbe9Clean(t *testing.T) {
	e := envOrSkip(t)
	client := e.s3Client()
	ctx := context.Background()
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(client, &s3.ListObjectsV2Input{Bucket: aws.String(e.bucket), Prefix: aws.String(prefix)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range page.Contents {
			keys = append(keys, *o.Key)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		if _, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(e.bucket), Key: aws.String(k)}); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("deleted %d objects", len(keys))
	if err := os.RemoveAll(e.dir); err != nil {
		t.Fatal(err)
	}
}
