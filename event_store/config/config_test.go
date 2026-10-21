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

func TestLogsDuckDBIsNextToEventsDuckDBByDefault(t *testing.T) {
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	if want := "/storage/duckdb/production/logs.duckdb"; cfg.LogsDuckDBPath != want {
		t.Errorf("logs DuckDB path = %q, want %q", cfg.LogsDuckDBPath, want)
	}
}

func TestLogsDuckDBPathComesFromTheConfigFile(t *testing.T) {
	cfg := loadFrom(t, writeConfig(t, "duckdb_path: /data/events.duckdb\nlogs_duckdb_path: /fast/logs.duckdb\n"))

	if want := "/fast/logs.duckdb"; cfg.LogsDuckDBPath != want {
		t.Errorf("logs DuckDB path = %q, want %q", cfg.LogsDuckDBPath, want)
	}
}

func TestLogFilesAreNextToEventsDuckDBByDefault(t *testing.T) {
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	if want := "/storage/duckdb/production/logs_parquet"; cfg.LogsParquetPath != want {
		t.Errorf("Log files path = %q, want %q", cfg.LogsParquetPath, want)
	}
}

func TestLogFilesPathComesFromTheConfigFile(t *testing.T) {
	cfg := loadFrom(t, writeConfig(t, "duckdb_path: /data/events.duckdb\nlogs_parquet_path: /fast/logs_parquet\n"))

	if want := "/fast/logs_parquet"; cfg.LogsParquetPath != want {
		t.Errorf("Log files path = %q, want %q", cfg.LogsParquetPath, want)
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

func TestDuckDBExtensionsAreWhereTheImagePutsThemByDefault(t *testing.T) {
	cfg := loadFrom(t, "../../deploy/event_store.yml")

	if want := "/usr/local/lib/solidtrace/duckdb_extensions"; cfg.DuckDBExtensionDirectory != want {
		t.Errorf("DuckDB extension directory = %q, want %q", cfg.DuckDBExtensionDirectory, want)
	}
}

func TestDuckDBExtensionDirectoryComesFromTheConfigFile(t *testing.T) {
	cfg := loadFrom(t, writeConfig(t, "duckdb_extension_directory: /home/dev/.cache/duckdb_extensions\n"))

	if want := "/home/dev/.cache/duckdb_extensions"; cfg.DuckDBExtensionDirectory != want {
		t.Errorf("DuckDB extension directory = %q, want %q", cfg.DuckDBExtensionDirectory, want)
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
