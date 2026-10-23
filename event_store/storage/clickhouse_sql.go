package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/chcol"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/solidtrace/event_store/pkg/chsql"
)

// SQL queries (ADR 0003) run on ClickHouse, each as its Project's SQL user:
// a read-only ClickHouse user that EventStore makes the first time this
// process runs SQL for the Project, with a password only this process holds.
//
//   - Its row policies on logs and events keep it to its Project's rows, and
//     to the explorer's range through two settings the policies read,
//     SQL_from and SQL_to. The range is a convenience, not a boundary: a
//     query may set them itself, and still reads only its Project.
//   - Its settings profile makes it read-only and caps its time, memory,
//     threads and result. Read-only, it can change no setting but the range,
//     so a query can't raise its limits.
//   - It has no grant but SELECT on logs and events: no other table, no
//     system table, and no table function that reads (file, url, s3,
//     remote, …).
//
// So isolation is ClickHouse's, whatever the SQL says, and EventStore runs
// the SQL as written.

// The settings the row policies read: the explorer's range, from inclusive
// to exclusive, as "2006-01-02 15:04:05.000000" in UTC.
const (
	sqlFromSetting = "SQL_from"
	sqlToSetting   = "SQL_to"
)

// SQLLimits are what one SQL query may use. They are set in the SQL users'
// settings profile, so that no query can raise them.
type SQLLimits struct {
	// Time is how long a query may run (max_execution_time).
	Time time.Duration
	// MemoryBytes is the most memory a query may use (max_memory_usage),
	// and the most temporary data it may spill to disk.
	MemoryBytes int64
	// Threads is the most threads a query may use (max_threads).
	Threads int
	// ResultRows is the most rows a query returns; it stops once it has
	// them (max_result_rows, with result_overflow_mode 'break').
	ResultRows int
}

// DefaultSQLLimits are an install's limits: 10 seconds, 1 GiB, half the
// CPUs and 100,000 rows. A slow query holds its threads from ingest and
// other reads for no longer than the time.
func DefaultSQLLimits() SQLLimits {
	return SQLLimits{Time: 10 * time.Second, MemoryBytes: 1 << 30, Threads: max(1, runtime.NumCPU()/2), ResultRows: 100_000}
}

// SQLColumn is a column of a SQL query's result: its name, and its
// ClickHouse type, such as String or Nullable(Float64).
type SQLColumn struct {
	Name string
	Type string
}

// SQLResult is what a SQL query returned: its columns, and its rows as JSON
// values (sqlValue).
type SQLResult struct {
	Columns []SQLColumn
	Rows    [][]any
	// Truncated is set when the query returned more rows than were read.
	Truncated bool
}

// SQLError is a SQL query's own failure, as ClickHouse says it: a mistake in
// it, something its user may not do, or a limit it reached.
type SQLError struct {
	Err error
	// Stopped is set when the query ran for its limit's time.
	Stopped bool
}

func (e *SQLError) Error() string { return e.Err.Error() }
func (e *SQLError) Unwrap() error { return e.Err }

// ClickHouseSQL runs SQL queries on ClickHouse, each as its Project's SQL
// user.
type ClickHouseSQL struct {
	ch     *ClickHouse
	limits SQLLimits

	// mu guards making the users: profileMade, and passwords, the password
	// of each Project's user made by this process.
	mu          sync.Mutex
	profileMade bool
	passwords   map[uint32]string
}

// NewClickHouseSQL runs SQL queries on ch's logs and events, within limits.
func NewClickHouseSQL(ch *ClickHouse, limits SQLLimits) *ClickHouseSQL {
	return &ClickHouseSQL{ch: ch, limits: limits, passwords: map[uint32]string{}}
}

// Limits are what each SQL query may use.
func (s *ClickHouseSQL) Limits() SQLLimits { return s.limits }

// RunSQL runs a SQL query as written, as projectID's SQL user, with logs and
// events showing the Project's rows of [from, to). It reads at most limit
// rows. If the query itself fails, the error is a *SQLError with
// ClickHouse's message. Cancelling ctx stops it. Each run records the
// query in ctx's ReadStatements.
func (s *ClickHouseSQL) RunSQL(ctx context.Context, projectID uint32, from, to time.Time, query string, limit int) (SQLResult, error) {
	from, to = from.UTC(), to.UTC()
	if !to.After(from) {
		to = from
	}
	result, err := s.run(ctx, projectID, from, to, query, limit)
	// The user was dropped or its password changed since this process made
	// it, as by another EventStore on the same database: make it again.
	if exception, ok := clickHouseException(err); ok && (exception.Code == chAuthenticationFailed || exception.Code == chUnknownUser) {
		s.forget(projectID)
		result, err = s.run(ctx, projectID, from, to, query, limit)
	}
	return result, err
}

func (s *ClickHouseSQL) run(ctx context.Context, projectID uint32, from, to time.Time, query string, limit int) (SQLResult, error) {
	user, password, err := s.user(ctx, projectID)
	if err != nil {
		return SQLResult{}, fmt.Errorf("making Project %d's SQL user: %w", projectID, err)
	}
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr:         []string{s.ch.address},
		Auth:         clickhouse.Auth{Database: s.ch.database, Username: user, Password: password},
		DialTimeout:  5 * time.Second,
		MaxOpenConns: 1,
	})
	if err != nil {
		return SQLResult{}, err
	}
	defer conn.Close()
	// The query's context ends with ctx, but has no deadline: clickhouse-go
	// would send one as max_execution_time, a setting the user can't
	// change. It is also cancelled once the rows wanted are read, so that
	// closing the rows doesn't wait for the rest.
	queryCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	defer context.AfterFunc(ctx, cancel)()
	queryCtx = clickhouse.Context(queryCtx, clickhouse.WithSettings(clickhouse.Settings{
		sqlFromSetting: clickhouse.CustomSetting{Value: chsql.TimeText(from)},
		sqlToSetting:   clickhouse.CustomSetting{Value: chsql.TimeText(to)},
	}))

	began := time.Now()
	result, err := readSQL(queryCtx, conn, query, limit)
	recordStatement(ctx, query, began, err)
	if exception, ok := clickHouseException(err); ok && exception.Code != chAuthenticationFailed && exception.Code != chUnknownUser {
		return SQLResult{}, &SQLError{Err: errors.New(exception.Message), Stopped: exception.Code == chTimeoutExceeded}
	}
	return result, err
}

// readSQL runs query on conn and reads its first limit rows.
func readSQL(ctx context.Context, conn clickhouse.Conn, query string, limit int) (SQLResult, error) {
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return SQLResult{}, err
	}
	defer rows.Close()
	types := rows.ColumnTypes()
	result := SQLResult{Columns: make([]SQLColumn, len(types)), Rows: [][]any{}}
	for i, t := range types {
		result.Columns[i] = SQLColumn{Name: t.Name(), Type: t.DatabaseTypeName()}
	}
	for rows.Next() {
		if len(result.Rows) == limit {
			result.Truncated = true
			break
		}
		dest := make([]any, len(types))
		for i, t := range types {
			dest[i] = reflect.New(t.ScanType()).Interface()
		}
		if err := rows.Scan(dest...); err != nil {
			return SQLResult{}, err
		}
		row := make([]any, len(dest))
		for i, d := range dest {
			row[i] = sqlValue(reflect.ValueOf(d).Elem())
		}
		result.Rows = append(result.Rows, row)
	}
	if result.Truncated {
		return result, nil
	}
	return result, rows.Err()
}

// user is projectID's SQL user, made if this process hasn't made it yet,
// and its password.
func (s *ClickHouseSQL) user(ctx context.Context, projectID uint32) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	name := s.userName(projectID)
	if password, ok := s.passwords[projectID]; ok {
		return name, password, nil
	}
	if !s.profileMade {
		if err := s.ch.conn.Exec(ctx, s.profileStatement()); err != nil {
			return "", "", err
		}
		s.profileMade = true
	}
	secret := make([]byte, 24)
	if _, err := rand.Read(secret); err != nil {
		return "", "", err
	}
	password := hex.EncodeToString(secret)
	if err := s.makeUser(ctx, projectID, name, password); err != nil {
		return "", "", err
	}
	s.passwords[projectID] = password
	return name, password, nil
}

// forget forgets projectID's user, so that the next query makes it again.
func (s *ClickHouseSQL) forget(projectID uint32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.passwords, projectID)
}

// The SQL users' names start with their database's, since users are the
// server's, not a database's.
func (s *ClickHouseSQL) profileName() string { return s.ch.database + "_sql" }
func (s *ClickHouseSQL) userName(projectID uint32) string {
	return sqlUserName(s.ch.database, projectID)
}

func sqlUserName(database string, projectID uint32) string {
	return fmt.Sprintf("%s_sql_%d", database, projectID)
}

// dropSQLUser drops a Project's SQL user of database, if it has one: its
// row policies, then the user. Project deletion drops it with the Project's
// Logs. Should SQL run for the Project again, the run makes it afresh.
func dropSQLUser(ctx context.Context, conn driver.Conn, database string, projectID uint32) error {
	user, db := chsql.Identifier(sqlUserName(database, projectID)), chsql.Identifier(database)
	for _, statement := range []string{
		fmt.Sprintf("DROP ROW POLICY IF EXISTS %s ON %s.logs, %s ON %s.events", user, db, user, db),
		fmt.Sprintf("DROP USER IF EXISTS %s", user),
	} {
		if err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("drop Project %d's SQL user: %w", projectID, err)
		}
	}
	return nil
}

// profileStatement makes the SQL users' settings profile: read-only, each
// limit fixed, and the range settings changeable, empty until set.
//
// JSON and Dynamic columns are sent flattened: clickhouse-go 2.49 can't read
// the form 26.8 sends a JSON column in once a sort has merged blocks of
// several parts ("unsupported column type" naming a path).
func (s *ClickHouseSQL) profileStatement() string {
	empty := chsql.String(chsql.TimeText(time.Unix(0, 0)))
	return fmt.Sprintf(`CREATE SETTINGS PROFILE OR REPLACE %s SETTINGS
		readonly = 1 CONST,
		output_format_native_use_flattened_dynamic_and_json_serialization = 1 CONST,
		max_execution_time = %s CONST,
		max_memory_usage = %d CONST,
		max_temporary_data_on_disk_size_for_query = %d CONST,
		max_threads = %d CONST,
		max_result_rows = %d CONST,
		result_overflow_mode = 'break' CONST,
		%s = %s CHANGEABLE_IN_READONLY,
		%s = %s CHANGEABLE_IN_READONLY`,
		chsql.Identifier(s.profileName()),
		strconv.FormatFloat(s.limits.Time.Seconds(), 'f', -1, 64),
		s.limits.MemoryBytes, s.limits.MemoryBytes, s.limits.Threads, s.limits.ResultRows,
		sqlFromSetting, empty, sqlToSetting, empty)
}

// makeUser makes projectID's SQL user afresh, with password: the user, its
// row policies, then its grants, so that it never reads a table without
// its policy.
func (s *ClickHouseSQL) makeUser(ctx context.Context, projectID uint32, name, password string) error {
	user, db := chsql.Identifier(name), chsql.Identifier(s.ch.database)
	inRange := func(column string) string {
		return fmt.Sprintf("project_id = %d AND %s >= toDateTime64(getSetting('%s'), 6, 'UTC') AND %s < toDateTime64(getSetting('%s'), 6, 'UTC')",
			projectID, column, sqlFromSetting, column, sqlToSetting)
	}
	statements := []string{
		fmt.Sprintf("CREATE USER OR REPLACE %s IDENTIFIED WITH sha256_password BY %s HOST IP '127.0.0.1', IP '::1' DEFAULT DATABASE %s SETTINGS PROFILE %s",
			user, chsql.String(password), db, chsql.Identifier(s.profileName())),
		fmt.Sprintf("CREATE ROW POLICY OR REPLACE %s ON %s.logs FOR SELECT USING %s TO %s", user, db, inRange("time"), user),
		fmt.Sprintf("CREATE ROW POLICY OR REPLACE %s ON %s.events FOR SELECT USING %s TO %s", user, db, inRange("timestamp"), user),
		fmt.Sprintf("GRANT SELECT ON %s.logs TO %s", db, user),
		fmt.Sprintf("GRANT SELECT ON %s.events TO %s", db, user),
	}
	for _, statement := range statements {
		if err := s.ch.conn.Exec(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

// sqlValue is a value clickhouse-go scanned as a JSON value: pointers
// followed and NULL as nil, a time as RFC 3339 in UTC, NaN and infinities as
// null, a decimal as a number, a UUID or address as text, a JSON column as
// an object, a Dynamic or Variant as the value it holds, a map as an object
// with its keys as text, and an array or tuple as a list.
func sqlValue(v reflect.Value) any {
	for v.IsValid() && (v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return nil
		}
		if _, ok := v.Interface().(*big.Int); ok {
			break
		}
		if json, ok := v.Interface().(*chcol.JSON); ok {
			return sqlValue(reflect.ValueOf(json.NestedMap()))
		}
		v = v.Elem()
	}
	if !v.IsValid() {
		return nil
	}
	switch x := v.Interface().(type) {
	case time.Time:
		return x.UTC().Format(time.RFC3339Nano)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil
		}
		return x
	case float32:
		return sqlValue(reflect.ValueOf(float64(x)))
	case decimal.Decimal:
		return x.InexactFloat64()
	case uuid.UUID:
		return x.String()
	case net.IP:
		return x.String()
	case *big.Int:
		return x
	case chcol.JSON:
		return sqlValue(reflect.ValueOf(x.NestedMap()))
	case chcol.Variant:
		return sqlValue(reflect.ValueOf(x.Any()))
	case []byte:
		return string(x)
	}
	switch v.Kind() {
	case reflect.Map:
		object := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			object[fmt.Sprint(sqlValue(iter.Key()))] = sqlValue(iter.Value())
		}
		return object
	case reflect.Slice, reflect.Array:
		list := make([]any, v.Len())
		for i := range list {
			list[i] = sqlValue(v.Index(i))
		}
		return list
	}
	return v.Interface()
}
