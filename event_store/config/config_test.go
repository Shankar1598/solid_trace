package config_test

import (
	"os"
	"path/filepath"
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
