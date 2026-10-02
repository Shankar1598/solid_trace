package logslayouts

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func smokeConfig(t *testing.T, l Layout) Config {
	return Config{
		Layout:            l,
		Dir:               filepath.Join(t.TempDir(), l.Name()),
		Seed:              1,
		Settings:          Settings{MemoryLimit: "1GB", Threads: 2},
		Unthrottled:       2 * time.Second,
		Target:            2 * time.Second,
		Burst:             time.Second,
		BurstFactor:       5,
		FlushLogs:         2000,
		RowGroupSizes:     []int{4096},
		VerifyMaxID:       1500,
		QueryTimeout:      time.Minute,
		SetBudget:         time.Minute,
		CheckpointTimeout: time.Minute,
		Logf:              t.Logf,
	}
}

func TestRunMeasuresEveryStageAndCleansUp(t *testing.T) {
	cfg := smokeConfig(t, mapLayout{})
	r, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Engine == "" || r.GeneratorLogsPerSec <= 0 {
		t.Errorf("engine %q, generator rate %v", r.Engine, r.GeneratorLogsPerSec)
	}
	var names []string
	for _, p := range r.Phases {
		names = append(names, p.Name)
		if p.Logs <= 0 || p.PeakRSSMB <= 0 || p.Flushes <= 0 {
			t.Errorf("phase %s: %+v", p.Name, p)
		}
	}
	if want := []string{"unthrottled", "target", "target_with_queries", "burst_5x"}; !reflect.DeepEqual(names, want) {
		t.Errorf("phases %v, want %v", names, want)
	}
	if r.Phases[2].QueriesRun == 0 {
		t.Error("no queries ran during target_with_queries")
	}
	if r.Rows <= cfg.VerifyMaxID || r.DBFileBytes <= 0 {
		t.Errorf("rows %d, db bytes %d", r.Rows, r.DBFileBytes)
	}
	if len(r.HotQueries) != len(Queries()) {
		t.Errorf("%d hot query timings, want %d", len(r.HotQueries), len(Queries()))
	}
	for _, q := range r.HotQueries {
		if q.Error != "" || q.ColdMs <= 0 || q.WarmMs <= 0 {
			t.Errorf("hot %+v", q)
		}
	}
	if len(r.Parquet) != 1 || r.Parquet[0].Files == 0 || r.Parquet[0].Bytes == 0 {
		t.Fatalf("parquet %+v", r.Parquet)
	}
	for _, mode := range [][]QueryTiming{r.Parquet[0].AsyncOn, r.Parquet[0].AsyncOff} {
		if len(mode) != len(Queries()) {
			t.Errorf("%d parquet timings, want %d", len(mode), len(Queries()))
		}
	}
	if len(r.Verify) != len(Queries()) || !reflect.DeepEqual(r.Verify, r.Parquet[0].Verify) {
		t.Errorf("hot and Parquet verification differ:\n hot %v\n pq  %v", r.Verify, r.Parquet[0].Verify)
	}
	if _, err := os.Stat(cfg.Dir); !os.IsNotExist(err) {
		t.Errorf("layout dir %s still exists (err %v)", cfg.Dir, err)
	}
}

func TestRunReportsVariantShredding(t *testing.T) {
	cfg := smokeConfig(t, variantLayout{})
	cfg.Settings.MemoryLimit = "2GB"
	cfg.Unthrottled, cfg.Target, cfg.Burst = time.Second, time.Second, time.Second
	r, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Shredding) == 0 {
		t.Error("no row-group shredding report for the VARIANT layout")
	}
	if md := Markdown([]*Report{r}); !strings.Contains(md, "VARIANT shredding") || !strings.Contains(md, "fetch_by_id") {
		t.Errorf("report is missing sections:\n%s", md)
	}
}

func TestRunSkipsQueriesPastTheSetBudget(t *testing.T) {
	cfg := smokeConfig(t, jsonLayout{})
	cfg.Unthrottled, cfg.Target, cfg.Burst = time.Second, time.Second, time.Second
	cfg.SetBudget = time.Nanosecond
	r, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.HotQueries[0].Skipped {
		t.Errorf("first query skipped: %+v", r.HotQueries[0])
	}
	if !r.HotQueries[len(r.HotQueries)-1].Skipped {
		t.Errorf("last query ran despite the spent budget: %+v", r.HotQueries[len(r.HotQueries)-1])
	}
}
