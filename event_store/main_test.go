package main_test

import (
	"os"
	"testing"

	"github.com/solidtrace/event_store/pkg/clickhousetest"
)

// TestMain runs the root tests with a database of their own on the dev
// ClickHouse (bin/clickhouse-dev).
func TestMain(m *testing.M) {
	os.Exit(clickhousetest.Main(m))
}
