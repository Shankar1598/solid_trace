package main_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/clickhousetest"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// startOn starts EventStore's stores as main does, on a storage directory
// that may hold the DuckDB release's logs.duckdb and prepare-clickhouse's
// prepared/, and returns why the start stopped, if it did.
func startOn(t *testing.T, dir string) error {
	t.Helper()
	pebble, err := storage.NewPebbleWriter(&config.Config{PebblePath: filepath.Join(dir, "pebble")})
	if err != nil {
		t.Fatalf("Open Pebble: %v", err)
	}
	defer pebble.Close()
	clickHouse, err := storage.OpenClickHouse(clickhousetest.Config(), nil)
	if err != nil {
		t.Fatalf("Open ClickHouse: %v", err)
	}
	defer clickHouse.Close()
	return storage.NewClickHouseLogs(clickHouse, pebble).UseOldLogFiles(storage.OldLogFiles{
		LogsDuckDB:  filepath.Join(dir, "logs.duckdb"),
		PreparedDir: filepath.Join(dir, "prepared"),
		LocalDir:    filepath.Join(dir, "logs_parquet"),
	})
}

func TestUnpreparedDuckDBDataStopsTheStart(t *testing.T) {
	logger.Init()
	clickhousetest.Empty(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "logs.duckdb"), []byte("the DuckDB release's Logs"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := startOn(t, dir)
	if err == nil || !strings.Contains(err.Error(), "prepare-clickhouse") {
		t.Fatalf("Expected the start to stop and name prepare-clickhouse, got %v", err)
	}
	// It stops each time, until the Logs are prepared.
	if err := startOn(t, dir); err == nil {
		t.Fatal("Expected the second start to stop too")
	}
}

func TestAStoreWithoutDuckDBDataStartsAndStartsAgain(t *testing.T) {
	logger.Init()
	clickhousetest.Empty(t)
	dir := t.TempDir()

	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected a new store to start, got %v", err)
	}
	// This release still makes an empty logs.duckdb at start (SQL mode's).
	if err := os.WriteFile(filepath.Join(dir, "logs.duckdb"), []byte("made by this release"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := startOn(t, dir); err != nil {
		t.Fatalf("Expected it to start again, got %v", err)
	}
}
