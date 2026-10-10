package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
	"github.com/solidtrace/event_store/pkg/logger"
)

// slowSQL is a query that would run for hours.
const slowSQL = "SELECT sum(hash(i)) FROM range(1000000000000) t(i)"

// oneConnectionLogStore is a LogStore with three Logs of Project 123 in its
// hot table, the minutes before now, whose pool holds one connection: each
// read takes the connection the last one gave back.
func oneConnectionLogStore(t *testing.T, now time.Time) *LogStore {
	t.Helper()
	logger.Init()
	dir := t.TempDir()
	events, err := NewDuckDBWriter(filepath.Join(dir, "events.duckdb"), filepath.Join(dir, "parquet"), "", "")
	if err != nil {
		t.Fatalf("NewDuckDBWriter: %v", err)
	}
	t.Cleanup(events.Close)
	store, err := NewLogStore(events, filepath.Join(dir, "logs.duckdb"), filepath.Join(dir, "logs_parquet"))
	if err != nil {
		t.Fatalf("NewLogStore: %v", err)
	}
	logs := make([]models.Log, 3)
	for i := range logs {
		logs[i] = models.Log{ID: uuid.New(), Time: now.Add(-time.Duration(i+1) * time.Minute), Message: fmt.Sprintf("log %d", i)}
	}
	if err := store.AppendLogBatches(123, []LogBatch{{ID: uuid.New(), Logs: logs}}, now); err != nil {
		t.Fatalf("AppendLogBatches: %v", err)
	}
	store.db.SetMaxOpenConns(1)
	store.db.SetMaxIdleConns(1)
	return store
}

func TestAScanPastItsContextIsStoppedPartWayAndItsConnectionStillReads(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	store := oneConnectionLogStore(t, now)
	from, to := now.Add(-time.Hour), now.Add(time.Hour)

	count := LogTableScan{
		ProjectID: 123, From: from, To: to,
		Select: func(LogColumns) string { return "id" },
		Query:  func(logs string) string { return "SELECT count(*) FROM (" + logs + ")" },
	}
	slowTable := count
	slowTable.Query = func(string) string { return slowSQL }
	slowRows := LogScan{
		ProjectID: 123, From: from, To: to, Limit: 10,
		Where: func(LogColumns) string { return "(" + slowSQL + ") IS NOT NULL" },
	}
	scans := []struct {
		name string
		scan func(context.Context) error
	}{
		{"a table", func(ctx context.Context) error { _, err := store.ScanLogTable(ctx, slowTable); return err }},
		{"Log rows", func(ctx context.Context) error { _, err := store.ScanLogs(ctx, slowRows); return err }},
	}
	for _, s := range scans {
		// The read begins well within 250 ms, so the context ends while
		// DuckDB runs the query.
		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		began := time.Now()
		err := s.scan(ctx)
		took := time.Since(began)
		cancel()
		if err == nil {
			t.Errorf("Expected a scan of %s past its context to fail", s.name)
		}
		if took > 5*time.Second {
			t.Errorf("Expected the scan of %s stopped soon after 250 ms, took %v", s.name, took)
		}

		// The connection it gave back reads as usual.
		table, err := store.ScanLogTable(context.Background(), count)
		if err != nil || len(table) != 1 || fmt.Sprint(table[0]) != "[3]" {
			t.Errorf("After stopping %s, expected a table counting 3 Logs, got %v (%v)", s.name, table, err)
		}
		page, err := store.ScanLogs(context.Background(), LogScan{ProjectID: 123, From: from, To: to, Limit: 10})
		if err != nil || len(page.Logs) != 3 {
			t.Errorf("After stopping %s, expected 3 Logs, got %d (%v)", s.name, len(page.Logs), err)
		}
	}
}
