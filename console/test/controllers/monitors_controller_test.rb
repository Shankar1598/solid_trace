# frozen_string_literal: true

require "test_helper"

class MonitorsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include MonitorTestHelper
  include ActiveSupport::Testing::TimeHelpers

  setup do
    @base_url = "http://127.0.0.1:4100"
    @previous_url = ENV["EVENT_STORE_URL"]
    ENV["EVENT_STORE_URL"] = @base_url

    @organization = create(:organization)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @project = create(:project, organization: @organization, name: "Storefront")
    @slack = @organization.integrations.create!(provider: "slack", name: "Alerts", settings: { "webhook_url" => "http://127.0.0.1:9/hook" })
    stub_monitor_query_check(@project)
    @monitor = create(:monitor, project: @project, name: "Checkout errors", integrations: [ @slack ])

    @outsider = create(:user)
    create(:organization_user, organization: create(:organization), user: @outsider, role: :admin)
    @foreign_project = create(:project, organization: create(:organization), name: "Elsewhere")
    @foreign_integration = @foreign_project.organization.integrations.create!(provider: "slack", settings: { "webhook_url" => "http://127.0.0.1:9/x" })

    sign_in_as(@member)
  end

  teardown do
    ENV["EVENT_STORE_URL"] = @previous_url
  end

  # ---------------------------------------------------------------------------
  # Pages
  # ---------------------------------------------------------------------------

  test "a member lists the Organization's Monitors with their status, Project, kind and last run" do
    stub_monitor_query_check(@project)
    firing = create(:monitor, project: @project, name: "Slow pages", last_run_at: Time.utc(2026, 10, 3, 9))
    firing.states.create!(group_key: "", status: "firing", since: Time.utc(2026, 10, 3, 9), last_value: 12)
    create(:monitor, project: @project, name: "Broken", last_error: "EventStore is unavailable")
    create(:monitor, project: @project, name: "Quiet", disabled_at: Time.current)
    other = create(:project, organization: create(:organization))
    stub_monitor_query_check(other)
    create(:monitor, project: other, name: "Not ours")

    props = inertia_page(monitors_url(org_slug: @organization.slug), "Monitors/Index")

    rows = props["monitors"].map { |m| m.values_at("name", "status", "kind", "firing_count") + [ m["project"]["name"] ] }
    assert_equal [
      [ "Broken", "error", "threshold", 0, "Storefront" ],
      [ "Checkout errors", "ok", "threshold", 0, "Storefront" ],
      [ "Quiet", "disabled", "threshold", 0, "Storefront" ],
      [ "Slow pages", "firing", "threshold", 1, "Storefront" ]
    ], rows
    assert_equal "2026-10-03T09:00:00Z", props["monitors"].last["last_run_at"]
  end

  test "every page knows the Organization's Monitors, for the palette" do
    props = inertia_page(monitors_url(org_slug: @organization.slug), "Monitors/Index")

    assert_equal [ { "id" => @monitor.id, "name" => "Checkout errors" } ], props["org_monitors"]
  end

  test "a member opens a Monitor with its groups, runs and Integrations" do
    @monitor.states.create!(group_key: { service: "api" }.to_json, status: "firing", since: Time.utc(2026, 10, 3, 9), last_value: 14)
    @monitor.runs.create!(ran_at: Time.utc(2026, 10, 3, 9), status: "ok", firing: 1, elapsed_ms: 40)
    @monitor.runs.create!(ran_at: Time.utc(2026, 10, 3, 9, 5), status: "error", error: "EventStore is unavailable")

    props = inertia_page(monitor_url(@monitor, org_slug: @organization.slug), "Monitors/Show")

    monitor = props["monitor"]
    assert_equal [ "Checkout errors", "firing", @monitor.query ], monitor.values_at("name", "status", "query")
    state = monitor["states"].sole
    assert_equal [ { "service" => "api" }, "firing", "2026-10-03T09:00:00Z", 14.0 ], state.values_at("group", "status", "since", "last_value")
    assert_equal [ "error", "ok" ], monitor["runs"].map { |r| r["status"] }
    assert_equal "EventStore is unavailable", monitor["runs"].first["error"]
    assert_equal [ [ @slack.id, "Alerts", "slack" ] ], monitor["integrations"].map { |i| i.values_at("id", "name", "provider") }
  end

  test "the New page starts from the explorer's Project, Query and range, with the Organization's Projects and Integrations" do
    query = "['#{@project.slug}'] | summarize count()"
    props = inertia_page(new_monitor_url(org_slug: @organization.slug, project_id: @project.id, query: query, range_minutes: 60), "Monitors/New")

    assert_equal [ @project.id, query, 60, "threshold" ], props["monitor"].values_at("project_id", "query", "range_minutes", "kind")
    assert_equal [ "Storefront" ], props["projects"].map { |p| p["name"] }
    assert_equal [ [ @slack.id, "Alerts", "slack" ] ], props["integrations"].map { |i| i.values_at("id", "name", "provider") }
  end

  test "the New page ignores another Organization's Project and a range out of bounds" do
    props = inertia_page(new_monitor_url(org_slug: @organization.slug, project_id: @foreign_project.id, range_minutes: 99_999_999), "Monitors/New")

    assert_equal [ @project.id, LogMonitor::MAX_RANGE_MINUTES ], props["monitor"].values_at("project_id", "range_minutes")
  end

  test "the Edit page has the Monitor and its Integrations" do
    props = inertia_page(edit_monitor_url(@monitor, org_slug: @organization.slug), "Monitors/Edit")

    assert_equal [ @monitor.id, "Checkout errors", [ @slack.id ] ], props["monitor"].values_at("id", "name", "integration_ids")
  end

  # ---------------------------------------------------------------------------
  # Saving
  # ---------------------------------------------------------------------------

  test "a member makes a Threshold monitor with its Integrations and is taken to it" do
    post monitors_url(org_slug: @organization.slug), params: { monitor: monitor_params(name: "Errors by service") }, as: :json

    monitor = @organization.monitors.find_by!(name: "Errors by service")
    assert_redirected_to monitor_url(monitor, org_slug: @organization.slug)
    assert_equal [ @project, "threshold", ">=", 5.0, 15, 2, true, true ],
      [ monitor.project, monitor.kind, monitor.operator, monitor.threshold, monitor.range_minutes, monitor.interval_minutes,
        monitor.notify_by_group, monitor.alert_on_no_data ]
    assert_equal [ @slack ], monitor.integrations
  end

  test "a Monitor on another Organization's Project is refused as having no Project" do
    assert_no_difference -> { LogMonitor.count } do
      post monitors_url(org_slug: @organization.slug), params: { monitor: monitor_params(project_id: @foreign_project.id) }, as: :json
    end

    assert_redirected_to new_monitor_url(org_slug: @organization.slug)
    assert inertia_page(new_monitor_url(org_slug: @organization.slug), "Monitors/New")["errors"]["project"].present?
  end

  test "another Organization's Integration is refused" do
    assert_no_difference -> { LogMonitor.count } do
      post monitors_url(org_slug: @organization.slug),
        params: { monitor: monitor_params(integration_ids: [ @slack.id, @foreign_integration.id ]) }, as: :json
    end

    errors = inertia_page(new_monitor_url(org_slug: @organization.slug), "Monitors/New")["errors"]
    assert_equal [ "must belong to the Monitor's Organization" ], errors["integrations"]
  end

  test "the Query check's errors come back on the Query" do
    post monitors_url(org_slug: @organization.slug),
      params: { monitor: monitor_params(query: "['#{@project.slug}']\n| where level == 'error'") }, as: :json

    errors = inertia_page(new_monitor_url(org_slug: @organization.slug), "Monitors/New")["errors"]
    assert_match "must end in a summarize", Array(errors["query"]).first
  end

  test "a member changes a Monitor and its Integrations" do
    patch monitor_url(@monitor, org_slug: @organization.slug),
      params: { monitor: { name: "Checkout errors (api)", threshold: 20, integration_ids: [] } }, as: :json

    assert_redirected_to monitor_url(@monitor, org_slug: @organization.slug)
    @monitor.reload
    assert_equal [ "Checkout errors (api)", 20.0, [] ], [ @monitor.name, @monitor.threshold, @monitor.integrations.to_a ]
  end

  test "a change refused keeps the Monitor's Integrations" do
    patch monitor_url(@monitor, org_slug: @organization.slug),
      params: { monitor: { name: "", integration_ids: [] } }, as: :json

    assert_redirected_to edit_monitor_url(@monitor, org_slug: @organization.slug)
    assert_equal [ "Checkout errors", [ @slack ] ], [ @monitor.reload.name, @monitor.integrations.to_a ]
    assert inertia_page(edit_monitor_url(@monitor, org_slug: @organization.slug), "Monitors/Edit")["errors"]["name"].present?
  end

  test "a member deletes a Monitor" do
    delete monitor_url(@monitor, org_slug: @organization.slug)

    assert_redirected_to monitors_url(org_slug: @organization.slug)
    assert_not LogMonitor.exists?(@monitor.id)
    assert @slack.reload
  end

  test "a member turns a Monitor off and on again" do
    patch disable_monitor_url(@monitor, org_slug: @organization.slug)
    assert_redirected_to monitor_url(@monitor, org_slug: @organization.slug)
    assert_not @monitor.reload.enabled?

    patch enable_monitor_url(@monitor, org_slug: @organization.slug)
    assert_redirected_to monitor_url(@monitor, org_slug: @organization.slug)
    assert @monitor.reload.enabled?
  end

  test "a Monitor whose Query no longer runs stays off, saying why" do
    @monitor.update!(disabled_at: Time.current)
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/validate").to_return(status: 503, body: "down")

    patch enable_monitor_url(@monitor, org_slug: @organization.slug)

    assert_not @monitor.reload.enabled?
    assert_match "EventStore is unavailable", flash[:alert]
  end

  # ---------------------------------------------------------------------------
  # Run now
  # ---------------------------------------------------------------------------

  test "Run now runs the Monitor at once: it fires and tells its Integrations" do
    travel_to Time.utc(2026, 10, 3, 10) do
      stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
        .with(body: hash_including("from" => "2026-10-03T09:54:00.000Z", "to" => "2026-10-03T09:59:00.000Z"))
        .to_return(status: 200, body: { table: { columns: [ COUNT_COLUMN ], rows: [ [ 42 ] ] } }.to_json)

      post run_monitor_url(@monitor, org_slug: @organization.slug)
    end

    assert_redirected_to monitor_url(@monitor, org_slug: @organization.slug)
    assert_equal "Ran the Monitor: 1 group firing.", flash[:notice]
    assert_equal [ "firing", 42.0 ], @monitor.states.sole.values_at(:status, :last_value)
    assert_equal [ "ok", 1 ], @monitor.runs.sole.values_at(:status, :firing)
    assert_equal [ [ @slack.id, "monitor_firing" ] ], Notification.where(monitor_id: @monitor.id).pluck(:integration_id, :kind)
  end

  test "Run now says why a run failed" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return(status: 503, body: "down")

    post run_monitor_url(@monitor, org_slug: @organization.slug)

    assert_redirected_to monitor_url(@monitor, org_slug: @organization.slug)
    assert_match "The run failed", flash[:alert]
    assert @monitor.reload.last_error.present?
  end

  test "Run now leaves a Monitor that is off alone" do
    @monitor.update!(disabled_at: Time.current)

    post run_monitor_url(@monitor, org_slug: @organization.slug)

    assert_equal "Turn the Monitor on to run it.", flash[:alert]
    assert_empty @monitor.runs
  end

  # ---------------------------------------------------------------------------
  # Preview
  # ---------------------------------------------------------------------------

  test "the preview runs a Query on one of the Organization's Projects as a chart runs, with its timeout" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
      .with(body: {
        query: "['#{@project.slug}'] | summarize count() by bin(time, 5m)",
        from: "2026-10-02T10:00:00.000Z",
        to: "2026-10-03T10:00:00.000Z",
        timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS,
      }.to_json)
      .to_return(status: 200, body: { table: { columns: [ COUNT_COLUMN ], rows: [ [ 3 ] ] }, elapsed_ms: 12 }.to_json)

    post preview_monitors_url(org_slug: @organization.slug), params: {
      project_id: @project.id, query: "['#{@project.slug}'] | summarize count() by bin(time, 5m)",
      from: "2026-10-02T10:00:00Z", to: "2026-10-03T10:00:00Z",
    }, as: :json

    assert_response :success
    assert_equal [ [ 3 ] ], response.parsed_body["table"]["rows"]
  end

  test "the preview won't run on another Organization's Project, or past the chart runs Console allows" do
    post preview_monitors_url(org_slug: @organization.slug), params: {
      project_id: @foreign_project.id, query: "x", from: "2026-10-02T10:00:00Z", to: "2026-10-03T10:00:00Z",
    }, as: :json
    assert_response :not_found

    holding_every_chart_run do
      post preview_monitors_url(org_slug: @organization.slug), params: {
        project_id: @project.id, query: "x", from: "2026-10-02T10:00:00Z", to: "2026-10-03T10:00:00Z",
      }, as: :json
    end
    assert_response :service_unavailable
  end

  # ---------------------------------------------------------------------------
  # Scoping
  # ---------------------------------------------------------------------------

  test "someone outside the Organization finds none of its Monitors" do
    sign_in_as(@outsider)
    org = @organization.slug

    get monitors_url(org_slug: org)
    assert_response :not_found
    get monitor_url(@monitor, org_slug: org)
    assert_response :not_found
    get new_monitor_url(org_slug: org)
    assert_response :not_found
    post monitors_url(org_slug: org), params: { monitor: monitor_params }, as: :json
    assert_response :not_found
    patch monitor_url(@monitor, org_slug: org), params: { monitor: { name: "Mine" } }, as: :json
    assert_response :not_found
    post run_monitor_url(@monitor, org_slug: org)
    assert_response :not_found
    patch disable_monitor_url(@monitor, org_slug: org)
    assert_response :not_found
    delete monitor_url(@monitor, org_slug: org)
    assert_response :not_found
    assert @monitor.reload.enabled?
  end

  test "a member can't reach another Organization's Monitor through their own" do
    other = create(:project, organization: create(:organization))
    stub_monitor_query_check(other)
    theirs = create(:monitor, project: other)

    get monitor_url(theirs, org_slug: @organization.slug)
    assert_response :not_found
    post run_monitor_url(theirs, org_slug: @organization.slug)
    assert_response :not_found
  end

  private

  def monitor_params(**overrides)
    {
      project_id: @project.id, name: "Errors", kind: "threshold",
      query: "['#{@project.slug}']\n| where level == 'error'\n| summarize count() by service",
      interval_minutes: 2, range_minutes: 15, operator: ">=", threshold: 5,
      notify_by_group: true, alert_on_no_data: true, integration_ids: [ @slack.id ],
    }.merge(overrides)
  end

  def inertia_page(url, component)
    get url, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal component, response.parsed_body["component"]
    response.parsed_body["props"]
  end

  def holding_every_chart_run(left = ChartRuns.limit, &block)
    left.zero? ? yield : ChartRuns.hold { holding_every_chart_run(left - 1, &block) }
  end
end
