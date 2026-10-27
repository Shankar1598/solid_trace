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
  def span(project, span_id, parent, op, description, start_ms, duration_ms, self_ms, segment: nil, status: "ok")
    {
      project_id: project.id, span_id: span_id, parent_span_id: parent, segment_id: segment || span_id,
      is_segment: segment.nil?, transaction: "OrdersController#show", op: op, description: description,
      group: "0011223344556677", repeats: 1, status: status,
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
          span(@api, "a5", "a1", "db.sql.active_record", "SELECT 1", 110, 5, 5, segment: "a1"),
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
      [ "c1", 3, 52_000, "Rates" ], [ "a5", 1, 110_000, "Api" ],
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

  test "the page says EventStore is unavailable when it can't answer" do
    stub_request(:post, @trace_url).to_return(status: 503, body: "")
    sign_in_as(@member)

    trace = trace_props(trace_url(org_slug: "acme", trace_id: TRACE_ID.upcase))

    assert_equal TRACE_ID, trace["trace_id"]
    assert_equal false, trace.dig("event_store", "available")
    assert_equal [], trace["spans"]
  end
end
