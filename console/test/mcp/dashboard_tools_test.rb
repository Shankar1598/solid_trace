# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Dashboard and Chart tools: an Organization's Dashboards, their Charts,
# and a Chart's data, each answer linking to the Dashboard.
class McpDashboardToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    @dashboard = create(:dashboard, organization: @acme, name: "Checkout", default_range: "24h")
    @chart = create(:dashboard_chart, dashboard: @dashboard, project: @shop, title: "Requests", y: 0, h: 4)

    theirs = create(:organization, slug: "theirs")
    @their_project = create(:project, organization: theirs, slug: "secret")
    @their_dashboard = create(:dashboard, organization: theirs, name: "Theirs")
    @their_chart = create(:dashboard_chart, dashboard: @their_dashboard, project: @their_project)
  end

  # Dashboards

  test "list_dashboards answers the Organization's Dashboards by name with their Chart counts, linking to its Dashboards page" do
    create(:dashboard, organization: @acme, name: "Api")

    answer = call_tool(:list_dashboards, user: @user)

    assert_not answer.error?
    assert_equal "acme", answer.json["organization"]
    assert_equal [ [ "Api", 0 ], [ "Checkout", 1 ] ], answer.json["dashboards"].map { |d| [ d["name"], d["chart_count"] ] }
    assert_equal "http://console.test/acme/dashboards", answer.link
  end

  test "get_dashboard answers the Dashboard with its Charts and their Projects' slugs, linking to it" do
    answer = call_tool(:get_dashboard, user: @user, dashboard: @dashboard.id)

    assert_not answer.error?
    assert_equal [ "Checkout", "24h", 1 ], answer.json.values_at("name", "default_range", "chart_count")
    chart = answer.json["charts"].sole
    assert_equal [ @chart.id, "Requests", "shop", @chart.query, "line" ], chart.values_at("id", "title", "project", "query", "viz")
    assert_equal "http://console.test/acme/dashboards/#{@dashboard.id}", answer.link
  end

  test "get_dashboard lists the Dashboard's filters in the bar's order: name, label, kind, multiple and options" do
    @dashboard.update!(filters: [
      { name: "service", label: "Service", kind: "select", multiple: true, options: { from: "field", project_id: @shop.id, field: "service" } },
      { name: "level", kind: "select", options: { from: "list", items: [ { label: "Errors", value: "error" }, { value: "warn" } ] } },
      { name: "q", kind: "search" },
    ])

    answer = call_tool(:get_dashboard, user: @user, dashboard: @dashboard.id)

    assert_equal [
      { "name" => "service", "label" => "Service", "kind" => "select", "multiple" => true,
        "options" => { "from" => "field", "project_id" => @shop.id, "field" => "service" } },
      { "name" => "level", "label" => "level", "kind" => "select", "multiple" => false,
        "options" => { "from" => "list", "items" => [ { "label" => "Errors", "value" => "error" }, { "label" => "warn", "value" => "warn" } ] } },
      { "name" => "q", "label" => "q", "kind" => "search" },
    ], answer.json["filters"]
  end

  test "create_dashboard makes a Dashboard by the user, linking to it" do
    answer = call_tool(:create_dashboard, user: @user, name: "Errors", description: "What failed", default_range: "7d", auto_refresh: "5m")

    assert_not answer.error?
    made = @acme.dashboards.find_by!(name: "Errors")
    assert_equal [ "What failed", "7d", "5m", @user ], [ made.description, made.default_range, made.auto_refresh, made.created_by ]
    assert_equal made.id, answer.json["id"]
    assert_equal [], answer.json["charts"]
    assert_equal "http://console.test/acme/dashboards/#{made.id}", answer.link
  end

  test "create_dashboard answers the Dashboard's validation errors and makes none" do
    answer = call_tool(:create_dashboard, user: @user, name: "Errors", default_range: "forever")

    assert answer.error?
    assert_equal "Not saved: Default range must be a span such as 15m, 1h or 7d.", answer.text
    assert_not Dashboard.exists?(name: "Errors")
    assert call_tool(:create_dashboard, user: @user, name: "Errors", auto_refresh: "10s").error?
  end

  test "create_dashboard takes filters, validated as the Dashboard page's are" do
    filters = [
      { name: "level", label: "Level", kind: "select", multiple: true, options: { from: "list", items: [ { value: "error" }, { value: "warn" } ] } },
      { name: "service", kind: "select", options: { from: "field", project_id: @shop.id, field: "service" } },
      { name: "q", kind: "search" },
    ]

    answer = call_tool(:create_dashboard, user: @user, name: "Errors", filters: filters)

    assert_not answer.error?
    made = @acme.dashboards.find_by!(name: "Errors")
    assert_equal %w[level service q], made.filter_names
    assert_equal [ "Level", true, [ "error", "warn" ] ], [ made.filters[0]["label"], made.filters[0]["multiple"], made.filters[0]["options"]["items"].pluck("value") ]
    assert_equal({ "from" => "field", "project_id" => @shop.id, "field" => "service" }, made.filters[1]["options"])
    assert_equal made.filters, answer.json["filters"]

    refused = call_tool(:create_dashboard, user: @user, name: "Bad filters", filters: [
      { name: "Service", kind: "select", options: { from: "field", project_id: @their_project.id, field: "service" } },
      { name: "q", kind: "search", multiple: true },
    ])
    assert_equal "Not saved: Filters[0] name must be lowercase letters, digits and _, starting with a letter or _, at most 40 characters; " \
                 "Filters[0] options project must be a Project of the Dashboard's Organization; Filters[1] multiple is for selects only.",
      refused.text
    assert_not Dashboard.exists?(name: "Bad filters")
  end

  test "update_dashboard changes only the fields given" do
    @dashboard.update!(description: "Orders")

    answer = call_tool(:update_dashboard, user: @user, dashboard: @dashboard.id, name: "Checkout v2", auto_refresh: "1m")

    assert_not answer.error?
    @dashboard.reload
    assert_equal [ "Checkout v2", "Orders", "24h", "1m" ], [ @dashboard.name, @dashboard.description, @dashboard.default_range, @dashboard.auto_refresh ]
    assert_equal "Checkout v2", answer.json["name"]
    assert_equal "http://console.test/acme/dashboards/#{@dashboard.id}", answer.link

    assert_match(/Give at least one of name/, call_tool(:update_dashboard, user: @user, dashboard: @dashboard.id).text)
    assert_equal "Not saved: Name can't be blank.", call_tool(:update_dashboard, user: @user, dashboard: @dashboard.id, name: "").text
  end

  test "update_dashboard replaces the filters with the whole list given, keeping them when the list is invalid" do
    @dashboard.update!(name: "Checkout", filters: [ { name: "q", kind: "search" } ])
    level = { name: "level", kind: "select", options: { from: "list", items: [ { value: "error" } ] } }

    answer = call_tool(:update_dashboard, user: @user, dashboard: @dashboard.id, filters: [ level, { name: "q", kind: "search" } ])

    assert_not answer.error?
    assert_equal %w[level q], @dashboard.reload.filter_names
    assert_equal %w[level q], answer.json["filters"].pluck("name")

    refused = call_tool(:update_dashboard, user: @user, dashboard: @dashboard.id, filters: [ level, level ])
    assert_equal "Not saved: Filters[1] name is already a filter's name.", refused.text
    assert_equal %w[level q], @dashboard.reload.filter_names

    assert_not call_tool(:update_dashboard, user: @user, dashboard: @dashboard.id, filters: []).error?
    assert_equal [ [], "Checkout" ], [ @dashboard.reload.filters, @dashboard.name ]
  end

  test "delete_dashboard deletes the Dashboard and its Charts, linking to the Dashboards page" do
    answer = call_tool(:delete_dashboard, user: @user, dashboard: @dashboard.id)

    assert_not answer.error?
    assert_equal "Deleted Dashboard #{@dashboard.id}, Checkout, and its 1 Chart.", answer.body
    assert_not Dashboard.exists?(@dashboard.id)
    assert_not DashboardChart.exists?(@chart.id)
    assert_equal "http://console.test/acme/dashboards", answer.link
  end

  # Charts

  test "add_chart places the Chart under every Chart, linking to the Dashboard" do
    stub_query_check(@shop)
    answer = call_tool(:add_chart, user: @user, dashboard: @dashboard.id, project: "shop", title: "Errors",
      query: "where level == \"error\" | summarize count() by bin_auto(time)", viz: "bar", w: 12, options: { unit: "ms", decimals: 1 })

    assert_not answer.error?
    added = @dashboard.charts.find_by!(title: "Errors")
    assert_equal [ @shop, "bar", 0, 4, 12, 4 ], [ added.project, added.viz, added.x, added.y, added.w, added.h ]
    assert_equal({ "unit" => "ms", "decimals" => 1 }, added.options)
    assert_equal [ added.id, "shop", 4 ], answer.json.values_at("id", "project", "y")
    assert_equal "http://console.test/acme/dashboards/#{@dashboard.id}", answer.link

    beside = call_tool(:add_chart, user: @user, dashboard: @dashboard.id, project: "shop", title: "Beside", query: "summarize count()", x: 6, y: 0)
    assert_equal [ 6, 0, "line" ], beside.json.values_at("x", "y", "viz")
  end

  test "add_chart answers the Chart's validation errors and adds none" do
    answer = call_tool(:add_chart, user: @user, dashboard: @dashboard.id, project: "shop", title: "Wide", query: "summarize count()", x: 8, w: 6)

    assert answer.error?
    assert_equal "Not saved: W must fit in 12 columns from x.", answer.text
    assert call_tool(:add_chart, user: @user, dashboard: @dashboard.id, project: "shop", title: "Pie", query: "summarize count()", viz: "pie").error?
    assert call_tool(:add_chart, user: @user, dashboard: @dashboard.id, project: "shop", title: "No query").error?
    assert_equal [ @chart ], @dashboard.charts.reload
  end

  test "update_chart changes only the fields given, keeping the options not given" do
    other = create(:project, organization: @acme, slug: "api")
    @chart.update!(options: { unit: "ms", stacked: true })
    check = stub_query_check(other).with(body: { query: "['api']\n| summarize count() by bin_auto(time)" }.to_json)

    answer = call_tool(:update_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id, project: "api", viz: "area",
      w: 12, options: { unit: "", decimals: 2 })

    assert_not answer.error?
    @chart.reload
    assert_equal [ other, "Requests", "area", 12, 4 ], [ @chart.project, @chart.title, @chart.viz, @chart.w, @chart.h ]
    assert_equal({ "stacked" => true, "decimals" => 2 }, @chart.options)
    assert_equal "['api']\n| summarize count() by bin_auto(time)", @chart.query
    assert_equal "api", answer.json["project"]
    assert_equal "http://console.test/acme/dashboards/#{@dashboard.id}", answer.link
    assert_requested check, times: 1

    assert_match(/Give at least one of project, title/, call_tool(:update_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id).text)
    assert_equal "Not saved: H must be in 1..24.", call_tool(:update_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id, h: 30).text
  end

  test "update_chart points the Query's source at a new Project, as the chart editor does, unless a Query is given" do
    stub_query_check(create(:project, organization: @acme, slug: "api"))
    stub_query_check(@shop)
    move = ->(**fields) { call_tool(:update_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id, **fields) }

    @chart.update!(query: "  [ 'shop' ] | where level == \"error\" | summarize count()")
    assert_not move.(project: "api").error?
    assert_equal "['api'] | where level == \"error\" | summarize count()", @chart.reload.query

    @chart.update!(query: "summarize count()")
    move.(project: "shop")
    assert_equal "summarize count()", @chart.reload.query

    move.(project: "api", query: "['shop'] | summarize count()")
    assert_equal "['shop'] | summarize count()", @chart.reload.query
  end

  test "add_chart and update_chart answer EventStore's mistakes in the Query, as the chart editor does, and save none" do
    api = create(:project, organization: @acme, slug: "api")
    shop_check = stub_query_check(@shop, mistakes: [ { message: "unknown stage wher", line: 1, column: 1, end_line: 1, end_column: 5 } ])
    api_check = stub_query_check(api, mistakes: [ { message: "expected ['api'], this Project's slug, or logs, got \"shop\"", line: 1, column: 2 } ])
    query = @chart.query

    added = call_tool(:add_chart, user: @user, dashboard: @dashboard.id, project: "shop", title: "Bad", query: "wher level = | summarize")
    assert added.error?
    assert_equal "Not saved: Query line 1, column 1: unknown stage wher.", added.text
    assert_equal [ @chart ], @dashboard.charts.reload

    changed = call_tool(:update_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id, title: "Bad", query: "wher level = | summarize")
    assert_equal "Not saved: Query line 1, column 1: unknown stage wher.", changed.text
    moved = call_tool(:update_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id, project: "api", query: "['shop'] | summarize count()")
    assert_equal "Not saved: Query line 1, column 2: expected ['api'], this Project's slug, or logs, got \"shop\".", moved.text
    @chart.reload
    assert_equal [ @shop, "Requests", query ], [ @chart.project, @chart.title, @chart.query ]

    assert_not call_tool(:update_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id, title: "Renamed").error?
    assert_requested shop_check, times: 2
    assert_requested api_check, times: 1
  end

  test "delete_chart deletes the Chart, linking to the Dashboard" do
    answer = call_tool(:delete_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id)

    assert_not answer.error?
    assert_equal "Deleted Chart #{@chart.id}, Requests, from Dashboard Checkout.", answer.body
    assert_not DashboardChart.exists?(@chart.id)
    assert_equal "http://console.test/acme/dashboards/#{@dashboard.id}", answer.link
  end

  # Another Organization's

  test "another Organization's Dashboard is not found, and nothing of it changes" do
    no_dashboard = "No such Dashboard. list_dashboards lists them."
    theirs = { dashboard: @their_dashboard.id }

    assert_equal no_dashboard, call_tool(:get_dashboard, user: @user, **theirs).text
    assert_equal no_dashboard, call_tool(:update_dashboard, user: @user, **theirs, name: "Mine").text
    assert_equal no_dashboard, call_tool(:delete_dashboard, user: @user, **theirs).text
    assert_equal no_dashboard, call_tool(:add_chart, user: @user, **theirs, project: "shop", title: "In", query: "summarize count()").text
    assert_equal no_dashboard, call_tool(:update_chart, user: @user, **theirs, chart: @their_chart.id, title: "Mine").text
    assert_equal no_dashboard, call_tool(:delete_chart, user: @user, **theirs, chart: @their_chart.id).text
    assert_equal no_dashboard, call_tool(:get_chart_data, user: @user, **theirs, chart: @their_chart.id).text
    assert_equal [ "Theirs", 1 ], [ @their_dashboard.reload.name, @their_dashboard.charts.count ]
    assert_equal "Checkout", call_tool(:list_dashboards, user: @user).json["dashboards"].sole["name"]
  end

  test "another Organization's Chart is not found on this Organization's Dashboard" do
    ours = { dashboard: @dashboard.id, chart: @their_chart.id }

    assert_equal "No such Chart. get_dashboard lists them.", call_tool(:update_chart, user: @user, **ours, title: "Mine").text
    assert_equal "No such Chart. get_dashboard lists them.", call_tool(:delete_chart, user: @user, **ours).text
    assert_equal "No such Chart. get_dashboard lists them.", call_tool(:get_chart_data, user: @user, **ours).text
    assert DashboardChart.exists?(@their_chart.id)
  end

  test "another Organization's Project is not found for a Chart" do
    answer = call_tool(:add_chart, user: @user, dashboard: @dashboard.id, project: "secret", title: "Theirs", query: "summarize count()")

    assert_equal "No Project secret in acme. list_projects lists them.", answer.text
    assert_equal "No Project secret in acme. list_projects lists them.",
      call_tool(:update_chart, user: @user, dashboard: @dashboard.id, chart: @chart.id, project: "secret").text
    assert_equal [ @chart ], @dashboard.charts.reload
    assert_equal @shop, @chart.reload.project
  end

  # get_chart_data

  test "get_chart_data runs the Chart's saved Query over the Dashboard's range, with the chart timeout, linking to the Dashboard" do
    travel_to Time.utc(2026, 10, 3, 10) do
      table = { columns: [ { name: "time", type: "datetime", bin_seconds: 3600 }, { name: "count_", type: "number", agg: "count" } ],
                rows: [ [ "2026-10-03T09:00:00Z", 12 ] ], truncated: false, fill_gaps: true }
      stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query")
        .with(body: { query: @chart.query, from: "2026-10-02T10:00:00.000Z", to: "2026-10-03T10:00:00.000Z",
                      timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS }.to_json)
        .to_return(status: 200, body: { table: table, elapsed_ms: 41 }.to_json)

      answer = call_tool(:get_chart_data, user: @user, dashboard: @dashboard.id, chart: @chart.id)

      assert_not answer.error?
      assert_equal({ "chart" => @chart.id, "title" => "Requests", "viz" => "line",
                     "from" => "2026-10-02T10:00:00.000Z", "to" => "2026-10-03T10:00:00.000Z",
                     "columns" => [ { "name" => "time", "type" => "datetime", "bin_seconds" => 3600 },
                                    { "name" => "count_", "type" => "number", "agg" => "count" } ],
                     "rows" => [ [ "2026-10-03T09:00:00Z", 12 ] ] }, answer.json)
      assert_equal "http://console.test/acme/dashboards/#{@dashboard.id}?range=24h", answer.link
    end
  end

  test "get_chart_data runs over the range given, and answers at most 100 rows, saying how many more" do
    rows = Array.new(150) { |i| [ "service-#{i}", i ] }
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query")
      .with(body: hash_including(from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z"))
      .to_return(status: 200, body: { table: { columns: [ { name: "service", type: "string" }, { name: "count_", type: "number" } ],
                                               rows: rows, truncated: false } }.to_json)

    answer = call_tool(:get_chart_data, user: @user, dashboard: @dashboard.id, chart: @chart.id,
      from: "2026-10-03T11:00:00+02:00", to: "2026-10-03T10:00:00Z")

    assert_not answer.error?
    assert_equal rows.first(100), answer.json["rows"]
    assert_equal "50 more: narrow the Query.", answer.json["more"]
    assert_equal "http://console.test/acme/dashboards/#{@dashboard.id}?from=2026-10-03T09%3A00%3A00.000Z&to=2026-10-03T10%3A00%3A00.000Z", answer.link
  end

  test "get_chart_data refuses a Chart whose Query answers Logs, saying to end it with summarize" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query").to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)

    answer = call_tool(:get_chart_data, user: @user, dashboard: @dashboard.id, chart: @chart.id, range: "15m")

    assert answer.error?
    assert_equal "This Query answers Logs, not a table. End it with summarize, count or project to chart it.", answer.text
  end

  test "get_chart_data answers EventStore's mistakes in the Query and a busy Console as tool errors" do
    run = stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query")
      .to_return(status: 400, body: { errors: [ { message: "unknown stage wher", line: 2, column: 3 } ] }.to_json)

    assert_equal "line 2, column 3: unknown stage wher", call_tool(:get_chart_data, user: @user, dashboard: @dashboard.id, chart: @chart.id).text

    holding_every_chart_run do
      assert_match(/busy/, call_tool(:get_chart_data, user: @user, dashboard: @dashboard.id, chart: @chart.id).text)
    end
    assert_requested run, times: 1
  end

  private

  # Answers EventStore's check of a Query on project, with mistakes or none.
  def stub_query_check(project, mistakes: [])
    stub_request(:post, "#{EventStore.base_url}/api/#{project.id}/logs/validate")
      .to_return(status: 200, body: { errors: mistakes, source: nil, chips: nil, stages: [] }.to_json)
  end

  def holding_every_chart_run(left = ChartRuns.limit, &block)
    left.zero? ? yield : ChartRuns.hold { holding_every_chart_run(left - 1, &block) }
  end
end
