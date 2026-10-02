// Command logsbench runs the logs storage-layout benchmark (logs ticket 09).
//
//	logsbench run -layout json -work /tmp/bench -out results/json.json
//	logsbench report results/json.json results/variant.json ... > report.md
//
// Run one layout per process, under the dev-box limits in the ticket:
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
		fmt.Fprintln(os.Stderr, "usage: logsbench run|report ...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "run":
		run(os.Args[2:])
	case "report":
		report(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(2)
	}
}

func run(args []string) {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	layout := fs.String("layout", "json", "json, variant, variant_noshred or map")
	work := fs.String("work", filepath.Join(os.TempDir(), "logsbench"), "working directory; the layout's files are deleted afterwards")
	out := fs.String("out", "", "report JSON path (default <layout>.json)")
	seed := fs.Uint64("seed", 1, "generator seed")
	memory := fs.String("memory", "2GB", "DuckDB memory_limit")
	threads := fs.Int("threads", 2, "DuckDB threads")
	unthrottled := fs.Duration("unthrottled", 2*time.Minute, "unthrottled phase")
	target := fs.Duration("target", 2*time.Minute, "target-rate phase; queries run during its second half")
	burst := fs.Duration("burst", time.Minute, "burst phase")
	burstFactor := fs.Int("burst-factor", 5, "burst rate as a multiple of the target")
	flush := fs.Int("flush", 5000, "Logs per flush when unthrottled")
	rowGroups := fs.String("rowgroups", "122880,30720", "Parquet row group sizes")
	verify := fs.Int64("verify", 100_000, "Logs covered by the cross-layout result check")
	queryTimeout := fs.Duration("query-timeout", 2*time.Minute, "per query run")
	keep := fs.Bool("keep", false, "keep the layout's files")
	fs.Parse(args)

	l, err := logslayouts.LayoutByName(*layout)
	if err != nil {
		log.Fatal(err)
	}
	var sizes []int
	for _, s := range strings.Split(*rowGroups, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			log.Fatalf("row group size %q: %v", s, err)
		}
		sizes = append(sizes, n)
	}
	if *out == "" {
		*out = l.Name() + ".json"
	}
	cfg := logslayouts.Config{
		Layout:        l,
		Dir:           filepath.Join(*work, l.Name()),
		Seed:          *seed,
		Settings:      logslayouts.Settings{MemoryLimit: *memory, Threads: *threads},
		Unthrottled:   *unthrottled,
		Target:        *target,
		Burst:         *burst,
		BurstFactor:   *burstFactor,
		FlushLogs:     *flush,
		RowGroupSizes: sizes,
		VerifyMaxID:   *verify,
		QueryTimeout:  *queryTimeout,
		Keep:          *keep,
		Logf:          log.Printf,
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
