# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

class DashboardFiltersControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @base_url = "http://127.0.0.1:4100"
    @previous_url = ENV["EVENT_STORE_URL"]
    ENV["EVENT_STORE_URL"] = @base_url

    @organization = create(:organization)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @project = create(:project, organization: @organization, name: "Storefront", slug: "storefront")
    @dashboard = create(:dashboard, organization: @organization, filters: [
      { name: "service", kind: "select", options: { from: "field", project_id: @project.id, field: "service" } },
      { name: "level", kind: "select", options: { from: "list", items: [ { value: "error" } ] } },
      { name: "q", kind: "search" }
    ])
    @range = { from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    sign_in_as(@member)
  end

  teardown do
    ENV["EVENT_STORE_URL"] = @previous_url
  end

  test "a field's values are its 100 most common in the filter's Project over the range, most common first, each as its text" do
    rows = [ [ "api", 12 ], [ 500, 3 ], [ 1.5, 2 ], [ true, 1 ] ]
    run = stub_values("['storefront'] | where isnotnull(['service']) | summarize count() by ['service'] | sort by count_ desc | take 100", rows)

    get values_url("service"), params: @range

    assert_response :success
    assert_requested run
    assert_equal [ { "value" => "api", "count" => 12 }, { "value" => "500", "count" => 3 }, { "value" => "1.5", "count" => 2 },
      { "value" => "true", "count" => 1 } ], response.parsed_body["values"]
  end

  test "the Project's slug and the field are escaped in their brackets" do
    @project.update_column(:slug, "it's")
    @dashboard.update!(filters: [ { name: "odd", kind: "select", options: { from: "field", project_id: @project.id, field: "o'brien\\path\tx" } } ])
    run = stub_values("['it\\'s'] | where isnotnull(['o\\'brien\\\\path\\tx']) | summarize count() by ['o\\'brien\\\\path\\tx'] " \
                      "| sort by count_ desc | take 100", [])

    get values_url("odd"), params: @range

    assert_response :success
    assert_requested run
    assert_equal [], response.parsed_body["values"]
  end

  test "a filter whose Project isn't one of the Organization's finds nothing, and runs nothing" do
    @dashboard.update_column(:filters, [ { "name" => "service", "label" => "service", "kind" => "select", "multiple" => false,
                                           "options" => { "from" => "field", "project_id" => create(:project).id, "field" => "service" } } ])
    run = stub_request(:post, %r{#{@base_url}/api/\d+/logs/query})

    get values_url("service"), params: @range

    assert_response :not_found
    assert_not_requested run
  end

  test "only a select of a field's values has values to load" do
    %w[level q nope].each do |name|
      get values_url(name), params: @range
      assert_response :not_found
    end
  end

  test "a field's values need a range of two times, and are a chart run" do
    get values_url("service"), params: { from: "yesterday" }
    assert_response :bad_request

    run = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
    holding_every_chart_run { get values_url("service"), params: @range }

    assert_response :service_unavailable
    assert_not_requested run
  end

  test "a field's values answer EventStore's errors" do
    errors = [ { message: "unknown field", line: 1, column: 1, end_line: 1, end_column: 5 } ]
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return(status: 400, body: { errors: errors }.to_json)

    get values_url("service"), params: @range

    assert_response :bad_request
    assert_equal "unknown field", response.parsed_body["errors"].first["message"]
  end

  private

  def values_url(name)
    values_dashboard_filter_url(@dashboard, name, org_slug: @organization.slug)
  end

  def stub_values(query, rows)
    table = { columns: [ { name: "service", type: "string" }, { name: "count_", type: "number", agg: "count" } ], rows: rows, truncated: false }
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
      .with(body: { query: query, from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z",
                    timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS }.to_json)
      .to_return(status: 200, body: { table: table }.to_json)
  end

  # Runs the block while this process holds every chart run it allows.
  def holding_every_chart_run(left = ChartRuns.limit, &block)
    left.zero? ? yield : ChartRuns.hold { holding_every_chart_run(left - 1, &block) }
  end
end
