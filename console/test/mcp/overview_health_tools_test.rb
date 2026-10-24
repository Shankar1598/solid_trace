# frozen_string_literal: true

require "test_helper"

# get_overview's Health: the same section the Overview page shows.
class McpOverviewHealthToolsTest < ActiveSupport::TestCase
  include McpTestHelper
  include MonitorTestHelper

  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @web = create(:project, organization: @acme, name: "Web", slug: "web")
    stub_monitor_query_check(@web)
    travel_to Time.utc(2026, 10, 2, 13, 0, 0)
  end

  test "get_overview answers Health, with Monitors and Issues even when EventStore can't answer" do
    create(:monitor, project: @web, name: "Checkout errors", last_error: "The Query has a mistake")
    create(:issue, project: @web, title: "NoMethodError", first_seen_at: 1.hour.ago)
    stub_request(:post, "#{EventStore.base_url}/overview").to_raise(Errno::ECONNREFUSED)

    got = call_tool(:get_overview, user: @user)

    assert_not got.error?
    health = got.json["health"]
    assert_equal [ "Checkout errors" ], health.dig("monitors", "failing").pluck("name")
    assert_equal 1, health.dig("issues", "new")
    assert_nil health["projects"], "EventStore's parts are null"
  end
end
