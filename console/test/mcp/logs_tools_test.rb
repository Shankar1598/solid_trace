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
                      limit: 50, timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS }.to_json)
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

    assert_equal "The run was stopped at its timeout. Try a shorter range or a narrower Query.",
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
