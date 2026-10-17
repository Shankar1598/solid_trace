# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

class DashboardsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @base_url = "http://127.0.0.1:4100"
    @previous_url = ENV["EVENT_STORE_URL"]
    ENV["EVENT_STORE_URL"] = @base_url

    @organization = create(:organization)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @project = create(:project, organization: @organization, name: "Storefront")
    @dashboard = create(:dashboard, organization: @organization, name: "Checkout")
    @chart = create(:dashboard_chart, dashboard: @dashboard, project: @project, title: "Requests", w: 6, h: 4)

    @outsider = create(:user)
    create(:organization_user, organization: create(:organization), user: @outsider, role: :admin)

    @foreign_project = create(:project, organization: create(:organization), name: "Elsewhere")
    @range = { from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    sign_in_as(@member)
  end

  teardown do
    ENV["EVENT_STORE_URL"] = @previous_url
  end

  test "a member lists the Organization's Dashboards with their chart counts" do
    create(:dashboard, organization: @organization, name: "Billing")
    create(:dashboard, organization: create(:organization), name: "Not ours")

    props = inertia_page(dashboards_url(org_slug: @organization.slug), "Dashboards/Index")

    assert_equal [ [ "Billing", 0 ], [ "Checkout", 1 ] ], props["dashboards"].map { |d| [ d["name"], d["chart_count"] ] }
  end

  test "every page knows the Organization's Dashboards, for the palette and Add to dashboard" do
    create(:dashboard, organization: create(:organization), name: "Not ours")

    props = inertia_page(dashboards_url(org_slug: @organization.slug), "Dashboards/Index")

    assert_equal [ { "id" => @dashboard.id, "name" => "Checkout" } ], props["org_dashboards"]
  end

  test "a member opens a Dashboard with its charts and the Organization's Projects" do
    props = inertia_page(dashboard_url(@dashboard, org_slug: @organization.slug), "Dashboards/Show")

    assert_equal "Checkout", props["dashboard"]["name"]
    assert_equal "1h", props["dashboard"]["default_range"]
    chart = props["dashboard"]["charts"].sole
    assert_equal [ "Requests", @project.id, @chart.query, "line", 0, 0, 6, 4 ], chart.values_at("title", "project_id", "query", "viz", "x", "y", "w", "h")
    assert_equal [ "Storefront" ], props["projects"].map { |p| p["name"] }
    assert props["projects"].first["log_retention_days"].positive?
  end

  test "a member makes a Dashboard from the page and is taken to it" do
    post dashboards_url(org_slug: @organization.slug), params: { dashboard: { name: "Latency", default_range: "24h" } }

    dashboard = @organization.dashboards.find_by!(name: "Latency")
    assert_redirected_to dashboard_url(dashboard, org_slug: @organization.slug)
    assert_equal [ "24h", "off", @member.id ], [ dashboard.default_range, dashboard.auto_refresh, dashboard.created_by_id ]
  end

  test "a member makes a Dashboard from the explorer as JSON" do
    post dashboards_url(org_slug: @organization.slug), params: { dashboard: { name: "Latency" } }, as: :json

    assert_response :created
    assert_equal "Latency", response.parsed_body["name"]
    assert_equal response.parsed_body["id"], @organization.dashboards.find_by!(name: "Latency").id
  end

  test "a Dashboard without a name is refused" do
    post dashboards_url(org_slug: @organization.slug), params: { dashboard: { name: " " } }, as: :json

    assert_response :unprocessable_content
    assert response.parsed_body["errors"]["name"].present?
  end

  test "a member renames a Dashboard and sets its range and auto-refresh" do
    patch dashboard_url(@dashboard, org_slug: @organization.slug, range: "6h"),
      params: { dashboard: { name: "Checkout v2", description: "The checkout path", default_range: "7d", auto_refresh: "1m" } }

    assert_redirected_to dashboard_url(@dashboard, org_slug: @organization.slug, range: "6h")
    @dashboard.reload
    assert_equal [ "Checkout v2", "The checkout path", "7d", "1m" ], [ @dashboard.name, @dashboard.description, @dashboard.default_range, @dashboard.auto_refresh ]
  end

  test "a member deletes a Dashboard and its charts" do
    delete dashboard_url(@dashboard, org_slug: @organization.slug)

    assert_redirected_to dashboards_url(org_slug: @organization.slug)
    assert_not Dashboard.exists?(@dashboard.id)
    assert_not DashboardChart.exists?(@chart.id)
  end

  test "a member adds a chart, which goes under the others" do
    post dashboard_charts_url(@dashboard, org_slug: @organization.slug), params: { chart: {
      project_id: @project.id, title: "Errors", query: "['#{@project.slug}'] | summarize count()", viz: "stat",
      w: 3, h: 2, options: { unit: "Logs", decimals: "0" },
    } }, as: :json

    assert_response :created
    chart = @dashboard.charts.find(response.parsed_body["id"])
    assert_equal [ "Errors", "stat", 0, 4, 3, 2 ], [ chart.title, chart.viz, chart.x, chart.y, chart.w, chart.h ]
    assert_equal({ "unit" => "Logs", "decimals" => 0 }, chart.options)
  end

  test "a member edits, duplicates and deletes a chart" do
    patch dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug),
      params: { chart: { title: "All requests", viz: "bar", options: { stacked: false } } }
    assert_redirected_to dashboard_url(@dashboard, org_slug: @organization.slug)
    assert_equal [ "All requests", "bar", { "stacked" => false } ], [ @chart.reload.title, @chart.viz, @chart.options ]

    post duplicate_dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), as: :json
    assert_response :created
    copy = @dashboard.charts.find(response.parsed_body["id"])
    assert_equal [ "All requests (copy)", @chart.query, "bar", 4 ], [ copy.title, copy.query, copy.viz, copy.y ]

    delete dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug)
    assert_redirected_to dashboard_url(@dashboard, org_slug: @organization.slug)
    assert_equal [ copy.id ], @dashboard.charts.reload.map(&:id)
  end

  test "a chart on a Project of another Organization is refused" do
    post dashboard_charts_url(@dashboard, org_slug: @organization.slug), params: { chart: {
      project_id: @foreign_project.id, title: "Theirs", query: "summarize count()", viz: "line",
    } }, as: :json

    assert_response :unprocessable_content
    assert response.parsed_body["errors"]["project"].present?
    assert_equal [ @chart.id ], @dashboard.charts.map(&:id)

    patch dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), params: { chart: { project_id: @foreign_project.id } }, as: :json

    assert_response :unprocessable_content
    assert_equal @project.id, @chart.reload.project_id
  end

  test "someone outside the Organization finds none of its Dashboards or charts" do
    sign_in_as(@outsider)
    run = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
    org = @organization.slug

    get dashboards_url(org_slug: org)
    assert_response :not_found
    get dashboard_url(@dashboard, org_slug: org)
    assert_response :not_found
    post dashboards_url(org_slug: org), params: { dashboard: { name: "Mine now" } }, as: :json
    assert_response :not_found
    patch dashboard_url(@dashboard, org_slug: org), params: { dashboard: { name: "Mine now" } }, as: :json
    assert_response :not_found
    patch layout_dashboard_url(@dashboard, org_slug: org), params: { layout: [ { id: @chart.id, x: 6, y: 0, w: 6, h: 4 } ] }, as: :json
    assert_response :not_found
    post dashboard_charts_url(@dashboard, org_slug: org), params: { chart: { project_id: @project.id, title: "x", query: "x" } }, as: :json
    assert_response :not_found
    patch dashboard_chart_url(@dashboard, @chart, org_slug: org), params: { chart: { title: "Mine now" } }, as: :json
    assert_response :not_found
    post duplicate_dashboard_chart_url(@dashboard, @chart, org_slug: org), as: :json
    assert_response :not_found
    delete dashboard_chart_url(@dashboard, @chart, org_slug: org), as: :json
    assert_response :not_found
    get data_dashboard_chart_url(@dashboard, @chart, org_slug: org), params: @range
    assert_response :not_found
    delete dashboard_url(@dashboard, org_slug: org)
    assert_response :not_found

    assert_not_requested run
    assert_equal [ "Checkout", "Requests", 0 ], [ @dashboard.reload.name, @chart.reload.title, @chart.x ]
  end

  test "a Dashboard of another Organization isn't found under this one" do
    theirs = create(:dashboard, organization: @foreign_project.organization)

    get dashboard_url(theirs, org_slug: @organization.slug)

    assert_response :not_found
  end

  test "a chart's data runs its saved Query over the range given, with a timeout, and answers the table" do
    table = {
      columns: [ { name: "time", type: "datetime", bin_seconds: 60 }, { name: "count_", type: "number", agg: "count" } ],
      rows: [ [ "2026-10-03T09:00:00Z", 12 ] ],
      truncated: false,
    }
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
      .with(body: {
        query: @chart.query,
        from: "2026-10-03T09:00:00.000Z",
        to: "2026-10-03T10:00:00.000Z",
        timeout_seconds: DashboardChartsController::RUN_TIMEOUT_SECONDS,
      }.to_json)
      .to_return(status: 200, body: { table: table, elapsed_ms: 41 }.to_json)

    get data_dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug),
      params: { from: "2026-10-03T11:00:00+02:00", to: "2026-10-03T10:00:00Z", query: "ignored" }

    assert_response :success
    assert_equal [ [ "2026-10-03T09:00:00Z", 12 ] ], response.parsed_body["table"]["rows"]
    assert_equal 60, response.parsed_body["table"]["columns"].first["bin_seconds"]
    assert_equal 41, response.parsed_body["elapsed_ms"]
  end

  test "a chart's data answers EventStore's errors for a Query with mistakes" do
    errors = [ { message: "unknown stage \"nope\"", line: 1, column: 1, end_line: 1, end_column: 5 } ]
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return(status: 400, body: { errors: errors }.to_json)

    get data_dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), params: @range

    assert_response :bad_request
    assert_equal "unknown stage \"nope\"", response.parsed_body["errors"].first["message"]
  end

  test "a chart's data answers 502 when EventStore can't" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return(status: 500, body: "boom")

    get data_dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), params: @range

    assert_response :bad_gateway
    assert_equal "EventStore is unavailable. Try again shortly.", response.parsed_body["error"]
  end

  test "a chart's data answers 504 with EventStore's message when the run passes its timeout" do
    message = "the run was stopped after 30 s, its timeout: try a shorter time range"
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return(status: 504, body: { error: message }.to_json)

    get data_dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), params: @range

    assert_response :gateway_timeout
    assert_equal message, response.parsed_body["error"]
  end

  test "a chart's data answers 503 at once, without running the Query, while Console runs as many charts as it allows" do
    run = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")

    holding_every_chart_run do
      get data_dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), params: @range
    end

    assert_response :service_unavailable
    assert_equal "Console is busy running other charts. Refresh in a moment.", response.parsed_body["error"]
    assert_not_requested run
  end

  test "a chart whose Query answers Logs says to end it with summarize" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)

    get data_dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), params: @range

    assert_response :unprocessable_content
    assert_match(/summarize/, response.parsed_body["error"])
  end

  test "a chart's data needs a range of two times" do
    run = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")

    get data_dashboard_chart_url(@dashboard, @chart, org_slug: @organization.slug), params: { from: "last hour", to: "now" }

    assert_response :bad_request
    assert_not_requested run
  end

  test "a member moves and resizes charts in one layout update" do
    other = create(:dashboard_chart, dashboard: @dashboard, project: @project, title: "Errors", x: 6, w: 6, h: 4)

    patch layout_dashboard_url(@dashboard, org_slug: @organization.slug), params: { layout: [
      { id: @chart.id, x: 0, y: 4, w: 12, h: 6 },
      { id: other.id, x: 0, y: 0, w: 4, h: 4 }
    ] }, as: :json

    assert_response :success
    assert_equal [ 0, 4, 12, 6 ], @chart.reload.slice(:x, :y, :w, :h).values
    assert_equal [ 0, 0, 4, 4 ], other.reload.slice(:x, :y, :w, :h).values
    assert_equal [ other.id, @chart.id ], response.parsed_body["charts"].map { |c| c["id"] }
  end

  test "a layout that doesn't fit the grid changes nothing" do
    other = create(:dashboard_chart, dashboard: @dashboard, project: @project, title: "Errors", x: 6, w: 6, h: 4)

    patch layout_dashboard_url(@dashboard, org_slug: @organization.slug), params: { layout: [
      { id: other.id, x: 0, y: 0, w: 4, h: 4 },
      { id: @chart.id, x: 10, y: 0, w: 6, h: 4 }
    ] }, as: :json

    assert_response :unprocessable_content
    assert_equal [ 6, 0, 6, 4 ], other.reload.slice(:x, :y, :w, :h).values
    assert_equal [ 0, 0, 6, 4 ], @chart.reload.slice(:x, :y, :w, :h).values
  end

  test "a layout naming another Dashboard's chart changes nothing" do
    theirs = create(:dashboard_chart, dashboard: create(:dashboard, organization: @organization), project: @project)

    patch layout_dashboard_url(@dashboard, org_slug: @organization.slug), params: { layout: [
      { id: @chart.id, x: 6, y: 0, w: 6, h: 4 },
      { id: theirs.id, x: 0, y: 0, w: 6, h: 4 }
    ] }, as: :json

    assert_response :not_found
    assert_equal 0, @chart.reload.x
  end

  private

  def inertia_page(url, component)
    get url, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal component, response.parsed_body["component"]
    response.parsed_body["props"]
  end

  # Runs the block while this process holds every chart run it allows.
  def holding_every_chart_run(left = ChartRuns.limit, &block)
    left.zero? ? yield : ChartRuns.hold { holding_every_chart_run(left - 1, &block) }
  end
end
