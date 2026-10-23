package config

import (
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Port       string
	QueryPort  string // Query API port, always bound to 127.0.0.1
	PebblePath string
	// LogsDuckDBPath is where a DuckDB release kept its logs.duckdb. EventStore
	// never opens it: it won't start while one is there that
	// prepare-clickhouse didn't prepare, and reads what it prepared from
	// prepared/ beside it.
	LogsDuckDBPath string
	// LogsParquetPath is the Log file store: the Log files a DuckDB release
	// wrote, read in place until Retention deletes them.
	LogsParquetPath string
	// LogsDiskLimitBytes caps the Logs of every Project together on
	// ClickHouse's local disk; past it, the oldest days move to the bucket,
	// or are deleted without one. 0 caps them at half of that disk
	// (storage.ClickHouseLogs.DiskLimit).
	LogsDiskLimitBytes int64
	// LogsBinCacheBytes is the memory the bin cache keeps charts' bins in;
	// 0 turns it off.
	LogsBinCacheBytes int64
	SQLitePath        string // Rails SQLite for project key auth and issues
	MessageQueuePath  string // Separate SQLite for event_store_messages

	// IngestMaxWaiting caps the requests waiting to store their Event.
	// Past it, ingest answers 503.
	IngestMaxWaiting int

	StorageTiers []StorageTier

	// ObjectStore is the bucket that older Logs move to, and that holds the
	// Log files of a DuckDB release, or nil when everything stays on local
	// disk.
	ObjectStore *ObjectStore

	// ClickHouse is the server EventStore keeps Logs and Events in.
	ClickHouse ClickHouse

	// CPUProfileDir is where EventStore writes a CPU profile twice an hour
	// (pkg/cpuprofile), or "" for none.
	CPUProfileDir string

	Debug bool

	// Warnings are about keys of the file that do nothing now, for the start
	// to log.
	Warnings []string
}

// DefaultLogsBinCacheMB is the bin cache's size, in MiB, unless the file says.
const DefaultLogsBinCacheMB = 64

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
	// LocalCacheHours is how long Logs stay on local disk: a part of
	// ClickHouse's logs table moves to the bucket once its newest Log is
	// this many hours old, and 0 moves it at once.
	LocalCacheHours int
	// KeyID and Secret come from SOLIDTRACE_OBJECT_STORE_KEY_ID and
	// SOLIDTRACE_OBJECT_STORE_SECRET, never from the file.
	KeyID  string
	Secret string
}

// ClickHouse is the clickhouse block: how EventStore reaches its ClickHouse
// server, which runs beside it (deploy/clickhouse, bin/clickhouse-dev).
type ClickHouse struct {
	// Address is the server's native protocol, host:port.
	Address string
	// Database is EventStore's database, which it makes at start.
	Database string
	User     string
	// Password comes from SOLIDTRACE_CLICKHOUSE_PASSWORD, never from the
	// file. deploy/install generates it; bin/clickhouse-dev's has none.
	Password string
	// ConfigDir is the directory ClickHouse reads as its conf.d, where
	// EventStore writes the storage policy's bucket volume from
	// object_store. Empty, EventStore leaves the policy as it is.
	ConfigDir string
	// StoragePolicy is the storage policy EventStore's tables are made on,
	// or "" for storage.ClickHouseStoragePolicy. It isn't in the file: tests
	// give theirs a policy with a bucket volume of their own.
	StoragePolicy string
}

// clickHouseFile is the clickhouse block as written in the file.
type clickHouseFile struct {
	Address   *string `yaml:"address"`
	Database  *string `yaml:"database"`
	User      *string `yaml:"user"`
	ConfigDir *string `yaml:"config_dir"`
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
	IngestPort       *string        `yaml:"ingest_port"`
	QueryPort        *string        `yaml:"query_port"`
	PebblePath       *string        `yaml:"pebble_path"`
	LogsParquetPath  *string        `yaml:"logs_parquet_path"`
	LogsDiskLimitGB  *int64         `yaml:"logs_disk_limit_gb"`
	LogsBinCacheMB   *int64         `yaml:"logs_bin_cache_mb"`
	SQLitePath       *string        `yaml:"sqlite_path"`
	MessageQueuePath *string        `yaml:"message_queue_path"`
	IngestMaxWaiting *int           `yaml:"ingest_max_waiting"`
	StorageTiers     *[]StorageTier `yaml:"storage_tiers"`
	ObjectStore      yaml.Node      `yaml:"object_store"`
	ClickHouse       yaml.Node      `yaml:"clickhouse"`
	CPUProfileDir    *string        `yaml:"cpu_profile_dir"`
	Debug            *bool          `yaml:"debug"`

	// A DuckDB release's keys. EventStore no longer uses DuckDB, so they
	// load with a warning. duckdb_path and logs_duckdb_path still say
	// where that release's logs.duckdb and Log files are.
	DuckDBPath          *string `yaml:"duckdb_path"`
	LogsDuckDBPath      *string `yaml:"logs_duckdb_path"`
	ParquetStoragePath  *string `yaml:"parquet_storage_path"`
	DuckDBTempDirectory *string `yaml:"duckdb_temp_directory"`
	DuckDBMemoryLimit   *string `yaml:"duckdb_memory_limit"`
	DuckDBExtensionDir  *string `yaml:"duckdb_extension_directory"`
}

// duckDBDir is where a DuckDB release kept its databases and Log files
// unless its config said otherwise: the development one, beside the other
// stores.
const duckDBDir = "../storage/duckdb/development"

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
		Port:             "4000",
		QueryPort:        "4100",
		PebblePath:       "../storage/pebble/development/events",
		LogsDuckDBPath:   duckDBDir + "/logs.duckdb",
		LogsParquetPath:  duckDBDir + "/logs_parquet",
		SQLitePath:       "../storage/sqlite/development/solid_trace.sqlite3",
		MessageQueuePath: "../storage/sqlite/development/message_queue.sqlite3",

		IngestMaxWaiting:  10000,
		LogsBinCacheBytes: DefaultLogsBinCacheMB << 20,

		// bin/clickhouse-dev's server. ConfigDir is relative to
		// event_store/, as the paths above are; in the Docker image, whose
		// working directory is /app, it is /storage/clickhouse/conf.d.
		ClickHouse: ClickHouse{
			Address:   "127.0.0.1:4200",
			Database:  "solidtrace",
			User:      "solidtrace",
			Password:  os.Getenv("SOLIDTRACE_CLICKHOUSE_PASSWORD"),
			ConfigDir: "../storage/clickhouse/conf.d",
		},

		Debug: false,
	}

	if err := applyFileConfig(cfg, path); err != nil {
		return nil, err
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
	duckDBReleasesLogs(cfg, &config)
	if config.LogsDiskLimitGB != nil {
		if *config.LogsDiskLimitGB <= 0 {
			return fmt.Errorf("logs_disk_limit_gb must be positive, got %d", *config.LogsDiskLimitGB)
		}
		cfg.LogsDiskLimitBytes = *config.LogsDiskLimitGB * 1_000_000_000
	}
	if config.LogsBinCacheMB != nil {
		if *config.LogsBinCacheMB < 0 {
			return fmt.Errorf("logs_bin_cache_mb can't be negative, got %d; 0 turns the bin cache off", *config.LogsBinCacheMB)
		}
		cfg.LogsBinCacheBytes = *config.LogsBinCacheMB << 20
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
	if config.ClickHouse.Kind != 0 {
		if err := clickHouse(&cfg.ClickHouse, &config.ClickHouse); err != nil {
			return fmt.Errorf("clickhouse: %w", err)
		}
	}
	if config.CPUProfileDir != nil {
		cfg.CPUProfileDir = *config.CPUProfileDir
	}
	if config.Debug != nil {
		cfg.Debug = *config.Debug
	}

	return nil
}

// duckDBReleasesLogs works out where a DuckDB release kept logs.duckdb and
// the Log files, and warns of each DuckDB key in the file.
//
// The Log files are at logs_parquet_path, else beside duckdb_path, as that
// release had them. logs.duckdb is at logs_duckdb_path, else beside
// duckdb_path, else beside the Log files, where a config with neither key
// keeps them both.
func duckDBReleasesLogs(cfg *Config, file *fileConfig) {
	given := func(value *string) bool { return value != nil && *value != "" }
	dir := duckDBDir
	if given(file.DuckDBPath) {
		dir = filepath.Dir(*file.DuckDBPath)
	}
	cfg.LogsParquetPath = filepath.Join(dir, "logs_parquet")
	if given(file.LogsParquetPath) {
		cfg.LogsParquetPath = *file.LogsParquetPath
		if !given(file.DuckDBPath) {
			dir = filepath.Dir(cfg.LogsParquetPath)
		}
	}
	cfg.LogsDuckDBPath = filepath.Join(dir, "logs.duckdb")
	if given(file.LogsDuckDBPath) {
		cfg.LogsDuckDBPath = *file.LogsDuckDBPath
	}

	const locates = " is used only to find a DuckDB release's logs.duckdb, which EventStore won't start on until prepare-clickhouse has prepared it, and its Log files, which are read until Retention deletes them: Logs and Events are in ClickHouse now"
	const ignored = " is ignored: EventStore no longer uses DuckDB, and Logs and Events are in ClickHouse. Remove it from the config"
	for _, key := range []struct {
		name  string
		value *string
		why   string
	}{
		{"duckdb_path", file.DuckDBPath, locates},
		{"logs_duckdb_path", file.LogsDuckDBPath, locates},
		{"parquet_storage_path", file.ParquetStoragePath, ignored},
		{"duckdb_temp_directory", file.DuckDBTempDirectory, ignored},
		{"duckdb_memory_limit", file.DuckDBMemoryLimit, ignored},
		{"duckdb_extension_directory", file.DuckDBExtensionDir, ignored},
	} {
		if key.value != nil {
			cfg.Warnings = append(cfg.Warnings, key.name+key.why)
		}
	}
}

// plainName is a ClickHouse identifier that needs no quoting.
var plainName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// clickHouse reads and checks the clickhouse block over ch's defaults. An
// empty block leaves them.
func clickHouse(ch *ClickHouse, node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil
	}
	if err := onlyKnownKeys(node, clickHouseFile{}); err != nil {
		return err
	}
	var file clickHouseFile
	if err := node.Decode(&file); err != nil {
		return err
	}
	if file.Address != nil {
		ch.Address = *file.Address
	}
	if file.Database != nil {
		ch.Database = *file.Database
	}
	if file.User != nil {
		ch.User = *file.User
	}
	if file.ConfigDir != nil {
		ch.ConfigDir = *file.ConfigDir
	}
	if _, port, err := net.SplitHostPort(ch.Address); err != nil || port == "" {
		return fmt.Errorf("address must be a host and port, such as 127.0.0.1:4200, got %q", ch.Address)
	}
	if !plainName.MatchString(ch.Database) {
		return fmt.Errorf("database must be letters, digits and _, not starting with a digit, got %q", ch.Database)
	}
	if ch.User == "" {
		return fmt.Errorf("user can't be empty")
	}
	return nil
}

// objectStore reads and checks the object_store block, with the keys from
// the environment.
func objectStore(node *yaml.Node) (*ObjectStore, error) {
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil, fmt.Errorf("the block is empty; remove it to keep everything on local disk")
	}
	// Checked strictly, so that a misspelt key fails rather than being
	// silently left at its default. The node is decoded in place, so errors
	// give lines in the file.
	if err := onlyKnownKeys(node, objectStoreFile{}); err != nil {
		return nil, err
	}
	var file objectStoreFile
	if err := node.Decode(&file); err != nil {
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

// onlyKnownKeys fails on a key of the mapping node that no yaml tag of
// fields names. yaml.v3 can only check this when decoding a whole document.
func onlyKnownKeys(node *yaml.Node, fields any) error {
	if node.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: must be a block of keys", node.Line)
	}
	known := map[string]bool{}
	t := reflect.TypeOf(fields)
	for i := range t.NumField() {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("yaml"), ",")
		known[name] = true
	}
	for i := 0; i < len(node.Content); i += 2 {
		if key := node.Content[i]; !known[key.Value] {
			return fmt.Errorf("line %d: field %s not found", key.Line, key.Value)
		}
	}
	return nil
}
