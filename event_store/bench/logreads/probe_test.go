// Package logreads is a probe of how long a run of Log rows takes to read one
// busy hour of a Project's Logs, for the Log read speed effort
// (.scratch/log-read-speed/). It builds synthetic Logs through the LogStore,
// one hour in a hot table and the same hour as a Log file, and times each
// Query through the Query compiler and LogStore.ScanLogs, or ScanLogTable for
// a table, as the Query route runs it, on 2 DuckDB threads. It is skipped
// unless LOGREADS_PROBE is set, and must finish in under a minute:
//
//	LOGREADS_PROBE=1 timeout 60 mise exec -- go test -count=1 -v ./bench/logreads/
//
// LOGREADS_PROBE_LOGS sets how many Logs the hour holds, a multiple of
// 50,000. The default, 300,000, is about as many as the dev box builds and
// reads in the minute; reads scale with the Logs read, so times at two sizes
// show the cost of a busier hour.
package logreads

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/google/uuid"
	"github.com/solidtrace/event_store/logquery"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/storage"
)

const (
	// fileProject's hour is exported to a Log file; hotProject's stays in its
	// hot table. Both hold the same Logs.
	fileProject uint32 = 1
	hotProject  uint32 = 2

	defaultLogs = 300_000
	// seedLogs are appended through the LogStore, and copied to make the
	// rest; the number of Logs is a multiple of it.
	seedLogs = 50_000
	// pageLimit and scanBudget are the Query route's defaults.
	pageLimit  = 200
	scanBudget = 1500 * time.Millisecond
	runs       = 3
)

// hour is the hour the Logs are in.
var hour = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

func TestLogReads(t *testing.T) {
	if os.Getenv("LOGREADS_PROBE") == "" {
		t.Skip("set LOGREADS_PROBE=1 to run")
	}
	logs := defaultLogs
	if s := os.Getenv("LOGREADS_PROBE_LOGS"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < seedLogs || n%seedLogs != 0 {
			t.Fatalf("LOGREADS_PROBE_LOGS must be a multiple of %d", seedLogs)
		}
		logs = n
	}
	store := buildLogs(t, logs)
	for _, project := range []struct {
		id    uint32
		files int
	}{{fileProject, 1}, {hotProject, 0}} {
		files, err := store.HourFiles(project.id, hour)
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != project.files {
			t.Fatalf("Project %d has %d Log files for the hour, want %d", project.id, len(files), project.files)
		}
	}

	// Each Query is a run of Log rows over the hour, and wants the Logs it
	// should find, as makeLog makes them.
	rare := logs / rareEvery
	term := `"` + rareTerm + `"`
	for _, read := range []struct {
		query string
		want  int
	}{
		{"", pageLimit},
		{"search " + term, rare},
		{"where message contains " + term, rare},
		{"where message contains_cs " + term, rare / 2},
		{"where message contains " + term + ` and shop == "s500"`, rare},
		{`where shop == "s500" and message contains ` + term, rare},
	} {
		for _, project := range []struct {
			name string
			id   uint32
		}{{"hot table", hotProject}, {"Log file", fileProject}} {
			took, found := timeRead(t, store, project.id, read.query)
			t.Logf("%-9s %6d ms %4d Logs  %s", project.name, took.Milliseconds(), found, cmp.Or(read.query, "(newest page)"))
			if found != read.want {
				t.Errorf("%s, %q found %d Logs, want %d", project.name, read.query, found, read.want)
			}
		}
	}

	// A text match and a condition on an Attribute only in the maps, written
	// in both orders, as Log rows and as table runs. A table run reads the
	// Logs that pass the condition, not the newest page.
	for _, read := range []struct {
		query string
		want  int
	}{
		{`where shop == "s500" | summarize count()`, logs / shops},
		{"where message contains " + term + ` and shop == "s500" | summarize count()`, rare},
		{`where shop == "s500" and message contains ` + term + " | summarize count()", rare},
		{"search " + term + ` | where shop == "s500" | summarize count()`, rare},
		{`where shop == "s500" | search ` + term + " | summarize count()", rare},
	} {
		for _, project := range []struct {
			name string
			id   uint32
		}{{"hot table", hotProject}, {"Log file", fileProject}} {
			took, found := timeRead(t, store, project.id, read.query)
			t.Logf("%-9s %6d ms %4d Logs  %s", project.name, took.Milliseconds(), found, read.query)
			if found != read.want {
				t.Errorf("%s, %q found %d Logs, want %d", project.name, read.query, found, read.want)
			}
		}
	}
}

// buildLogs stores the hour's Logs for both Projects and returns a LogStore
// that reads them on 2 threads. Appending through the LogStore takes about
// 50 µs a Log, too slow for the probe's minute, so for each Project it appends
// the hour's first seedLogs Logs as Log processing does, then copies them in
// SQL, shifted in time, to fill the hour. The copies keep the hot table's
// columns and its rows in time order. fileProject's hour is then exported to
// a Log file listed in the manifest. Setup runs on 4 threads.
func buildLogs(t *testing.T, logs int) *storage.LogStore {
	t.Helper()
	dir := t.TempDir()
	// The hot window runs from the start of the previous hour to the start
	// of the next, so at half past the hour its Logs go to the hot table.
	inHour := hour.Add(30 * time.Minute)
	start := time.Now()

	events, store := openStore(t, dir, 4)
	appendSeed(t, store, fileProject, logs, inHour)
	events.Close()
	copySeed(t, dir, fileProject, logs)
	built := time.Since(start)

	events, store = openStore(t, dir, 4)
	if err := store.ExportLogs(hour.Add(2*time.Hour + 5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	exported := time.Since(start) - built
	appendSeed(t, store, hotProject, logs, inHour)
	events.Close()
	copySeed(t, dir, hotProject, logs)
	t.Logf("%d Logs: built in %v, exported in %v, built again in %v", logs,
		built.Round(time.Millisecond), exported.Round(time.Millisecond), (time.Since(start) - built - exported).Round(time.Millisecond))

	events, store = openStore(t, dir, 2)
	t.Cleanup(events.Close)
	return store
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

// appendSeed appends the hour's first seedLogs Logs to a Project's hot table
// as Log processing does, in one batch.
func appendSeed(t *testing.T, store *storage.LogStore, projectID uint32, logs int, now time.Time) {
	t.Helper()
	batch := storage.LogBatch{ID: uuid.Must(uuid.NewV7()), Logs: make([]models.Log, 0, seedLogs)}
	for i := range seedLogs {
		batch.Logs = append(batch.Logs, makeLog(i, logs))
	}
	if err := store.AppendLogBatches(projectID, []storage.LogBatch{batch}, now); err != nil {
		t.Fatal(err)
	}
}

// copySeed fills the rest of the hour in a Project's hot table with copies of
// its seed Logs, each later in time by the seed's span and with new ids, in
// time order, as makeLog would make them.
func copySeed(t *testing.T, dir string, projectID uint32, logs int) {
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
	table := fmt.Sprintf("logs.logs_hot_%d", projectID)
	span := time.Duration(seedLogs) * time.Hour / time.Duration(logs)
	for c := 1; c < logs/seedLogs; c++ {
		_, err := tx.Exec(fmt.Sprintf(`INSERT INTO %s
			SELECT * REPLACE (time + to_microseconds(%d) AS time, uuidv7() AS id)
			FROM %s WHERE time < TIMESTAMP '%s'`,
			table, (time.Duration(c) * span).Microseconds(), table, hour.Add(span).Format("2006-01-02 15:04:05.999999")))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// timeRead runs a Query over the hour as the Query route does, and returns
// its best time of a few runs and how many Logs it found: the Logs of a page
// of Log rows, or the count of a table's one row.
func timeRead(t *testing.T, store *storage.LogStore, projectID uint32, text string) (time.Duration, int) {
	t.Helper()
	var best time.Duration
	found := 0
	for run := range runs {
		start := time.Now()
		query, err := logquery.Compile(text, logquery.Bounds{From: hour, To: hour.Add(time.Hour), Now: hour.Add(2 * time.Hour)})
		if err != nil {
			t.Fatalf("%q: %v", text, err)
		}
		from, to := query.Range()
		if query.Table() {
			found = countTable(t, store, projectID, query, from, to)
		} else {
			page, err := store.ScanLogs(context.Background(), storage.LogScan{
				ProjectID: projectID,
				From:      from,
				To:        to,
				Where:     query.Where,
				Limit:     pageLimit,
				Budget:    scanBudget,
			})
			if err != nil {
				t.Fatalf("%q: %v", text, err)
			}
			found = len(page.Logs)
		}
		if took := time.Since(start); run == 0 || took < best {
			best = took
		}
	}
	return best, found
}

// countTable runs a Query whose table is one count, as the Query route runs
// a table without the bin cache, and returns the count.
func countTable(t *testing.T, store *storage.LogStore, projectID uint32, query *logquery.Query, from, to time.Time) int {
	t.Helper()
	rows, err := store.ScanLogTable(context.Background(), storage.LogTableScan{
		ProjectID: projectID,
		From:      from,
		To:        to,
		Where:     query.Where,
		Select:    query.Select,
		Query:     func(logs string) string { return query.TableSQL(logs, pageLimit) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0]) != 1 {
		t.Fatalf("table has %d rows, want one count", len(rows))
	}
	n, err := strconv.Atoi(fmt.Sprint(rows[0][0]))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// Each Log has a message of 200 to 580 bytes, and Attributes: some promoted,
// and some only in the maps, since the first Log fills the day's promoted set
// with 100 keys. Every rareEvery-th Log's message holds rareTerm, in lower
// case or upper case by turns, and all of them have shop s500.
const (
	rareTerm  = "zebrafish"
	rareEvery = 25_000
	shops     = 1000
)

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
		job started finished failed skipped batch flushed index updated
		document fetched stored deleted user account profile settings loaded
		saved webhook delivered received signature verified rate limit exceeded`)
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
	if i%rareEvery == rareEvery/2 {
		term := rareTerm
		if i/rareEvery%2 == 1 {
			term = strings.ToUpper(rareTerm)
		}
		message.WriteString(" " + term)
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
	log.Attributes.String["shop"] = fmt.Sprintf("s%03d", i%shops)
	log.Attributes.String["region"] = regions[r.IntN(len(regions))]
	log.Attributes.String["request_id"] = fmt.Sprintf("%016x", r.Uint64())
	return log
}
