package logslayouts

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/duckdb/duckdb-go/v2"
)

// Settings caps DuckDB for the shared dev box.
type Settings struct {
	MemoryLimit string
	Threads     int
}

func OpenDB(path string, s Settings) (*sql.DB, error) {
	db, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, err
	}
	for _, q := range []string{
		fmt.Sprintf("SET GLOBAL memory_limit = '%s'", s.MemoryLimit),
		fmt.Sprintf("SET GLOBAL threads = %d", s.Threads),
	} {
		if _, err := db.Exec(q); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", q, err)
		}
	}
	return db, nil
}

const fixedColumns = `id BIGINT, project_id INTEGER, time TIMESTAMP, level VARCHAR, message VARCHAR,
	service VARCHAR, environment VARCHAR, trace_id VARCHAR, span_id VARCHAR`

const fixedNames = "id, project_id, time, level, message, service, environment, trace_id, span_id"

// A Layout is one way of storing Attributes in the hot table `logs`.
type Layout interface {
	Name() string
	// attrColumns is the DDL for the Attribute column(s).
	attrColumns() string
	// StrAttr is a SQL expression for the key's string value, or NULL.
	StrAttr(key string) string
	// NumAttr is a SQL expression for the key's value as DOUBLE when it is a
	// number, else NULL (so a mixed-type key matches only its numeric values).
	// DuckDB evaluates a CASE branch's casts on rows the condition rejects, so
	// casts inside one must be TRY_CAST.
	NumAttr(key string) string
	// AttrsJSON are SQL expressions whose JSON objects together hold all of a
	// Log's Attributes.
	AttrsJSON() []string
	// ParquetOptions are extra COPY options for the layout's Parquet files.
	ParquetOptions() string
	NewWriter(ctx context.Context, db *sql.DB) (Writer, error)
}

// A Writer appends batches of Logs; each Write is one flush.
type Writer interface {
	Write(ctx context.Context, logs []Log) error
	Close() error
}

func Layouts() []Layout { return []Layout{jsonLayout{}, variantLayout{}, mapLayout{}} }

func LayoutByName(name string) (Layout, error) {
	for _, l := range Layouts() {
		if l.Name() == name {
			return l, nil
		}
	}
	return nil, fmt.Errorf("unknown layout %q", name)
}

func CreateTable(ctx context.Context, db *sql.DB, l Layout) error {
	_, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE logs (%s, %s)", fixedColumns, l.attrColumns()))
	return err
}

// Batch generates Logs start..start+n-1.
func Batch(g *Generator, start int64, n int) []Log {
	logs := make([]Log, n)
	for i := range logs {
		logs[i] = g.Log(start + int64(i))
	}
	return logs
}

func sqlString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func jsonPath(key string) string { return sqlString(`$."` + key + `"`) }

func nullable(s string) driver.Value {
	if s == "" {
		return nil
	}
	return s
}

func fixedValues(l Log) []driver.Value {
	return []driver.Value{l.ID, l.ProjectID, l.Time, l.Level, l.Message, l.Service, l.Environment,
		nullable(l.TraceID), nullable(l.SpanID)}
}

// appenderWriter appends rows on one connection and flushes once per Write.
type appenderWriter struct {
	conn *sql.Conn
	app  *duckdb.Appender
	row  func(Log) []driver.Value
	// afterFlush runs on conn after each flush.
	afterFlush func(ctx context.Context, conn *sql.Conn) error
}

func newAppenderWriter(ctx context.Context, db *sql.DB, setup []string, catalog, table string, row func(Log) []driver.Value) (*appenderWriter, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	for _, q := range setup {
		if _, err := conn.ExecContext(ctx, q); err != nil {
			conn.Close()
			return nil, err
		}
	}
	w := &appenderWriter{conn: conn, row: row}
	err = conn.Raw(func(dc any) error {
		w.app, err = duckdb.NewAppender(dc.(driver.Conn), catalog, "main", table)
		return err
	})
	if err != nil {
		conn.Close()
		return nil, err
	}
	return w, nil
}

func (w *appenderWriter) Write(ctx context.Context, logs []Log) error {
	for _, l := range logs {
		if err := w.app.AppendRow(w.row(l)...); err != nil {
			return err
		}
	}
	if err := w.app.FlushWithCancel(ctx); err != nil {
		return err
	}
	if w.afterFlush != nil {
		return w.afterFlush(ctx, w.conn)
	}
	return nil
}

func (w *appenderWriter) Close() error {
	err := w.app.Close()
	if cerr := w.conn.Close(); err == nil {
		err = cerr
	}
	return err
}

// jsonLayout stores Attributes as JSON text. The column is VARCHAR, which is
// how DuckDB stores its JSON type anyway: duckdb-go's appender JSON-encodes a
// Go string bound to a JSON column, storing a JSON string, not the object.
type jsonLayout struct{}

func (jsonLayout) Name() string        { return "json" }
func (jsonLayout) attrColumns() string { return "attrs VARCHAR" }
func (jsonLayout) StrAttr(k string) string {
	return fmt.Sprintf("json_extract_string(attrs, %s)", jsonPath(k))
}
func (jsonLayout) NumAttr(k string) string {
	return fmt.Sprintf("CASE WHEN json_type(attrs, %[1]s) IN ('BIGINT', 'UBIGINT', 'DOUBLE') THEN json_extract(attrs, %[1]s)::DOUBLE END", jsonPath(k))
}
func (jsonLayout) AttrsJSON() []string    { return []string{"attrs"} }
func (jsonLayout) ParquetOptions() string { return "" }
func (jsonLayout) NewWriter(ctx context.Context, db *sql.DB) (Writer, error) {
	return newAppenderWriter(ctx, db, nil, "", "logs", func(l Log) []driver.Value {
		return append(fixedValues(l), string(l.AttrsJSON))
	})
}

// variantLayout stores Attributes as VARIANT. The Go appender cannot write
// VARIANT, so each batch is appended as JSON text to a temporary staging
// table and converted with text::JSON::VARIANT in one INSERT.
type variantLayout struct{}

func (variantLayout) Name() string        { return "variant" }
func (variantLayout) attrColumns() string { return "attrs VARIANT" }

// StrAttr uses variant_extract_string: on Parquet shredded by default (one
// column per key seen), a filter on variant_extract(...)::VARCHAR raises an
// INTERNAL error that invalidates the database on this alpha.
func (variantLayout) StrAttr(k string) string {
	return fmt.Sprintf("variant_extract_string(attrs, %s)", sqlString(k))
}
func (variantLayout) NumAttr(k string) string {
	return fmt.Sprintf("CASE WHEN variant_typeof(variant_extract(attrs, %[1]s)) IN ('INT8', 'INT16', 'INT32', 'INT64', 'UINT8', 'UINT16', 'UINT32', 'UINT64', 'FLOAT', 'DOUBLE') OR variant_typeof(variant_extract(attrs, %[1]s)) LIKE 'DECIMAL%%' THEN TRY_CAST(variant_extract(attrs, %[1]s) AS DOUBLE) END", sqlString(k))
}
func (variantLayout) AttrsJSON() []string { return []string{"attrs::JSON"} }

// ParquetOptions shreds only the common keys. By default DuckDB shreds every
// key it sees (about 3,000 columns here), which on this alpha is slow, runs
// out of memory at the default row group size and crashes filters; see
// TestDefaultVariantParquet.
func (variantLayout) ParquetOptions() string {
	return "SHREDDING {attrs: '" + commonKeysStruct() + "'}"
}

// commonKeysStruct is a STRUCT type of the common keys; mixed-type keys stay VARIANT.
func commonKeysStruct() string {
	fields := make([]string, len(commonKeys))
	for i, k := range commonKeys {
		fields[i] = fmt.Sprintf(`"%s" %s`, k.name, commonKeySQLType(k))
	}
	return "STRUCT(" + strings.Join(fields, ", ") + ")"
}

func commonKeySQLType(k commonKey) string {
	if strings.HasPrefix(k.name, "mixed.") {
		return "VARIANT"
	}
	switch k.gen(rand.New(rand.NewPCG(0, 0))).Kind {
	case KindInt:
		return "BIGINT"
	case KindFloat:
		return "DOUBLE"
	case KindBool:
		return "BOOLEAN"
	}
	return "VARCHAR"
}
func (variantLayout) NewWriter(ctx context.Context, db *sql.DB) (Writer, error) {
	w, err := newAppenderWriter(ctx, db,
		[]string{fmt.Sprintf("CREATE TEMP TABLE logs_stage (%s, attrs VARCHAR)", fixedColumns)},
		"temp", "logs_stage",
		func(l Log) []driver.Value { return append(fixedValues(l), string(l.AttrsJSON)) })
	if err != nil {
		return nil, err
	}
	w.afterFlush = func(ctx context.Context, conn *sql.Conn) error {
		for _, q := range []string{
			"BEGIN",
			fmt.Sprintf("INSERT INTO logs SELECT %s, attrs::JSON::VARIANT FROM logs_stage", fixedNames),
			"DELETE FROM logs_stage",
			"COMMIT",
		} {
			if _, err := conn.ExecContext(ctx, q); err != nil {
				conn.ExecContext(context.Background(), "ROLLBACK")
				return fmt.Errorf("%s: %w", q, err)
			}
		}
		return nil
	}
	return w, nil
}

// mapLayout stores Attributes in one MAP per value type. Integers go in the
// DOUBLE map, which is exact below 2^53.
type mapLayout struct{}

func (mapLayout) Name() string { return "map" }
func (mapLayout) attrColumns() string {
	return "attrs_str MAP(VARCHAR, VARCHAR), attrs_num MAP(VARCHAR, DOUBLE), attrs_bool MAP(VARCHAR, BOOLEAN)"
}
func (mapLayout) StrAttr(k string) string { return fmt.Sprintf("attrs_str[%s]", sqlString(k)) }
func (mapLayout) NumAttr(k string) string { return fmt.Sprintf("attrs_num[%s]", sqlString(k)) }
func (mapLayout) ParquetOptions() string  { return "" }
func (mapLayout) AttrsJSON() []string {
	return []string{"to_json(attrs_str)", "to_json(attrs_num)", "to_json(attrs_bool)"}
}
func (mapLayout) NewWriter(ctx context.Context, db *sql.DB) (Writer, error) {
	return newAppenderWriter(ctx, db, nil, "", "logs", func(l Log) []driver.Value {
		s, n, b := duckdb.Map{}, duckdb.Map{}, duckdb.Map{}
		for _, a := range l.Attrs {
			switch a.Value.Kind {
			case KindString:
				s[a.Key] = a.Value.S
			case KindInt:
				n[a.Key] = float64(a.Value.I)
			case KindFloat:
				n[a.Key] = a.Value.F
			case KindBool:
				b[a.Key] = a.Value.B
			}
		}
		return append(fixedValues(l), s, n, b)
	})
}
