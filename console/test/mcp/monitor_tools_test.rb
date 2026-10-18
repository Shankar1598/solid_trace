# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Monitor tools: an Organization's Monitors and Integrations, saving a
# Monitor with its Integrations, turning it off and on, and Run now, each
# answer linking to the Monitor.
class McpMonitorToolsTest < ActiveSupport::TestCase
  include McpTestHelper
  include MonitorTestHelper

  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @shop = create(:project, organization: @acme, name: "Shop", slug: "shop")
    @slack = @acme.integrations.create!(provider: "slack", name: "Alerts", settings: { "webhook_url" => "http://127.0.0.1:9/hook" })
    stub_monitor_query_check(@shop)
    @monitor = create(:monitor, project: @shop, name: "Checkout errors", integrations: [ @slack ])

    theirs = create(:organization, slug: "theirs")
    @their_project = create(:project, organization: theirs, slug: "secret")
    @their_integration = theirs.integrations.create!(provider: "slack", name: "Theirs", settings: { "webhook_url" => "http://127.0.0.1:9/x" })
    stub_monitor_query_check(@their_project)
    @their_monitor = create(:monitor, project: @their_project, name: "Theirs", integrations: [ @their_integration ])
  end

  # Reading

  test "list_monitors answers the Organization's Monitors by name with their status, firing groups and last run, linking to its Monitors page" do
    @monitor.update_columns(last_run_at: Time.utc(2026, 10, 3, 9))
    @monitor.states.create!(group_key: { service: "web" }.to_json, status: "firing", since: Time.utc(2026, 10, 3, 9), last_value: 14)
    @monitor.states.create!(group_key: { service: "api" }.to_json, status: "ok", since: Time.utc(2026, 10, 3, 9), last_value: 1)
    create(:monitor, project: @shop, name: "Api quiet", disabled_at: Time.current)

    answer = call_tool(:list_monitors, user: @user)

    assert_not answer.error?
    assert_equal "acme", answer.json["organization"]
    assert_equal [ [ "Api quiet", "disabled", false, 0, [] ], [ "Checkout errors", "firing", true, 1, [ { "service" => "web" } ] ] ],
      answer.json["monitors"].map { |m| m.values_at("name", "status", "enabled", "firing_count", "firing_groups") }
    assert_equal [ @monitor.id, "threshold", "shop", "2026-10-03T09:00:00Z", nil ],
      answer.json["monitors"].last.values_at("id", "kind", "project", "last_run_at", "last_error")
    assert_equal "http://console.test/acme/monitors", answer.link
  end

  test "get_monitor answers the Monitor with its groups, last runs and Integrations, linking to it" do
    @monitor.states.create!(group_key: { service: "api" }.to_json, status: "firing", since: Time.utc(2026, 10, 3, 9), last_value: 14)
    12.times { |i| @monitor.runs.create!(ran_at: Time.utc(2026, 10, 3, 9, i), status: "ok", firing: 1, elapsed_ms: 40) }

    answer = call_tool(:get_monitor, user: @user, monitor: @monitor.id)

    assert_not answer.error?
    assert_equal [ "Checkout errors", "shop", @monitor.query, "firing", [ @slack.id ] ],
      answer.json.values_at("name", "project", "query", "status", "integration_ids")
    assert_equal [ { "service" => "api" }, "firing", 14.0 ], answer.json["states"].sole.values_at("group", "status", "last_value")
    assert_nil answer.json["more_states"]
    assert_equal 10, answer.json["runs"].size
    assert_equal [ "2026-10-03T09:11:00Z", "ok", 1 ], answer.json["runs"].first.values_at("ran_at", "status", "firing")
    assert_equal [ { "id" => @slack.id, "name" => "Alerts", "provider" => "slack", "enabled" => true } ], answer.json["integrations"]
    assert_equal "http://console.test/acme/monitors/#{@monitor.id}", answer.link
  end

  test "get_monitor gives at most 100 groups, firing first, saying how many more" do
    since = Time.utc(2026, 10, 3, 9)
    MonitorState.insert_all(Array.new(102) do |i|
      { monitor_id: @monitor.id, group_key: { n: format("%03d", i) }.to_json, status: i == 101 ? "firing" : "ok", since: since, last_value: i }
    end)

    answer = call_tool(:get_monitor, user: @user, monitor: @monitor.id)

    assert_equal 100, answer.json["states"].size
    assert_equal [ { "n" => "101" }, "firing" ], answer.json["states"].first.values_at("group", "status")
    assert_equal "2 more groups: the Monitor's page lists them.", answer.json["more_states"]
  end

  test "list_integrations answers the Organization's Integrations by name without their settings, linking to its Integrations page" do
    @acme.integrations.create!(provider: "pagerduty", name: "On call", active: false, settings: { "routing_key" => "secret-key" })

    answer = call_tool(:list_integrations, user: @user)

    assert_not answer.error?
    assert_equal "acme", answer.json["organization"]
    assert_equal [ [ "Alerts", "slack", true ], [ "On call", "pagerduty", false ] ],
      answer.json["integrations"].map { |i| i.values_at("name", "provider", "enabled") }
    assert_equal %w[enabled id name provider], answer.json["integrations"].first.keys.sort
    assert_no_match(/secret-key|hook/, answer.text)
    assert_equal "http://console.test/acme/settings/integrations", answer.link
  end

  # Saving

  test "create_monitor makes a Monitor with its Integrations once EventStore has checked its Query, linking to it" do
    query = "['shop']\n| where level == 'error'\n| summarize count() by service"

    answer = call_tool(:create_monitor, user: @user, project: "shop", name: "Errors by service", kind: "threshold", query: query,
      interval_minutes: 2, range_minutes: 15, operator: ">=", threshold: 5, notify_by_group: true, integration_ids: [ @slack.id ])

    assert_not answer.error?
    made = @acme.monitors.find_by!(name: "Errors by service")
    assert_equal [ @shop, "threshold", query, ">=", 5.0, 15, 2, true, false, true, [ @slack ] ],
      [ made.project, made.kind, made.query, made.operator, made.threshold, made.range_minutes, made.interval_minutes,
        made.notify_by_group, made.alert_on_no_data, made.enabled?, made.integrations.to_a ]
    assert_equal [ made.id, "shop", [ @slack.id ], "ok" ], answer.json.values_at("id", "project", "integration_ids", "status")
    assert_equal "http://console.test/acme/monitors/#{made.id}", answer.link
    assert_requested(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/validate") { |request| JSON.parse(request.body)["query"] == query }
  end

  test "create_monitor answers the Query check's and the Monitor's errors and makes none" do
    logs = call_tool(:create_monitor, user: @user, project: "shop", name: "Errors", kind: "threshold",
      query: "['shop']\n| where level == 'error'", operator: ">", threshold: 1)
    assert logs.error?
    assert_match(/\ANot saved: Query must end in a summarize/, logs.text)

    no_threshold = call_tool(:create_monitor, user: @user, project: "shop", name: "Errors", kind: "threshold", query: "['shop']\n| summarize count()")
    assert_equal "Not saved: Operator must be >, >=, < or <=; Threshold is not a number.", no_threshold.text
    assert_not LogMonitor.exists?(name: "Errors")
  end

  test "another Organization's Integration is refused, and nothing is saved" do
    made = call_tool(:create_monitor, user: @user, project: "shop", name: "Errors", kind: "threshold", query: @monitor.query,
      operator: ">", threshold: 1, integration_ids: [ @slack.id, @their_integration.id ])
    assert_equal "Not saved: Integrations must belong to the Monitor's Organization.", made.text
    assert_not LogMonitor.exists?(name: "Errors")

    changed = call_tool(:update_monitor, user: @user, monitor: @monitor.id, name: "Renamed", integration_ids: [ @their_integration.id ])
    assert_equal "Not saved: Integrations must belong to the Monitor's Organization.", changed.text
    assert_equal [ "Checkout errors", [ @slack ] ], [ @monitor.reload.name, @monitor.integrations.to_a ]
  end

  test "update_monitor changes only the fields given, and replaces the Integrations when they're given" do
    pager = @acme.integrations.create!(provider: "pagerduty", name: "On call", settings: { "routing_key" => "key" })

    answer = call_tool(:update_monitor, user: @user, monitor: @monitor.id, name: "Checkout errors (api)", threshold: 20, integration_ids: [ pager.id ])

    assert_not answer.error?
    @monitor.reload
    assert_equal [ "Checkout errors (api)", 20.0, ">", [ pager ] ], [ @monitor.name, @monitor.threshold, @monitor.operator, @monitor.integrations.to_a ]
    assert_equal [ "Checkout errors (api)", [ pager.id ] ], answer.json.values_at("name", "integration_ids")
    assert_equal "http://console.test/acme/monitors/#{@monitor.id}", answer.link

    call_tool(:update_monitor, user: @user, monitor: @monitor.id, threshold: 30)
    assert_equal [ pager ], @monitor.reload.integrations.to_a, "Integrations not given are kept"
    call_tool(:update_monitor, user: @user, monitor: @monitor.id, integration_ids: [])
    assert_empty @monitor.reload.integrations

    assert_match(/\AGive at least one of project, enabled, name/, call_tool(:update_monitor, user: @user, monitor: @monitor.id).text)
    assert_equal "Not saved: Name can't be blank.", call_tool(:update_monitor, user: @user, monitor: @monitor.id, name: "").text
  end

  test "update_monitor points the Query's source at a new Project, as the Monitor form does, unless a query is given" do
    api = create(:project, organization: @acme, slug: "api")
    stub_monitor_query_check(api)

    assert_not call_tool(:update_monitor, user: @user, monitor: @monitor.id, project: "api").error?
    assert_equal [ api, "['api']\n| where level == 'error'\n| summarize count()" ], [ @monitor.reload.project, @monitor.query ]

    query = "['shop']\n| summarize count() by service"
    assert_not call_tool(:update_monitor, user: @user, monitor: @monitor.id, project: "shop", query: query).error?
    assert_equal [ @shop, query ], [ @monitor.reload.project, @monitor.query ]

    assert_equal "No Project secret in acme. list_projects lists them.",
      call_tool(:update_monitor, user: @user, monitor: @monitor.id, project: "secret").text
    assert_equal @shop, @monitor.reload.project
  end

  # On and off

  test "update_monitor turns a Monitor off, resolving its firing groups, and on again, checking its Query again" do
    @monitor.states.create!(group_key: "", status: "firing", since: Time.current, last_value: 42)
    WebMock::RequestRegistry.instance.reset!
    validate = "#{EventStore.base_url}/api/#{@shop.id}/logs/validate"

    off = call_tool(:update_monitor, user: @user, monitor: @monitor.id, enabled: false)

    assert_not off.error?
    assert_equal [ false, "disabled" ], off.json.values_at("enabled", "status")
    assert_equal [ [ @slack.id, "monitor_resolved" ] ], Notification.where(monitor_id: @monitor.id).pluck(:integration_id, :kind)
    assert_not_requested :post, validate
    off_at = @monitor.reload.disabled_at
    travel 1.minute do
      assert_not call_tool(:update_monitor, user: @user, monitor: @monitor.id, enabled: false).error?
    end
    assert_equal off_at, @monitor.reload.disabled_at, "a Monitor already off stays off from when it was turned off"

    on = call_tool(:update_monitor, user: @user, monitor: @monitor.id, enabled: true)

    assert_not on.error?
    assert_equal [ true, "ok" ], on.json.values_at("enabled", "status")
    assert @monitor.reload.enabled?
    assert_requested :post, validate, times: 1
  end

  test "a Monitor whose Query no longer runs stays off, saying why" do
    @monitor.update!(disabled_at: Time.current)
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/validate").to_return(status: 503, body: "down")

    answer = call_tool(:update_monitor, user: @user, monitor: @monitor.id, enabled: true)

    assert answer.error?
    assert_equal "Not saved, so the Monitor stays off: Query could not be checked, as EventStore is unavailable. Try again shortly.",
      answer.text
    assert_not @monitor.reload.enabled?
  end

  test "delete_monitor deletes the Monitor, linking to the Monitors page" do
    answer = call_tool(:delete_monitor, user: @user, monitor: @monitor.id)

    assert_not answer.error?
    assert_equal "Deleted Monitor #{@monitor.id}, Checkout errors.", answer.body
    assert_not LogMonitor.exists?(@monitor.id)
    assert Integration.exists?(@slack.id)
    assert_equal "http://console.test/acme/monitors", answer.link
  end

  # Run now

  test "run_monitor runs a free Monitor at once: it fires, tells its Integrations, and says what it found" do
    travel_to Time.utc(2026, 10, 3, 10) do
      stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query")
        .with(body: hash_including("from" => "2026-10-03T09:54:00.000Z", "to" => "2026-10-03T09:59:00.000Z"))
        .to_return(status: 200, body: { table: { columns: [ COUNT_COLUMN ], rows: [ [ 42 ] ] } }.to_json)

      answer = call_tool(:run_monitor, user: @user, monitor: @monitor.id)

      assert_not answer.error?
      assert_equal "Ran the Monitor: 1 group firing.", answer.body
      assert_equal "http://console.test/acme/monitors/#{@monitor.id}", answer.link
    end
    assert_equal [ "firing", 42.0 ], @monitor.states.sole.values_at(:status, :last_value)
    assert_equal [ "ok", 1 ], @monitor.runs.sole.values_at(:status, :firing)
    assert_equal [ [ @slack.id, "monitor_firing" ] ], Notification.where(monitor_id: @monitor.id).pluck(:integration_id, :kind)
  end

  test "run_monitor says what a Match monitor's run matched" do
    stub_monitor_query_check(@shop)
    match = create(:monitor, :match, project: @shop, name: "Panics")
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query").with(body: hash_including("after_batch"))
      .to_return(status: 200, body: { logs: [], total: 0, ingest_cursor: NIL_CURSOR, next_cursor: nil }.to_json)

    assert_equal "Ran the Monitor: 0 Logs matched.", call_tool(:run_monitor, user: @user, monitor: match.id).body
  end

  test "run_monitor while another run of the Monitor holds it runs nothing, and says so" do
    @monitor.update_columns(run_started_at: Time.current)
    WebMock::RequestRegistry.instance.reset!

    answer = call_tool(:run_monitor, user: @user, monitor: @monitor.id)

    assert answer.error?
    assert_equal "The Monitor is running now: get_monitor shows that run when it ends.", answer.text
    assert_not_requested :post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query"
    assert_empty @monitor.runs
  end

  test "run_monitor says why a run failed, and leaves a Monitor that is off alone" do
    stub_request(:post, "#{EventStore.base_url}/api/#{@shop.id}/logs/query").to_return(status: 503, body: "down")

    failed = call_tool(:run_monitor, user: @user, monitor: @monitor.id)
    assert failed.error?
    assert_match(/\AThe run failed: /, failed.text)
    assert_equal [ "error" ], @monitor.runs.pluck(:status)

    @monitor.update!(disabled_at: Time.current)
    off = call_tool(:run_monitor, user: @user, monitor: @monitor.id)
    assert_equal "Turn the Monitor on to run it: update_monitor with enabled true.", off.text
    assert_equal 1, @monitor.runs.count
  end

  # Another Organization's

  test "another Organization's Monitor is not found, and its Monitors and Integrations aren't listed" do
    no_monitor = "No such Monitor. list_monitors lists them."
    assert_equal no_monitor, call_tool(:get_monitor, user: @user, monitor: @their_monitor.id).text
    assert_equal no_monitor, call_tool(:update_monitor, user: @user, monitor: @their_monitor.id, name: "Mine").text
    assert_equal no_monitor, call_tool(:update_monitor, user: @user, monitor: @their_monitor.id, enabled: false).text
    assert_equal no_monitor, call_tool(:delete_monitor, user: @user, monitor: @their_monitor.id).text
    assert_equal no_monitor, call_tool(:run_monitor, user: @user, monitor: @their_monitor.id).text
    assert_equal [ "Theirs", true, 0 ], [ @their_monitor.reload.name, @their_monitor.enabled?, @their_monitor.runs.count ]
    assert_equal "No Project secret in acme. list_projects lists them.",
      call_tool(:create_monitor, user: @user, project: "secret", name: "Theirs", kind: "threshold", query: "summarize count()").text
    assert_equal [ "Checkout errors" ], call_tool(:list_monitors, user: @user).json["monitors"].map { |m| m["name"] }
    assert_equal [ "Alerts" ], call_tool(:list_integrations, user: @user).json["integrations"].map { |i| i["name"] }
  end
end
