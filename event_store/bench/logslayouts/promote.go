package logslayouts

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// PromotionRowGroupSize is ADR 0004's row group size (ticket 09 preferred
// about 30k rows to 122,880).
const PromotionRowGroupSize = 30720

// A PromotedKey is an Attribute key of one map type written as its own
// Parquet column, named "<map>.<key>" and removed from the map.
type PromotedKey struct{ Map, Key string }

func (p PromotedKey) Column() string { return p.Map + "." + p.Key }

// ExportedFile is one Project's Logs for one hour.
type ExportedFile struct {
	Path     string
	Project  int32
	Hour     time.Time
	Rows     int64
	Promoted []PromotedKey
}

// ExportStats splits an export's time into choosing each file's top keys and
// writing the files.
type ExportStats struct {
	TopKeysMs, CopyMs float64
}

func quoteIdent(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }

// ExportProjectHours writes the Logs with from <= time < until to
// dir/project=<id>/day=<d>/hour=<h>/<name>.parquet, one zstd file per Project
// per hour, sorted by time. For the MAP layout each file's top `promote`
// (map, key) pairs by count become typed columns and leave the maps; other
// layouts are written as they are.
func ExportProjectHours(ctx context.Context, db *sql.DB, l Layout, dir string, from, until time.Time, promote int, name string) ([]ExportedFile, ExportStats, error) {
	var st ExportStats
	if _, ok := l.(mapLayout); !ok {
		promote = 0
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf(`SELECT project_id, date_trunc('hour', time) AS h, count(*) FROM logs
		WHERE time >= %s AND time < %s GROUP BY ALL ORDER BY h, project_id`, timestamp(from), timestamp(until)))
	if err != nil {
		return nil, st, err
	}
	var files []ExportedFile
	for rows.Next() {
		var f ExportedFile
		if err := rows.Scan(&f.Project, &f.Hour, &f.Rows); err != nil {
			rows.Close()
			return nil, st, err
		}
		files = append(files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, st, err
	}

	for i := range files {
		f := &files[i]
		where := fmt.Sprintf("project_id = %d AND time >= %s AND time < %s AND time >= %s AND time < %s",
			f.Project, timestamp(f.Hour), timestamp(f.Hour.Add(time.Hour)), timestamp(from), timestamp(until))
		if promote > 0 {
			t0 := time.Now()
			if f.Promoted, err = topKeys(ctx, db, where, promote); err != nil {
				return files, st, err
			}
			st.TopKeysMs += ms(time.Since(t0))
		}
		d := filepath.Join(dir, fmt.Sprintf("project=%d", f.Project), "day="+f.Hour.Format("2006-01-02"), "hour="+f.Hour.Format("15"))
		if err := os.MkdirAll(d, 0o755); err != nil {
			return files, st, err
		}
		f.Path = filepath.Join(d, name+".parquet")
		t0 := time.Now()
		q := fmt.Sprintf("COPY (SELECT %s FROM logs WHERE %s ORDER BY time, id) TO %s (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE %d%s)",
			exportColumns(l, f.Promoted), where, sqlString(f.Path), PromotionRowGroupSize, optsSuffix(l))
		if _, err := db.ExecContext(ctx, q); err != nil {
			return files, st, err
		}
		st.CopyMs += ms(time.Since(t0))
	}
	return files, st, nil
}

func optsSuffix(l Layout) string {
	if o := l.ParquetOptions(); o != "" {
		return ", " + o
	}
	return ""
}

// topKeys ranks the (map, key) pairs of the string, number and bool maps by
// how many Logs carry them. Arrays (attrs_json) are never promoted.
func topKeys(ctx context.Context, db *sql.DB, where string, n int) ([]PromotedKey, error) {
	var parts []string
	for _, c := range mapColumns[:3] {
		parts = append(parts, fmt.Sprintf("SELECT %s AS m, unnest(map_keys(%s)) AS k FROM logs WHERE %s", sqlString(c.name), c.name, where))
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT m, k FROM (%s) GROUP BY m, k ORDER BY count(*) DESC, m, k LIMIT %d",
		strings.Join(parts, " UNION ALL "), n))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PromotedKey
	for rows.Next() {
		var p PromotedKey
		if err := rows.Scan(&p.Map, &p.Key); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// exportColumns selects every column, with promoted keys pulled out of their
// maps into columns of their own.
func exportColumns(l Layout, promoted []PromotedKey) string {
	if len(promoted) == 0 {
		return "*"
	}
	cols := []string{fixedNames}
	for _, c := range mapColumns {
		var keys []string
		for _, p := range promoted {
			if p.Map == c.name {
				keys = append(keys, sqlString(p.Key))
				cols = append(cols, fmt.Sprintf("%s[%s] AS %s", c.name, sqlString(p.Key), quoteIdent(p.Column())))
			}
		}
		if len(keys) == 0 {
			cols = append(cols, c.name)
			continue
		}
		cols = append(cols, fmt.Sprintf("map_from_entries(list_filter(map_entries(%[1]s), e -> NOT list_contains([%[2]s], e.key))) AS %[1]s",
			c.name, strings.Join(keys, ", ")))
	}
	return strings.Join(cols, ", ")
}

// Query forms over promoted Parquet.
const (
	// FormMap reads Attributes from the maps only (no promoted columns).
	FormMap = "map"
	// FormCoalesce reads every file with union_by_name and each key as
	// coalesce(column, map[key]), as ADR 0004 describes.
	FormCoalesce = "coalesce"
	// FormPerFile reads each file on its own, using the column where that
	// file promoted the key and the map elsewhere, under UNION ALL BY NAME.
	FormPerFile = "per_file"
)

// queriedKeys are the (map, key) pairs the query set reads.
var queriedKeys = []PromotedKey{
	{"attrs_string", CommonKey}, {"attrs_string", RareKey}, {"attrs_number", MixedKey}, {"attrs_string", GroupKey},
}

// promotedLayout reads Parquet written by ExportProjectHours.
type promotedLayout struct {
	mapLayout
	form string
	// columns are the promoted columns present in at least one file.
	columns map[string]bool
}

// newPromotedLayout reads one Project's files.
func newPromotedLayout(form string, files []ExportedFile, project int32) promotedLayout {
	l := promotedLayout{form: form, columns: map[string]bool{}}
	for _, f := range files {
		if f.Project != project {
			continue
		}
		for _, p := range f.Promoted {
			l.columns[p.Column()] = true
		}
	}
	return l
}

func (l promotedLayout) Name() string { return "map/" + l.form }

func (l promotedLayout) attr(m, k string) string {
	p := PromotedKey{m, k}
	switch {
	case l.form == FormPerFile:
		return quoteIdent("q:" + p.Column())
	case l.columns[p.Column()]:
		return fmt.Sprintf("coalesce(%s, %s[%s])", quoteIdent(p.Column()), m, sqlString(k))
	}
	return fmt.Sprintf("%s[%s]", m, sqlString(k))
}

func (l promotedLayout) StrAttr(k string) string { return l.attr("attrs_string", k) }
func (l promotedLayout) NumAttr(k string) string { return l.attr("attrs_number", k) }

// AttrsJSON puts promoted columns back into their maps, one object per map.
func (l promotedLayout) AttrsJSON() []string {
	var out []string
	for _, c := range mapColumns {
		var entries []string
		for col := range l.columns {
			if m, k, _ := strings.Cut(col, "."); m == c.name {
				entries = append(entries, fmt.Sprintf("{'key': %s, 'value': %s}", sqlString(k), quoteIdent(col)))
			}
		}
		if len(entries) == 0 {
			out = append(out, fmt.Sprintf("to_json(%s)", c.name))
			continue
		}
		slices.Sort(entries)
		out = append(out, fmt.Sprintf("to_json(map_from_entries(list_filter(map_entries(%s) || [%s], e -> e.value IS NOT NULL)))",
			c.name, strings.Join(entries, ", ")))
	}
	return out
}

// PromotedSource is the FROM clause for one Project's files in the given form.
func PromotedSource(form string, files []ExportedFile, project int32) string {
	var paths []string
	var mine []ExportedFile
	for _, f := range files {
		if f.Project == project {
			paths = append(paths, sqlString(f.Path))
			mine = append(mine, f)
		}
	}
	if form != FormPerFile {
		return fmt.Sprintf("read_parquet([%s], union_by_name = true)", strings.Join(paths, ", "))
	}
	var branches []string
	for _, f := range mine {
		cols := []string{"*"}
		for _, q := range queriedKeys {
			expr := fmt.Sprintf("%s[%s]", q.Map, sqlString(q.Key))
			if slices.Contains(f.Promoted, q) {
				expr = quoteIdent(q.Column())
			}
			cols = append(cols, fmt.Sprintf("%s AS %s", expr, quoteIdent("q:"+q.Column())))
		}
		branches = append(branches, fmt.Sprintf("SELECT %s FROM read_parquet(%s)", strings.Join(cols, ", "), sqlString(f.Path)))
	}
	return "(" + strings.Join(branches, " UNION ALL BY NAME ") + ")"
}
