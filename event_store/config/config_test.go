package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/solidtrace/event_store/config"
)

// loadFrom loads the config the way the deploy does: from the file named by
// EVENT_STORE_CONFIG.
func loadFrom(t *testing.T, path string) *config.Config {
	t.Helper()
	t.Setenv("EVENT_STORE_CONFIG", path)
	return config.Load()
}

// writeConfig writes a config file and returns its path.
func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "event_store.yml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDeployConfigIngestsOnTCPPort4000(t *testing.T) {
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	if cfg.Port != "4000" {
		t.Errorf("ingest port = %q, want 4000", cfg.Port)
	}
}

func TestIngestPortDefaultsTo4000WithoutAConfigFile(t *testing.T) {
	cfg := loadFrom(t, filepath.Join(t.TempDir(), "missing.yml"))

	if cfg.Port != "4000" {
		t.Errorf("ingest port = %q, want 4000", cfg.Port)
	}
}

func TestIngestPortComesFromTheConfigFile(t *testing.T) {
	cfg := loadFrom(t, writeConfig(t, "ingest_port: \"4500\"\n"))

	if cfg.Port != "4500" {
		t.Errorf("ingest port = %q, want 4500", cfg.Port)
	}
}

// A config written for the old deploy, whose ingest listened on a unix
// socket behind a proxy, still loads. Ingest then listens on the default port.
func TestConfigWithTheOldIngestSocketIngestsOnTCPPort4000(t *testing.T) {
	cfg := loadFrom(t, writeConfig(t, "ingest_socket: /var/run/solid-trace/event_store.sock\n"))

	if cfg.Port != "4000" {
		t.Errorf("ingest port = %q, want 4000", cfg.Port)
	}
}

// EventStore no longer opens DuckDB, but a config from a DuckDB release
// still loads. Each DuckDB key gets a warning, so the operator knows it does
// nothing now.
func TestTheDuckDBKeysAreIgnoredWithAWarningEach(t *testing.T) {
	keys := []string{"duckdb_path", "logs_duckdb_path", "parquet_storage_path", "duckdb_temp_directory", "duckdb_memory_limit", "duckdb_extension_directory"}
	cfg, err := config.LoadFile(writeConfig(t, `
duckdb_path: /storage/duckdb/production/events.duckdb
logs_duckdb_path: /storage/duckdb/production/logs.duckdb
parquet_storage_path: /storage_cold/duckdb/production/events_parquet
duckdb_temp_directory: /storage_local/duckdb/tmp
duckdb_memory_limit: 7GB
duckdb_extension_directory: /usr/local/lib/solidtrace/duckdb_extensions
`))
	if err != nil {
		t.Fatalf("A DuckDB release's config failed to load: %v", err)
	}
	if len(cfg.Warnings) != len(keys) {
		t.Errorf("Expected a warning for each of %v, got %q", keys, cfg.Warnings)
	}
	for _, key := range keys {
		warned := false
		for _, warning := range cfg.Warnings {
			warned = warned || strings.HasPrefix(warning, key+" ")
		}
		if !warned {
			t.Errorf("Expected a warning about %s, got %q", key, cfg.Warnings)
		}
	}
}

func TestTheDeployConfigLoadsWithNoWarning(t *testing.T) {
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	if len(cfg.Warnings) != 0 {
		t.Errorf("Expected no warnings, got %q", cfg.Warnings)
	}
}

// The start looks for the DuckDB release's logs.duckdb, which it won't start
// on unprepared, and the Log files written before the switch, which are read
// until Retention deletes them, where that release kept them.
func TestTheDuckDBReleasesLogsAreLookedForWhereItKeptThem(t *testing.T) {
	cases := []struct {
		name, yaml        string
		logsDuckDB, files string
	}{
		{"the deploy config", "", "/storage/duckdb/production/logs.duckdb", "/storage/duckdb/production/logs_parquet"},
		{"no config file", "missing", "../storage/duckdb/development/logs.duckdb", "../storage/duckdb/development/logs_parquet"},
		{"a DuckDB release's config", "duckdb_path: /data/events.duckdb\n", "/data/logs.duckdb", "/data/logs_parquet"},
		{"logs_duckdb_path", "duckdb_path: /data/events.duckdb\nlogs_duckdb_path: /fast/logs.duckdb\n", "/fast/logs.duckdb", "/data/logs_parquet"},
		{"logs_parquet_path", "logs_parquet_path: /fast/logs_parquet\n", "/fast/logs.duckdb", "/fast/logs_parquet"},
		{"logs_parquet_path and duckdb_path", "duckdb_path: /data/events.duckdb\nlogs_parquet_path: /fast/logs_parquet\n", "/data/logs.duckdb", "/fast/logs_parquet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := "../../deploy/event_store.yml"
			switch c.yaml {
			case "":
			case "missing":
				path = filepath.Join(t.TempDir(), "missing.yml")
			default:
				path = writeConfig(t, c.yaml)
			}
			cfg := loadFrom(t, path)
			if cfg.LogsDuckDBPath != c.logsDuckDB {
				t.Errorf("logs.duckdb = %q, want %q", cfg.LogsDuckDBPath, c.logsDuckDB)
			}
			if cfg.LogsParquetPath != c.files {
				t.Errorf("Log files = %q, want %q", cfg.LogsParquetPath, c.files)
			}
		})
	}
}

func TestLogsDiskLimitIsUnsetByDefault(t *testing.T) {
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	if cfg.LogsDiskLimitBytes != 0 {
		t.Errorf("Logs disk limit = %d bytes, want 0 (half the disk)", cfg.LogsDiskLimitBytes)
	}
}

func TestLogsDiskLimitComesFromTheConfigFileInGB(t *testing.T) {
	cfg := loadFrom(t, writeConfig(t, "logs_disk_limit_gb: 800\n"))

	if want := int64(800_000_000_000); cfg.LogsDiskLimitBytes != want {
		t.Errorf("Logs disk limit = %d bytes, want %d", cfg.LogsDiskLimitBytes, want)
	}
}

func TestCPUProfilesAreOffUnlessTheConfigFileNamesADirectory(t *testing.T) {
	if got := loadFrom(t, filepath.Join(t.TempDir(), "missing.yml")).CPUProfileDir; got != "" {
		t.Errorf("Default CPU profile directory = %q, want none", got)
	}
	if got := loadFrom(t, writeConfig(t, "cpu_profile_dir: /storage/profiles\n")).CPUProfileDir; got != "/storage/profiles" {
		t.Errorf("CPU profile directory = %q, want /storage/profiles", got)
	}
}

func TestTheBinCacheTakes64MiBUnlessTheConfigFileSays(t *testing.T) {
	if got := loadFrom(t, filepath.Join(t.TempDir(), "missing.yml")).LogsBinCacheBytes; got != 64<<20 {
		t.Errorf("Default bin cache = %d bytes, want 64 MiB", got)
	}
	if got := loadFrom(t, writeConfig(t, "logs_bin_cache_mb: 256\n")).LogsBinCacheBytes; got != 256<<20 {
		t.Errorf("Bin cache = %d bytes, want 256 MiB", got)
	}
	if got := loadFrom(t, writeConfig(t, "logs_bin_cache_mb: 0\n")).LogsBinCacheBytes; got != 0 {
		t.Errorf("Bin cache = %d bytes, want 0, off", got)
	}
	if _, err := config.LoadFile(writeConfig(t, "logs_bin_cache_mb: -1\n")); err == nil || !strings.Contains(err.Error(), "logs_bin_cache_mb can't be negative") {
		t.Errorf("Expected a negative bin cache to fail, got %v", err)
	}
}

// setObjectStoreKeys sets the bucket's key in the environment.
func setObjectStoreKeys(t *testing.T) {
	t.Helper()
	t.Setenv("SOLIDTRACE_OBJECT_STORE_KEY_ID", "GOOG1EXAMPLE")
	t.Setenv("SOLIDTRACE_OBJECT_STORE_SECRET", "example-secret")
}

func TestWithNoObjectStoreEverythingStaysLocal(t *testing.T) {
	setObjectStoreKeys(t)
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	if cfg.ObjectStore != nil {
		t.Errorf("object store = %+v, want none", cfg.ObjectStore)
	}
}

func TestAGCSObjectStoreGetsGCSsEndpointAndTheDefaults(t *testing.T) {
	setObjectStoreKeys(t)
	cfg := loadFrom(t, writeConfig(t, "object_store:\n  kind: gcs\n  bucket: acme-logs\n"))

	want := config.ObjectStore{
		Kind: "gcs", Bucket: "acme-logs", Prefix: "solidtrace/", Region: "auto",
		Endpoint: "https://storage.googleapis.com", LocalCacheHours: 24,
		KeyID: "GOOG1EXAMPLE", Secret: "example-secret",
	}
	if cfg.ObjectStore == nil || *cfg.ObjectStore != want {
		t.Errorf("object store = %+v, want %+v", cfg.ObjectStore, want)
	}
}

func TestAnS3ObjectStoreTakesItsEndpointPrefixAndCacheHoursFromTheFile(t *testing.T) {
	setObjectStoreKeys(t)
	cfg := loadFrom(t, writeConfig(t, `object_store:
  kind: s3
  bucket: logs
  prefix: /team/solidtrace
  endpoint: http://127.0.0.1:9000/
  local_cache_hours: 0
`))

	want := config.ObjectStore{
		Kind: "s3", Bucket: "logs", Prefix: "team/solidtrace/", Region: "us-east-1",
		Endpoint: "http://127.0.0.1:9000", LocalCacheHours: 0,
		KeyID: "GOOG1EXAMPLE", Secret: "example-secret",
	}
	if cfg.ObjectStore == nil || *cfg.ObjectStore != want {
		t.Errorf("object store = %+v, want %+v", cfg.ObjectStore, want)
	}
}

func TestAnObjectStoreOnAWSKeepsItsRegionAndMayHaveNoPrefix(t *testing.T) {
	setObjectStoreKeys(t)
	cfg := loadFrom(t, writeConfig(t, "object_store:\n  kind: s3\n  bucket: logs\n  region: eu-west-1\n  prefix: \"\"\n"))

	if s := cfg.ObjectStore; s == nil || s.Region != "eu-west-1" || s.Endpoint != "" || s.Prefix != "" {
		t.Errorf("object store = %+v, want region eu-west-1, AWS's endpoint and no prefix", s)
	}
}

func TestABadObjectStoreFailsToLoadSayingWhatIsWrong(t *testing.T) {
	cases := []struct {
		name, yaml, want string
		noKeys           bool
	}{
		{"no kind", "object_store:\n  bucket: logs\n", `object_store: kind must be s3 or gcs, got ""`, false},
		{"another kind", "object_store:\n  kind: azure\n  bucket: logs\n", `object_store: kind must be s3 or gcs, got "azure"`, false},
		{"no bucket", "object_store:\n  kind: gcs\n", "object_store: bucket is required", false},
		{"a path for a bucket", "object_store:\n  kind: gcs\n  bucket: gs://logs\n", `object_store: bucket must be a bucket's name, with no / or spaces, got "gs://logs"`, false},
		{"an endpoint with no scheme", "object_store:\n  kind: s3\n  bucket: logs\n  endpoint: minio:9000\n", `object_store: endpoint must be a URL such as https://s3.example.com or http://127.0.0.1:9000, got "minio:9000"`, false},
		{"AWS with no region", "object_store:\n  kind: s3\n  bucket: logs\n", "object_store: region is required for AWS S3, such as us-east-1", false},
		{"negative cache hours", "object_store:\n  kind: gcs\n  bucket: logs\n  local_cache_hours: -1\n", "object_store: local_cache_hours can't be negative, got -1", false},
		{"a misspelt key", "object_store:\n  kind: gcs\n  bucket: logs\n  local_cache_hour: 2\n", "field local_cache_hour not found", false},
		{"an empty block", "object_store:\n", "object_store: the block is empty; remove it to keep everything on local disk", false},
		{"no keys", "object_store:\n  kind: gcs\n  bucket: logs\n", "object_store: set the bucket's key in SOLIDTRACE_OBJECT_STORE_KEY_ID and SOLIDTRACE_OBJECT_STORE_SECRET", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.noKeys {
				t.Setenv("SOLIDTRACE_OBJECT_STORE_KEY_ID", "")
				t.Setenv("SOLIDTRACE_OBJECT_STORE_SECRET", "")
			} else {
				setObjectStoreKeys(t)
			}
			_, err := config.LoadFile(writeConfig(t, c.yaml))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want one saying %q", err, c.want)
			}
		})
	}
}

func TestAnObjectStoreErrorGivesTheLineInTheFile(t *testing.T) {
	setObjectStoreKeys(t)
	cases := []struct{ name, block, want string }{
		{"a misspelt key", "  kind: gcs\n  bucket: logs\n  local_cache_hour: 2\n", "line 8: field local_cache_hour not found"},
		{"a value of the wrong type", "  kind: gcs\n  bucket: logs\n  local_cache_hours: many\n", "line 8: cannot unmarshal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yaml := "ingest_port: \"4000\"\nquery_port: \"4100\"\n# The bucket.\n\nobject_store:\n" + c.block
			_, err := config.LoadFile(writeConfig(t, yaml))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want one saying %q", err, c.want)
			}
		})
	}
}

func TestWithNoConfigFileEventStoreUsesTheDevClickHouse(t *testing.T) {
	t.Setenv("SOLIDTRACE_CLICKHOUSE_PASSWORD", "")
	cfg := loadFrom(t, filepath.Join(t.TempDir(), "missing.yml"))

	// bin/clickhouse-dev's server, whose solidtrace user has no password.
	want := config.ClickHouse{
		Address: "127.0.0.1:4200", Database: "solidtrace", User: "solidtrace",
		ConfigDir: "../storage/clickhouse/conf.d",
	}
	if cfg.ClickHouse != want {
		t.Errorf("clickhouse = %+v, want %+v", cfg.ClickHouse, want)
	}
}

func TestTheDeployConfigReachesComposesClickHouseWithTheGeneratedPassword(t *testing.T) {
	t.Setenv("SOLIDTRACE_CLICKHOUSE_PASSWORD", "from-deploy-install")
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	want := config.ClickHouse{
		Address: "127.0.0.1:4200", Database: "solidtrace", User: "solidtrace",
		Password: "from-deploy-install", ConfigDir: "/storage/clickhouse/conf.d",
	}
	if cfg.ClickHouse != want {
		t.Errorf("clickhouse = %+v, want %+v", cfg.ClickHouse, want)
	}
}

func TestTheClickHouseBlockTakesItsKeysFromTheFile(t *testing.T) {
	t.Setenv("SOLIDTRACE_CLICKHOUSE_PASSWORD", "secret")
	cfg := loadFrom(t, writeConfig(t, `clickhouse:
  address: 10.0.0.5:9000
  database: logs_2
  user: event_store
  config_dir: /etc/clickhouse-server/conf.d
`))

	want := config.ClickHouse{
		Address: "10.0.0.5:9000", Database: "logs_2", User: "event_store",
		Password: "secret", ConfigDir: "/etc/clickhouse-server/conf.d",
	}
	if cfg.ClickHouse != want {
		t.Errorf("clickhouse = %+v, want %+v", cfg.ClickHouse, want)
	}
}

func TestABadClickHouseBlockFailsToLoadSayingWhatIsWrong(t *testing.T) {
	cases := []struct{ name, yaml, want string }{
		{"a misspelt key", "clickhouse:\n  adress: 127.0.0.1:4200\n", "line 2: field adress not found"},
		{"an address with no port", "clickhouse:\n  address: localhost\n", `clickhouse: address must be a host and port, such as 127.0.0.1:4200, got "localhost"`},
		{"a database that isn't a plain name", "clickhouse:\n  database: solid-trace\n", `clickhouse: database must be letters, digits and _, not starting with a digit, got "solid-trace"`},
		{"no user", "clickhouse:\n  user: \"\"\n", "clickhouse: user can't be empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := config.LoadFile(writeConfig(t, c.yaml))
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("error = %v, want one saying %q", err, c.want)
			}
		})
	}
}
