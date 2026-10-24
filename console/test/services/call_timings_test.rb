# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The lines CallTimings writes for MCP tool calls and Log reads, with
# EventStore stubbed.
class CallTimingsTest < ActiveSupport::TestCase
  include McpTestHelper

  FROM = Time.utc(2026, 10, 21, 9)
  TO = Time.utc(2026, 10, 21, 10)

  setup do
    @output = StringIO.new
    @previous_output = Rails.configuration.x.call_timings_output
    Rails.configuration.x.call_timings_output = @output
    @previous_version = Rails.configuration.x.solidtrace_version
    Rails.configuration.x.solidtrace_version = "v0.8.0"

    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @shop = create(:project, organization: @acme, slug: "shop")
    @query_url = "#{EventStore.base_url}/api/#{@shop.id}/logs/query"
    # Every read is timed, not only the MCP server's; the tests below that
    # turn it off say so.
    InstanceSetting.current.update!(log_all_reads: true)
  end

  teardown do
    Rails.configuration.x.call_timings_output = @previous_output
    Rails.configuration.x.solidtrace_version = @previous_version
  end

  test "a Query's run is a log_read line with its Query, range, rows, origin, time taken and the release that ran it" do
    stub_request(:post, @query_url).to_return(status: 200, body: { logs: [ { id: "a" }, { id: "b" } ] }.to_json)

    CallTimings::Context.set(origin: "logs#query", request_id: "req-1") do
      EventStore.query_logs(project_id: @shop.id, query: "where level == \"error\"", from: FROM, to: TO)
    end

    line = sole_line
    assert_equal({
      "level" => "info", "kind" => "log_read", "outcome" => "ok", "read" => "query", "origin" => "logs#query",
      "request_id" => "req-1", "version" => "v0.8.0", "project_id" => @shop.id, "query" => "where level == \"error\"",
      "from" => "2026-10-21T09:00:00.000Z", "to" => "2026-10-21T10:00:00.000Z", "rows" => 2,
    }, line.except("time", "message", "duration_ms"))
    assert_kind_of Numeric, line["duration_ms"]
    assert_match(/\ALog read query, [\d.]+ ms\z/, line["message"])
  end

  test "a read asks EventStore to log its SQL with the read's request id and user, the name percent-encoded" do
    @user.update!(name: "Ana María")
    run = stub_request(:post, @query_url).with(headers: {
      "X-Log-SQL" => "1", "X-Request-Id" => "req-1", "X-User-Email" => @user.email, "X-User-Name" => "Ana%20Mar%C3%ADa",
    }).to_return(status: 200, body: { logs: [] }.to_json)

    CallTimings::Context.set(request_id: "req-1", user: @user) do
      EventStore.query_logs(project_id: @shop.id, query: "", from: FROM, to: TO)
    end

    assert_requested run
  end

  test "a table's rows and a histogram's buckets are counted" do
    stub_request(:post, @query_url).to_return(status: 200, body: { table: { columns: [], rows: [ [ 1 ], [ 2 ], [ 3 ] ] } }.to_json)
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/histogram")
      .to_return(status: 200, body: { buckets: [ { count: 1 } ], count: 1 }.to_json)

    EventStore.query_logs(project_id: @shop.id, query: "summarize count()", from: FROM, to: TO)
    EventStore.logs_histogram(project_id: @shop.id, query: "", from: FROM, to: TO)

    assert_equal [ [ "query", 3 ], [ "histogram", 1 ] ], lines.map { |line| line.values_at("read", "rows") }
  end

  test "a refused, timed out or unreachable read says so, warns and still raises" do
    stub_request(:post, @query_url).to_return(
      { status: 400, body: { errors: [ { message: "unknown field" } ] }.to_json },
      { status: 504, body: { error: "timed out" }.to_json },
      { status: 500, body: "boom" },
    )

    assert_raises(EventStore::Rejected) { EventStore.query_logs(project_id: @shop.id, query: "where nope", from: FROM, to: TO) }
    assert_raises(EventStore::TimedOut) { EventStore.query_logs(project_id: @shop.id, query: "", from: FROM, to: TO) }
    assert_raises(EventStore::Unavailable) { EventStore.query_logs(project_id: @shop.id, query: "", from: FROM, to: TO) }

    assert_equal [ "rejected", "timed_out", "unavailable" ], lines.map { |line| line["outcome"] }
    assert_equal [ "warn" ], lines.map { |line| line["level"] }.uniq
    assert lines.all? { |line| line["error"].present? }
  end

  test "SQL is timed with its text, and refused while SQL queries are off" do
    InstanceSetting.current.update!(sql_queries_enabled: false)

    assert_raises(EventStore::SQLQueriesOff) { EventStore.run_logs_sql(project_id: @shop.id, sql: "select 1", from: FROM, to: TO) }

    assert_equal [ "sql", "select 1", "sql_off" ], sole_line.values_at("read", "query", "outcome")
  end

  test "an MCP tool call is an mcp_tool line with its arguments and user, and its Log reads have its origin" do
    stub_request(:post, @query_url).to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)

    call_tool(:query_logs, user: @user, project: "shop", query: "where level == \"error\"", range: "1h")

    read, tool = lines
    assert_equal [ "log_read", "mcp:query_logs", @user.id, @user.email, @user.name ],
      read.values_at("kind", "origin", "user_id", "user_email", "user_name")
    assert_equal({
      "level" => "info", "kind" => "mcp_tool", "outcome" => "ok", "tool" => "query_logs",
      "arguments" => { "project" => "shop", "query" => "where level == \"error\"", "range" => "1h" },
      "user_id" => @user.id, "user_email" => @user.email, "user_name" => @user.name, "version" => "v0.8.0",
    }, tool.except("time", "message", "duration_ms"))
    assert_match(/\AMCP tool query_logs, [\d.]+ ms\z/, tool["message"])
  end

  test "an MCP tool call's secret arguments, such as the SMTP password, are filtered from its line" do
    @user.update!(instance_admin: true)

    call_tool(:update_email_settings, user: @user, smtp_username: "apikey", smtp_password: "s3cret-pass")

    assert_equal({ "smtp_username" => "apikey", "smtp_password" => "[FILTERED]" }, sole_line["arguments"])
    assert_not_includes @output.string, "s3cret-pass"
  end

  test "a tool's error answer is an error outcome with its text" do
    call_tool(:query_logs, user: @user, project: "nowhere", query: "")

    assert_equal [ "mcp_tool", "error", "warn" ], sole_line.values_at("kind", "outcome", "level")
    assert_includes sole_line["error"], "No Project nowhere"
  end

  test "with log_all_reads off, only the MCP server's reads are timed and have their SQL logged" do
    InstanceSetting.current.update!(log_all_reads: false)
    quiet = stub_request(:post, @query_url).with { |request| !request.headers.key?("X-Log-Sql") }
      .to_return(status: 200, body: { logs: [] }.to_json)

    CallTimings::Context.set(origin: "dashboard_charts#data") do
      EventStore.query_logs(project_id: @shop.id, query: "", from: FROM, to: TO)
    end
    assert_requested quiet
    assert_empty lines

    WebMock.reset!
    logged = stub_request(:post, @query_url).with(headers: { "X-Log-SQL" => "1" }).to_return(status: 200, body: { logs: [] }.to_json)
    call_tool(:query_logs, user: @user, project: "shop", query: "")

    assert_requested logged
    assert_equal [ "log_read", "mcp_tool" ], lines.map { |line| line["kind"] }
  end

  test "with log_mcp_reads off too, the MCP server's reads aren't timed but its tool calls are" do
    InstanceSetting.current.update!(log_all_reads: false, log_mcp_reads: false)
    quiet = stub_request(:post, @query_url).with { |request| !request.headers.key?("X-Log-Sql") }
      .to_return(status: 200, body: { logs: [] }.to_json)

    call_tool(:query_logs, user: @user, project: "shop", query: "")

    assert_requested quiet
    assert_equal [ "mcp_tool" ], lines.map { |line| line["kind"] }
  end

  test "with log_all_reads on, the MCP server's reads are timed even with log_mcp_reads off" do
    InstanceSetting.current.update!(log_all_reads: true, log_mcp_reads: false)
    logged = stub_request(:post, @query_url).with(headers: { "X-Log-SQL" => "1" }).to_return(status: 200, body: { logs: [] }.to_json)

    call_tool(:query_logs, user: @user, project: "shop", query: "")

    assert_requested logged
    assert_equal [ "log_read", "mcp_tool" ], lines.map { |line| line["kind"] }
  end

  test "a line that can't be written doesn't fail the read" do
    closed = StringIO.new
    closed.close
    Rails.configuration.x.call_timings_output = closed
    stub_request(:post, @query_url).to_return(status: 200, body: { logs: [] }.to_json)

    assert_equal({ "logs" => [] }, EventStore.query_logs(project_id: @shop.id, query: "", from: FROM, to: TO))
  end

  private

  def lines
    @output.string.lines.map { |line| JSON.parse(line) }
  end

  def sole_line
    lines.sole
  end
end
