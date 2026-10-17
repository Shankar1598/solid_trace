package config

import (
	"bytes"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port            string
	QueryPort       string // Query API port, always bound to 127.0.0.1
	PebblePath      string
	DuckDBPath      string
	LogsDuckDBPath  string // logs.duckdb, attached to the DuckDB of DuckDBPath
	LogsParquetPath string // Where the hourly export writes Log files
	// LogsDiskLimitBytes caps the Log files of every Project together; the
	// oldest hours are deleted past it. 0 caps them at half the disk that
	// holds them (storage.NewLogDiskLimit).
	LogsDiskLimitBytes int64
	ParquetStoragePath string // Path for archived Parquet files
	SQLitePath         string // Rails SQLite for project key auth and issues
	MessageQueuePath   string // Separate SQLite for event_store_messages

	// IngestMaxWaiting caps the requests waiting to store their Event.
	// Past it, ingest answers 503.
	IngestMaxWaiting int

	DuckDBTempDirectory string
	DuckDBMemoryLimit   string
	// DuckDBExtensionDirectory holds the DuckDB extensions EventStore loads,
	// such as httpfs for an ObjectStore. The Docker image fills the default
	// at build time; DuckDB never downloads one itself.
	DuckDBExtensionDirectory string

	StorageTiers []StorageTier

	// ObjectStore is the bucket that keeps Log files and the Event
	// archive, or nil when everything stays on local disk.
	ObjectStore *ObjectStore

	Debug bool
}

// DefaultDuckDBExtensionDirectory is where the Docker image puts DuckDB's
// extensions, as <version>/<platform>/<name>.duckdb_extension.
const DefaultDuckDBExtensionDirectory = "/usr/local/lib/solidtrace/duckdb_extensions"

// ObjectStore kinds.
const (
	ObjectStoreS3  = "s3"
	ObjectStoreGCS = "gcs"
)

// GCSEndpoint is the S3-compatible endpoint of Google Cloud Storage, which
// takes HMAC keys.
const GCSEndpoint = "https://storage.googleapis.com"

// ObjectStore is the object_store block: a bucket reached through the S3
// API, on S3 or anything that speaks it (GCS with HMAC keys, MinIO, R2).
type ObjectStore struct {
	Kind   string // ObjectStoreS3 or ObjectStoreGCS
	Bucket string
	// Prefix is put before every key: empty, or ending in "/".
	Prefix string
	Region string
	// Endpoint is the URL of the S3 API, such as http://127.0.0.1:9000, or
	// empty for AWS's own. It is GCSEndpoint when Kind is gcs and none is
	// given.
	Endpoint string
	// LocalCacheHours is how long a Log file's local copy is kept after it
	// is uploaded; 0 deletes it once uploaded.
	LocalCacheHours int
	// KeyID and Secret come from SOLIDTRACE_OBJECT_STORE_KEY_ID and
	// SOLIDTRACE_OBJECT_STORE_SECRET, never from the file.
	KeyID  string
	Secret string
}

// objectStoreFile is the object_store block as written in the file.
type objectStoreFile struct {
	Kind            string  `yaml:"kind"`
	Bucket          string  `yaml:"bucket"`
	Prefix          *string `yaml:"prefix"`
	Region          string  `yaml:"region"`
	Endpoint        string  `yaml:"endpoint"`
	LocalCacheHours *int    `yaml:"local_cache_hours"`
}

type fileConfig struct {
	IngestPort          *string        `yaml:"ingest_port"`
	QueryPort           *string        `yaml:"query_port"`
	PebblePath          *string        `yaml:"pebble_path"`
	DuckDBPath          *string        `yaml:"duckdb_path"`
	LogsDuckDBPath      *string        `yaml:"logs_duckdb_path"`
	LogsParquetPath     *string        `yaml:"logs_parquet_path"`
	LogsDiskLimitGB     *int64         `yaml:"logs_disk_limit_gb"`
	ParquetStoragePath  *string        `yaml:"parquet_storage_path"`
	SQLitePath          *string        `yaml:"sqlite_path"`
	MessageQueuePath    *string        `yaml:"message_queue_path"`
	IngestMaxWaiting    *int           `yaml:"ingest_max_waiting"`
	DuckDBTempDirectory *string        `yaml:"duckdb_temp_directory"`
	DuckDBMemoryLimit   *string        `yaml:"duckdb_memory_limit"`
	DuckDBExtensionDir  *string        `yaml:"duckdb_extension_directory"`
	StorageTiers        *[]StorageTier `yaml:"storage_tiers"`
	ObjectStore         yaml.Node      `yaml:"object_store"`
	Debug               *bool          `yaml:"debug"`
}

// Load reads the config from the file EVENT_STORE_CONFIG names, or
// event_store.yml, over the defaults. It exits if the file can't be read or
// holds a bad value.
func Load() *Config {
	configPath := "event_store.yml"
	if v := os.Getenv("EVENT_STORE_CONFIG"); v != "" {
		configPath = v
	}
	cfg, err := LoadFile(configPath)
	if err != nil {
		log.Fatalf("Failed to load %s: %v", configPath, err)
	}
	return cfg
}

// LoadFile reads the config from the file at path over the defaults. A
// missing file gives the defaults.
func LoadFile(path string) (*Config, error) {
	cfg := &Config{
		Port:               "4000",
		QueryPort:          "4100",
		PebblePath:         "../storage/pebble/development/events",
		DuckDBPath:         "../storage/duckdb/development/events.duckdb",
		ParquetStoragePath: "../storage/duckdb/development/events_parquet",
		SQLitePath:         "../storage/sqlite/development/solid_trace.sqlite3",
		MessageQueuePath:   "../storage/sqlite/development/message_queue.sqlite3",

		IngestMaxWaiting: 10000,

		DuckDBTempDirectory:      "",
		DuckDBMemoryLimit:        "",
		DuckDBExtensionDirectory: DefaultDuckDBExtensionDirectory,

		Debug: false,
	}

	if err := applyFileConfig(cfg, path); err != nil {
		return nil, err
	}
	if cfg.LogsDuckDBPath == "" {
		cfg.LogsDuckDBPath = filepath.Join(filepath.Dir(cfg.DuckDBPath), "logs.duckdb")
	}
	if cfg.LogsParquetPath == "" {
		cfg.LogsParquetPath = filepath.Join(filepath.Dir(cfg.DuckDBPath), "logs_parquet")
	}

	return cfg, nil
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
	if config.QueryPort != nil && *config.QueryPort != "" {
		cfg.QueryPort = *config.QueryPort
	}
	if config.PebblePath != nil && *config.PebblePath != "" {
		cfg.PebblePath = *config.PebblePath
	}
	if config.DuckDBPath != nil && *config.DuckDBPath != "" {
		cfg.DuckDBPath = *config.DuckDBPath
	}
	if config.LogsDuckDBPath != nil && *config.LogsDuckDBPath != "" {
		cfg.LogsDuckDBPath = *config.LogsDuckDBPath
	}
	if config.LogsParquetPath != nil && *config.LogsParquetPath != "" {
		cfg.LogsParquetPath = *config.LogsParquetPath
	}
	if config.LogsDiskLimitGB != nil {
		if *config.LogsDiskLimitGB <= 0 {
			return fmt.Errorf("logs_disk_limit_gb must be positive, got %d", *config.LogsDiskLimitGB)
		}
		cfg.LogsDiskLimitBytes = *config.LogsDiskLimitGB * 1_000_000_000
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
	if config.DuckDBExtensionDir != nil && *config.DuckDBExtensionDir != "" {
		cfg.DuckDBExtensionDirectory = *config.DuckDBExtensionDir
	}
	if config.StorageTiers != nil {
		cfg.StorageTiers = *config.StorageTiers
	}
	if config.ObjectStore.Kind != 0 {
		store, err := objectStore(&config.ObjectStore)
		if err != nil {
			return fmt.Errorf("object_store: %w", err)
		}
		cfg.ObjectStore = store
	}
	if config.Debug != nil {
		cfg.Debug = *config.Debug
	}

	return nil
}

// objectStore reads and checks the object_store block, with the keys from
// the environment.
func objectStore(node *yaml.Node) (*ObjectStore, error) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil, fmt.Errorf("the block is empty; remove it to keep everything on local disk")
	}
	// Decoded strictly, so that a misspelt key fails rather than being
	// silently left at its default.
	raw, err := yaml.Marshal(node)
	if err != nil {
		return nil, err
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var file objectStoreFile
	if err := decoder.Decode(&file); err != nil {
		return nil, err
	}

	store := &ObjectStore{
		Kind:            file.Kind,
		Bucket:          file.Bucket,
		Prefix:          "solidtrace/",
		Region:          file.Region,
		Endpoint:        strings.TrimSuffix(file.Endpoint, "/"),
		LocalCacheHours: 24,
		KeyID:           os.Getenv("SOLIDTRACE_OBJECT_STORE_KEY_ID"),
		Secret:          os.Getenv("SOLIDTRACE_OBJECT_STORE_SECRET"),
	}
	switch store.Kind {
	case ObjectStoreS3:
	case ObjectStoreGCS:
		if store.Endpoint == "" {
			store.Endpoint = GCSEndpoint
		}
		if store.Region == "" {
			store.Region = "auto"
		}
	default:
		return nil, fmt.Errorf("kind must be s3 or gcs, got %q", store.Kind)
	}
	if store.Bucket == "" {
		return nil, fmt.Errorf("bucket is required")
	}
	if strings.ContainsAny(store.Bucket, "/ ") {
		return nil, fmt.Errorf("bucket must be a bucket's name, with no / or spaces, got %q", store.Bucket)
	}
	if file.Prefix != nil {
		store.Prefix = strings.TrimLeft(*file.Prefix, "/")
		if store.Prefix != "" && !strings.HasSuffix(store.Prefix, "/") {
			store.Prefix += "/"
		}
	}
	if store.Endpoint != "" {
		u, err := url.Parse(store.Endpoint)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return nil, fmt.Errorf("endpoint must be a URL such as https://s3.example.com or http://127.0.0.1:9000, got %q", file.Endpoint)
		}
		if store.Region == "" {
			// MinIO and most S3-compatible stores accept any region.
			store.Region = "us-east-1"
		}
	}
	if store.Region == "" {
		return nil, fmt.Errorf("region is required for AWS S3, such as us-east-1")
	}
	if file.LocalCacheHours != nil {
		if *file.LocalCacheHours < 0 {
			return nil, fmt.Errorf("local_cache_hours can't be negative, got %d", *file.LocalCacheHours)
		}
		store.LocalCacheHours = *file.LocalCacheHours
	}
	if store.KeyID == "" || store.Secret == "" {
		return nil, fmt.Errorf("set the bucket's key in SOLIDTRACE_OBJECT_STORE_KEY_ID and SOLIDTRACE_OBJECT_STORE_SECRET")
	}
	return store, nil
}
