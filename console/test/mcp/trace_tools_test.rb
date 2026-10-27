# frozen_string_literal: true

require "test_helper"

# get_trace: one trace across the Organization's Projects, summarised so that
# a large one fits an agent's context, from the builder the trace page uses.
class McpTraceToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  TRACE_ID = "6d2f0e4a9b3d4e6f8a7b6c5d4e3f2b10"

  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @api = create(:project, organization: @acme, slug: "api")
    @rates = create(:project, organization: @acme, slug: "rates")
    create(:project, organization: create(:organization, slug: "theirs"), slug: "secret")
    @trace_url = "#{EventStore.base_url}/trace"
    @issue = create(:issue, project: @api, title: "Payment declined")
    @print = create(:issue_fingerprint, issue: @issue)
  end

  def span(span_id, parent, op, description, start_us, duration_us, self_us, group: span_id, project: @api)
    {
      project_id: project.id, span_id: span_id, parent_span_id: parent, segment_id: "s0", is_segment: parent == "",
      transaction: "ReportsController#show", op: op, description: description, group: group, repeats: 1, status: "ok",
      start: (Time.utc(2026, 10, 2, 11) + Rational(start_us, 1_000_000)).iso8601(6),
      duration_us: duration_us, self_time_us: self_us, sample_rate: 1.0,
      environment: "production", release: "api@1.4.0", server_name: "web-1", attributes: {},
    }
  end

  def links
    {
      logs: [
        { project_id: @api.id, id: "0199a000-0000-7000-8000-000000000001", time: "2026-10-02T11:00:00.5Z", message: "Building report", trace_id: TRACE_ID, attributes: {} },
        { project_id: @rates.id, id: "0199a000-0000-7000-8000-000000000002", time: "2026-10-02T11:00:01Z", message: "Fetching rates", trace_id: TRACE_ID, attributes: {} }
      ],
      more_logs: false, logs_from: "2026-10-02T10:55:00Z", logs_to: "2026-10-02T11:05:02Z",
      issues: [ { project_id: @api.id, issue_fingerprint_id: @print.id, events: [ { uuid: "e-1", timestamp: "2026-10-02T11:00:01.5Z" } ] } ],
    }
  end

  test "get_trace keeps a trace of 2,000 Spans within its budget, the most self time first, with its Issues and query_logs arguments" do
    # A report: 1,500 Spans each its own, and an N+1 of 499 queries under
    # the segment, one after another.
    spans = [ span("s0", "", "http.server", "ReportsController#show", 0, 2_000_000, 1_000) ]
    1_500.times do |i|
      spans << span(format("a%05d", i), "s0", "function", "step #{i} " + ("x" * 300), i * 1_000, 1_000 - (i % 900), 1_000 - (i % 900))
    end
    499.times do |i|
      spans << span(format("q%05d", i), "s0", "db.sql.query", "SELECT * FROM rows WHERE id = #{i}", 1_500_000 + i * 1_000, 900, 900, group: "rows")
    end
    asked = stub_request(:post, @trace_url)
      .with(body: { project_ids: [ @api.id, @rates.id ], trace_id: TRACE_ID }.to_json)
      .to_return(status: 200, body: { trace_id: TRACE_ID, spans: spans }.merge(links).to_json)

    got = call_tool(:get_trace, user: @user, trace: TRACE_ID)

    assert_not got.error?
    assert_requested asked
    trace = got.json
    assert_operator got.body.bytesize, :<, 64_000, "get_trace answered #{got.body.bytesize} bytes"
    assert_equal [ TRACE_ID, true, 2_000, 2_000_000 ], trace.values_at("trace_id", "sampled", "span_count", "duration_us")
    # The 499 queries are one row; with the segment and 1,500 steps that's
    # 1,502 rows. Every one is on the critical path, as the steps and
    # queries ran one after another. The answer gives 200 rows in all: its
    # one Transaction, the first 50 of the critical path, and 149 rows.
    rows = trace["spans"]
    assert_equal [ 1, 50, 149 ], [ trace["transactions"].size, trace["critical_path"].size, rows.size ]
    assert_equal({ "transactions" => 0, "critical_path" => 1_452, "rows" => 1_353, "spans" => 2_000 - 499 - 148 },
      trace["left_out"])
    assert_equal [ "q00000", 499, 449_100, true ], rows.first.values_at("span_id", "count", "self_time_us", "critical")
    assert_equal rows.map { |r| r["self_time_us"] }.sort.reverse, rows.map { |r| r["self_time_us"] }
    assert_operator rows.map { |r| r["description"].size }.max, :<=, TraceView::SUMMARY_DESCRIPTION
    assert_equal [ "s0", "a00000", "a00001" ], trace["critical_path"].first(3)
    # Its Issues, for get_issue, and the query_logs arguments that read its
    # Logs in each Project that has any.
    assert_equal [ { "project" => "api", "issue" => @issue.number, "title" => "Payment declined", "events" => 1, "event" => "e-1" } ],
      trace["issues"]
    assert_equal [ "api", "rates" ], trace["trace_logs"].map { |t| t.dig("arguments", "project") }
    assert_equal({ "tool" => "query_logs", "arguments" => {
      "organization" => "acme", "project" => "api", "query" => "where trace_id == \"#{TRACE_ID}\"",
      "from" => "2026-10-02T10:55:00.000Z", "to" => "2026-10-02T11:05:02.000Z",
    } }, trace["trace_logs"].first)
    assert_equal "http://console.test/acme/traces/#{TRACE_ID}", got.link
  end

  test "get_trace answers an unsampled trace's Issues and Logs around the time given, linking to the page at that time" do
    asked = stub_request(:post, @trace_url)
      .with(body: { project_ids: [ @api.id, @rates.id ], trace_id: TRACE_ID, time: "2026-10-02T11:00:01.000Z" }.to_json)
      .to_return(status: 200, body: { trace_id: TRACE_ID, spans: [] }.merge(links).to_json)

    got = call_tool(:get_trace, user: @user, organization: "acme", trace: TRACE_ID.upcase, time: "2026-10-02T11:00:01Z")

    assert_not got.error?
    assert_requested asked
    assert_equal [ false, [], 1, 2 ], [ got.json["sampled"], got.json["spans"], got.json["issues"].size, got.json["trace_logs"].size ]
    assert_equal "http://console.test/acme/traces/#{TRACE_ID}?time=2026-10-02T11%3A00%3A01Z", got.link
  end

  test "get_trace refuses what isn't a trace id" do
    got = call_tool(:get_trace, user: @user, trace: "not-a-trace")

    assert got.error?
    assert_match(/32 hex digits/, got.text)
  end
end
