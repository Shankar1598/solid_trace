package config

import (
	"fmt"
	"log"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port               string
	SocketPath         string
	QueryPort          string // Query API port, always bound to 127.0.0.1
	PebblePath         string
	DuckDBPath         string
	ParquetStoragePath string // Path for archived Parquet files
	SQLitePath         string // Rails SQLite for project key auth and issues
	MessageQueuePath   string // Separate SQLite for event_store_messages

	// IngestMaxWaiting caps the requests waiting to store their Event.
	// Past it, ingest answers 503.
	IngestMaxWaiting int

	DuckDBTempDirectory string
	DuckDBMemoryLimit   string

	StorageTiers []StorageTier

	Debug bool
}

type fileConfig struct {
	IngestPort          *string        `yaml:"ingest_port"`
	IngestSocket        *string        `yaml:"ingest_socket"`
	QueryPort           *string        `yaml:"query_port"`
	PebblePath          *string        `yaml:"pebble_path"`
	DuckDBPath          *string        `yaml:"duckdb_path"`
	ParquetStoragePath  *string        `yaml:"parquet_storage_path"`
	SQLitePath          *string        `yaml:"sqlite_path"`
	MessageQueuePath    *string        `yaml:"message_queue_path"`
	IngestMaxWaiting    *int           `yaml:"ingest_max_waiting"`
	DuckDBTempDirectory *string        `yaml:"duckdb_temp_directory"`
	DuckDBMemoryLimit   *string        `yaml:"duckdb_memory_limit"`
	StorageTiers        *[]StorageTier `yaml:"storage_tiers"`
	Debug               *bool          `yaml:"debug"`
}

func Load() *Config {
	cfg := &Config{
		Port:               "4000",
		SocketPath:         "",
		QueryPort:          "4100",
		PebblePath:         "../storage/pebble/development/events",
		DuckDBPath:         "../storage/duckdb/development/events.duckdb",
		ParquetStoragePath: "../storage/duckdb/development/events_parquet",
		SQLitePath:         "../storage/sqlite/development/solid_trace.sqlite3",
		MessageQueuePath:   "../storage/sqlite/development/message_queue.sqlite3",

		IngestMaxWaiting: 10000,

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
	if config.QueryPort != nil && *config.QueryPort != "" {
		cfg.QueryPort = *config.QueryPort
	}
	if config.PebblePath != nil && *config.PebblePath != "" {
		cfg.PebblePath = *config.PebblePath
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
	if config.IngestMaxWaiting != nil {
		if *config.IngestMaxWaiting <= 0 {
			return fmt.Errorf("ingest_max_waiting must be positive, got %d", *config.IngestMaxWaiting)
		}
		cfg.IngestMaxWaiting = *config.IngestMaxWaiting
	}
	if config.DuckDBTempDirectory != nil {
		cfg.DuckDBTempDirectory = *config.DuckDBTempDirectory
	}
	if config.DuckDBMemoryLimit != nil {
		cfg.DuckDBMemoryLimit = *config.DuckDBMemoryLimit
	}
	if config.StorageTiers != nil {
		cfg.StorageTiers = *config.StorageTiers
	}
	if config.Debug != nil {
		cfg.Debug = *config.Debug
	}

	return nil
}
