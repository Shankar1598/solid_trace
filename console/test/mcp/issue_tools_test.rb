# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# The Issue tools: an Organization's Issues listed as its Issues page lists
# them, an Issue with one of its Events, resolving, assigning and commenting,
# each answer linking to the Issue.
class McpIssueToolsTest < ActiveSupport::TestCase
  include McpTestHelper

  setup do
    @user = create(:user, name: "Ann", email: "ann@acme.test")
    @acme = create(:organization, slug: "acme")
    @membership = create(:organization_user, organization: @acme, user: @user, role: :member)
    @shop = create(:project, organization: @acme, slug: "shop")
    @api = create(:project, organization: @acme, slug: "api")
    @issue = create(:issue, project: @shop, title: "NoMethodError in checkout", culprit: "CheckoutController#create",
      last_seen_at: Time.utc(2026, 10, 3, 9))

    theirs = create(:organization, slug: "theirs")
    @their_issue = create(:issue, project: create(:project, organization: theirs, slug: "secret"), title: "Theirs")
  end

  # Listing

  test "list_issues answers the Organization's Issues, the most recently seen first, linking to its Issues page" do
    create(:issue, project: @api, title: "Timeout in api", last_seen_at: Time.utc(2026, 10, 3, 10), status: :resolved)
    @issue.update!(assignee: @membership)

    answer = call_tool(:list_issues, user: @user)

    assert_not answer.error?
    assert_equal [ "acme", 1, 1, 2 ], answer.json.values_at("organization", "page", "total_pages", "total_count")
    assert_equal [ [ "api", "Timeout in api", "resolved" ], [ "shop", "NoMethodError in checkout", "unresolved" ] ],
      answer.json["issues"].map { |issue| issue.values_at("project", "title", "status") }
    assert_equal({ "project" => "shop", "number" => @issue.number, "title" => "NoMethodError in checkout",
                   "culprit" => "CheckoutController#create", "status" => "unresolved", "kind" => "error", "events_count" => 0,
                   "first_seen_at" => @issue.created_at.iso8601, "last_seen_at" => "2026-10-03T09:00:00Z",
                   "assignee" => { "name" => "Ann", "email" => "ann@acme.test" } },
      answer.json["issues"].last)
    assert_equal "http://console.test/acme/issues", answer.link
  end

  test "list_issues filters by Project, status and title, sorts by first seen, and links to the Issues page so filtered" do
    create(:issue, project: @shop, title: "NoMethodError in cart", status: :resolved)
    newer = create(:issue, project: @shop, title: "NoMethodError in search", first_seen_at: 1.minute.from_now)
    create(:issue, project: @api, title: "NoMethodError in api")

    answer = call_tool(:list_issues, user: @user, project: "shop", status: "unresolved", query: "NoMethod", sort: "first_seen")

    assert_not answer.error?
    assert_equal [ newer.number, @issue.number ], answer.json["issues"].map { |issue| issue["number"] }
    assert_equal "http://console.test/acme/issues?project_id=#{@shop.id}&query=NoMethod&sort=first_seen&status=unresolved", answer.link
  end

  test "list_issues gives the page asked for, #{IssueList::PER_PAGE} a page" do
    IssueList::PER_PAGE.times { |i| create(:issue, project: @api, title: "Old #{i}", last_seen_at: Time.utc(2026, 10, 1)) }

    answer = call_tool(:list_issues, user: @user, page: 2)

    assert_equal [ 2, 2, IssueList::PER_PAGE + 1 ], answer.json.values_at("page", "total_pages", "total_count")
    assert_equal 1, answer.json["issues"].size
    assert_equal "http://console.test/acme/issues?page=2", answer.link
  end

  test "list_issues refuses another Organization's Project" do
    answer = call_tool(:list_issues, user: @user, project: "secret")

    assert answer.error?
    assert_equal "No Project secret in acme. list_projects lists them.", answer.text
  end
end
