# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The instance settings tools: the install's settings as Settings → Instance
# shows them, and the four that apply at once, which only an instance admin
# reads or changes. They take no Organization, never answer a secret, and
# can't change Sign-up or anything that needs a restart.
class McpInstanceSettingsToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    # EventStore's config, with its ClickHouse password set and no bucket.
    stub_request(:get, %r{/config\z}).to_return(status: 200, headers: { "Content-Type" => "application/json" },
      body: { "ingest_port" => "4000", "query_port" => "4100", "pebble_path" => "/storage/pebble",
        "message_queue_path" => "/storage/mq.sqlite3", "sqlite_path" => "/storage/console.sqlite3",
        "logs_parquet_path" => "/storage/logs_parquet",
        "logs_disk_limit" => { "configured_bytes" => 0, "effective_bytes" => 50_000_000_000 },
        "logs_bin_cache_bytes" => 64 << 20, "logs_heavy_read_slots" => 1, "ingest_max_waiting" => 10_000,
        "clickhouse" => { "address" => "clickhouse:9000", "database" => "solidtrace", "user" => "solidtrace",
          "password_set" => true, "config_dir" => "" },
        "object_store" => nil, "cpu_profile_dir" => "", "debug" => false, "defaults" => [] }.to_json)
    @acme =create(:organization, name: "Acme", slug: "acme")
    @instance_admin = create(:user, name: "Grace", email: "grace@example.com", instance_admin: true)
    create(:organization_user, organization: @acme, user: @instance_admin, role: :admin)
    @org_admin = create(:user, name: "Ada", email: "ada@example.com")
    create(:organization_user, organization: @acme, user: @org_admin, role: :admin)
  end

  test "get_instance_settings answers the page's sections, never a secret's value, and links to the page" do
    InstanceSetting.current.update!(sql_queries_enabled: false, default_log_retention_days: 14)

    answer = with_env("SECRET_KEY_BASE" => "k3y-base-v4lue", "SENTRY_DSN" => "https://s3cret@sentry.example/1") do
      call_tool(:get_instance_settings, user: @instance_admin)
    end

    assert_not answer.error?, answer.text
    assert_equal({ "sql_queries_enabled" => false, "sql_queries_description" => InstanceSetting::SQL_QUERIES_DESCRIPTION,
      "default_log_retention_days" => 14, "default_trace_retention_days" => 14, "log_all_reads" => false,
      "log_mcp_reads" => true, "send_console_issues" => true, "trace_console" => true,
      "logs_text_match_budget_mb" => 4096 }, answer.json["editable"])
    assert_equal({ "open" => false, "command" => "bin/rails instance_settings:set SIGN_UP_OPEN=true|false" },
      answer.json["sign_up"])
    console = answer.json["restart_to_change"].find { |group| group["name"] == "Console" }
    assert_equal [ true, true ], console["settings"].select { |row| row["secret"] }.pluck("value")
    assert_not_includes answer.text, "k3y-base-v4lue"
    assert_not_includes answer.text, "s3cret"
    assert_equal "http://console.test/acme/settings/instance", answer.link
  end

  test "update_instance_settings changes the settings given, and answers the settings" do
    answer = call_tool(:update_instance_settings, user: @instance_admin, sql_queries_enabled: false,
      default_log_retention_days: 7, log_all_reads: true)

    assert_not answer.error?, answer.text
    settings = InstanceSetting.current
    assert_equal [ false, 7, true ], [ settings.sql_queries_enabled, settings.default_log_retention_days, settings.log_all_reads ]
    assert_equal [ false, 7, true ],
      answer.json["editable"].values_at("sql_queries_enabled", "default_log_retention_days", "log_all_reads")
    assert_equal "http://console.test/acme/settings/instance", answer.link

    call_tool(:update_instance_settings, user: @instance_admin, log_all_reads: false)
    settings = InstanceSetting.current
    assert_equal [ false, 7, false ], [ settings.sql_queries_enabled, settings.default_log_retention_days, settings.log_all_reads ]
  end

  test "update_instance_settings changes the default trace Retention, and refuses one below a day" do
    answer = call_tool(:update_instance_settings, user: @instance_admin, default_trace_retention_days: 3)

    assert_not answer.error?, answer.text
    assert_equal 3, InstanceSetting.current.default_trace_retention_days
    assert_equal 3, answer.json.dig("editable", "default_trace_retention_days")

    refused = call_tool(:update_instance_settings, user: @instance_admin, default_trace_retention_days: 0)
    assert_equal "Not saved: Default trace retention days must be greater than 0.", refused.text
    assert_equal 3, InstanceSetting.current.default_trace_retention_days
  end

  test "update_instance_settings changes the text-match budget, to 0 too, and refuses a negative one" do
    answer = call_tool(:update_instance_settings, user: @instance_admin, logs_text_match_budget_mb: 10_240)

    assert_not answer.error?, answer.text
    assert_equal 10_240, InstanceSetting.current.logs_text_match_budget_mb
    assert_equal 10_240, answer.json.dig("editable", "logs_text_match_budget_mb")

    assert_not call_tool(:update_instance_settings, user: @instance_admin, logs_text_match_budget_mb: 0).error?
    assert_equal 0, InstanceSetting.current.logs_text_match_budget_mb

    refused = call_tool(:update_instance_settings, user: @instance_admin, logs_text_match_budget_mb: -1)
    assert_equal "Not saved: Text-match budget (MiB) must be greater than or equal to 0.", refused.text
    assert_equal 0, InstanceSetting.current.logs_text_match_budget_mb
    schema = McpTools::UpdateInstanceSettings.input_schema_value.to_h
    assert_equal "integer", schema.dig(:properties, :logs_text_match_budget_mb, :type)
  end

  test "update_instance_settings turns MCP reads' logging off, even while log-all-reads is on" do
    InstanceSetting.current.update!(log_all_reads: true)

    answer = call_tool(:update_instance_settings, user: @instance_admin, log_mcp_reads: false)

    assert_not answer.error?, answer.text
    assert_not InstanceSetting.current.log_mcp_reads?
    assert_equal [ true, false ], answer.json["editable"].values_at("log_all_reads", "log_mcp_reads")
  end

  test "update_instance_settings turns off sending Console's Issues and tracing Console" do
    answer = call_tool(:update_instance_settings, user: @instance_admin, send_console_issues: false, trace_console: false)

    assert_not answer.error?, answer.text
    settings = InstanceSetting.current
    assert_equal [ false, false ], [ settings.send_console_issues, settings.trace_console ]
    assert_equal [ false, false ], answer.json["editable"].values_at("send_console_issues", "trace_console")
    schema = McpTools::UpdateInstanceSettings.input_schema_value.to_h
    assert_equal [ "boolean", "boolean" ], schema[:properties].values_at(:send_console_issues, :trace_console).pluck(:type)
  end

  test "update_instance_settings takes log_mcp_reads as a boolean" do
    schema = McpTools::UpdateInstanceSettings.input_schema_value.to_h
    assert_equal "boolean", schema.dig(:properties, :log_mcp_reads, :type)
  end

  test "update_instance_settings answers a bad Retention as a tool error, and saves nothing" do
    answer = call_tool(:update_instance_settings, user: @instance_admin, log_all_reads: true, default_log_retention_days: 0)

    assert answer.error?
    assert_equal "Not saved: Default log retention days must be greater than 0.", answer.text
    assert_not InstanceSetting.current.log_all_reads?
    assert_equal 30, InstanceSetting.current.default_log_retention_days
  end

  test "update_instance_settings can't open Sign-up" do
    answer = call_tool(:update_instance_settings, user: @instance_admin, sign_up_open: true, log_all_reads: true)

    assert_not answer.error?, answer.text
    assert InstanceSetting.current.log_all_reads?
    assert_not InstanceSetting.current.sign_up_open?
    assert_equal false, answer.json.dig("sign_up", "open")
  end

  test "an Organization admin who isn't an instance admin is refused by each tool, and changes nothing" do
    {
      get_instance_settings: [ {}, "read the instance settings" ],
      update_instance_settings: [ { sql_queries_enabled: false, log_all_reads: true, default_trace_retention_days: 3 },
        "change the instance settings" ],
    }.each do |tool, (arguments, doing)|
      answer = call_tool(tool, user: @org_admin, **arguments)
      assert answer.error?, tool
      assert_equal "Only an instance admin can #{doing}.", answer.text
    end
    assert InstanceSetting.current.sql_queries_enabled?
    assert_not InstanceSetting.current.log_all_reads?
    assert_equal 14, InstanceSetting.current.default_trace_retention_days
  end

  private

  def with_env(values)
    previous = values.keys.index_with { |key| ENV[key] }
    values.each { |key, value| ENV[key] = value }
    yield
  ensure
    previous.each { |key, value| ENV[key] = value }
  end
end
