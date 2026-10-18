package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/pkg/sqltext"
)

// newTestDuckDB is a DuckDBWriter in a directory of its own.
func newTestDuckDB(t *testing.T) *DuckDBWriter {
	t.Helper()
	logger.Init()
	dir := t.TempDir()
	w, err := NewDuckDBWriter(filepath.Join(dir, "events.duckdb"), filepath.Join(dir, "parquet"), "", "")
	if err != nil {
		t.Fatalf("NewDuckDBWriter: %v", err)
	}
	t.Cleanup(w.Close)
	return w
}

// testExtensionDirectory holds httpfs for the tests that read a bucket:
// SOLIDTRACE_TEST_DUCKDB_EXTENSION_DIRECTORY, or the dev box's
// ~/.cache/solid-trace/duckdb_extensions.
func testExtensionDirectory(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("SOLIDTRACE_TEST_DUCKDB_EXTENSION_DIRECTORY"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(home, ".cache", "solid-trace", "duckdb_extensions")
}

func TestUsingABucketWithoutHttpfsInTheExtensionDirectoryFailsSayingWhereItGoes(t *testing.T) {
	w := newTestDuckDB(t)
	dir := t.TempDir()
	cfg := &config.ObjectStore{Kind: config.ObjectStoreGCS, Bucket: "acme", Prefix: "solidtrace/", Region: "auto",
		Endpoint: config.GCSEndpoint, KeyID: "GOOG1EXAMPLE", Secret: "example-secret"}

	err := w.UseBucket(cfg, dir)
	if err == nil || !strings.Contains(err.Error(), dir+"/v") || !strings.Contains(err.Error(), "httpfs.duckdb_extension") {
		t.Errorf("Expected an error naming where httpfs goes in %s, got %v", dir, err)
	}
}

func TestABucketGivesDuckDBMoreThreadsThanCPUsForRemoteReads(t *testing.T) {
	dir := testExtensionDirectory(t)
	if matches, _ := filepath.Glob(filepath.Join(dir, "*", "*", "httpfs.duckdb_extension")); len(matches) == 0 {
		t.Skipf("no httpfs in %s", dir)
	}
	w := newTestDuckDB(t)
	cfg := &config.ObjectStore{Kind: config.ObjectStoreS3, Bucket: "acme", Prefix: "solidtrace/", Region: "us-east-1",
		KeyID: "EXAMPLE", Secret: "example-secret"}
	if err := w.UseBucket(cfg, dir); err != nil {
		t.Fatalf("UseBucket: %v", err)
	}
	ctx := context.Background()
	conn, err := w.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var threads int
	if err := conn.QueryRowContext(ctx, "SELECT current_setting('threads')").Scan(&threads); err != nil || threads != 4*runtime.NumCPU() {
		t.Errorf("Expected %d threads on a pooled connection, got %d (%v)", 4*runtime.NumCPU(), threads, err)
	}
}

// closedAddress is a host and port on loopback where nothing listens.
func closedAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// With a bucket, httpfs is loaded, and it could fetch any address the server
// reaches: http:// and https:// URLs, an S3 URL's ?s3_endpoint=, or a bucket
// name that is a host, as in s3://host:port#/x (MCP 12). Every URL here is
// refused or sent to S3's own host, so the internal server is never reached.
func TestWithABucketSQLReachesNoOtherHost(t *testing.T) {
	dir := testExtensionDirectory(t)
	if matches, _ := filepath.Glob(filepath.Join(dir, "*", "*", "httpfs.duckdb_extension")); len(matches) == 0 {
		t.Skipf("no httpfs in %s", dir)
	}
	// It stands for an internal service, and counts every connection, so a
	// TLS attempt counts too.
	var connections atomic.Int64
	internal := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("internal\n"))
	}))
	internal.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	internal.Start()
	defer internal.Close()
	host := internal.Listener.Addr().String()

	w := newTestDuckDB(t)
	cfg := &config.ObjectStore{Kind: config.ObjectStoreS3, Bucket: "acme", Prefix: "solidtrace/", Region: "us-east-1",
		Endpoint: "http://" + closedAddress(t), KeyID: "EXAMPLE", Secret: "example-secret"}
	if err := w.UseBucket(cfg, dir); err != nil {
		t.Fatalf("UseBucket: %v", err)
	}
	ctx := context.Background()
	conn, err := w.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// S3 URLs outside the bucket go to S3's public host. Nothing listens at
	// this one either, so the test stays offline.
	if _, err := conn.ExecContext(ctx, "SET GLOBAL s3_endpoint = "+sqltext.String(closedAddress(t))); err != nil {
		t.Fatal(err)
	}

	for _, url := range []string{
		"http://" + host + "/",
		"https://" + host + "/",
		"s3://other/x?s3_endpoint=" + host + "&s3_use_ssl=false&s3_url_style=path",
		"s3://acme/x?s3_endpoint=" + host + "&s3_use_ssl=false&s3_url_style=path",
		"s3://" + host + "#/x",
		"s3a://" + host + "#/x",
		"r2://" + host + "#/x",
	} {
		before := connections.Load()
		var content string
		err := conn.QueryRowContext(ctx, "SELECT content FROM read_text("+sqltext.String(url)+")").Scan(&content)
		if err == nil || connections.Load() != before {
			t.Errorf("Expected %s to reach no other host, got %q (%v) and %d connections", url, content, err, connections.Load()-before)
		}
	}

	var content string
	err = conn.QueryRowContext(ctx, "SELECT content FROM read_text('hf://datasets/acme/logs/x.txt')").Scan(&content)
	if err == nil || !strings.Contains(err.Error(), "HuggingFaceFileSystem has been disabled") {
		t.Errorf("Expected hf:// to be refused, got %q (%v)", content, err)
	}
}

// An AWS bucket's secret names its URL style. A secret without one follows
// the path style UseBucket sets for every other S3 URL, and AWS buckets are
// read at <bucket>.s3.<region>.amazonaws.com.
func TestAnAWSBucketsSecretKeepsVirtualHostedURLs(t *testing.T) {
	aws := bucketSecretSQL(&config.ObjectStore{Kind: config.ObjectStoreS3, Bucket: "acme", Region: "eu-west-1", KeyID: "EXAMPLE", Secret: "example-secret"})
	if !strings.Contains(aws, "URL_STYLE 'vhost'") {
		t.Errorf("Expected an AWS bucket's secret to name URL_STYLE 'vhost', got %s", aws)
	}
	minIO := bucketSecretSQL(&config.ObjectStore{Kind: config.ObjectStoreS3, Bucket: "acme", Region: "us-east-1",
		Endpoint: "http://127.0.0.1:9000", KeyID: "EXAMPLE", Secret: "example-secret"})
	if !strings.Contains(minIO, "URL_STYLE 'path'") || strings.Contains(minIO, "vhost") {
		t.Errorf("Expected an endpoint's secret to name URL_STYLE 'path' only, got %s", minIO)
	}
}

func TestAFilePutInTheBucketIsReadByItsURLOnEveryPooledConnection(t *testing.T) {
	cfg := minIOConfig(t)
	store := minIOStore(t, cfg)
	w := newTestDuckDB(t)
	if err := w.UseBucket(cfg, testExtensionDirectory(t)); err != nil {
		t.Fatalf("UseBucket: %v", err)
	}
	ctx := context.Background()

	local := filepath.Join(t.TempDir(), "data.parquet")
	if _, err := w.db.Exec(fmt.Sprintf("COPY (SELECT range AS n FROM range(1000)) TO %s (FORMAT parquet)", sqltext.String(local))); err != nil {
		t.Fatal(err)
	}
	key := "project=1/day=2026-10-01/hour=07/data.parquet"
	if err := store.Put(ctx, key, local); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Three connections held at once are three different ones.
	conns := make([]*sql.Conn, 3)
	for i := range conns {
		conn, err := w.db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conns[i] = conn
	}
	for i, conn := range conns {
		var count, sum int64
		err := conn.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*), sum(n) FROM read_parquet(%s)", sqltext.String(store.URL(key)))).Scan(&count, &sum)
		if err != nil || count != 1000 || sum != 499500 {
			t.Errorf("Connection %d: expected 1000 rows summing to 499500, got %d and %d (%v)", i, count, sum, err)
		}
	}

	// A local file and the bucket's in one list.
	var count int64
	err := conns[0].QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM read_parquet([%s, %s], union_by_name = true)",
		sqltext.String(local), sqltext.String(store.URL(key)))).Scan(&count)
	if err != nil || count != 2000 {
		t.Errorf("Expected 2000 rows from the local file and the bucket's, got %d (%v)", count, err)
	}
}

// The image downloads httpfs for DUCKDB_VERSION, and DuckDB loads only an
// extension built for its own version, so the two must move together.
func TestTheImagesHttpfsIsForTheDuckDBInGoMod(t *testing.T) {
	dockerfile, err := os.ReadFile(filepath.Join("..", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	arg := regexp.MustCompile(`(?m)^ARG DUCKDB_VERSION=(\S+)$`).FindSubmatch(dockerfile)
	if arg == nil {
		t.Fatal("the Dockerfile has no ARG DUCKDB_VERSION=")
	}
	var version string
	if err := newTestDuckDB(t).db.QueryRow("SELECT version()").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if string(arg[1]) != version {
		t.Errorf("Dockerfile DUCKDB_VERSION is %s, but duckdb-go in go.mod is DuckDB %s: change DUCKDB_VERSION to match", arg[1], version)
	}
}
