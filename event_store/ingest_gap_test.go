package main_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	eventstore "github.com/solidtrace/event_store"
	"github.com/solidtrace/event_store/handler"
	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// TestAStartAfterAnIngestGapHoldsARunThatAsks starts EventStore on a Pebble
// whose last ingest heartbeat is 2 min old, as after ingest was down for
// 2 min. Through the query port, a Query that asks for hold_if_incomplete
// answers {"incomplete"} with ingest_gap until about 2 min after the start,
// and a Query that does not ask answers its table.
func TestAStartAfterAnIngestGapHoldsARunThatAsks(t *testing.T) {
	logger.Init()
	resetTestDB(t)
	clickhousetest.Empty(t)
	cfg := startConfig(t, "../storage/sqlite/test/solid_trace.sqlite3", clickhousetest.Config())
	pebble, err := storage.NewPebbleWriter(cfg)
	if err != nil {
		t.Fatalf("Open Pebble: %v", err)
	}
	const gap = 2 * time.Minute
	if err := pebble.WriteIngestBeat(time.Now().Add(-gap)); err != nil {
		t.Fatalf("Write a heartbeat: %v", err)
	}
	pebble.StopWrites()
	pebble.Close()

	started := time.Now()
	runEventStore(t, cfg)
	queryURL := "http://127.0.0.1:" + cfg.QueryPort + "/api/123/logs/query"
	type thresholdRun struct {
		Query            string    `json:"query"`
		From             time.Time `json:"from"`
		To               time.Time `json:"to"`
		HoldIfIncomplete bool      `json:"hold_if_incomplete,omitempty"`
	}
	type answer struct {
		Incomplete *struct {
			Reason string     `json:"reason"`
			Until  *time.Time `json:"until"`
		} `json:"incomplete"`
		Table json.RawMessage `json:"table"`
	}
	// run answers 0 while the query port takes no connections.
	run := func(hold bool) (int, answer, string) {
		now := time.Now()
		raw, _ := json.Marshal(thresholdRun{
			Query: "summarize count()", From: now.Add(-10 * time.Minute), To: now.Add(-time.Minute), HoldIfIncomplete: hold,
		})
		resp, err := http.Post(queryURL, "application/json", bytes.NewReader(raw))
		if err != nil {
			return 0, answer{}, err.Error()
		}
		defer resp.Body.Close()
		answered, _ := io.ReadAll(resp.Body)
		status, body := resp.StatusCode, string(answered)
		var got answer
		if status == 200 {
			if err := json.Unmarshal([]byte(body), &got); err != nil {
				t.Fatalf("Decode %s: %v", body, err)
			}
		}
		return status, got, body
	}

	// The query port answers 503 until the ClickHouse start has passed.
	deadline := time.Now().Add(60 * time.Second)
	status, got, body := run(false)
	for status != 200 && time.Now().Before(deadline) {
		time.Sleep(200 * time.Millisecond)
		status, got, body = run(false)
	}
	if status != 200 || got.Incomplete != nil || got.Table == nil {
		t.Fatalf("A Query that does not ask: expected 200 with the table, got %d: %s", status, body)
	}

	status, got, body = run(true)
	if status != 200 || got.Incomplete == nil || got.Table != nil {
		t.Fatalf("A Query that asks: expected 200 with incomplete and no table, got %d: %s", status, body)
	}
	if got.Incomplete.Reason != handler.IncompleteIngestGap || got.Incomplete.Until == nil {
		t.Fatalf("A Query that asks: expected ingest_gap with until, got %s", body)
	}
	if until := *got.Incomplete.Until; until.Before(started.Add(gap)) || until.After(time.Now().Add(gap+5*time.Second)) {
		t.Fatalf("Expected the gap to hold until about %v after the start at %v, got until %v", gap, started, until)
	}
}

// TestTheLastIngestHeartbeatIsWrittenAsIngestCloses stops EventStore while
// its start waits for a ClickHouse that takes connections and never
// answers, so the rest of the shutdown takes seconds. The last ingest
// heartbeat is about the time that ingest closed, not the end of the
// shutdown: so the next start measures its gap from the close. The whole
// shutdown fits Docker's 10 s stop grace period.
func TestTheLastIngestHeartbeatIsWrittenAsIngestCloses(t *testing.T) {
	logger.Init()
	resetTestDB(t)
	sqlitePath := "../storage/sqlite/test/solid_trace.sqlite3"
	seedProjectKey(t, sqlitePath, 123, "test_public_key")
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	go func() {
		var conns []net.Conn
		defer func() {
			for _, c := range conns {
				c.Close()
			}
		}()
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	clickHouse := clickhousetest.Config()
	clickHouse.Address = silent.Addr().String()
	cfg := startConfig(t, sqlitePath, clickHouse)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { defer close(stopped); eventstore.Run(ctx, cfg) }()
	if status, body := waitForIngest(t, "http://127.0.0.1:"+cfg.Port, `{"message": "before the shutdown"}`); status != 200 {
		t.Fatalf("Log ingest: expected 200, got %d: %s", status, body)
	}
	time.Sleep(2 * time.Second)
	cancelled := time.Now()
	cancel()
	select {
	case <-stopped:
	case <-time.After(60 * time.Second):
		t.Fatalf("EventStore did not stop within 60 s")
	}
	took := time.Since(cancelled)

	pebble, err := storage.NewPebbleWriter(cfg)
	if err != nil {
		t.Fatalf("Open Pebble: %v", err)
	}
	defer pebble.Close()
	beat, ok, err := pebble.LastIngestBeat()
	if err != nil || !ok {
		t.Fatalf("Expected a last ingest heartbeat, got %v (%v)", ok, err)
	}
	if after := beat.Sub(cancelled); after < -time.Millisecond || after > time.Second {
		t.Errorf("Expected the last heartbeat within 1 s of the stop, as ingest closed, got %v after it (the shutdown took %v)", after, took)
	}
	if took > 10*time.Second {
		t.Errorf("The shutdown took %v, more than Docker's 10 s grace period", took)
	}
}
