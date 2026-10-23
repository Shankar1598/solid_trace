package logprocessing

import (
	"context"
	"fmt"
	"time"

	"github.com/solidtrace/event_store/storage"
)

// PrepareStore is the DuckDB LogStore, logs.duckdb, as prepare-clickhouse
// readies it. *storage.LogStore is the production adapter.
type PrepareStore interface {
	// PrepareForClickHouse files every Log stored, finishes deletions,
	// uploads every file and writes the prepared files and the marker to
	// the prepared folder dir.
	PrepareForClickHouse(ctx context.Context, dir, release string, now time.Time) error
}

// PrepareClickHouse is prepare-clickhouse, the last DuckDB release's offline
// command that readies an install for the ClickHouse release. It runs with
// the stack stopped. Unless the prepared folder dir has a marker already, it
// processes every Log batch waiting in Pebble with processor, whose worker
// isn't running, then has store file, upload and write out everything else.
// alreadyPrepared reports a marker found, when it does nothing.
func PrepareClickHouse(ctx context.Context, processor *Processor, store PrepareStore, dir, release string) (alreadyPrepared bool, err error) {
	marker, err := storage.ReadClickHouseMarker(dir)
	if err != nil {
		return false, err
	}
	if marker != nil {
		return true, nil
	}
	if err := processor.Prepare(); err != nil {
		return false, err
	}
	if err := processor.ProcessAll(); err != nil {
		return false, fmt.Errorf("process the Log batches waiting: %w", err)
	}
	return false, store.PrepareForClickHouse(ctx, dir, release, processor.clock.Now())
}
