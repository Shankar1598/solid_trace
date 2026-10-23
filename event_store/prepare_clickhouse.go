package main

import (
	"context"
	"os"

	"github.com/solidtrace/event_store/config"
	"github.com/solidtrace/event_store/logprocessing"
	"github.com/solidtrace/event_store/logtransform"
	"github.com/solidtrace/event_store/pkg/clock"
	"github.com/solidtrace/event_store/pkg/logger"
	"github.com/solidtrace/event_store/storage"
)

// prepareClickHouseCommand is the name of the offline command that readies
// an install of the last DuckDB release for the ClickHouse release:
//
//	event_store prepare-clickhouse
//
// It runs with the stack stopped, against the stores the config names.
const prepareClickHouseCommand = "prepare-clickhouse"

// release is the release this binary was built as, which the Docker image
// sets, or dev.
func release() string {
	if v := os.Getenv("SOLIDTRACE_VERSION"); v != "" {
		return v
	}
	return "dev"
}

// prepareClickHouse runs prepare-clickhouse (logprocessing.PrepareClickHouse)
// and returns the process's exit code. It opens logs.duckdb on a DuckDB of
// its own, so that events.duckdb is never opened, and Pebble only for its
// Log batches. Once the install has a marker, it opens nothing.
func prepareClickHouse(cfg *config.Config) int {
	fail := func(message string, err error) int {
		logger.L.Error(message, "error", err)
		return 1
	}
	const stopped = "; prepare-clickhouse runs with the stack stopped"

	dir := storage.PreparedDir(cfg.LogsDuckDBPath)
	marker, err := storage.ReadClickHouseMarker(dir)
	if err != nil {
		return fail("Failed to read the marker of an earlier prepare-clickhouse", err)
	}
	if marker != nil {
		logger.L.Info("The install was prepared for the ClickHouse release already; nothing to do", "dir", dir, "release", marker.Release, "prepared_at", marker.PreparedAt)
		return 0
	}

	batches, err := storage.NewPebbleWriter(cfg)
	if err != nil {
		return fail("Failed to open Pebble"+stopped, err)
	}
	defer batches.Close()

	// DuckDB spills next to logs.duckdb unless the config says where.
	tempDir := cfg.DuckDBTempDirectory
	if tempDir == "" {
		tempDir = cfg.LogsDuckDBPath + ".tmp"
	}
	duck, err := storage.NewLogsDuckDB(tempDir, cfg.DuckDBMemoryLimit)
	if err != nil {
		return fail("Failed to open DuckDB", err)
	}
	defer duck.Close()
	logStore, err := storage.NewLogStore(duck, cfg.LogsDuckDBPath, cfg.LogsParquetPath)
	if err != nil {
		return fail("Failed to open logs DuckDB"+stopped, err)
	}
	if cfg.ObjectStore != nil {
		err = logStore.UseObjectStore(storage.NewObjectStore(cfg.ObjectStore), cfg.ObjectStore.LocalCacheHours)
	} else {
		err = logStore.UseLocalDiskOnly()
	}
	if err != nil {
		return fail("Failed to open the Log files' store", err)
	}

	consoleSettings, err := storage.NewConsoleSettings(cfg.SQLitePath)
	if err != nil {
		return fail("Failed to open Console settings", err)
	}
	defer consoleSettings.Close()
	logStore.UseLogFileOrders(consoleSettings)

	processor := logprocessing.New(batches, logStore, logtransform.New(consoleSettings, clock.System{}), clock.System{})
	if _, err := logprocessing.PrepareClickHouse(context.Background(), processor, logStore, dir, release()); err != nil {
		return fail("prepare-clickhouse failed, so the install isn't ready for the ClickHouse release: fix the cause and run it again", err)
	}
	return 0
}
