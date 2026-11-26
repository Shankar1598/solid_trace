package main_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	eventstore "github.com/solidtrace/event_store"
	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/pkg/logger"
)

// TestIngestTakesLogsWhileClickHouseStarts runs EventStore's start against a
// ClickHouse that refuses connections at first, as one that loads its tables
// after a restart of the machine does. Ingest answers 200 from the start,
// reads answer 503 until ClickHouse comes up, and then the Logs ingested
// before are read from ClickHouse, with no restart of EventStore.
func TestIngestTakesLogsWhileClickHouseStarts(t *testing.T) {
	logger.Init()
	resetTestDB(t)
	clickhousetest.Empty(t)
	sqlitePath := "../storage/sqlite/test/solid_trace.sqlite3"
	seedProjectKey(t, sqlitePath, 123, "test_public_key")

	clickHouse := clickhousetest.Config()
	upstream := clickHouse.Address
	clickHouse.Address = freeAddress(t)
	dir := t.TempDir()
	cfg := config.Defaults()
	cfg.Port = port(freeAddress(t))
	cfg.QueryPort = port(freeAddress(t))
	cfg.PebblePath = filepath.Join(dir, "pebble")
	cfg.LogsDuckDBPath = filepath.Join(dir, "logs.duckdb")
	cfg.LogsParquetPath = filepath.Join(dir, "logs_parquet")
	cfg.SQLitePath = sqlitePath
	cfg.MessageQueuePath = "../storage/sqlite/test/message_queue.sqlite3"
	cfg.EvidencePath = filepath.Join(dir, "evidence")
	cfg.LogsDiskLimitBytes = 1 << 30
	cfg.ClickHouse = clickHouse
	ingestURL := "http://127.0.0.1:" + cfg.Port
	queryURL := "http://127.0.0.1:" + cfg.QueryPort

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	started := time.Now()
	go func() {
		defer close(stopped)
		eventstore.Run(ctx, cfg)
	}()
	t.Cleanup(func() {
		cancel()
		<-stopped
	})

	// While ClickHouse refuses connections.
	const sent = 20
	for i := range sent {
		status, body := waitForIngest(t, ingestURL, fmt.Sprintf(`{"message": "while ClickHouse starts %d"}`, i))
		if status != 200 {
			t.Fatalf("Log ingest with ClickHouse down: expected 200, got %d: %s", status, body)
		}
	}
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("The first 200s of Log ingest came %v after the start, more than 5 s", took)
	}
	// Event ingest and Transaction ingest write only into Pebble too.
	if status, body := postRaw(t, ingestURL+"/api/123/store?sentry_key=test_public_key", "application/json", string(getSampleEventJSON())); status != 200 {
		t.Errorf("Event ingest with ClickHouse down: expected 200, got %d: %s", status, body)
	}
	transaction := sentTransaction{fixture: "sentry-ruby-6.3-rails", traceID: traceIDOf(1), at: time.Now()}.envelope(t)
	if status, body := postRaw(t, ingestURL+"/api/123/envelope?sentry_key=test_public_key", "application/x-sentry-envelope", transaction); status != 200 {
		t.Errorf("Transaction ingest with ClickHouse down: expected 200, got %d: %s", status, body)
	}
	status, body := post(t, queryURL+"/api/123/logs/query", queryRequest{From: allTime.from, To: allTime.to})
	if status != 503 || !strings.Contains(body, "ClickHouse is starting") {
		t.Errorf("A Query with ClickHouse down: expected 503 that says ClickHouse is starting, got %d: %s", status, body)
	}
	status, body = get(t, queryURL+"/api/123/logs/fields")
	if status != 503 {
		t.Errorf("Fields with ClickHouse down: expected 503, got %d: %s", status, body)
	}
	// The ingest port's /health answers 200, so a load balancer keeps ingest
	// in service. The query port's answers 503.
	for url, want := range map[string]int{ingestURL + "/health": 200, queryURL + "/health": 503} {
		status, body = get(t, url)
		var health struct{ Status, Ingest, ClickHouse string }
		json.Unmarshal([]byte(body), &health)
		if status != want || health.Ingest != "up" || health.ClickHouse != "starting" {
			t.Errorf("%s with ClickHouse down: expected %d with ingest up and ClickHouse starting, got %d: %s", url, want, status, body)
		}
	}

	// ClickHouse refuses for some seconds, so the start tries again.
	time.Sleep(time.Until(started.Add(3 * time.Second)))
	if status, body = get(t, queryURL+"/api/123/logs/fields"); status != 503 {
		t.Errorf("Fields 3 s after the start with ClickHouse down: expected 503, got %d: %s", status, body)
	}

	// ClickHouse comes up.
	proxy(t, clickHouse.Address, upstream)
	var logs []listedLog
	deadline := time.Now().Add(60 * time.Second)
	for len(logs) < sent && time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		status, body = post(t, queryURL+"/api/123/logs/query", queryRequest{From: allTime.from, To: allTime.to, Limit: 100})
		if status != 200 {
			continue
		}
		var page queryPage
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("Decode query page %q: %v", body, err)
		}
		logs = page.Logs
	}
	if len(logs) != sent {
		t.Fatalf("After ClickHouse came up, expected the %d Logs ingested before, got %d (last answer %d: %s)", sent, len(logs), status, body)
	}
	for _, url := range []string{ingestURL + "/health", queryURL + "/health"} {
		if status, body = get(t, url); status != 200 || !strings.Contains(body, `"clickhouse":"ready"`) {
			t.Errorf("%s once ClickHouse is ready: expected 200 with ClickHouse ready, got %d: %s", url, status, body)
		}
	}
}

// waitForIngest posts one Log to Project 123 as soon as the ingest port
// takes connections.
func waitForIngest(t *testing.T, ingestURL, log string) (int, string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		resp, err := http.Post(ingestURL+"/api/123/logs?sentry_key=test_public_key", "application/json", strings.NewReader(log))
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, string(body)
		}
		if time.Now().After(deadline) {
			t.Fatalf("Ingest did not take connections within 10 s: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func post(t *testing.T, url string, body any) (int, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	return postRaw(t, url, "application/json", string(raw))
}

func postRaw(t *testing.T, url, contentType, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(url, contentType, strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(answer)
}

func get(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	answer, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(answer)
}

// freeAddress is a loopback address that nothing listens on now.
func freeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func port(address string) string {
	_, p, _ := net.SplitHostPort(address)
	return p
}

// proxy listens on address and sends each connection on to upstream, until
// the test ends. A connection ends when either side closes it.
func proxy(t *testing.T, address, upstream string) {
	t.Helper()
	ln, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("Listen on %s: %v", address, err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			client, err := ln.Accept()
			if err != nil {
				return
			}
			server, err := net.Dial("tcp", upstream)
			if err != nil {
				client.Close()
				continue
			}
			pipe := func(to, from net.Conn) {
				io.Copy(to, from)
				to.Close()
				from.Close()
			}
			go pipe(server, client)
			go pipe(client, server)
		}
	}()
}
