// Command logsbench runs the logs storage benchmark (logs tickets 09 and 14).
//
//	logsbench run -layout json -work /tmp/bench -out results/json.json
//	logsbench report results/json.json results/map.json ... > report.md
//	logsbench cycles -hour 200000 -cycles 10 -out results/cycles.json > cycles.md
//
// The defaults take about two minutes per layout; slow query sets are cut
// short by -set-budget. Run one layout per process, under the dev-box limits
// in the ticket:
//
//	systemd-run --user --scope -p MemoryMax=3G mise exec -- go run ./bench/logslayouts/cmd/logsbench run ...
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/solidtrace/event_store/bench/logslayouts"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: logsbench run|report|cycles ...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		run(os.Args[2:])
	case "report":
		report(os.Args[2:])
	case "cycles":
		cycles(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	layout := fs.String("layout", "json", "json or map")
	work := fs.String("work", filepath.Join(os.TempDir(), "logsbench"), "working directory; the layout's files are deleted afterwards")
	out := fs.String("out", "", "report JSON path (default <layout>.json)")
	seed := fs.Uint64("seed", 1, "generator seed")
	memory := fs.String("memory", "2GB", "DuckDB memory_limit")
	threads := fs.Int("threads", 2, "DuckDB threads")
	unthrottled := fs.Duration("unthrottled", 30*time.Second, "unthrottled phase")
	target := fs.Duration("target", 30*time.Second, "target-rate phase; queries run during its second half")
	burst := fs.Duration("burst", 15*time.Second, "burst phase")
	burstFactor := fs.Int("burst-factor", 5, "burst rate as a multiple of the target")
	flush := fs.Int("flush", 5000, "Logs per flush when unthrottled")
	rowGroups := fs.String("rowgroups", "", "row group sizes for ticket 09's all-Project hourly Parquet, comma-separated (none by default)")
	promote := fs.String("promote", "0,10,20,40", "N to try for per-Project hourly Parquet with the top N keys promoted, comma-separated")
	verify := fs.Int64("verify", 50_000, "Logs covered by the cross-layout result check")
	queryTimeout := fs.Duration("query-timeout", 20*time.Second, "per query run")
	setBudget := fs.Duration("set-budget", 30*time.Second, "per query set; the rest of a slow set is skipped")
	checkpointTimeout := fs.Duration("checkpoint-timeout", time.Minute, "final checkpoint; past it the layout stops")
	keep := fs.Bool("keep", false, "keep the layout's files")
	fs.Parse(args)

	l, err := logslayouts.LayoutByName(*layout)
	if err != nil {
		log.Fatal(err)
	}
	sizes, promoted := ints(*rowGroups), ints(*promote)
	if *out == "" {
		*out = l.Name() + ".json"
	}
	cfg := logslayouts.Config{
		Layout:            l,
		Dir:               filepath.Join(*work, l.Name()),
		Seed:              *seed,
		Settings:          logslayouts.Settings{MemoryLimit: *memory, Threads: *threads},
		Unthrottled:       *unthrottled,
		Target:            *target,
		Burst:             *burst,
		BurstFactor:       *burstFactor,
		FlushLogs:         *flush,
		RowGroupSizes:     sizes,
		Promote:           promoted,
		VerifyMaxID:       *verify,
		QueryTimeout:      *queryTimeout,
		SetBudget:         *setBudget,
		CheckpointTimeout: *checkpointTimeout,
		Keep:              *keep,
		Logf:              log.Printf,
	}
	r, runErr := logslayouts.Run(context.Background(), cfg)
	if r != nil {
		b, _ := json.MarshalIndent(r, "", "  ")
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s", *out)
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}

func ints(list string) []int {
	var out []int
	for _, s := range strings.Split(list, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		if err != nil {
			log.Fatalf("%q: %v", s, err)
		}
		out = append(out, n)
	}
	return out
}

// cycles runs the hot-table export cycles (logs ticket 14) and prints a
// Markdown table; the JSON report goes to -out.
func cycles(args []string) {
	fs := flag.NewFlagSet("cycles", flag.ExitOnError)
	work := fs.String("work", filepath.Join(os.TempDir(), "logsbench"), "working directory; deleted afterwards")
	out := fs.String("out", "cycles.json", "report JSON path")
	seed := fs.Uint64("seed", 1, "generator seed")
	memory := fs.String("memory", "2GB", "DuckDB memory_limit")
	threads := fs.Int("threads", 2, "DuckDB threads")
	hour := fs.Int64("hour", 200_000, "Logs per logical hour")
	n := fs.Int("cycles", 10, "hours to export")
	factor := fs.Int("factor", 0, "ingest rate as a multiple of the target; 0 is unthrottled")
	flush := fs.Int("flush", 5000, "Logs per flush when unthrottled")
	promote := fs.Int("promote", 20, "keys promoted per file")
	checkpoint := fs.Bool("checkpoint", true, "CHECKPOINT after each delete")
	fs.Parse(args)

	r, runErr := logslayouts.RunCycles(context.Background(), logslayouts.CyclesConfig{
		Dir:        filepath.Join(*work, "cycles"),
		Seed:       *seed,
		Settings:   logslayouts.Settings{MemoryLimit: *memory, Threads: *threads},
		HourLogs:   *hour,
		Cycles:     *n,
		Factor:     *factor,
		FlushLogs:  *flush,
		Promote:    *promote,
		Checkpoint: *checkpoint,
		Logf:       log.Printf,
	})
	if r != nil {
		b, _ := json.MarshalIndent(r, "", "  ")
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			log.Fatal(err)
		}
		fmt.Print(logslayouts.CyclesMarkdown(r))
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}

func report(paths []string) {
	var reports []*logslayouts.Report
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			log.Fatal(err)
		}
		var r logslayouts.Report
		if err := json.Unmarshal(b, &r); err != nil {
			log.Fatalf("%s: %v", p, err)
		}
		reports = append(reports, &r)
	}
	fmt.Print(logslayouts.Markdown(reports))
}
