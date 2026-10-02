package logslayouts

import (
	"fmt"
	"reflect"
	"strings"
)

// FullDayLogs is a day at the target rate, for the projection.
const FullDayLogs = LogsPerSecond * 86400

// Markdown renders reports (one per layout, the JSON baseline first) as the
// tables the findings doc quotes.
func Markdown(reports []*Report) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }
	if len(reports) == 0 {
		return ""
	}
	r0 := reports[0]
	w("Engine `%s` on %s. DuckDB memory_limit %s, threads %d.\n\n", r0.Engine, r0.Machine, r0.Settings.MemoryLimit, r0.Settings.Threads)
	w("Engine settings: %v\n\n", r0.EngineSettings)

	w("### Writes\n\n| Layout | Generator alone (Logs/s) | Phase | Target MB/s | Logs/s | MB/s | Flush p50 / p99 / max ms | Max backlog (Logs) | Kept up | Peak RSS MB | CPU %% | DuckDB mem MB | Queries during (p50 / max ms, errors) |\n|---|---|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, r := range reports {
		for i, p := range r.Phases {
			gen := ""
			if i == 0 {
				gen = fmt.Sprintf("%.0f", r.GeneratorLogsPerSec)
			}
			target, kept, queries := "max", "", ""
			if p.TargetBytesPerS > 0 {
				target = fmt.Sprintf("%.1f", p.TargetBytesPerS/1e6)
				kept = map[bool]string{true: "yes", false: "**no**"}[p.KeptUp]
			}
			if p.QueriesRun > 0 {
				queries = fmt.Sprintf("%d (%.0f / %.0f, %d)", p.QueriesRun, p.QueryP50Ms, p.QueryMaxMs, p.QueryErrors)
			}
			w("| %s | %s | %s | %s | %.0f | %.2f | %.0f / %.0f / %.0f | %d | %s | %.0f | %.0f | %.0f | %s |\n",
				r.Layout, gen, p.Name, target, p.LogsPerSec, p.MBPerSec, p.FlushP50Ms, p.FlushP99Ms, p.FlushMaxMs,
				p.MaxBacklogLogs, kept, p.PeakRSSMB, p.CPUPercent, p.DuckDBMemoryMB, queries)
		}
	}

	w("\n### Size on disk\n\n| Layout | Rows | Generated MB | Final checkpoint ms | .duckdb MB | B/Log | Parquet MB (row group size) |\n|---|---|---|---|---|---|---|\n")
	for _, r := range reports {
		var pq []string
		for _, p := range r.Parquet {
			pq = append(pq, fmt.Sprintf("%.1f (%d)", float64(p.Bytes)/1e6, p.RowGroupSize))
		}
		var gen int64
		for _, p := range r.Phases {
			gen += p.Bytes
		}
		bpl := 0.0
		if r.Rows > 0 {
			bpl = float64(r.DBFileBytes) / float64(r.Rows)
		}
		w("| %s | %d | %.0f | %.0f | %.1f | %.0f | %s |\n", r.Layout, r.Rows, float64(gen)/1e6, r.CheckpointMs,
			float64(r.DBFileBytes)/1e6, bpl, strings.Join(pq, ", "))
	}

	var shredded bool
	for _, r := range reports {
		shredded = shredded || len(r.Shredding) > 0
	}
	if shredded {
		w("\n### VARIANT shredding in the hot table (after the final checkpoint)\n\n| Layout | Row groups | Shredded | Rows in unshredded row groups | Unshredded row-group sizes |\n|---|---|---|---|---|\n")
		for _, r := range reports {
			if len(r.Shredding) == 0 {
				continue
			}
			var n int
			var unRows int64
			var sizes []string
			for _, s := range r.Shredding {
				if s.Shredded {
					n++
				} else {
					unRows += s.Rows
					sizes = append(sizes, fmt.Sprint(s.Rows))
				}
			}
			if len(sizes) > 8 {
				sizes = append(sizes[:8], "…")
			}
			w("| %s | %d | %d | %d | %s |\n", r.Layout, len(r.Shredding), n, unRows, strings.Join(sizes, ", "))
		}
	}

	queryTable := func(title string, get func(r *Report) []QueryTiming, coldOnly bool) {
		w("\n### %s\n\n| Query |", title)
		for _, r := range reports {
			if coldOnly {
				w(" %s cold |", r.Layout)
			} else {
				w(" %s cold / warm |", r.Layout)
			}
		}
		w("\n|---|%s\n", strings.Repeat("---|", len(reports)))
		for i, q := range Queries() {
			w("| %s |", q.Name)
			for _, r := range reports {
				ts := get(r)
				switch {
				case i >= len(ts):
					w(" – |")
				case ts[i].Error != "":
					w(" error |")
				case coldOnly:
					w(" %s |", msStr(ts[i].ColdMs))
				default:
					w(" %s / %s |", msStr(ts[i].ColdMs), msStr(ts[i].WarmMs))
				}
			}
			w("\n")
		}
	}
	queryTable("Hot table queries (ms)", func(r *Report) []QueryTiming { return r.HotQueries }, false)
	if r0.HotPrefetchSetting != "" {
		queryTable(fmt.Sprintf("Hot table, cold, storage_block_prefetch = %s (ms)", r0.HotPrefetchSetting),
			func(r *Report) []QueryTiming { return r.HotQueriesPrefetch }, true)
	}

	w("\n### Parquet export\n\n| Layout | Row group size | Export ms | Files | Row groups | Leaf columns | MB | Peak RSS MB, async on / off |\n|---|---|---|---|---|---|---|---|\n")
	for _, r := range reports {
		for _, p := range r.Parquet {
			w("| %s | %d | %.0f | %d | %d | %d | %.1f | %.0f / %.0f |\n", r.Layout, p.RowGroupSize, p.ExportMs, p.Files,
				p.RowGroups, p.Columns, float64(p.Bytes)/1e6, p.AsyncOnRSSMB, p.AsyncOffRSSMB)
		}
	}
	for i, p := range r0.Parquet {
		queryTable(fmt.Sprintf("Parquet, row group %d, async on (ms)", p.RowGroupSize), func(r *Report) []QueryTiming {
			if i < len(r.Parquet) {
				return r.Parquet[i].AsyncOn
			}
			return nil
		}, false)
		queryTable(fmt.Sprintf("Parquet, row group %d, async off: read_ahead_depth = 0 (ms)", p.RowGroupSize), func(r *Report) []QueryTiming {
			if i < len(r.Parquet) {
				return r.Parquet[i].AsyncOff
			}
			return nil
		}, false)
	}

	w("\n### Results match the JSON baseline\n\nOver Logs 0..%d of Project 1. ✓ = identical rows (and count/id checksum); Parquet is compared with the same layout's hot table.\n\n| Query |", r0.VerifyMaxID)
	for _, r := range reports {
		w(" %s hot | %s Parquet |", r.Layout, r.Layout)
	}
	w("\n|---|%s\n", strings.Repeat("---|---|", len(reports)))
	for _, q := range Queries() {
		w("| %s |", q.Name)
		base, haveBase := r0.Verify[q.Name]
		for _, r := range reports {
			got, ok := r.Verify[q.Name]
			switch {
			case !ok || !haveBase || r.VerifyMaxID != r0.VerifyMaxID:
				w(" – |")
			case reflect.DeepEqual(got, base):
				w(" ✓ |")
			default:
				w(" **differs** |")
			}
			pqOK := ok
			for _, p := range r.Parquet {
				pq, has := p.Verify[q.Name]
				pqOK = pqOK && has && reflect.DeepEqual(pq, got)
			}
			w(" %s |", map[bool]string{true: "✓", false: "**differs or missing**"}[pqOK && len(r.Parquet) > 0])
		}
		w("\n")
	}

	w("\n### Projection to a full day (%d Logs), not measured\n\nWarm latency of the queries that scan the whole time range, scaled linearly by rows. This is a projection: it ignores memory pressure, cache misses and the larger row counts per file a day brings.\n\n| Query |", FullDayLogs)
	for _, r := range reports {
		w(" %s hot s | %s Parquet s |", r.Layout, r.Layout)
	}
	w("\n|---|%s\n", strings.Repeat("---|---|", len(reports)))
	for i, q := range Queries() {
		if !q.Scan {
			continue
		}
		w("| %s |", q.Name)
		for _, r := range reports {
			scale := float64(FullDayLogs) / float64(max(r.Rows, 1))
			hot, pq := "–", "–"
			if i < len(r.HotQueries) && r.HotQueries[i].Error == "" {
				hot = fmt.Sprintf("%.1f", r.HotQueries[i].WarmMs*scale/1000)
			}
			if len(r.Parquet) > 0 && i < len(r.Parquet[0].AsyncOn) && r.Parquet[0].AsyncOn[i].Error == "" {
				pq = fmt.Sprintf("%.1f", r.Parquet[0].AsyncOn[i].WarmMs*scale/1000)
			}
			w(" %s | %s |", hot, pq)
		}
		w("\n")
	}

	var errs []string
	for _, r := range reports {
		for _, e := range r.Errors {
			errs = append(errs, fmt.Sprintf("- %s: %s", r.Layout, e))
		}
	}
	if len(errs) > 0 {
		w("\n### Errors\n\n%s\n", strings.Join(errs, "\n"))
	}
	return b.String()
}

func msStr(v float64) string {
	if v >= 100 {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}
