package handler

import (
	"github.com/gofiber/fiber/v2"
	"github.com/solidtrace/event_store/config"
)

// ConfigHandler answers the config EventStore loaded at start, for Console's
// Settings → Instance. Secrets are answered only as whether they're set.
// The DuckDB-era warnings and the test-only storage policy are left out.
type ConfigHandler struct {
	answer configAnswer
}

type configAnswer struct {
	IngestPort         string             `json:"ingest_port"`
	QueryPort          string             `json:"query_port"`
	PebblePath         string             `json:"pebble_path"`
	MessageQueuePath   string             `json:"message_queue_path"`
	SQLitePath         string             `json:"sqlite_path"`
	LogsParquetPath    string             `json:"logs_parquet_path"`
	LogsDiskLimit      diskLimitAnswer    `json:"logs_disk_limit"`
	LogsBinCacheBytes  int64              `json:"logs_bin_cache_bytes"`
	LogsHeavyReadSlots int                `json:"logs_heavy_read_slots"`
	IngestMaxWaiting   int                `json:"ingest_max_waiting"`
	ClickHouse         clickHouseAnswer   `json:"clickhouse"`
	ObjectStore        *objectStoreAnswer `json:"object_store"`
	CPUProfileDir      string             `json:"cpu_profile_dir"`
	Debug              bool               `json:"debug"`
	// Defaults are the file's keys whose value is EventStore's default
	// (config.Defaults), so the page can say so.
	Defaults []string `json:"defaults"`
}

// diskLimitAnswer is the disk limit as configured, 0 for the default of
// half ClickHouse's local disk, and the bytes worked out from it at start.
type diskLimitAnswer struct {
	ConfiguredBytes int64 `json:"configured_bytes"`
	EffectiveBytes  int64 `json:"effective_bytes"`
}

type clickHouseAnswer struct {
	Address     string `json:"address"`
	Database    string `json:"database"`
	User        string `json:"user"`
	PasswordSet bool   `json:"password_set"`
	ConfigDir   string `json:"config_dir"`
}

type objectStoreAnswer struct {
	Kind            string `json:"kind"`
	Bucket          string `json:"bucket"`
	Prefix          string `json:"prefix"`
	Region          string `json:"region"`
	Endpoint        string `json:"endpoint"`
	LocalCacheHours int    `json:"local_cache_hours"`
	KeyIDSet        bool   `json:"key_id_set"`
	SecretSet       bool   `json:"secret_set"`
}

// NewConfigHandler answers cfg, with diskLimitBytes the disk limit in effect
// (storage.ClickHouseLogs.DiskLimit).
func NewConfigHandler(cfg *config.Config, diskLimitBytes int64) *ConfigHandler {
	answer := configAnswer{
		IngestPort:         cfg.Port,
		QueryPort:          cfg.QueryPort,
		PebblePath:         cfg.PebblePath,
		MessageQueuePath:   cfg.MessageQueuePath,
		SQLitePath:         cfg.SQLitePath,
		LogsParquetPath:    cfg.LogsParquetPath,
		LogsDiskLimit:      diskLimitAnswer{ConfiguredBytes: cfg.LogsDiskLimitBytes, EffectiveBytes: diskLimitBytes},
		LogsBinCacheBytes:  cfg.LogsBinCacheBytes,
		LogsHeavyReadSlots: cfg.LogsHeavyReadSlots,
		IngestMaxWaiting:   cfg.IngestMaxWaiting,
		ClickHouse: clickHouseAnswer{
			Address:     cfg.ClickHouse.Address,
			Database:    cfg.ClickHouse.Database,
			User:        cfg.ClickHouse.User,
			PasswordSet: cfg.ClickHouse.Password != "",
			ConfigDir:   cfg.ClickHouse.ConfigDir,
		},
		CPUProfileDir: cfg.CPUProfileDir,
		Debug:         cfg.Debug,
	}
	if store := cfg.ObjectStore; store != nil {
		answer.ObjectStore = &objectStoreAnswer{
			Kind:            store.Kind,
			Bucket:          store.Bucket,
			Prefix:          store.Prefix,
			Region:          store.Region,
			Endpoint:        store.Endpoint,
			LocalCacheHours: store.LocalCacheHours,
			KeyIDSet:        store.KeyID != "",
			SecretSet:       store.Secret != "",
		}
	}
	answer.Defaults = defaultKeys(cfg)
	return &ConfigHandler{answer: answer}
}

// defaultKeys lists the file's keys whose value in cfg is the default.
func defaultKeys(cfg *config.Config) []string {
	d := config.Defaults()
	keys := []string{}
	for _, key := range []struct {
		name      string
		isDefault bool
	}{
		{"ingest_port", cfg.Port == d.Port},
		{"query_port", cfg.QueryPort == d.QueryPort},
		{"pebble_path", cfg.PebblePath == d.PebblePath},
		{"message_queue_path", cfg.MessageQueuePath == d.MessageQueuePath},
		{"sqlite_path", cfg.SQLitePath == d.SQLitePath},
		{"logs_parquet_path", cfg.LogsParquetPath == d.LogsParquetPath},
		{"logs_disk_limit_gb", cfg.LogsDiskLimitBytes == d.LogsDiskLimitBytes},
		{"logs_bin_cache_mb", cfg.LogsBinCacheBytes == d.LogsBinCacheBytes},
		{"logs_heavy_read_slots", cfg.LogsHeavyReadSlots == d.LogsHeavyReadSlots},
		{"ingest_max_waiting", cfg.IngestMaxWaiting == d.IngestMaxWaiting},
		{"clickhouse.address", cfg.ClickHouse.Address == d.ClickHouse.Address},
		{"clickhouse.database", cfg.ClickHouse.Database == d.ClickHouse.Database},
		{"clickhouse.user", cfg.ClickHouse.User == d.ClickHouse.User},
		{"clickhouse.config_dir", cfg.ClickHouse.ConfigDir == d.ClickHouse.ConfigDir},
		{"object_store", cfg.ObjectStore == nil},
		{"cpu_profile_dir", cfg.CPUProfileDir == d.CPUProfileDir},
		{"debug", cfg.Debug == d.Debug},
	} {
		if key.isDefault {
			keys = append(keys, key.name)
		}
	}
	return keys
}

// Config answers the loaded config.
// GET /config
func (h *ConfigHandler) Config(c *fiber.Ctx) error {
	return c.JSON(h.answer)
}
