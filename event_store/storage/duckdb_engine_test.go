package storage

import (
	"database/sql"
	"strings"
	"testing"
)

// EventStore links the DuckDB 2.0 alpha (see scripts/fetch-duckdb). Without
// -tags=duckdb_use_lib, Go silently builds against the 1.5 engine bundled with
// the driver, so fail loudly instead.
func TestDuckDBEngineVersion(t *testing.T) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatalf("Failed to open DuckDB: %v", err)
	}
	defer db.Close()

	var version string
	if err := db.QueryRow("SELECT version()").Scan(&version); err != nil {
		t.Fatalf("Failed to query version: %v", err)
	}
	if !strings.HasPrefix(version, "v2.0") {
		t.Fatalf("Expected DuckDB engine v2.0, got %s. Run through mise (GOFLAGS=-tags=duckdb_use_lib) after scripts/fetch-duckdb.", version)
	}
}
