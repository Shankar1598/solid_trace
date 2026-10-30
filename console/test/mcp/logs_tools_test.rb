# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Logs tools: a Query's Logs or table, one Log, a Project's Fields, the
# Query language, and SQL while it's on, each answer linking to the Logs
# explorer, with EventStore stubbed.
class McpLogsToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  NOW = Time.utc(2026, 10, 17, 10)
  ERROR_LOG = {
    "id" => "0199a8b2-6a1e-7c3d-9f00-000000000002", "time" => "2026-10-17T09:59:58.123Z", "level" => "error",
    "service" => "checkout", "message" => "Payment failed:\n\"card declined\"", "trace_id" => "abc",
    "attributes" => { "status" => 402, "code" => "402", "retry" => false },
  }.freeze
  BARE_LOG = { "id" => "0199a8b2-6a1e-7c3d-9f00-000000000001", "time" => "2026-10-17T09:30:00Z", "message" => "started", "attributes" => {} }.freeze

  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    @their_project = create(:project, organization: create(:organization, slug: "theirs"), slug: "secret")
    @query_url = "#{EventStore.base_url}/api/#{@shop.id}/logs/query"
  end

  # query_logs

  test "query_logs answers Logs one a line, newest first, with the cursor of older ones, linking to the explorer" do
    travel_to NOW do
      stub_request(:post, @query_url)
        .with(body: { query: "where level == \"error\"", from: "2026-10-17T09:00:00.000Z", to: "2026-10-17T10:00:00.000Z",
                      limit: 50, timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS, check_budget: true }.to_json)
        .to_return(status: 200, body: { logs: [ ERROR_LOG, BARE_LOG ], next_cursor: "c2", elapsed_ms: 12 }.to_json)

      answer = call_tool(:query_logs, user: @user, project: "shop", query: "where level == \"error\"")

      assert_not answer.error?
      assert_equal [
        "2 Logs of shop from 2026-10-17T09:00:00.000Z to 2026-10-17T10:00:00.000Z, newest first. " \
        "A line is time, level, service, id, message, then up to 8 Attributes as name=value; get_log gives the whole Log.",
        "2026-10-17T09:59:58.123Z error checkout 0199a8b2-6a1e-7c3d-9f00-000000000002 \"Payment failed:\\n\\\"card declined\\\"\" " \
        "status=402 code=\"402\" retry=false",
        "2026-10-17T09:30:00Z - - 0199a8b2-6a1e-7c3d-9f00-000000000001 \"started\"",
        "Older Logs: call query_logs again with the same query, cursor \"c2\", from 2026-10-17T09:00:00.000Z and to 2026-10-17T10:00:00.000Z."
      ], answer.body.lines(chomp: true)
      assert_explorer({ "range" => "1h", "mode" => "query", "q" => "where level == \"error\"" }, answer.link)
    end
  end

  test "query_logs reads the page a cursor names, as many Logs as limit asks, over from and to" do
    run = stub_request(:post, @query_url)
      .with(body: hash_including(query: "", from: "2026-10-17T07:00:00.000Z", to: "2026-10-17T08:00:00.000Z", cursor: "c2", limit: 200))
      .to_return(status: 200, body: { logs: [ BARE_LOG ], next_cursor: nil }.to_json)

    answer = call_tool(:query_logs, user: @user, project: "shop", cursor: "c2", limit: 200,
      from: "2026-10-17T09:00:00+02:00", to: "2026-10-17T08:00:00Z")

    assert_not answer.error?
    assert_requested run
    assert_equal 2, answer.body.lines.size, "no line about older Logs without a cursor"
    assert_explorer({ "from" => "2026-10-17T07:00:00.000Z", "to" => "2026-10-17T08:00:00.000Z", "mode" => "query" }, answer.link)
  end

  test "query_logs keeps a limit between 1 and 200" do
    run = stub_request(:post, @query_url)

    [ 0, 201 ].each { |limit| assert call_tool(:query_logs, user: @user, project: "shop", limit: limit).error? }
    assert_not_requested run
  end

  test "a Log's line cuts a long message and long values, shows 8 Attributes and quotes names that aren't plain words" do
    attributes = { "user agent" => "curl", "http.method" => "GET", "body" => "x" * 150, "tags" => Array.new(40) { "tag" },
                   "a" => 1, "b" => nil, "c" => 2.5, "d" => { "e" => true }, "f" => 1, "g" => 2 }
    log = { "id" => "i", "time" => "t", "level" => "info", "service" => "api", "message" => "m" * 600, "attributes" => attributes }

    line = McpTools::LogLine.of(log)

    assert_equal "t info api i \"#{'m' * 500}…\" \"user agent\"=\"curl\" http.method=\"GET\" body=\"#{'x' * 100}…\" " \
                 "tags=#{JSON.generate(Array.new(40) { 'tag' })[0, 100]}… a=1 b=null c=2.5 d={\"e\":true} +2 more", line
  end

  test "query_logs answers a table as its columns and at most 100 rows, saying how many more" do
    rows = Array.new(150) { |i| [ "service-#{i}", i ] }
    query = "summarize count() by service"
    stub_request(:post, @query_url).with(body: hash_including(query: query))
      .to_return(status: 200, body: { table: { columns: [ { name: "service", type: "string" }, { name: "count_", type: "number", agg: "count" } ],
                                               rows: rows, truncated: false, fill_gaps: false } }.to_json)

    travel_to NOW do
      answer = call_tool(:query_logs, user: @user, project: "shop", query: query, range: "15m")

      assert_not answer.error?
      assert_equal({ "project" => "shop", "from" => "2026-10-17T09:45:00.000Z", "to" => "2026-10-17T10:00:00.000Z",
                     "columns" => [ { "name" => "service", "type" => "string" }, { "name" => "count_", "type" => "number", "agg" => "count" } ],
                     "rows" => rows.first(100), "more" => "50 more: narrow the Query." }, answer.json)
      assert_explorer({ "range" => "15m", "mode" => "query", "q" => query }, answer.link)
    end
  end

  test "query_logs cuts a table's long messages at 500 characters, so counting by message stays small" do
    stack = "Payment failed\n#{"  at checkout.rb:42\n" * 500}"
    query = 'where level == "error" | summarize count() by message'
    stub_request(:post, @query_url).with(body: hash_including(query: query))
      .to_return(status: 200, body: { table: { columns: [ { name: "message", type: "string" }, { name: "count_", type: "number", agg: "count" } ],
                                               rows: [ [ stack, 12 ], [ "timeout", 3 ] ], truncated: false, fill_gaps: false } }.to_json)

    answer = call_tool(:query_logs, user: @user, project: "shop", query: query)

    assert_equal [ [ "#{stack[0, 500]}…", 12 ], [ "timeout", 3 ] ], answer.json["rows"]
  end

  # EventStore's refusal of a Query over the budget, as it answers it.
  REFUSAL = {
    "message" => "Not run: this Query matches text … past the 4 GiB a Query that matches text may read.\n" \
                 "A narrower Query that fits, over the same range:\nwhere file == \"cron.log\" | search \"timeout\"",
    "estimate" => { "from" => "2026-10-10T10:00:00Z", "to" => "2026-10-17T10:00:00Z", "text_match" => true, "logs" => 612_864_000,
                    "bytes" => 269_841_600_000, "budget_bytes" => 4 << 30, "over_budget" => true, "heavy" => true, "forced" => false },
    "largest_range" => { "from" => "2026-10-17T07:20:00Z", "to" => "2026-10-17T10:00:00Z", "bytes" => 4_283_200_000 },
    "top_files" => [ { "service" => "web", "file" => "access.log", "logs" => 252_000_000, "bytes" => 100_800_000_000 } ],
    "narrower_query" => { "query" => "where file == \"cron.log\" | search \"timeout\"",
                          "from" => "2026-10-10T10:00:00Z", "to" => "2026-10-17T10:00:00Z" },
  }.freeze

  test "query_logs answers a Query over the budget with EventStore's refusal and the narrower Query's arguments, not an error" do
    travel_to NOW do
      stub_request(:post, @query_url).with(body: hash_including(query: "search \"timeout\"", check_budget: true))
        .to_return(status: 200, body: { refusal: REFUSAL, elapsed_ms: 3 }.to_json)

      answer = call_tool(:query_logs, user: @user, project: "shop", query: "search \"timeout\"", range: "7d")

      assert_not answer.error?
      assert answer.body.start_with?(REFUSAL["message"]), answer.body
      assert_equal "To run the narrower Query: call query_logs with query \"where file == \\\"cron.log\\\" | search \\\"timeout\\\"\", " \
                   "from 2026-10-10T10:00:00Z and to 2026-10-17T10:00:00Z.", answer.body.lines(chomp: true).last
      assert_no_match(/force/i, answer.body)
      assert_explorer({ "range" => "7d", "mode" => "query", "q" => "search \"timeout\"" }, answer.link)
    end
  end

  test "query_logs with force runs the Query as written, which EventStore runs at a lower priority" do
    run = stub_request(:post, @query_url).with(body: hash_including(query: "search \"timeout\"", check_budget: true, force: true))
      .to_return(status: 200, body: { logs: [ BARE_LOG ], next_cursor: nil }.to_json)

    answer = call_tool(:query_logs, user: @user, project: "shop", query: "search \"timeout\"", range: "7d", force: true)

    assert_not answer.error?
    assert_requested run
    assert_equal 2, answer.body.lines.size
  end

  test "query_logs with estimate answers how much the Query would read, without running it" do
    stub_request(:post, @query_url).with(body: hash_including(query: "search \"timeout\"", estimate: true))
      .to_return(status: 200, body: { estimate: REFUSAL["estimate"], refusal: REFUSAL }.to_json)

    answer = call_tool(:query_logs, user: @user, project: "shop", query: "search \"timeout\"", range: "7d", estimate: true)

    assert_not answer.error?
    assert_equal "Estimate, not run: the Query would read about 612,864,000 Logs, 251.3 GiB of their text and Attributes, " \
                 "from 2026-10-10T10:00:00Z to 2026-10-17T10:00:00Z. It matches text, so it may read 4 GiB: it would be refused, as below.",
                 answer.body.lines(chomp: true).first
    assert_includes answer.body, REFUSAL["message"]

    stub_request(:post, @query_url).with(body: hash_including(query: "", estimate: true))
      .to_return(status: 200, body: { estimate: REFUSAL["estimate"].merge("text_match" => false, "over_budget" => false), refusal: nil }.to_json)
    plain = call_tool(:query_logs, user: @user, project: "shop", range: "7d", estimate: true)
    assert_match(/It matches no text, so it is never refused\.\z/, plain.body)
  end

  test "query_logs says force is only for when the user asks for the Query to run as written" do
    assert_includes McpTools::QueryLogs.description_value, "Pass force only when the user asks for the Query to run as written"
    assert_equal "boolean", McpTools::QueryLogs.input_schema_value.to_h.dig(:properties, :force, :type)
    assert_equal "boolean", McpTools::QueryLogs.input_schema_value.to_h.dig(:properties, :estimate, :type)
  end

  test "query_logs answers a Query's mistakes with their positions" do
    stub_request(:post, @query_url).to_return(status: 400, body: {
      errors: [ { message: "unknown stage wher", line: 1, column: 1 }, { message: "expected a value", line: 2, column: 12 } ],
    }.to_json)

    answer = call_tool(:query_logs, user: @user, project: "shop", query: "wher level ==")

    assert answer.error?
    assert_equal "line 1, column 1: unknown stage wher\nline 2, column 12: expected a value", answer.text
  end

  test "query_logs answers a busy Console and a run stopped at its timeout as tool errors" do
    run = stub_request(:post, @query_url).to_return(status: 504, body: {
      errors: [ { message: "the run was stopped after 30 s, its timeout: try a shorter time range" } ],
    }.to_json)

    assert_equal "The run was stopped after 30 s, its timeout: try a shorter time range.",
      call_tool(:query_logs, user: @user, project: "shop", range: "30d").text

    holding_every_chart_run do
      answer = call_tool(:query_logs, user: @user, project: "shop")
      assert answer.error?
      assert_equal "Console is busy running other Queries and charts. Run this again in a moment.", answer.text
    end
    assert_requested run, times: 1
  end

  test "query_logs reads only the user's Organizations' Projects" do
    run = stub_request(:post, %r{/logs/query})

    assert_equal "No Project secret in acme. list_projects lists them.", call_tool(:query_logs, user: @user, project: "secret").text
    assert_match(/No Organization theirs/, call_tool(:query_logs, user: @user, organization: "theirs", project: "secret").text)
    assert_not_requested run
  end

  # get_log

  test "get_log answers the whole Log, looking in its hour, linking to the explorer over its second" do
    stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/logs/#{ERROR_LOG['id']}")
      .with(query: { time: "2026-10-17T09:59:58.123000000Z" })
      .to_return(status: 200, body: ERROR_LOG.to_json)

    answer = call_tool(:get_log, user: @user, project: "shop", log: ERROR_LOG["id"], time: "2026-10-17T11:59:58.123+02:00")

    assert_not answer.error?
    assert_equal ERROR_LOG, answer.json
    assert_explorer({ "from" => "2026-10-17T09:59:58.000Z", "to" => "2026-10-17T09:59:59.000Z", "mode" => "query" }, answer.link)
  end

  test "get_log says when there is no such Log, asking EventStore only for a Log id" do
    fetch = stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/logs/#{BARE_LOG['id']}").to_return(status: 404)

    assert_equal "No Log #{BARE_LOG['id']} in shop. query_logs gives Logs' ids and times.",
      call_tool(:get_log, user: @user, project: "shop", log: BARE_LOG["id"]).text
    assert_equal "No Log nope in shop. query_logs gives Logs' ids and times.", call_tool(:get_log, user: @user, project: "shop", log: "nope").text
    assert_equal "time must be an ISO 8601 time, such as the Log's time in its query_logs line.",
      call_tool(:get_log, user: @user, project: "shop", log: BARE_LOG["id"], time: "yesterday").text
    assert_requested fetch, times: 1
  end

  # list_fields

  test "list_fields answers the built-in fields and the Attributes as EventStore counts them, over the last day" do
    fields = { "built_in" => [ { "name" => "time", "type" => "datetime" }, { "name" => "level", "type" => "string" } ],
               "attributes" => [ { "name" => "status", "type" => "number", "logs" => 12 }, { "name" => "status", "type" => "string", "logs" => 3 } ],
               "truncated" => false }
    travel_to NOW do
      stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/logs/fields")
        .with(query: { from: "2026-10-16T10:00:00.000Z", to: "2026-10-17T10:00:00.000Z" })
        .to_return(status: 200, body: fields.to_json)

      answer = call_tool(:list_fields, user: @user, project: "shop")

      assert_not answer.error?
      assert_equal({ "project" => "shop", "from" => "2026-10-16T10:00:00.000Z", "to" => "2026-10-17T10:00:00.000Z",
                     "built_in" => fields["built_in"], "attributes" => fields["attributes"] }, answer.json)
      assert_explorer({ "range" => "24h", "mode" => "query" }, answer.link)
    end
  end

  test "list_fields reads the range and limit given, and says when there were more Attributes" do
    stub_request(:get, "#{EventStore.base_url}/api/#{@shop.id}/logs/fields")
      .with(query: { from: "2026-10-10T00:00:00.000Z", to: "2026-10-17T00:00:00.000Z", limit: "1" })
      .to_return(status: 200, body: { built_in: [], attributes: [ { name: "status", type: "number", logs: 12 } ], truncated: true }.to_json)

    answer = call_tool(:list_fields, user: @user, project: "shop", from: "2026-10-10T00:00:00Z", to: "2026-10-17T00:00:00Z", limit: 1)

    assert_not answer.error?
    assert_equal "More Attributes than these 1: give a larger limit, at most 500.", answer.json["more"]
    assert call_tool(:list_fields, user: @user, project: "shop", limit: 501).error?
  end

  # query_language_reference

  test "query_language_reference answers the Query language's reference" do
    answer = call_tool(:query_language_reference, user: @user)

    assert_not answer.error?
    assert_equal Rails.root.join("app/mcp/docs/query-language.md").read, answer.text
    assert_nil answer.link
  end

  test "the MCP server's copy of the Query language's reference is the same as docs/logs/query-language.md" do
    same = Rails.root.join("../docs/logs/query-language.md").read == Rails.root.join("app/mcp/docs/query-language.md").read
    assert same, "Copy docs/logs/query-language.md to console/app/mcp/docs/query-language.md."
  end

  test "the tools that run or save a Query point agents at service, file and Reading less" do
    assert_not_includes McpServer::INSTRUCTIONS, "log_file_order"
    [ McpTools::QueryLogs, McpTools::AddChart, McpTools::CreateMonitor ].each do |tool|
      [ "service and file", "Reading less" ].each { |part| assert_includes tool.description_value, part, tool.name }
      assert_not_includes tool.description_value, "log_file_order", tool.name
    end
    assert_not_includes McpTools::ListProjects.description_value, "log_file_order"
    assert_includes Rails.root.join("app/mcp/docs/query-language.md").read, "## Reading less"
  end

  # run_sql

  test "run_sql runs SQL on the Project's logs with the SQL timeout, answering at most 100 rows, linking to SQL mode" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    sql = "SELECT service, count(*) AS logs FROM logs GROUP BY service"
    rows = Array.new(120) { |i| [ "service-#{i}", i ] }
    travel_to NOW do
      stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/sql")
        .with(body: { sql: sql, from: "2026-10-16T10:00:00.000Z", to: "2026-10-17T10:00:00.000Z",
                      timeout_seconds: 10 }.to_json)
        .to_return(status: 200, body: { table: { columns: [ { name: "service", type: "string" }, { name: "logs", type: "number" } ],
                                                 rows: rows, truncated: false }, elapsed_ms: 8 }.to_json)

      answer = call_tool(:run_sql, user: @user, project: "shop", sql: sql, range: "24h")

      assert_not answer.error?
      assert_equal({ "project" => "shop", "from" => "2026-10-16T10:00:00.000Z", "to" => "2026-10-17T10:00:00.000Z",
                     "columns" => [ { "name" => "service", "type" => "string" }, { "name" => "logs", "type" => "number" } ],
                     "rows" => rows.first(100), "more" => "20 more: narrow the Query." }, answer.json)
      assert_explorer({ "range" => "24h", "mode" => "sql", "sql" => sql }, answer.link)
    end
  end

  test "run_sql answers EventStore's refusals and SQL mistakes as it says them" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    refusal = "shop_sql_1: Not enough privileges. To execute this query, it's necessary to have the grant SELECT ON solidtrace.log_key_counts."
    syntax_error = "Syntax error: failed at position 1 (SELEC): SELEC 1. Expected one of: Query, SELECT, WITH, end of query."
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/sql")
      .to_return({ status: 400, body: { error: refusal }.to_json },
                 { status: 400, body: { error: syntax_error }.to_json })

    refused = call_tool(:run_sql, user: @user, project: "shop", sql: "SELECT * FROM log_key_counts")
    assert refused.error?
    assert_equal refusal, refused.text
    assert_equal syntax_error, call_tool(:run_sql, user: @user, project: "shop", sql: "SELEC 1").text
  end

  test "run_sql answers SQL EventStore stopped at its timeout with EventStore's message" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/sql").to_return(status: 504, body: {
      errors: [ { message: "the run was stopped after 10 s, its timeout: try a shorter time range or a simpler query" } ],
    }.to_json)

    answer = call_tool(:run_sql, user: @user, project: "shop", sql: "SELECT 1")

    assert answer.error?
    assert_equal "The run was stopped after 10 s, its timeout: try a shorter time range or a simpler query.", answer.text
  end

  test "run_sql is refused while SQL queries are off, without asking EventStore" do
    InstanceSetting.current.update!(sql_queries_enabled: false)
    run = stub_request(:post, %r{/logs/sql})

    answer = run_tool(McpTools::RunSql, user: @user, project: "shop", sql: "SELECT 1")

    assert answer.error?
    assert_equal "SQL queries are turned off on this instance.", answer.text
    assert_not McpTools::RunSql.listed?
    assert_not_requested run
  end

  test "run_sql waits for no SQL while Console is busy" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    run = stub_request(:post, %r{/logs/sql})

    holding_every_chart_run do
      assert_match(/busy/, call_tool(:run_sql, user: @user, project: "shop", sql: "SELECT 1").text)
    end
    assert_not_requested run
  end

  # The link

  test "an explorer link is built as lib/logs/explorerState.ts reads it" do
    links = McpTools::Links.new(CONSOLE_URL)

    # lib/logs/mcpLinks.test.ts checks the explorer opens each url in its state.
    JSON.parse(file_fixture("explorer_links.json").read)["links"].each do |link|
      given = link["given"].symbolize_keys
      range = McpTools::TimeRange.parse(**given.slice(:range, :from, :to))
      assert_equal link["url"], links.explorer(@shop, range: range, **given.slice(:query, :sql, :viz))
    end
  end

  test "the Logs tools only read" do
    [ McpTools::QueryLogs, McpTools::GetLog, McpTools::ListFields, McpTools::QueryLanguageReference, McpTools::RunSql ].each do |tool|
      assert_equal({ readOnlyHint: true, destructiveHint: false, idempotentHint: true, openWorldHint: false }, tool.annotations_value.to_h.compact,
        tool.name_value)
    end
  end

  private

  # Checks that link opens the explorer on the Project shop with search.
  def assert_explorer(search, link)
    url = URI(link)
    assert_equal "http://console.test/acme/projects/shop/logs", "#{url.scheme}://#{url.host}#{url.path}"
    assert_equal search, URI.decode_www_form(url.query).to_h
  end

  def holding_every_chart_run(left = ChartRuns.limit, &block)
    left.zero? ? yield : ChartRuns.hold { holding_every_chart_run(left - 1, &block) }
  end
end
