package handler_test

import (
	"os"
	"testing"

	"github.com/solidtrace/event_store/pkg/clickhousetest"
)

// TestMain runs the handler tests with a database of their own on the dev
// ClickHouse (bin/clickhouse-dev), which the logs routes read.
func TestMain(m *testing.M) {
	os.Exit(clickhousetest.Main(m))
}
