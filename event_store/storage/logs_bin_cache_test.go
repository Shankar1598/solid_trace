package storage

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/solidtrace/event_store/models"
)

// hourlyMessages is a table made bin by bin: Logs counted by hour, then by
// message, ordered by hour first, at most limit rows.
func hourlyMessages(limit int) LogTableScan {
	return LogTableScan{
		ProjectID: promotionProject,
		From:      october(1, 0, 30),
		To:        october(1, 9, 0),
		Select:    func(LogColumns) string { return "time, message" },
		Query: func(logs string) string {
			return fmt.Sprintf(`WITH logs AS (%s)
				SELECT time_bucket(to_microseconds(3600000000), time, TIMESTAMP '1970-01-01') AS hour, message, count(*) AS n
				FROM logs GROUP BY 1, 2 ORDER BY ALL LIMIT %d`, logs, limit)
		},
		Bins: &TableBins{Size: time.Hour, Query: "hourly messages", Limit: limit},
	}
}

// A run stopped at its limit keeps only the bins it saw whole, so a run with
// a higher limit gets every bin's rows.
func TestABinnedTableCutAtItsLimitKeepsOnlyItsWholeBins(t *testing.T) {
	store := newTestLogStore(t)
	var logs []models.Log
	for hour := range 8 {
		for i := range 3 {
			at := october(1, hour, 10+i)
			logs = append(logs, models.Log{ID: uuid.New(), Time: at, Message: fmt.Sprintf("m%d", i)})
		}
	}
	appendLogs(t, store, october(1, 0, 0), logs...)
	if err := store.ExportLogs(october(1, 12, 0)); err != nil {
		t.Fatal(err)
	}
	scan := func(limit int) [][]any {
		t.Helper()
		rows, err := store.ScanLogTable(context.Background(), hourlyMessages(limit))
		if err != nil {
			t.Fatalf("ScanLogTable: %v", err)
		}
		return rows
	}
	store.UseBinCache(0)
	whole, cut := scan(100), scan(7)
	// 00:00's Logs are before the range.
	if len(whole) != 3*7 || len(cut) != 7 {
		t.Fatalf("Expected 21 rows, and 7 when cut, got %d and %d", len(whole), len(cut))
	}

	store.UseBinCache(1 << 20)
	// From 00:30, the bin of 00:00 isn't whole, and those of 01:00 to 08:00
	// are. The cut run sees 01:00 and 02:00 whole, and 03:00 in part, so it
	// keeps two bins; run again, it takes them, and keeps 03:00 and 04:00.
	if got := scan(7); !reflect.DeepEqual(got, cut) {
		t.Errorf("Expected the cut run's rows with the cache\n got:  %v\n want: %v", got, cut)
	}
	if hits, misses := store.BinCacheStats(); hits != 0 || misses != 8 {
		t.Errorf("Expected 8 bins missed, got %d hits and %d misses", hits, misses)
	}
	if got := scan(7); !reflect.DeepEqual(got, cut) {
		t.Errorf("Expected the cut run's rows run again\n got:  %v\n want: %v", got, cut)
	}
	if hits, _ := store.BinCacheStats(); hits != 2 {
		t.Errorf("Expected the 2 whole bins taken from the cache, got %d", hits)
	}
	if got := scan(100); !reflect.DeepEqual(got, whole) {
		t.Errorf("Expected every row once not cut\n got:  %v\n want: %v", got, whole)
	}
	if got := scan(100); !reflect.DeepEqual(got, whole) {
		t.Errorf("Expected every row from the cache\n got:  %v\n want: %v", got, whole)
	}
	if hits, misses := store.BinCacheStats(); hits != 0+2+4+8 || misses != 8+6+4+0 {
		t.Errorf("Expected the last run to take all 8 whole bins from the cache, got %d hits and %d misses in all", hits, misses)
	}
}

func TestTheBinCacheDropsTheLeastRecentlyUsedBinsPastItsSize(t *testing.T) {
	rows := func(message string) [][]any { return [][]any{{october(1, 0, 0), message, int64(1)}} }
	one := binEntryBytes("a", rows("a"))
	cache := newBinCache(3 * one)
	cache.put("a", rows("a"))
	cache.put("b", rows("b"))
	cache.put("c", rows("c"))
	cache.get("a")
	cache.put("d", rows("d"))
	for key, want := range map[string]bool{"a": true, "b": false, "c": true, "d": true} {
		if _, ok := cache.get(key); ok != want {
			t.Errorf("Bin %s kept = %v, want %v", key, ok, want)
		}
	}
	if cache.bytes != 3*one {
		t.Errorf("Expected the cache to hold 3 bins' bytes, got %d of %d", cache.bytes, 3*one)
	}
	cache.put("huge", [][]any{{october(1, 0, 0), string(make([]byte, 4*one)), int64(1)}})
	if _, ok := cache.get("huge"); ok {
		t.Errorf("Expected a bin larger than the cache not kept")
	}
}
