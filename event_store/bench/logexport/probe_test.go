// Package logexport is a probe of how long the Log export holds Log
// processing up, for the EventStore performance effort
// (.scratch/eventstore-performance/). It builds synthetic Logs through the
// LogStore, a busy hour to export and a busier hour after it, then runs the export
// while a writer appends small Log batches as Log processing does, and times
// the export and the longest wait of an append. It is skipped unless
// LOGEXPORT_PROBE is set, and must finish in under a minute:
//
//	LOGEXPORT_PROBE=1 timeout 60 taskset -c 0,1 mise exec -- go test -count=1 -v ./bench/logexport/
//
// LOGEXPORT_PROBE_LOGS sets how many Logs each hour holds, a multiple of
// 50,000 (300,000 by default), and LOGEXPORT_PROBE_THREADS how many threads
// DuckDB runs the export on (2 by default; production runs 8 on 2 vCPUs).
package logexport

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

const (
	project uint32 = 1

	defaultLogs = 300_000
	// seedLogs are appended through the LogStore, and copied to make the
	// rest; the number of Logs is a multiple of it.
	seedLogs = 50_000
	// appendLogs are the Logs of each batch the writer appends while the
	// export runs, and appendEvery how often it starts one; every
	// lateEvery-th batch is late, in the exported hour.
	appendLogs  = 200
	appendEvery = 20 * time.Millisecond
	lateEvery   = 10
)

// hour is the hour the export moves to a Log file. The hour after it stays in
// the hot table, and the writer's Logs go to the hour after that.
var hour = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func TestLogExport(t *testing.T) {
	if os.Getenv("LOGEXPORT_PROBE") == "" {
		t.Skip("set LOGEXPORT_PROBE=1 to run")
	}
	logs := envInt(t, "LOGEXPORT_PROBE_LOGS", defaultLogs)
	if logs < seedLogs || logs%seedLogs != 0 {
		t.Fatalf("LOGEXPORT_PROBE_LOGS must be a multiple of %d", seedLogs)
	}
	threads := envInt(t, "LOGEXPORT_PROBE_THREADS", 2)
	// The export logs how long its steps take.
	logger.Init()

	dir := t.TempDir()
	start := time.Now()
	events, store := openStore(t, dir, 4)
	appendSeed(t, store, logs)
	events.Close()
	copySeed(t, dir, logs)
	t.Logf("%d Logs in the hour to export, %d in the hour after: built in %v", logs, 2*logs, time.Since(start).Round(time.Millisecond))

	events, store = openStore(t, dir, threads)
	now := hour.Add(2*time.Hour + 5*time.Minute)
	stop := make(chan struct{})
	var waits []time.Duration
	appended := 0
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			case <-time.After(appendEvery):
			}
			at := now
			if i%lateEvery == lateEvery-1 {
				at = hour.Add(30 * time.Minute)
			}
			batch := storage.LogBatch{ID: uuid.Must(uuid.NewV7())}
			for j := range appendLogs {
				log := makeLog(j, logs)
				log.ID, log.Time = uuid.Must(uuid.NewV7()), at
				batch.Logs = append(batch.Logs, log)
			}
			began := time.Now()
			if err := store.AppendLogBatches(project, []storage.LogBatch{batch}, now); err != nil {
				t.Error(err)
				return
			}
			waits = append(waits, time.Since(began))
			appended += appendLogs
		}
	})
	// The writer runs alone a little first, so its usual wait shows.
	time.Sleep(time.Second)
	began := time.Now()
	if err := store.ExportLogs(now); err != nil {
		t.Fatal(err)
	}
	exported := time.Since(began)
	time.Sleep(time.Second)
	close(stop)
	wg.Wait()
	events.Close()

	slices.Sort(waits)
	t.Logf("export took %v on %d threads; %d appends of %d Logs: median wait %v, p99 %v, longest %v",
		exported.Round(time.Millisecond), threads, len(waits), appendLogs,
		waits[len(waits)/2].Round(time.Millisecond), waits[len(waits)*99/100].Round(time.Millisecond), waits[len(waits)-1].Round(time.Millisecond))
	checkEveryLogKept(t, dir, 3*logs+appended)
}

// checkEveryLogKept checks that the Log files, the hot table and logs_late
// hold want Logs between them, so that the export lost none and copied none
// twice, and that the exported hour has its file.
func checkEveryLogKept(t *testing.T, dir string, want int) {
	t.Helper()
	db, err := sql.Open("duckdb", filepath.Join(dir, "logs.duckdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var inFiles, files, hot, late int
	if err := db.QueryRow("SELECT coalesce(sum(log_count), 0), count(*) FROM logs.log_files WHERE project_id = ?", project).Scan(&inFiles, &files); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM logs.logs_hot_%d", project)).Scan(&hot); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM logs.logs_late WHERE project_id = ?", project).Scan(&late); err != nil {
		t.Fatal(err)
	}
	t.Logf("after the export: %d Logs in %d files, %d in the hot table, %d late", inFiles, files, hot, late)
	if files == 0 {
		t.Error("the exported hour has no Log file")
	}
	if got := inFiles + hot + late; got != want {
		t.Errorf("%d Logs kept, want %d", got, want)
	}
}

func envInt(t *testing.T, name string, fallback int) int {
	t.Helper()
	s := os.Getenv(name)
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return n
}

// openStore opens the probe's DuckDB in dir on the given number of threads,
// and its LogStore.
func openStore(t *testing.T, dir string, threads int) (*storage.DuckDBWriter, *storage.LogStore) {
	t.Helper()
	events, err := storage.NewDuckDBWriter(fmt.Sprintf("%s?threads=%d", filepath.Join(dir, "events.duckdb"), threads), filepath.Join(dir, "parquet"), "", "2GB")
	if err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewLogStore(events, filepath.Join(dir, "logs.duckdb"), filepath.Join(dir, "logs_parquet"))
	if err != nil {
		events.Close()
		t.Fatal(err)
	}
	if err := store.UseLocalDiskOnly(); err != nil {
		events.Close()
		t.Fatal(err)
	}
	return events, store
}

// appendSeed appends the hour's first seedLogs Logs to the Project's hot
// table as Log processing does, in one batch.
func appendSeed(t *testing.T, store *storage.LogStore, logs int) {
	t.Helper()
	batch := storage.LogBatch{ID: uuid.Must(uuid.NewV7()), Logs: make([]models.Log, 0, seedLogs)}
	for i := range seedLogs {
		batch.Logs = append(batch.Logs, makeLog(i, logs))
	}
	if err := store.AppendLogBatches(project, []storage.LogBatch{batch}, hour.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
}

// copySeed fills the rest of the hour in the hot table with copies of its
// seed Logs, each later in time by the seed's span and with new ids, then
// copies the whole hour into the hour after it twice. So the hot table keeps
// more row groups after the export than a checkpoint's vacuum merges (3), as
// production's hours of about 100 row groups each do; with fewer, the
// checkpoint after the export rewrites every one left.
func copySeed(t *testing.T, dir string, logs int) {
	t.Helper()
	// Opened by itself, logs.duckdb is the catalog "logs", as when attached.
	db, err := sql.Open("duckdb", filepath.Join(dir, "logs.duckdb")+"?threads=4")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// In one transaction, DuckDB writes the copies' full row groups to the
	// file as it goes, rather than to its write-ahead log.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	table := fmt.Sprintf("logs.logs_hot_%d", project)
	copyRows := func(shift time.Duration, before time.Time) {
		_, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s
			SELECT * REPLACE (time + to_microseconds(%d) AS time, uuidv7() AS id)
			FROM %s WHERE time < TIMESTAMP '%s'`,
			table, shift.Microseconds(), table, before.Format("2006-01-02 15:04:05.999999")))
		if err != nil {
			t.Fatal(err)
		}
	}
	span := time.Duration(seedLogs) * time.Hour / time.Duration(logs)
	for c := 1; c < logs/seedLogs; c++ {
		copyRows(time.Duration(c)*span, hour.Add(span))
	}
	copyRows(time.Hour, hour.Add(time.Hour))
	copyRows(time.Hour, hour.Add(time.Hour))
	// Each large INSERT leaves its last row group part full, where Log
	// processing's small commits fill each one, so the table is written again.
	if _, err := tx.Exec(fmt.Sprintf("CREATE OR REPLACE TABLE %s AS SELECT * FROM %s ORDER BY time, id", table, table)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// Each Log has a message of 200 to 580 bytes, and Attributes: some promoted,
// and some only in the maps, since the first Log fills the day's promoted set
// with 100 keys.
var (
	methods  = []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}
	routes   = []string{"/api/v1/orders", "/api/v1/products", "/api/v1/carts", "/api/v1/search", "/api/v1/users", "/health"}
	levels   = []string{"info", "info", "info", "info", "debug", "warn", "error"}
	services = []string{"api", "checkout", "worker", "search"}
	regions  = []string{"us-east-1", "us-west-2", "eu-west-1", "ap-south-1"}
	words    = strings.Fields(`request handled cache miss hit upstream downstream retry
		timeout connection pool acquired released query rows returned took
		session token refreshed validated order product cart checkout payment
		authorized captured declined inventory reserved shipment queued worker
		job started finished failed skipped batch flushed index updated`)
)

// makeLog returns the i-th of the hour's logs Logs, the same each time.
func makeLog(i, logs int) models.Log {
	r := rand.New(rand.NewPCG(1, uint64(i)))
	method, route := methods[r.IntN(len(methods))], routes[r.IntN(len(routes))]
	status := []float64{200, 200, 200, 200, 201, 204, 400, 404, 500}[r.IntN(9)]
	duration := float64(r.IntN(2000))
	user := fmt.Sprintf("u-%05d", r.IntN(50_000))

	var message strings.Builder
	fmt.Fprintf(&message, "%s %s/%d completed with status %d in %dms for user %s",
		method, route, r.IntN(1_000_000), int(status), int(duration), user)
	for length := 200 + r.IntN(380); message.Len() < length; {
		message.WriteByte(' ')
		if r.IntN(8) == 0 {
			fmt.Fprintf(&message, "id=%016x", r.Uint64())
		} else {
			message.WriteString(words[r.IntN(len(words))])
		}
	}

	log := models.Log{
		ID:          uuid.Must(uuid.NewV7()),
		Time:        hour.Add(time.Duration(i) * time.Hour / time.Duration(logs)),
		Level:       levels[r.IntN(len(levels))],
		Message:     message.String(),
		Service:     services[r.IntN(len(services))],
		Environment: "production",
		TraceID:     fmt.Sprintf("%016x%016x", r.Uint64(), r.Uint64()),
		SpanID:      fmt.Sprintf("%016x", r.Uint64()),
		Attributes: models.Attributes{
			String: map[string]string{"method": method, "route": route, "user_id": user},
			Number: map[string]float64{"status": status, "duration_ms": duration},
		},
	}
	if i == 0 {
		// The day's first Log promotes its keys until the set holds 100.
		for k := len(log.Attributes.String) + len(log.Attributes.Number); k < storage.MaxPromotedKeys; k++ {
			log.Attributes.String[fmt.Sprintf("filler_%02d", k)] = "x"
		}
		return log
	}
	log.Attributes.String["shop"] = fmt.Sprintf("s%03d", i%1000)
	log.Attributes.String["region"] = regions[r.IntN(len(regions))]
	log.Attributes.String["request_id"] = fmt.Sprintf("%016x", r.Uint64())
	return log
}
