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

  test "another Organization's Monitor is not found, and its Monitors and Integrations aren't listed" do
    assert_equal "No such Monitor. list_monitors lists them.", call_tool(:get_monitor, user: @user, monitor: @their_monitor.id).text
    assert_equal [ "Checkout errors" ], call_tool(:list_monitors, user: @user).json["monitors"].map { |m| m["name"] }
    assert_equal [ "Alerts" ], call_tool(:list_integrations, user: @user).json["integrations"].map { |i| i["name"] }
  end
end
