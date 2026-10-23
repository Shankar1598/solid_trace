// Package clickhousetest gives a Go test package a database of its own on
// the dev ClickHouse that bin/clickhouse-dev starts.
//
// A package that reaches ClickHouse runs its tests through Main:
//
//	func TestMain(m *testing.M) { os.Exit(clickhousetest.Main(m)) }
//
// Its database, named after the worktree and the package's directory, is made
// fresh before the tests and dropped after them, so packages and worktrees
// can test at once. Config is how EventStore reaches it; Empty makes it fresh
// again within the run, for a test that starts from nothing.
package clickhousetest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/solidtrace/event_store/config"
)

// database is the package's database, set by Main.
var database string

// Main makes the calling package's database fresh, runs its tests and drops
// the database, returning the exit code for os.Exit.
func Main(m *testing.M) int {
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		fmt.Fprintln(os.Stderr, "clickhousetest: can't find the calling test package")
		return 1
	}
	database = databaseName(file)
	if err := drop(database, true); err != nil {
		fmt.Fprintf(os.Stderr, "clickhousetest: %v\nIs the dev ClickHouse running? Start it with bin/clickhouse-dev.\n", err)
		return 1
	}
	code := m.Run()
	if err := drop(database, false); err != nil {
		fmt.Fprintf(os.Stderr, "clickhousetest: %v\n", err)
	}
	return code
}

// Config is the package's database on the dev ClickHouse, as EventStore's
// clickhouse block would give it. It has no ConfigDir: tests leave the
// server's storage policy alone.
func Config() config.ClickHouse {
	if database == "" {
		panic("clickhousetest: call clickhousetest.Main from the package's TestMain")
	}
	return config.ClickHouse{
		Address:  devAddress,
		Database: database,
		User:     devUser,
		Password: os.Getenv("SOLIDTRACE_CLICKHOUSE_PASSWORD"),
	}
}

// Empty drops the package's database and makes it again, with no tables and
// no SQL users.
func Empty(t testing.TB) {
	t.Helper()
	if err := drop(Config().Database, true); err != nil {
		t.Fatalf("Failed to empty the test database: %v", err)
	}
}

// drop drops db, and the SQL users, row policies and settings profile that
// storage.ClickHouseSQL makes for its Projects, which are the server's, not
// the database's: they are named after it, as <db>_sql and <db>_sql_<id>.
// With again set, it makes db again.
func drop(db string, again bool) error {
	conn, err := open()
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// Every name is plain: [a-z0-9_].
	var statements []string
	rows, err := conn.Query(ctx, `
		SELECT concat('DROP ROW POLICY IF EXISTS ', short_name, ' ON ', database, '.', table)
		FROM system.row_policies WHERE database = {db:String}
		UNION ALL
		SELECT concat('DROP USER IF EXISTS ', name) FROM system.users WHERE startsWith(name, {db:String} || '_sql_')
		UNION ALL
		SELECT concat('DROP SETTINGS PROFILE IF EXISTS ', name) FROM system.settings_profiles WHERE name = {db:String} || '_sql'`,
		clickhouse.Named("db", db))
	if err != nil {
		return fmt.Errorf("listing %s's SQL users on %s: %w", db, devAddress, err)
	}
	for rows.Next() {
		var statement string
		if err := rows.Scan(&statement); err != nil {
			rows.Close()
			return err
		}
		statements = append(statements, statement)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	statements = append(statements, "DROP DATABASE IF EXISTS "+db+" SYNC")
	if again {
		statements = append(statements, "CREATE DATABASE "+db)
	}
	for _, statement := range statements {
		if err := conn.Exec(ctx, statement); err != nil {
			return fmt.Errorf("%s on %s: %w", statement, devAddress, err)
		}
	}
	return nil
}

// bin/clickhouse-dev's server and EventStore's user on it.
const (
	devAddress = "127.0.0.1:4200"
	devUser    = "solidtrace"
)

var notPlain = regexp.MustCompile(`[^a-z0-9_]+`)

// databaseName names the database of the test package whose file is
// file: test_<worktree>_<package directory>, the root package as main.
func databaseName(file string) string {
	// This file is <checkout>/event_store/pkg/clickhousetest/clickhousetest.go.
	_, self, _, _ := runtime.Caller(0)
	eventStore := filepath.Dir(filepath.Dir(filepath.Dir(self)))
	worktree := filepath.Base(filepath.Dir(eventStore))
	pkg, err := filepath.Rel(eventStore, filepath.Dir(file))
	if err != nil || pkg == "." {
		pkg = "main"
	}
	name := "test_" + worktree + "_" + pkg
	return strings.Trim(notPlain.ReplaceAllString(strings.ToLower(name), "_"), "_")
}

// open connects to the dev ClickHouse as EventStore's user.
func open() (driver.Conn, error) {
	return clickhouse.Open(&clickhouse.Options{
		Addr:        []string{devAddress},
		Auth:        clickhouse.Auth{Username: devUser, Password: os.Getenv("SOLIDTRACE_CLICKHOUSE_PASSWORD")},
		DialTimeout: 5 * time.Second,
	})
}
