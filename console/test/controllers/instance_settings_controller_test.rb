# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# Settings → Instance: the install's settings. Those that apply at once (SQL
# queries, the default Retention, log-all-reads) change here; Sign-up and
# everything that needs a restart are shown read-only. Only an instance admin
# sees or changes them.
class InstanceSettingsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  # EventStore's GET /config: the config it loaded at start, with a bucket,
  # every secret set, and the disk limit left at half of ClickHouse's disk.
  EVENT_STORE_CONFIG = {
    "ingest_port" => "4000", "query_port" => "4100",
    "pebble_path" => "/storage/pebble", "message_queue_path" => "/storage/sqlite/message_queue.sqlite3",
    "sqlite_path" => "/storage/sqlite/solid_trace.sqlite3", "logs_parquet_path" => "/storage/duckdb/logs_parquet",
    "logs_disk_limit" => { "configured_bytes" => 0, "effective_bytes" => 53_687_091_200 },
    "logs_bin_cache_bytes" => 64 << 20, "ingest_max_waiting" => 10_000,
    "clickhouse" => { "address" => "clickhouse:9000", "database" => "solidtrace", "user" => "solidtrace",
      "password_set" => true, "config_dir" => "/storage/clickhouse/conf.d" },
    "object_store" => { "kind" => "gcs", "bucket" => "acme-logs", "prefix" => "solidtrace/", "region" => "auto",
      "endpoint" => "https://storage.googleapis.com", "local_cache_hours" => 24, "key_id_set" => true, "secret_set" => true },
    "cpu_profile_dir" => "", "debug" => false,
    "defaults" => [ "ingest_port", "query_port", "logs_disk_limit_gb", "logs_bin_cache_mb", "ingest_max_waiting",
      "clickhouse.database", "clickhouse.user", "cpu_profile_dir", "debug" ],
  }.freeze

  setup do
    stub_request(:get, %r{/config\z}).to_return(status: 200, body: EVENT_STORE_CONFIG.to_json,
      headers: { "Content-Type" => "application/json" })
    @organization = create(:organization)
    @instance_admin = create(:user, instance_admin: true)
    @organization.organization_users.create!(user: @instance_admin, role: :admin)
    @org_admin = create(:user)
    @organization.organization_users.create!(user: @org_admin, role: :admin)
  end

  test "an Organization admin who isn't an instance admin is refused the page and its update" do
    sign_in_as(@org_admin)

    get instance_settings_url(org_slug: @organization.slug)
    assert_redirected_to root_url
    assert_equal "You are not authorized to perform this action.", flash[:alert]

    patch instance_settings_url(org_slug: @organization.slug),
      params: { instance_settings: { sql_queries_enabled: false, log_all_reads: true } }
    assert_redirected_to root_url
    assert InstanceSetting.current.sql_queries_enabled?
    assert_not InstanceSetting.current.log_all_reads?
  end

  test "an instance admin sees the settings that apply at once, Sign-up, and Console's environment" do
    InstanceSetting.current.update!(sql_queries_enabled: false, default_log_retention_days: 14)
    sign_in_as(@instance_admin)

    get instance_settings_url(org_slug: @organization.slug), headers: inertia

    assert_response :success
    assert_equal "Settings/Instance", response.parsed_body["component"]
    settings = response.parsed_body.dig("props", "settings")
    assert_equal({ "sql_queries_enabled" => false, "sql_queries_description" => InstanceSetting::SQL_QUERIES_DESCRIPTION,
      "default_log_retention_days" => 14, "log_all_reads" => false }, settings["editable"])
    assert_equal false, settings.dig("sign_up", "open")
    assert_equal "bin/rails instance_settings:set SIGN_UP_OPEN=true|false", settings.dig("sign_up", "command")

    console = settings["restart_to_change"].find { |group| group["name"] == "Console" }
    version = console["settings"].find { |row| row["source"] == "SOLIDTRACE_VERSION" }
    assert_equal({ "label" => "Version", "source" => "SOLIDTRACE_VERSION", "value" => "dev", "default" => true,
      "secret" => false }, version)
    assert_equal [ "CONSOLE_URL", "PUBLIC_INGEST_URL", "EVENT_STORE_URL", "EVENT_STORE_INGEST_URL", "SOLIDTRACE_VERSION",
      "RAILS_MAX_THREADS", "JOB_CONCURRENCY", "RAILS_LOG_LEVEL", "SECRET_KEY_BASE", "SENTRY_DSN" ],
      console["settings"].pluck("source")
  end

  test "a setting given in the environment says it isn't the default" do
    sign_in_as(@instance_admin)

    with_env("EVENT_STORE_URL" => "http://10.0.0.5:4100", "JOB_CONCURRENCY" => "3") do
      get instance_settings_url(org_slug: @organization.slug), headers: inertia
    end

    rows = console_rows
    assert_equal [ "http://10.0.0.5:4100", false ], rows["EVENT_STORE_URL"].values_at("value", "default")
    assert_equal [ "3", false ], rows["JOB_CONCURRENCY"].values_at("value", "default")
    assert_equal [ "http://127.0.0.1:4000", true ], rows["EVENT_STORE_INGEST_URL"].values_at("value", "default")
  end

  test "secrets show only whether they are set, never their value" do
    sign_in_as(@instance_admin)

    with_env("SECRET_KEY_BASE" => "k3y-base-v4lue", "SENTRY_DSN" => nil) do
      get instance_settings_url(org_slug: @organization.slug), headers: inertia
    end

    rows = console_rows
    assert_equal({ "label" => "Secret key base", "source" => "SECRET_KEY_BASE", "value" => true, "default" => false,
      "secret" => true }, rows["SECRET_KEY_BASE"])
    assert_equal [ false, true, true ], rows["SENTRY_DSN"].values_at("value", "default", "secret")
    assert_not_includes response.body, "k3y-base-v4lue"
  end

  test "an instance admin sees EventStore's config as it loaded it, each row naming its event_store.yml key" do
    sign_in_as(@instance_admin)

    get instance_settings_url(org_slug: @organization.slug), headers: inertia

    rows = event_store_group["settings"]
    assert_equal [ "ingest_port", "query_port", "clickhouse.address", "clickhouse.database", "clickhouse.user",
      "SOLIDTRACE_CLICKHOUSE_PASSWORD", "clickhouse.config_dir", "pebble_path", "message_queue_path", "sqlite_path",
      "logs_parquet_path", "logs_disk_limit_gb", "logs_bin_cache_mb", "ingest_max_waiting", "object_store.kind",
      "object_store.bucket", "object_store.prefix", "object_store.region", "object_store.endpoint",
      "object_store.local_cache_hours", "SOLIDTRACE_OBJECT_STORE_KEY_ID", "SOLIDTRACE_OBJECT_STORE_SECRET",
      "cpu_profile_dir", "debug" ], rows.pluck("source")
    rows = rows.index_by { |row| row["source"] }
    assert_equal({ "label" => "ClickHouse address", "source" => "clickhouse.address", "value" => "clickhouse:9000",
      "default" => false, "secret" => false }, rows["clickhouse.address"])
    assert_equal [ "solidtrace", true ], rows["clickhouse.user"].values_at("value", "default")
    assert_equal [ "Half of ClickHouse's disk: 53.7 GB (53687091200 bytes)", true ],
      rows["logs_disk_limit_gb"].values_at("value", "default")
    assert_equal "64 MiB", rows["logs_bin_cache_mb"]["value"]
    assert_equal [ 10_000, true ], rows["ingest_max_waiting"].values_at("value", "default")
    assert_equal [ "acme-logs", false ], rows["object_store.bucket"].values_at("value", "default")
    assert_equal 24, rows["object_store.local_cache_hours"]["value"]
    assert_nil rows["cpu_profile_dir"]["value"]
    assert_equal [ false, true ], rows["debug"].values_at("value", "default")
    %w[SOLIDTRACE_CLICKHOUSE_PASSWORD SOLIDTRACE_OBJECT_STORE_KEY_ID SOLIDTRACE_OBJECT_STORE_SECRET].each do |source|
      assert_equal [ true, false, true ], rows[source].values_at("value", "default", "secret"), source
    end
  end

  test "a disk limit given in the file shows its bytes, and no bucket shows as none" do
    stub_request(:get, %r{/config\z}).to_return(status: 200, headers: { "Content-Type" => "application/json" },
      body: EVENT_STORE_CONFIG.merge(
        "logs_disk_limit" => { "configured_bytes" => 100_000_000_000, "effective_bytes" => 100_000_000_000 },
        "object_store" => nil, "defaults" => EVENT_STORE_CONFIG["defaults"] - [ "logs_disk_limit_gb" ] + [ "object_store" ],
      ).to_json)
    sign_in_as(@instance_admin)

    get instance_settings_url(org_slug: @organization.slug), headers: inertia

    rows = event_store_group["settings"].index_by { |row| row["source"] }
    assert_equal [ "100 GB (100000000000 bytes)", false ], rows["logs_disk_limit_gb"].values_at("value", "default")
    assert_equal({ "label" => "Bucket", "source" => "object_store", "value" => "None, Logs stay on local disk",
      "default" => true, "secret" => false }, rows["object_store"])
    assert_empty rows.keys.grep(/\Aobject_store\.|OBJECT_STORE/)
  end

  test "with EventStore unreachable, the page still renders and the EventStore group says it's unavailable" do
    stub_request(:get, %r{/config\z}).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@instance_admin)

    get instance_settings_url(org_slug: @organization.slug), headers: inertia

    assert_response :success
    group = event_store_group
    assert_nil group["settings"]
    assert_match(/\ACouldn't read EventStore's config: .*refused/i, group["unavailable"])
    assert_equal 30, response.parsed_body.dig("props", "settings", "editable", "default_log_retention_days")
    assert console_rows.present?
  end

  test "with EventStore too slow to answer, the EventStore group says it timed out" do
    stub_request(:get, %r{/config\z}).to_timeout
    sign_in_as(@instance_admin)

    get instance_settings_url(org_slug: @organization.slug), headers: inertia

    assert_response :success
    assert_match(/\ACouldn't read EventStore's config: .*timed out after \d+s/i, event_store_group["unavailable"])
  end

  test "saving changes SQL queries, the default Retention and log-all-reads, and the page shows them" do
    sign_in_as(@instance_admin)

    save(sql_queries_enabled: false, default_log_retention_days: "7", log_all_reads: true)

    assert_redirected_to instance_settings_url(org_slug: @organization.slug)
    assert_equal "Instance settings saved", flash[:notice]
    get instance_settings_url(org_slug: @organization.slug), headers: inertia
    assert_equal [ false, 7, true ], response.parsed_body.dig("props", "settings", "editable")
      .values_at("sql_queries_enabled", "default_log_retention_days", "log_all_reads")
  end

  test "a Retention below one day, or not a whole number, comes back as a form error and saves nothing" do
    sign_in_as(@instance_admin)

    [ "0", "1.5", "seven" ].each do |days|
      save(default_log_retention_days: days, log_all_reads: true)

      assert_redirected_to instance_settings_url(org_slug: @organization.slug)
      get instance_settings_url(org_slug: @organization.slug), headers: inertia
      assert response.parsed_body.dig("props", "errors", "default_log_retention_days").present?, days
      assert_equal 30, InstanceSetting.current.default_log_retention_days
      assert_not InstanceSetting.current.log_all_reads?
    end
  end

  test "an update carrying Sign-up or any other setting changes nothing but the three" do
    sign_in_as(@instance_admin)

    save(sign_up_open: true, smtp_address: "evil.example.com", meta_organization_seeded: true, log_all_reads: true)

    settings = InstanceSetting.current
    assert settings.log_all_reads?
    assert_not settings.sign_up_open?
    assert_nil settings.smtp_address
    assert_not settings.meta_organization_seeded?
  end

  private

  def event_store_group
    response.parsed_body.dig("props", "settings", "restart_to_change").find { |group| group["name"] == "EventStore" }
  end

  def console_rows
    response.parsed_body.dig("props", "settings", "restart_to_change")
      .find { |group| group["name"] == "Console" }["settings"].index_by { |row| row["source"] }
  end

  def with_env(values)
    previous = values.keys.index_with { |key| ENV[key] }
    values.each { |key, value| ENV[key] = value }
    yield
  ensure
    previous.each { |key, value| ENV[key] = value }
  end

  def save(**settings)
    patch instance_settings_url(org_slug: @organization.slug), params: { instance_settings: settings }, as: :json
  end

  def inertia
    { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
  end
end
