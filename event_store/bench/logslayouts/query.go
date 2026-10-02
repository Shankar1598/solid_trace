package logslayouts

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Params bound every query to one Project and an inclusive time range.
type Params struct {
	Project      int32
	Since, Until time.Time
	// MaxID caps ids so layouts that wrote different amounts can be compared
	// on the same Logs; negative means no cap.
	MaxID   int64
	FetchID int64
}

// VerifyParams covers Logs 0..maxID of Project 1.
func VerifyParams(g *Generator, maxID int64) Params {
	p := Params{Project: 1, Since: Base, Until: g.Log(maxID).Time, MaxID: maxID}
	for p.FetchID = maxID / 2; g.Log(p.FetchID).ProjectID != p.Project; p.FetchID++ {
	}
	return p
}

// TimedParams covers every Log written, Logs 0..lastID.
func TimedParams(g *Generator, lastID int64) Params {
	p := VerifyParams(g, lastID)
	p.MaxID = -1
	return p
}

func timestamp(t time.Time) string {
	return "TIMESTAMP '" + t.UTC().Format("2006-01-02 15:04:05.999999") + "'"
}

func (p Params) where() string {
	w := fmt.Sprintf("project_id = %d AND time >= %s AND time <= %s", p.Project, timestamp(p.Since), timestamp(p.Until))
	if p.MaxID >= 0 {
		w += fmt.Sprintf(" AND id <= %d", p.MaxID)
	}
	return w
}

type Query struct {
	Name string
	// Scan is true when the query reads the whole time range, so its latency
	// grows with the data; the full-day projection only scales these.
	Scan bool
	SQL  func(l Layout, src string, p Params) string
	// Check, when set, is an untimed query that must also match across
	// layouts: a count and an id checksum over every matching Log.
	Check func(l Layout, src string, p Params) string
	// AttrsCols marks queries whose trailing columns are the layout's AttrsJSON.
	AttrsCols bool
}

func filterQuery(name string, scan bool, pred func(l Layout, p Params) string) Query {
	return Query{
		Name: name,
		Scan: scan,
		SQL: func(l Layout, src string, p Params) string {
			return fmt.Sprintf("SELECT id, time, level, service, message FROM %s WHERE %s AND %s ORDER BY time DESC, id DESC LIMIT 100",
				src, p.where(), pred(l, p))
		},
		Check: func(l Layout, src string, p Params) string {
			return fmt.Sprintf("SELECT count(*), coalesce(bit_xor(id), 0) FROM %s WHERE %s AND %s", src, p.where(), pred(l, p))
		},
	}
}

// Queries is the ticket's query set.
func Queries() []Query {
	return []Query{
		filterQuery("errors_last_15m", false, func(_ Layout, p Params) string {
			return fmt.Sprintf("level IN ('error', 'fatal') AND time > %s", timestamp(p.Until.Add(-15*time.Minute)))
		}),
		{
			Name: "count_by_service_per_minute",
			Scan: true,
			SQL: func(_ Layout, src string, p Params) string {
				return fmt.Sprintf("SELECT service, time_bucket(INTERVAL 1 MINUTE, time) AS minute, count(*) AS n FROM %s WHERE %s GROUP BY ALL ORDER BY minute, service", src, p.where())
			},
		},
		filterQuery("common_attr_eq", true, func(l Layout, _ Params) string {
			return fmt.Sprintf("%s = %s", l.StrAttr(CommonKey), sqlString(CommonValue))
		}),
		filterQuery("rare_attr_eq", true, func(l Layout, _ Params) string {
			return fmt.Sprintf("%s = %s", l.StrAttr(RareKey), sqlString(RareValue))
		}),
		filterQuery("mixed_numeric_range", true, func(l Layout, _ Params) string {
			return fmt.Sprintf("%s BETWEEN %v AND %v", l.NumAttr(MixedKey), MixedLo, MixedHi)
		}),
		filterQuery("message_substring", true, func(_ Layout, _ Params) string {
			return fmt.Sprintf("contains(message, %s)", sqlString(Needle))
		}),
		{
			Name: "group_by_attr_top10",
			Scan: true,
			SQL: func(l Layout, src string, p Params) string {
				return fmt.Sprintf("SELECT g, count(*) AS n FROM (SELECT %s AS g FROM %s WHERE %s) WHERE g IS NOT NULL GROUP BY g ORDER BY n DESC, g LIMIT 10",
					l.StrAttr(GroupKey), src, p.where())
			},
		},
		{
			Name:      "fetch_by_id",
			AttrsCols: true,
			SQL: func(l Layout, src string, p Params) string {
				cols := fixedNames
				for _, e := range l.AttrsJSON() {
					cols += ", (" + e + ")::VARCHAR"
				}
				return fmt.Sprintf("SELECT %s FROM %s WHERE project_id = %d AND id = %d", cols, src, p.Project, p.FetchID)
			},
		},
	}
}

// Result is a query's rows as strings, plus its Check rows if it has one.
type Result struct {
	Rows  [][]string
	Check [][]string
}

func RunQuery(ctx context.Context, db *sql.DB, l Layout, q Query, src string, p Params) (Result, error) {
	attrsCols := 0
	if q.AttrsCols {
		attrsCols = len(l.AttrsJSON())
	}
	var res Result
	var err error
	if res.Rows, err = queryStrings(ctx, db, q.SQL(l, src, p), attrsCols); err != nil {
		return res, err
	}
	if q.Check != nil {
		res.Check, err = queryStrings(ctx, db, q.Check(l, src, p), 0)
	}
	return res, err
}

// queryStrings formats every value as a string. The last attrsCols columns
// are JSON objects, merged into one canonical (key-sorted) object.
func queryStrings(ctx context.Context, db *sql.DB, query string, attrsCols int) ([][]string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out [][]string
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make([]string, 0, len(cols))
		for _, v := range vals[:len(cols)-attrsCols] {
			row = append(row, format(v))
		}
		if attrsCols > 0 {
			merged := map[string]any{}
			for _, v := range vals[len(cols)-attrsCols:] {
				if v == nil {
					continue
				}
				if err := json.Unmarshal([]byte(format(v)), &merged); err != nil {
					return nil, fmt.Errorf("attributes %q: %w", v, err)
				}
			}
			b, _ := json.Marshal(merged)
			row = append(row, string(b))
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func format(v any) string {
	switch v := v.(type) {
	case nil:
		return "NULL"
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	case []byte:
		return string(v)
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	return fmt.Sprint(v)
}

// ParquetSource reads every Parquet file in dir.
func ParquetSource(dir string) string {
	return fmt.Sprintf("read_parquet(%s)", sqlString(filepath.Join(dir, "*.parquet")))
}

// ExportHourlyParquet writes one Parquet file per logical hour of `logs`,
// sorted by time, and returns their paths.
func ExportHourlyParquet(ctx context.Context, db *sql.DB, l Layout, dir string, rowGroupSize int) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var first, last time.Time
	if err := db.QueryRowContext(ctx, "SELECT min(time), max(time) FROM logs").Scan(&first, &last); err != nil {
		return nil, err
	}
	opts := l.ParquetOptions()
	if opts != "" {
		opts = ", " + opts
	}
	var files []string
	for h := first.Truncate(time.Hour); !h.After(last); h = h.Add(time.Hour) {
		path := filepath.Join(dir, h.Format("2006-01-02T15")+".parquet")
		q := fmt.Sprintf("COPY (SELECT * FROM logs WHERE time >= %s AND time < %s ORDER BY time, id) TO %s (FORMAT parquet, COMPRESSION zstd, ROW_GROUP_SIZE %d%s)",
			timestamp(h), timestamp(h.Add(time.Hour)), sqlString(path), rowGroupSize, opts)
		if _, err := db.ExecContext(ctx, q); err != nil {
			return files, err
		}
		files = append(files, path)
	}
	return files, nil
}
