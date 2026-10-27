# frozen_string_literal: true

require "test_helper"

# list_transactions and get_transaction: a Project's Transactions and one
# Transaction's breakdown, as the Performance pages show them.
class McpPerformanceToolsTest < ActiveSupport::TestCase
  include McpTestHelper
  include PerformanceAnswers

  setup do
    @user = create(:user)
    @acme = create(:organization, slug: "acme")
    create(:organization_user, organization: @acme, user: @user, role: :member)
    @api = create(:project, organization: @acme, name: "Api", slug: "api")
    create(:project, organization: create(:organization, slug: "theirs"), slug: "secret")
    travel_to Time.utc(2026, 10, 2, 12, 0, 0)
  end

  test "list_transactions answers Performance over the last day, linking to the page" do
    asked = stub_transactions(@api, from: "2026-10-01T12:00:00.000Z", to: "2026-10-02T12:00:00.000Z")

    got = call_tool(:list_transactions, user: @user, project: "api")

    assert_not got.error?
    assert_requested asked
    assert_performance(got.json, span: "24h", environment: nil)
    assert_equal "http://console.test/acme/projects/api/performance?range=24h", got.link
  end

  test "list_transactions reads one environment over a range" do
    asked = stub_transactions(@api, from: "2026-09-25T12:00:00.000Z", to: "2026-10-02T12:00:00.000Z", environment: "production")

    got = call_tool(:list_transactions, user: @user, project: "api", range: "7d", environment: "production")

    assert_requested asked
    assert_equal "production", got.json["environment"]
    assert_equal "http://console.test/acme/projects/api/performance?environment=production&range=7d", got.link
  end

  test "get_transaction answers a Transaction's breakdown, linking to its page" do
    asked = stub_transaction(@api, "OrdersController#show", from: "2026-10-01T12:00:00.000Z", to: "2026-10-02T12:00:00.000Z")

    got = call_tool(:get_transaction, user: @user, project: "api", name: "OrdersController#show")

    assert_not got.error?
    assert_requested asked
    assert_transaction(got.json)
    assert_equal "http://console.test/acme/projects/api/performance/transaction?name=OrdersController%23show&range=24h", got.link
  end

  test "another Organization's Project and an unavailable EventStore are tool errors" do
    got = call_tool(:list_transactions, user: @user, organization: "theirs", project: "secret")
    assert got.error?

    stub_request(:post, "#{EventStore.base_url}/api/#{@api.id}/transaction").to_return(status: 503, body: "")
    got = call_tool(:get_transaction, user: @user, project: "api", name: "OrdersController#show")
    assert got.error?
    assert_match "EventStore", got.text
  end
end
