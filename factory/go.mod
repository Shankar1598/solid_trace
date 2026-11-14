module github.com/solidtrace/factory

go 1.27.3

require (
	github.com/mattn/go-sqlite3 v1.14.33
	github.com/solidtrace/cli v0.0.0
)

// The factory module may import the CLI's packages, because the CLI is a
// client of public APIs. It imports nothing from Console or EventStore
// (ADR 0019); imports_test.go checks this.
replace github.com/solidtrace/cli => ../cli
