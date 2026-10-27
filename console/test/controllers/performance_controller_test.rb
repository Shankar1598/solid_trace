# frozen_string_literal: true

require "test_helper"

# A Project's Performance page, its Transactions by name, and a Transaction's
# page, from EventStore's /transactions and /transaction, for any member.
class PerformanceControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper
  include PerformanceAnswers

  setup do
    @organization = create(:organization, slug: "acme")
    @member = create(:user)
    @organization.organization_users.create!(user: @member, role: :member)
    @api = create(:project, organization: @organization, name: "Api", slug: "api")
    create(:project, organization: create(:organization, slug: "theirs"), slug: "secret")
    travel_to Time.utc(2026, 10, 2, 12, 0, 0)
    sign_in_as(@member)
  end

  def props(path, component, key)
    get path, headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal component, response.parsed_body["component"]
    response.parsed_body.dig("props", key)
  end

  test "Performance lists the Project's Transactions over the last day, with each number's change" do
    asked = stub_transactions(@api, from: "2026-10-01T12:00:00.000Z", to: "2026-10-02T12:00:00.000Z")

    performance = props(project_performance_path(@api, org_slug: "acme"), "Performance/Show", "performance")

    assert_requested asked
    assert_performance(performance, span: "24h", environment: nil)
  end

  test "the range and environment come from the URL" do
    asked = stub_transactions(@api, from: "2026-09-25T12:00:00.000Z", to: "2026-10-02T12:00:00.000Z", environment: "production")

    performance = props(project_performance_path(@api, org_slug: "acme", range: "7d", environment: "production"),
      "Performance/Show", "performance")

    assert_requested asked
    assert_equal "7d", performance.dig("range", "span")
    assert_equal "production", performance["environment"]

    asked = stub_transactions(@api, from: "2026-10-02T09:00:00.000Z", to: "2026-10-02T10:00:00.000Z")
    performance = props(project_performance_path(@api, org_slug: "acme", from: "2026-10-02T09:00:00Z", to: "2026-10-02T10:00:00Z"),
      "Performance/Show", "performance")
    assert_requested asked
    assert_equal [ nil, "2026-10-02T09:00:00.000Z", "2026-10-02T10:00:00.000Z" ], performance["range"].values_at("span", "from", "to")
  end

  test "a Transaction's page shows its releases, self time, repeats and slowest traces" do
    asked = stub_transaction(@api, "OrdersController#show", from: "2026-10-01T12:00:00.000Z", to: "2026-10-02T12:00:00.000Z")

    transaction = props(transaction_project_performance_path(@api, org_slug: "acme", name: "OrdersController#show"),
      "Performance/Transaction", "transaction")

    assert_requested asked
    assert_transaction(transaction)
  end

  test "the pages say EventStore is unavailable when it can't answer" do
    stub_request(:post, %r{/api/#{@api.id}/transactions?\z}).to_return(status: 503, body: "")

    performance = props(project_performance_path(@api, org_slug: "acme"), "Performance/Show", "performance")
    assert_equal false, performance.dig("event_store", "available")
    assert_equal [], performance["transactions"]

    transaction = props(transaction_project_performance_path(@api, org_slug: "acme", name: "OrdersController#show"),
      "Performance/Transaction", "transaction")
    assert_equal false, transaction.dig("event_store", "available")
    assert_equal [], transaction["slowest"]
  end

  test "the Organization's Performance opens the Project last seen there, else the newest" do
    @api.update!(created_at: 1.day.ago)
    create(:project, organization: @organization, name: "Web", slug: "web")

    get performance_path(org_slug: "acme")
    assert_redirected_to project_performance_path("web", org_slug: "acme")

    stub_transactions(@api, from: "2026-10-01T12:00:00.000Z", to: "2026-10-02T12:00:00.000Z")
    props(project_performance_path(@api, org_slug: "acme"), "Performance/Show", "performance")
    get performance_path(org_slug: "acme")
    assert_redirected_to project_performance_path("api", org_slug: "acme")
  end

  test "the Organization's Performance with no Project goes to Projects" do
    @organization.projects.destroy_all

    get performance_path(org_slug: "acme")

    assert_redirected_to projects_path(org_slug: "acme")
    assert_equal "Create a Project to see its Performance.", flash[:notice]
  end

  test "another Organization's Project is not found" do
    get project_performance_path("secret", org_slug: "theirs")
    assert_response :not_found
    get project_performance_path("secret", org_slug: "acme")
    assert_response :not_found
  end
end
