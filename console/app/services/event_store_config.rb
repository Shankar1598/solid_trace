# frozen_string_literal: true

# EventStore's config as it loaded it at start (EventStore.config), as rows
# like ConsoleEnvironment's: each setting's label, its event_store.yml key
# (or, for a secret, the env var it comes from), the value in effect, whether
# that's the default, and whether it's a secret. A secret's value is only
# whether it's set. Changing any of them takes a restart.
class EventStoreConfig
  # config is EventStore's GET /config answer.
  def initialize(config)
    @config = config
    @defaults = config.fetch("defaults", [])
  end

  def settings
    clickhouse = @config["clickhouse"]
    [
      row("Ingest port", "ingest_port", @config["ingest_port"]),
      row("Query port", "query_port", @config["query_port"]),
      row("ClickHouse address", "clickhouse.address", clickhouse["address"]),
      row("ClickHouse database", "clickhouse.database", clickhouse["database"]),
      row("ClickHouse user", "clickhouse.user", clickhouse["user"]),
      secret_row("ClickHouse password", "SOLIDTRACE_CLICKHOUSE_PASSWORD", clickhouse["password_set"]),
      row("ClickHouse config directory", "clickhouse.config_dir", clickhouse["config_dir"]),
      row("Pebble", "pebble_path", @config["pebble_path"]),
      row("Message queue", "message_queue_path", @config["message_queue_path"]),
      row("Console's SQLite", "sqlite_path", @config["sqlite_path"]),
      row("Log file store", "logs_parquet_path", @config["logs_parquet_path"]),
      row("Disk limit", "logs_disk_limit_gb", disk_limit),
      row("Bin cache", "logs_bin_cache_mb", bin_cache),
      row("Most ingest requests waiting", "ingest_max_waiting", @config["ingest_max_waiting"]),
      *bucket_rows,
      row("CPU profile directory", "cpu_profile_dir", @config["cpu_profile_dir"]),
      row("Debug", "debug", @config["debug"])
    ]
  end

  private

  def row(label, source, value)
    value = nil if value == ""
    { label: label, source: source, value: value, default: @defaults.include?(source), secret: false }
  end

  def secret_row(label, source, set)
    { label: label, source: source, value: set, default: !set, secret: true }
  end

  # The bucket older Logs move to, or a row saying there's none.
  def bucket_rows
    store = @config["object_store"]
    return [ row("Bucket", "object_store", "None, Logs stay on local disk") ] if store.nil?

    [
      row("Bucket kind", "object_store.kind", store["kind"]),
      row("Bucket", "object_store.bucket", store["bucket"]),
      row("Bucket prefix", "object_store.prefix", store["prefix"]),
      row("Bucket region", "object_store.region", store["region"]),
      row("Bucket endpoint", "object_store.endpoint", store["endpoint"]),
      row("Hours on local disk", "object_store.local_cache_hours", store["local_cache_hours"]),
      secret_row("Bucket key id", "SOLIDTRACE_OBJECT_STORE_KEY_ID", store["key_id_set"]),
      secret_row("Bucket secret", "SOLIDTRACE_OBJECT_STORE_SECRET", store["secret_set"])
    ]
  end

  # The disk limit in decimal GB, as logs_disk_limit_gb gives it, and in
  # bytes. Left out of the file, it's half of ClickHouse's disk as EventStore
  # worked it out at start.
  def disk_limit
    limit = @config["logs_disk_limit"]
    bytes = limit["effective_bytes"]
    size = "#{(bytes / 1e9).round(1).to_s.delete_suffix('.0')} GB (#{bytes} bytes)"
    limit["configured_bytes"].to_i.positive? ? size : "Half of ClickHouse's disk: #{size}"
  end

  def bin_cache
    bytes = @config["logs_bin_cache_bytes"]
    bytes.zero? ? "Off" : "#{bytes >> 20} MiB"
  end
end
