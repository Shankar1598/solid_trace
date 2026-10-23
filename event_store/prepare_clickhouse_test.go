package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// A second prepare-clickhouse finds the marker before it opens Pebble,
// logs.duckdb or the Log files, so it does nothing at all: none of their paths
// is created.
func TestPrepareClickHouseWithAMarkerOpensNoStore(t *testing.T) {
	logger.Init()
	dir := t.TempDir()
	cfg := &config.Config{
		PebblePath:      filepath.Join(dir, "pebble"),
		DuckDBPath:      filepath.Join(dir, "duckdb", "events.duckdb"),
		LogsDuckDBPath:  filepath.Join(dir, "duckdb", "logs.duckdb"),
		LogsParquetPath: filepath.Join(dir, "logs_parquet"),
		SQLitePath:      filepath.Join(dir, "solid_trace.sqlite3"),
	}
	prepared := storage.PreparedDir(cfg.LogsDuckDBPath)
	if err := os.MkdirAll(prepared, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(prepared, "marker.json")
	if err := os.WriteFile(marker, []byte(`{"release":"v0.9.1","prepared_at":"2026-10-02T00:30:00Z"}`), 0644); err != nil {
		t.Fatal(err)
	}

	if code := prepareClickHouse(cfg); code != 0 {
		t.Fatalf("prepare-clickhouse exited %d, want 0", code)
	}

	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		paths = append(paths, rel)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".", "duckdb", "duckdb/prepared", "duckdb/prepared/marker.json"}
	if !slices.Equal(paths, want) {
		t.Fatalf("paths after a second run = %v, want %v", paths, want)
	}
}
