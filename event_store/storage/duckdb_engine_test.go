package storage

import (
	"database/sql"
	"strings"
	"testing"
)

// EventStore runs the DuckDB 1.5 engine bundled with the duckdb-go driver.
// Data files written by a 2.0 engine cannot be opened by 1.5, so fail loudly if
// a build links a different engine (for example via -tags=duckdb_use_lib).
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
	if !strings.HasPrefix(version, "v1.5") {
		t.Fatalf("Expected DuckDB engine v1.5, got %s. Build without -tags=duckdb_use_lib so the driver's bundled engine is used.", version)
	}
}
