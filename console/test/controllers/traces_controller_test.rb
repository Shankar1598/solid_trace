# frozen_string_literal: true

require "test_helper"

# The trace page: one trace's Spans across the Organization's Projects, from
# EventStore's /trace, for any member.
class TracesControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  TRACE_ID = "6d2f0e4a9b3d4e6f8a7b6c5d4e3f2b10"

  setup do
    @organization = create(:organization, slug: "acme")
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    @api = create(:project, organization: @organization, name: "Api", slug: "api")
    @rates = create(:project, organization: @organization, name: "Rates", slug: "rates")
    @theirs = create(:project, organization: create(:organization, slug: "theirs"), name: "Secret", slug: "secret")
    @trace_url = "#{EventStore.base_url}/trace"
  end

  # A Span as EventStore's /trace answers it.
  def span(project, span_id, parent, op, description, start_ms, duration_ms, self_ms, segment: nil, status: "ok", group: span_id)
    {
      project_id: project.id, span_id: span_id, parent_span_id: parent, segment_id: segment || span_id,
      is_segment: segment.nil?, transaction: "OrdersController#show", op: op, description: description,
      group: group, repeats: 1, status: status,
      start: (Time.utc(2026, 10, 2, 11) + Rational(start_ms, 1000)).iso8601(6),
      duration_us: duration_ms * 1000, self_time_us: self_ms * 1000, sample_rate: 0.25,
      environment: "production", release: "api@1.4.0", server_name: "web-1", attributes: {},
    }
  end

  def trace_props(path)
    get path, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "Traces/Show", response.parsed_body["component"]
    response.parsed_body.dig("props", "trace")
  end

  test "a member sees a trace across two of their Projects as a waterfall nested by parent" do
    asked = stub_request(:post, @trace_url)
      .with(body: { project_ids: [ @api.id, @rates.id ], trace_id: TRACE_ID }.to_json)
      .to_return(status: 200, body: {
        trace_id: TRACE_ID,
        spans: [
          span(@api, "a1", "", "http.server", "OrdersController#show", 0, 120, 70),
          span(@api, "a3", "a1", "template.render", "orders/show.html.erb", 40, 60, 30, segment: "a1", status: ""),
          span(@api, "a4", "a3", "http.client", "GET https://rates.example.com/rates", 50, 30, 30, segment: "a1"),
          span(@rates, "c1", "a4", "http.server", "GET /rates", 52, 26, 26),
          span(@api, "a5", "a1", "db.sql.active_record", "SELECT 1", 110, 5, 5, segment: "a1")
        ],
      }.to_json)
    sign_in_as(@member)

    trace = trace_props(trace_url(org_slug: "acme", trace_id: TRACE_ID))

    assert_requested asked
    assert_equal TRACE_ID, trace["trace_id"]
    assert_equal({ "available" => true }, trace["event_store"])
    assert_equal 120_000, trace["duration_us"]
    # Each Span under its parent, children in start order, with its depth
    # and its start from the trace's.
    assert_equal [
      [ "a1", 0, 0, "Api" ], [ "a3", 1, 40_000, "Api" ], [ "a4", 2, 50_000, "Api" ],
      [ "c1", 3, 52_000, "Rates" ], [ "a5", 1, 110_000, "Api" ]
    ], trace["spans"].map { |s| [ s["span_id"], s["depth"], s["offset_us"], s.dig("project", "name") ] }
    rates = trace["spans"][3]
    assert_equal({ "id" => @rates.id, "name" => "Rates", "slug" => "rates" }, rates["project"])
    assert_equal [ "http.server", "GET /rates", "ok", 26_000, 26_000 ],
      rates.values_at("op", "description", "status", "duration_us", "self_time_us")
  end

  test "another Organization's trace shows nothing of it" do
    asked = stub_request(:post, @trace_url)
      .with(body: { project_ids: [ @api.id, @rates.id ], trace_id: TRACE_ID }.to_json)
      .to_return(status: 200, body: { trace_id: TRACE_ID, spans: [ span(@theirs, "x1", "", "http.server", "Secret#show", 0, 10, 10) ] }.to_json)
    sign_in_as(@member)

    trace = trace_props(trace_url(org_slug: "acme", trace_id: TRACE_ID))

    # Only the member's own Projects are asked for, and a Span of any other
    # is left out.
    assert_requested asked
    assert_equal [], trace["spans"]
    assert_not_includes response.body, "Secret"

    get trace_url(org_slug: "theirs", trace_id: TRACE_ID)
    assert_response :not_found
  end

  test "the page says EventStore is unavailable when it can't answer, still showing the trace as Console holds it" do
    stub_request(:post, @trace_url).to_return(status: 503, body: "")
    sign_in_as(@member)

    trace = trace_props(trace_url(org_slug: "acme", trace_id: TRACE_ID.upcase, time: "2026-10-02T11:00:00Z"))

    assert_equal TRACE_ID, trace["trace_id"]
    assert_equal "2026-10-02T11:00:00Z", trace["time"]
    assert_equal false, trace.dig("event_store", "available")
    assert_equal [ [], [], [] ], trace.values_at("spans", "logs", "issues")
    assert_nil trace["sampled"]
  end

  # A Log as EventStore's /trace answers it.
  def log(project, id, time, message)
    { project_id: project.id, id: id, time: time, level: "info", message: message, service: "web", trace_id: TRACE_ID, attributes: {} }
  end

  test "a trace's page lists its Logs in time order and its Issues, each linked" do
    payment = create(:issue, project: @api, title: "Payment declined")
    stale = create(:issue, project: @rates, title: "Rates stale")
    payment_print = create(:issue_fingerprint, issue: payment)
    stale_print = create(:issue_fingerprint, issue: stale)
    stub_request(:post, @trace_url)
      .with(body: { project_ids: [ @api.id, @rates.id ], trace_id: TRACE_ID }.to_json)
      .to_return(status: 200, body: {
        trace_id: TRACE_ID,
        spans: [ span(@api, "a1", "", "http.server", "OrdersController#show", 0, 120, 120) ],
        logs: [
          log(@api, "0199a000-0000-7000-8000-000000000001", "2026-10-02T11:00:00.05Z", "Rendering order"),
          log(@rates, "0199a000-0000-7000-8000-000000000002", "2026-10-02T11:00:00.06Z", "Fetching rates")
        ],
        more_logs: false, logs_from: "2026-10-02T10:55:00Z", logs_to: "2026-10-02T11:05:00.12Z",
        issues: [
          { project_id: @rates.id, issue_fingerprint_id: stale_print.id, events: [ { uuid: "c-1", timestamp: "2026-10-02T11:00:00.07Z" } ] },
          { project_id: @api.id, issue_fingerprint_id: payment_print.id, events: [
            { uuid: "a-1", timestamp: "2026-10-02T11:00:00.1Z" }, { uuid: "a-2", timestamp: "2026-10-02T11:00:00.11Z" }
          ] }
        ],
      }.to_json)
    sign_in_as(@member)

    trace = trace_props(trace_url(org_slug: "acme", trace_id: TRACE_ID))

    assert_equal true, trace["sampled"]
    assert_equal [ [ "Rendering order", "Api" ], [ "Fetching rates", "Rates" ] ],
      trace["logs"].map { |l| [ l["message"], l.dig("project", "name") ] }
    # Each Log opens in the explorer over its second, among the trace's Logs.
    assert_equal "/acme/projects/rates/logs?from=2026-10-02T11%3A00%3A00.000Z&mode=query" \
                 "&q=where+trace_id+%3D%3D+%22#{TRACE_ID}%22&to=2026-10-02T11%3A00%3A01.000Z", trace["logs"][1]["url"]
    assert_equal false, trace["more_logs"]
    # Each Project's Logs of the trace, over the time read.
    links = trace["logs_links"].to_h { |l| [ l.dig("project", "name"), l["url"] ] }
    assert_equal [ "Api", "Rates" ], links.keys
    assert_equal "/acme/projects/api/logs?from=2026-10-02T10%3A55%3A00.000Z&mode=query" \
                 "&q=where+trace_id+%3D%3D+%22#{TRACE_ID}%22&to=2026-10-02T11%3A05%3A00.120Z", links["Api"]
    # Each Issue with its Events in the trace, opening on its first.
    assert_equal [
      [ "Rates stale", "Rates", 1, "/acme/projects/rates/issues/#{stale.number}?event_id=c-1" ],
      [ "Payment declined", "Api", 2, "/acme/projects/api/issues/#{payment.number}?event_id=a-1" ]
    ], trace["issues"].map { |i| [ i["title"], i.dig("project", "name"), i["events"], i["url"] ] }
  end

  test "an unsampled trace opens around its time with its Logs and Issues" do
    payment = create(:issue, project: @api, title: "Payment declined")
    print = create(:issue_fingerprint, issue: payment)
    asked = stub_request(:post, @trace_url)
      .with(body: { project_ids: [ @api.id, @rates.id ], trace_id: TRACE_ID, time: "2026-10-02T11:30:02.000Z" }.to_json)
      .to_return(status: 200, body: {
        trace_id: TRACE_ID, spans: [],
        logs: [ log(@api, "0199a000-0000-7000-8000-000000000003", "2026-10-02T11:30:02Z", "Card declined") ],
        more_logs: false, logs_from: "2026-10-02T11:25:02Z", logs_to: "2026-10-02T11:35:02Z",
        issues: [ { project_id: @api.id, issue_fingerprint_id: print.id, events: [ { uuid: "a-1", timestamp: "2026-10-02T11:30:01Z" } ] } ],
      }.to_json)
    sign_in_as(@member)

    trace = trace_props(trace_url(org_slug: "acme", trace_id: TRACE_ID, time: "2026-10-02T11:30:02Z"))

    assert_requested asked
    assert_equal false, trace["sampled"]
    assert_equal [], trace["spans"]
    assert_equal [ "Card declined" ], trace["logs"].map { |l| l["message"] }
    assert_equal [ "Payment declined" ], trace["issues"].map { |i| i["title"] }
  end

  test "repeated Spans collapse into one row with a count and total, and the critical path is marked" do
    stub_request(:post, @trace_url).to_return(status: 200, body: {
      trace_id: TRACE_ID,
      spans: [
        span(@api, "a1", "", "http.server", "OrdersController#show", 0, 120, 45),
        # An N+1: three queries of one group, one after another.
        span(@api, "a10", "a1", "db.sql.active_record", "SELECT * FROM line_items WHERE id = 1", 10, 5, 5, segment: "a1", group: "items"),
        span(@api, "a11", "a1", "db.sql.active_record", "SELECT * FROM line_items WHERE id = 2", 20, 5, 5, segment: "a1", group: "items"),
        span(@api, "a12", "a1", "db.sql.active_record", "SELECT * FROM line_items WHERE id = 3", 30, 5, 5, segment: "a1", group: "items"),
        span(@api, "a3", "a1", "template.render", "orders/show.html.erb", 40, 60, 20, segment: "a1"),
        # Under the view, the HTTP call is what it waited for; the cache
        # read ran beside it.
        span(@api, "a4", "a3", "http.client", "GET https://rates.example.com/rates", 50, 30, 30, segment: "a1"),
        span(@api, "a6", "a3", "cache.get", "rates", 60, 10, 10, segment: "a1"),
        span(@api, "a5", "a1", "db.sql.active_record", "COMMIT", 110, 5, 5, segment: "a1")
      ],
    }.to_json)
    sign_in_as(@member)

    trace = trace_props(trace_url(org_slug: "acme", trace_id: TRACE_ID))

    assert_equal [
      [ "a1", 0, 1, 120_000, 45_000, true ],
      [ "a10", 1, 3, 15_000, 15_000, true ],
      [ "a3", 1, 1, 60_000, 20_000, true ],
      [ "a4", 2, 1, 30_000, 30_000, true ],
      [ "a6", 2, 1, 10_000, 10_000, false ],
      [ "a5", 1, 1, 5_000, 5_000, true ]
    ], trace["spans"].map { |s| s.values_at("span_id", "depth", "count", "duration_us", "self_time_us", "critical") }
    assert_equal 8, trace["span_count"]
  end
end
