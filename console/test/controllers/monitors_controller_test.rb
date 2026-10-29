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
    failing = create(:monitor, project: @project, name: "Still firing", last_error: "EventStore is unavailable")
    failing.states.create!(group_key: "", status: "firing", since: Time.utc(2026, 10, 3, 9), last_value: 30)
    other = create(:project, organization: create(:organization))
    stub_monitor_query_check(other)
    create(:monitor, project: other, name: "Not ours")

    props = inertia_page(monitors_url(org_slug: @organization.slug), "Monitors/Index")

    rows = props["monitors"].map { |m| m.values_at("name", "status", "kind", "firing_count") + [ m["project"]["name"] ] }
    assert_equal [
      [ "Broken", "error", "threshold", 0, "Storefront" ],
      [ "Checkout errors", "ok", "threshold", 0, "Storefront" ],
      [ "Quiet", "disabled", "threshold", 0, "Storefront" ],
      [ "Slow pages", "firing", "threshold", 1, "Storefront" ],
      [ "Still firing", "firing", "threshold", 1, "Storefront" ]
    ], rows
    assert_equal "EventStore is unavailable", props["monitors"].last["last_error"], "a firing Monitor's failed run is noted"
    assert_equal "2026-10-03T09:00:00Z", props["monitors"][3]["last_run_at"]
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

  test "unticking an Integration on a firing Monitor resolves its firing groups there" do
    @monitor.states.create!(group_key: "", status: "firing", since: Time.current, last_value: 42)

    patch monitor_url(@monitor, org_slug: @organization.slug),
      params: { monitor: { integration_ids: [] } }, as: :json

    assert_redirected_to monitor_url(@monitor, org_slug: @organization.slug)
    assert_equal [ [ @slack.id, "monitor_resolved", "", true ] ],
      Notification.where(monitor_id: @monitor.id).map { |row| [ row.integration_id, row.kind, row.group_key, row.payload["unlinked"] ] }
  end

  test "a member makes a Monitor comparing the change from the range just before, and its pages have it" do
    post monitors_url(org_slug: @organization.slug),
      params: { monitor: monitor_params(name: "Errors doubled", compare: "change", baseline: "previous", operator: "rises", threshold: 100) },
      as: :json

    monitor = @organization.monitors.find_by!(name: "Errors doubled")
    assert_redirected_to monitor_url(monitor, org_slug: @organization.slug)
    assert_equal [ "change", "previous", "rises", 100.0 ], [ monitor.compare, monitor.baseline, monitor.operator, monitor.threshold ]

    monitor.states.create!(group_key: "", status: "firing", since: Time.current, last_value: 120, last_baseline: 60)
    shown = inertia_page(monitor_url(monitor, org_slug: @organization.slug), "Monitors/Show")["monitor"]
    assert_equal [ "change", "previous", "rises" ], shown.values_at("compare", "baseline", "operator")
    assert_equal [ 120.0, 60.0 ], shown["states"].sole.values_at("last_value", "last_baseline")
    edited = inertia_page(edit_monitor_url(monitor, org_slug: @organization.slug), "Monitors/Edit")["monitor"]
    assert_equal [ "change", "previous" ], edited.values_at("compare", "baseline")
  end

  test "a Monitor comparing the change with a value operator, no Baseline or a percent of 0 is refused beside those fields" do
    post monitors_url(org_slug: @organization.slug),
      params: { monitor: monitor_params(name: "Refused", compare: "change", operator: ">", threshold: 0) }, as: :json

    assert_not LogMonitor.exists?(name: "Refused")
    errors = inertia_page(new_monitor_url(org_slug: @organization.slug), "Monitors/New")["errors"]
    assert_equal [ [ "must be rises, falls or changes" ], [ "must be previous, the range just before" ],
                   [ "must be a percent greater than 0" ] ], errors.values_at("operator", "baseline", "threshold")
  end

  test "switching a firing Monitor to compare the change resolves its groups; changing the threshold or direction doesn't" do
    @monitor.states.create!(group_key: "", status: "firing", since: Time.current, last_value: 42)

    patch monitor_url(@monitor, org_slug: @organization.slug),
      params: { monitor: { compare: "change", baseline: "previous", operator: "rises", threshold: 50 } }, as: :json

    assert_redirected_to monitor_url(@monitor, org_slug: @organization.slug)
    assert_empty @monitor.states.reload
    assert_equal [ [ "monitor_resolved", true ] ],
      Notification.where(monitor_id: @monitor.id).map { |row| [ row.kind, row.payload["compare_changed"] ] }

    @monitor.states.create!(group_key: "", status: "firing", since: Time.current, last_value: 120, last_baseline: 60)
    patch monitor_url(@monitor, org_slug: @organization.slug), params: { monitor: { operator: "changes", threshold: 75 } }, as: :json

    assert @monitor.states.reload.sole.status_firing?
    assert_equal 1, Notification.where(monitor_id: @monitor.id).count
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

  # A write transaction holds SQLite's write lock, so EventStore's check, which
  # can take as long as a run, must not run inside one.
  test "turning a Monitor on, or changing its Query, checks the Query before any transaction opens" do
    @monitor.update!(disabled_at: Time.current)
    outside = ActiveRecord::Base.lease_connection.open_transactions
    seen = []
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").with(body: hash_including("from", "to")).to_return do
      seen << ActiveRecord::Base.lease_connection.open_transactions
      { status: 200, body: { table: { columns: [ COUNT_COLUMN ], rows: [] } }.to_json }
    end

    patch enable_monitor_url(@monitor, org_slug: @organization.slug)
    assert @monitor.reload.enabled?

    patch monitor_url(@monitor, org_slug: @organization.slug),
      params: { monitor: { query: "summarize count() by service", integration_ids: [] } }, as: :json
    assert_equal "summarize count() by service", @monitor.reload.query

    assert_equal [ outside, outside ], seen
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

  test "Run now while a scheduled run of the Monitor is going runs nothing, and says so" do
    @monitor.update_columns(run_started_at: Time.current)
    WebMock::RequestRegistry.instance.reset!

    post run_monitor_url(@monitor, org_slug: @organization.slug)

    assert_redirected_to monitor_url(@monitor, org_slug: @organization.slug)
    assert_equal "The Monitor is running now: its run shows here when it ends.", flash[:alert]
    assert_not_requested(:post, "#{@base_url}/api/#{@project.id}/logs/query")
    assert_empty @monitor.runs
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

  # Comparing the change, the groups that would fire now come from the
  # server, read and compared as a run reads and compares them.

  test "the groups preview reads the range a run would now, then the range just before it, with the run's timeouts" do
    travel_to Time.utc(2026, 10, 17, 12, 0, 0)
    asked = []
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return do |request|
      asked << JSON.parse(request.body).values_at("query", "from", "to", "timeout_seconds")
      { status: 200, body: { table: { columns: [ SERVICE, COUNT_COLUMN ], rows: [ [ "api", 3 ] ], truncated: false } }.to_json }
    end

    answer = preview_groups(range_minutes: 15)

    assert_equal [ change_query, "2026-10-17T11:44:00.000Z", "2026-10-17T11:59:00.000Z", LogMonitor::RUN_TIMEOUT_SECONDS ], asked.first
    assert_equal [ change_query, "2026-10-17T11:29:00.000Z", "2026-10-17T11:44:00.000Z" ], asked.last.first(3)
    assert_operator asked.last.last, :<=, LogMonitor::RUN_TIMEOUT_SECONDS
    assert_equal [ "2026-10-17T11:44:00Z", "2026-10-17T11:59:00Z", "2026-10-17T11:29:00Z", "2026-10-17T11:44:00Z" ],
      answer.values_at("from", "to", "baseline_from", "baseline_to")
    assert_equal [ { "group" => { "service" => "api" }, "value" => 3, "baseline" => 3, "firing" => false } ], answer["groups"]
  end

  test "a group would fire when its number rises, falls or changes by at least the threshold percent" do
    answer_ranges(now: [ [ "api", 90 ], [ "web", 30 ], [ "jobs", 80 ] ],
      before: [ [ "api", 60 ], [ "web", 60 ], [ "jobs", 60 ] ])

    assert_equal [ "api" ], firing_in(preview_groups(operator: "rises"))
    assert_equal [ "web" ], firing_in(preview_groups(operator: "falls"))
    assert_equal [ "api", "web" ], firing_in(preview_groups(operator: "changes"))
  end

  test "in the groups preview a rise from a Baseline of 0 is the biggest rise, a fall below 0 the biggest fall, and 0 then and now no change" do
    answer_ranges(now: [ [ "api", 5 ], [ "web", 0 ], [ "jobs", -3 ] ], before: [ [ "api", 0 ], [ "web", 0 ], [ "jobs", 0 ] ])

    assert_equal [ "api" ], firing_in(preview_groups(operator: "rises", threshold: 1_000_000))
    assert_equal [ "jobs" ], firing_in(preview_groups(operator: "falls", threshold: 1_000_000))
    assert_equal [ "api", "jobs" ], firing_in(preview_groups(operator: "changes", threshold: 1))
  end

  test "in the groups preview a row now with none in the Baseline rises from 0, and a row in the Baseline with none now isn't listed" do
    answer_ranges(now: [ [ "api", 4 ] ], before: [ [ "web", 100 ] ])

    answer = preview_groups(operator: "changes")

    assert_equal [ { "group" => { "service" => "api" }, "value" => 4, "baseline" => 0, "firing" => true } ], answer["groups"]
  end

  test "in the groups preview a negative number's change is worked out from the Baseline's size" do
    answer_ranges(now: [ [ "api", -50 ], [ "web", -150 ] ], before: [ [ "api", -100 ], [ "web", -100 ] ])

    assert_equal [ "api" ], firing_in(preview_groups(operator: "rises")), "-100 to -50 is up 50%"
    assert_equal [ "web" ], firing_in(preview_groups(operator: "falls")), "-100 to -150 is down 50%"
  end

  test "not notifying by group, the groups preview has the one group of the worst change, not the worst number" do
    answer_ranges(now: [ [ "api", 110 ], [ "web", 20 ], [ "jobs", 40 ] ], before: [ [ "api", 100 ], [ "web", 10 ], [ "jobs", 100 ] ])

    one = ->(operator) { preview_groups(operator: operator, notify_by_group: false)["groups"].sole }

    assert_equal({ "group" => {}, "value" => 20, "baseline" => 10, "firing" => true }, one.("rises"), "web is up 100%")
    assert_equal({ "group" => {}, "value" => 40, "baseline" => 100, "firing" => true }, one.("falls"), "jobs is down 60%")
    assert_equal({ "group" => {}, "value" => 20, "baseline" => 10, "firing" => true }, one.("changes"), "+100% beats -60%")
  end

  test "with alert on no data the groups preview fires on no rows now, whatever the Baseline had" do
    answer_ranges(now: [], before: [ [ "api", 100 ] ])

    assert_equal [ { "group" => {}, "value" => nil, "baseline" => nil, "firing" => true } ],
      preview_groups(alert_on_no_data: true)["groups"]
    assert_empty preview_groups["groups"]
  end

  test "with no threshold yet the groups preview lists the groups with nothing firing" do
    answer_ranges(now: [ [ "api", 90 ] ], before: [ [ "api", 10 ] ])

    assert_equal [ { "group" => { "service" => "api" }, "value" => 90, "baseline" => 10, "firing" => false } ],
      preview_groups(threshold: "")["groups"]
  end

  test "a Baseline read that fails, times out or is cut says why in place of the groups" do
    cut = { table: { columns: [ SERVICE, COUNT_COLUMN ], rows: [ [ "api", 200 ] ], truncated: true } }.to_json
    {
      { status: 500, body: "Internal error" } => [ :bad_gateway, /EventStore is unavailable/ ],
      { status: 504, body: { errors: [ { message: "the run was stopped after 30 s" } ] }.to_json } => [ :gateway_timeout, /stopped after 30 s/ ],
      { status: 200, body: cut } => [ :unprocessable_content, /\A#{Regexp.escape(MonitorEvaluation::TABLE_CUT)}\z/ ],
    }.each do |answer, (status, error)|
      answer_ranges(now: [ [ "api", 10 ] ], before: [ [ "api", 60 ] ])
      baseline_from = (Time.current - LogMonitor::PROCESSING_LAG - 10.minutes).utc.iso8601(3)
      stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").with(body: hash_including("from" => baseline_from))
        .to_return(**answer)

      preview_groups

      assert_response status
      assert_match error, response.parsed_body["error"]
    end
  end

  test "the groups preview won't run on another Organization's Project, or past the chart runs Console allows" do
    preview_groups(project_id: @foreign_project.id)
    assert_response :not_found

    holding_every_chart_run { preview_groups }
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

  SERVICE = { "name" => "service", "type" => "string" }.freeze

  def change_query
    "['#{@project.slug}'] | summarize count() by service"
  end

  # The groups preview's answer for a Monitor comparing the change from the
  # range just before, by service, with overrides.
  def preview_groups(**overrides)
    post preview_groups_monitors_url(org_slug: @organization.slug), params: {
      monitor: {
        project_id: @project.id, query: change_query, range_minutes: 5, compare: "change", baseline: "previous",
        operator: "rises", threshold: 50, notify_by_group: true, alert_on_no_data: false,
      }.merge(overrides),
    }, as: :json
    response.parsed_body
  end

  # Freezes the clock and answers the 5-minute range a run now reads with
  # now, and the range just before it with before: rows of service and
  # count_.
  def answer_ranges(now:, before:, range_minutes: 5)
    freeze_time
    to = Time.current - LogMonitor::PROCESSING_LAG
    { to => now, to - range_minutes.minutes => before }.each do |end_at, rows|
      stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
        .with(body: hash_including("from" => (end_at - range_minutes.minutes).utc.iso8601(3)))
        .to_return(status: 200, body: { table: { columns: [ SERVICE, COUNT_COLUMN ], rows: rows, truncated: false } }.to_json)
    end
  end

  # The services of the groups a preview says would fire.
  def firing_in(answer)
    assert_response :success
    answer["groups"].select { |g| g["firing"] }.map { |g| g["group"]["service"] }.sort
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
