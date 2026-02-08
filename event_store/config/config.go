package config

import (
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port               string
	SocketPath         string
	RocksDBPath        string
	RocksDBPaths       []DBPath
	DuckDBPath         string
	ParquetStoragePath string // Path for archived Parquet files
	SQLitePath         string // Rails SQLite for project key auth and issues
	MessageQueuePath   string // Separate SQLite for event_store_messages

	RocksDBBatchSize    int
	RocksDBFlushTimeout time.Duration
	RocksDBChannelSize  int

	DuckDBFlushTimeout time.Duration
	DuckDBChannelSize  int

	DuckDBTempDirectory string
	DuckDBMemoryLimit   string

	Debug bool
}

type DBPath struct {
	Path         string `yaml:"path"`
	TargetSizeGB uint64 `yaml:"target_size_gb"`
}

type fileConfig struct {
	IngestPort          *string  `yaml:"ingest_port"`
	IngestSocket        *string  `yaml:"ingest_socket"`
	RocksDBPath         *string  `yaml:"rocksdb_path"`
	RocksDBPaths        []DBPath `yaml:"rocksdb_db_paths"`
	DuckDBPath          *string  `yaml:"duckdb_path"`
	ParquetStoragePath  *string  `yaml:"parquet_storage_path"`
	SQLitePath          *string  `yaml:"sqlite_path"`
	MessageQueuePath    *string  `yaml:"message_queue_path"`
	RocksDBBatchSize    *int     `yaml:"rocksdb_batch_size"`
	RocksDBFlushTimeout *string  `yaml:"rocksdb_flush_timeout"`
	RocksDBChannelSize  *int     `yaml:"rocksdb_channel_size"`
	DuckDBFlushTimeout  *string  `yaml:"duckdb_flush_timeout"`
	DuckDBChannelSize   *int     `yaml:"duckdb_channel_size"`
	DuckDBTempDirectory *string  `yaml:"duckdb_temp_directory"`
	DuckDBMemoryLimit   *string  `yaml:"duckdb_memory_limit"`
	Debug               *bool    `yaml:"debug"`
}

func Load() *Config {
	cfg := &Config{
		Port:               "4000",
		SocketPath:         "",
		RocksDBPath:        "../storage/rocksdb/development/events",
		DuckDBPath:         "../storage/duckdb/development/events.duckdb",
		ParquetStoragePath: "../storage/duckdb/development/events_parquet",
		SQLitePath:         "../storage/sqlite/development/solid_trace.sqlite3",
		MessageQueuePath:   "../storage/sqlite/development/message_queue.sqlite3",

		RocksDBBatchSize:    1000,
		RocksDBFlushTimeout: 200 * time.Millisecond,
		RocksDBChannelSize:  50000,

		DuckDBFlushTimeout: 1 * time.Second,
		DuckDBChannelSize:  100000,

		DuckDBTempDirectory: "",
		DuckDBMemoryLimit:   "",

		Debug: false,
	}

	configPath := "event_store.yml"
	if v := os.Getenv("EVENT_STORE_CONFIG"); v != "" {
		configPath = v
	}

	if err := applyFileConfig(cfg, configPath); err != nil {
		log.Fatalf("Failed to load %s: %v", configPath, err)
	}

	return cfg
}

func applyFileConfig(cfg *Config, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var config fileConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return err
	}

	if config.IngestPort != nil && *config.IngestPort != "" {
		cfg.Port = *config.IngestPort
	}
	if config.IngestSocket != nil {
		cfg.SocketPath = *config.IngestSocket
	}
	if config.RocksDBPath != nil && *config.RocksDBPath != "" {
		cfg.RocksDBPath = *config.RocksDBPath
	}
	if len(config.RocksDBPaths) > 0 {
		cfg.RocksDBPaths = config.RocksDBPaths
	}
	if config.DuckDBPath != nil && *config.DuckDBPath != "" {
		cfg.DuckDBPath = *config.DuckDBPath
	}
	if config.ParquetStoragePath != nil && *config.ParquetStoragePath != "" {
		cfg.ParquetStoragePath = *config.ParquetStoragePath
	}
	if config.SQLitePath != nil && *config.SQLitePath != "" {
		cfg.SQLitePath = *config.SQLitePath
	}
	if config.MessageQueuePath != nil && *config.MessageQueuePath != "" {
		cfg.MessageQueuePath = *config.MessageQueuePath
	}
	if config.RocksDBBatchSize != nil {
		cfg.RocksDBBatchSize = *config.RocksDBBatchSize
	}
	if config.RocksDBFlushTimeout != nil && *config.RocksDBFlushTimeout != "" {
		d, err := time.ParseDuration(*config.RocksDBFlushTimeout)
		if err != nil {
			return err
		}
		cfg.RocksDBFlushTimeout = d
	}
	if config.RocksDBChannelSize != nil {
		cfg.RocksDBChannelSize = *config.RocksDBChannelSize
	}
	if config.DuckDBFlushTimeout != nil && *config.DuckDBFlushTimeout != "" {
		d, err := time.ParseDuration(*config.DuckDBFlushTimeout)
		if err != nil {
			return err
		}
		cfg.DuckDBFlushTimeout = d
	}
	if config.DuckDBChannelSize != nil {
		cfg.DuckDBChannelSize = *config.DuckDBChannelSize
	}
	if config.DuckDBTempDirectory != nil {
		cfg.DuckDBTempDirectory = *config.DuckDBTempDirectory
	}
	if config.DuckDBMemoryLimit != nil {
		cfg.DuckDBMemoryLimit = *config.DuckDBMemoryLimit
	}
	if config.Debug != nil {
		cfg.Debug = *config.Debug
	}

	return nil
}
