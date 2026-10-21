# frozen_string_literal: true

require "test_helper"
require "rake"

# The operator's side of the instance settings: there is no UI for them.
class InstanceSettingsTaskTest < ActiveSupport::TestCase
  setup do
    Rails.application.load_tasks unless Rake::Task.task_defined?("instance_settings:set")
  end

  test "a fresh install has SQL queries on and a 30-day default Log Retention" do
    out = run_task("instance_settings:show")

    assert_includes out, "SQL queries: on"
    assert_includes out, "Default Log Retention: 30 days"
    assert InstanceSetting.current.sql_queries_enabled?
    assert_equal 30, InstanceSetting.current.default_log_retention_days
  end

  test "the operator turns SQL queries on and is told what that allows" do
    out = run_task("instance_settings:set", "SQL_QUERIES_ENABLED" => "true")

    assert InstanceSetting.current.sql_queries_enabled?
    assert_includes out, "SQL queries: on"
    [ "read-only", "read only the logs and events views", "their own Organization's Projects", "starve ingest" ].each do |allowed|
      assert_includes out, allowed
    end
    [ "every Organization's Logs", "read files on the server", "read the bucket", "web address" ].each do |refused|
      assert_not_includes out, refused
    end
  end

  test "the operator turns SQL queries back off" do
    run_task("instance_settings:set", "SQL_QUERIES_ENABLED" => "on")
    run_task("instance_settings:set", "SQL_QUERIES_ENABLED" => "off")

    assert_not InstanceSetting.current.sql_queries_enabled?
  end

  test "a fresh install has sign-up closed" do
    out = run_task("instance_settings:show")

    assert_includes out, "Sign-up: closed"
    assert_not InstanceSetting.current.sign_up_open?
  end

  test "the operator opens sign-up and closes it again" do
    out = run_task("instance_settings:set", "SIGN_UP_OPEN" => "true")

    assert InstanceSetting.current.sign_up_open?
    assert_includes out, "Sign-up: open"

    run_task("instance_settings:set", "SIGN_UP_OPEN" => "off")

    assert_not InstanceSetting.current.sign_up_open?
  end

  test "a fresh install times only the MCP server's reads, and the operator turns on every read's" do
    assert_includes run_task("instance_settings:show"), "Log all reads: off: only the MCP server's"

    out = run_task("instance_settings:set", "LOG_ALL_READS" => "true")

    assert InstanceSetting.current.log_all_reads?
    assert_includes out, "Log all reads: on"
  end

  test "the operator sets the default Log Retention" do
    out = run_task("instance_settings:set", "DEFAULT_LOG_RETENTION_DAYS" => "14")

    assert_equal 14, InstanceSetting.current.default_log_retention_days
    assert_includes out, "Default Log Retention: 14 days"
  end

  test "invalid values are rejected and change nothing" do
    [ "0", "-1", "1.5", "two" ].each do |days|
      assert_raises(SystemExit, "#{days.inspect} should be rejected") do
        run_task("instance_settings:set", "DEFAULT_LOG_RETENTION_DAYS" => days)
      end
    end
    assert_raises(SystemExit) do
      run_task("instance_settings:set", "SQL_QUERIES_ENABLED" => "yes please", "DEFAULT_LOG_RETENTION_DAYS" => "7")
    end
    assert_raises(SystemExit) { run_task("instance_settings:set", "SIGN_UP_OPEN" => "maybe") }
    assert_raises(SystemExit) { run_task("instance_settings:set") }

    assert InstanceSetting.current.sql_queries_enabled?
    assert_not InstanceSetting.current.sign_up_open?
    assert_equal 30, InstanceSetting.current.default_log_retention_days
  end

  private

  def run_task(name, env = {})
    previous = env.keys.index_with { |key| ENV[key] }
    env.each { |key, value| ENV[key] = value }
    task = Rake::Task[name]
    task.reenable
    out, _err = capture_io { task.invoke }
    out
  ensure
    previous.each { |key, value| ENV[key] = value }
  end
end
