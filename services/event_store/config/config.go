package config

import (
	"os"
	"time"
)

type Config struct {
	Port             string
	RocksDBPath      string
	DuckDBPath       string
	SQLitePath       string // Rails SQLite for project key auth and issues
	MessageQueuePath string // Separate SQLite for event_store_messages

	RocksDBBatchSize    int
	RocksDBFlushTimeout time.Duration
	RocksDBChannelSize  int

	DuckDBFlushTimeout time.Duration
	DuckDBChannelSize  int

	Debug bool
}

func Load() *Config {
	return &Config{
		Port:             getEnv("INGEST_PORT", "4000"),
		RocksDBPath:      getEnv("ROCKSDB_PATH", "../../storage/rocksdb/development/events"),
		DuckDBPath:       getEnv("DUCKDB_PATH", "../../storage/duckdb/development/events.duckdb"),
		SQLitePath:       getEnv("SQLITE_PATH", "../../storage/sqlite/solid_trace_development.sqlite3"),
		MessageQueuePath: getEnv("MESSAGE_QUEUE_PATH", "../../storage/sqlite/solid_trace_development_message_queue.sqlite3"),

		RocksDBBatchSize:    1000,
		RocksDBFlushTimeout: 200 * time.Millisecond,
		RocksDBChannelSize:  50000,

		DuckDBFlushTimeout: 1 * time.Second,
		DuckDBChannelSize:  100000,

		Debug: getEnv("DEBUG", "false") == "true",
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
