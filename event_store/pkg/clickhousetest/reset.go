package clickhousetest

import (
	"context"
	"fmt"
	"maps"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// schema is the package's database as Reset made it, once its migrations
// ran. Reset empties a database that still has this schema. Empty clears
// it.
var schema *tables

// tables is a database's schema: the CREATE query of each table and view,
// by name, and the versions in schema_migrations.
type tables struct {
	create     map[string]string
	noData     map[string]bool // views and Null tables, which TRUNCATE refuses
	migrations string
}

func (s *tables) same(other *tables) bool {
	return maps.Equal(s.create, other.create) && s.migrations == other.migrations
}

// Reset gives a test the package's database with migrate's schema and no
// rows, users or row policies. The first call drops the database, makes it
// again and runs migrate on it, which makes the tables. A later call keeps
// those tables, as long as no test changed them or schema_migrations: it
// drops the SQL users, row policies, settings profile and each table that
// migrate did not make, and it empties each table that keeps rows, but not
// schema_migrations. When a test changed the schema, Reset makes the
// database again, as the first call does.
func Reset(t testing.TB, migrate func() error) {
	t.Helper()
	if schema != nil {
		kept, err := emptyTables(Config().Database, schema)
		if err != nil {
			t.Fatalf("Failed to empty the test database: %v", err)
		}
		if kept {
			return
		}
	}
	Empty(t)
	if err := migrate(); err != nil {
		t.Fatalf("Failed to migrate the test database: %v", err)
	}
	conn, err := open()
	if err != nil {
		t.Fatalf("Failed to read the test database's schema: %v", err)
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	migrated, err := readSchema(ctx, conn, Config().Database)
	if err != nil {
		t.Fatalf("Failed to read the test database's schema: %v", err)
	}
	schema = migrated
}

// emptyTables empties db, if its tables are want's: it reports false, with
// nothing done, if a table of want is not there as want has it.
func emptyTables(db string, want *tables) (bool, error) {
	conn, err := open()
	if err != nil {
		return false, err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	have, err := readSchema(ctx, conn, db)
	if err != nil {
		return false, err
	}
	kept := &tables{create: map[string]string{}, migrations: have.migrations}
	var extra []string
	for name, create := range have.create {
		if _, ok := want.create[name]; ok {
			kept.create[name] = create
		} else {
			extra = append(extra, name)
		}
	}
	if !want.same(kept) {
		return false, nil
	}
	statements, err := dropSQLUsers(ctx, conn, db)
	if err != nil {
		return false, err
	}
	for _, name := range extra {
		statements = append(statements, "DROP TABLE IF EXISTS "+db+"."+quote(name)+" SYNC")
	}
	for name := range want.create {
		if !want.noData[name] && name != "schema_migrations" {
			statements = append(statements, "TRUNCATE TABLE "+db+"."+quote(name)+" SYNC")
		}
	}
	for _, statement := range statements {
		if err := conn.Exec(ctx, statement); err != nil {
			return false, fmt.Errorf("%s on %s: %w", statement, devAddress, err)
		}
	}
	return true, nil
}

// readSchema reads db's tables and its schema_migrations.
func readSchema(ctx context.Context, conn driver.Conn, db string) (*tables, error) {
	rows, err := conn.Query(ctx, `
		SELECT name, create_table_query, engine IN ('View', 'MaterializedView', 'Null', 'Dictionary')
		FROM system.tables WHERE database = {db:String} AND NOT is_temporary`,
		clickhouse.Named("db", db))
	if err != nil {
		return nil, fmt.Errorf("listing %s's tables on %s: %w", db, devAddress, err)
	}
	defer rows.Close()
	s := &tables{create: map[string]string{}, noData: map[string]bool{}}
	for rows.Next() {
		var name, create string
		var noData bool
		if err := rows.Scan(&name, &create, &noData); err != nil {
			return nil, err
		}
		s.create[name] = create
		s.noData[name] = noData
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if _, ok := s.create["schema_migrations"]; ok {
		row := conn.QueryRow(ctx, "SELECT arrayStringConcat(arraySort(groupArray(toString(version))), ',') FROM "+db+".schema_migrations")
		if err := row.Scan(&s.migrations); err != nil {
			return nil, fmt.Errorf("reading %s's schema_migrations on %s: %w", db, devAddress, err)
		}
	}
	return s, nil
}

// quote is name as an identifier: a materialized view's inner table has a
// name such as .inner_id.<uuid>.
func quote(name string) string {
	return "`" + name + "`"
}
