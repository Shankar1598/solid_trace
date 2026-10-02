# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

class IssuesControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user
    @project = create(:project, organization: @organization)
    @issue = create(:issue, project: @project) # This will be issue #1

    sign_in_as(@user)
  end

  test "should show issue using number param" do
    get project_issue_url(@project, @issue, org_slug: @organization.slug)
    assert_response :success
  end

  test "should resolve issue using number param" do
    patch resolve_project_issue_url(@project, @issue, org_slug: @organization.slug)
    assert_redirected_to project_issue_url(@project, @issue, org_slug: @organization.slug)
    assert_equal "resolved", @issue.reload.status
  end

  test "should unresolve issue using number param" do
    @issue.update!(status: :resolved)
    patch unresolve_project_issue_url(@project, @issue, org_slug: @organization.slug)
    assert_redirected_to project_issue_url(@project, @issue, org_slug: @organization.slug)
    assert_equal "unresolved", @issue.reload.status
  end

  test "issues list serves counts from the issue without calling EventStore" do
    @issue.update!(times_seen: 7, first_seen_at: Time.utc(2026, 1, 1), last_seen_at: Time.utc(2026, 1, 3))
    create(:issue, project: @project)
    stub_request(:any, /#{Regexp.escape(EventStore.base_url)}/)

    issue = issues_page["issues"].find { |i| i["id"] == @issue.id }

    assert_not_requested :any, /#{Regexp.escape(EventStore.base_url)}/
    assert_equal 7, issue["events_count"]
    assert_equal "2026-01-01T00:00:00Z", issue["first_seen_at"]
    assert_equal "2026-01-03T00:00:00Z", issue["last_seen_at"]
  end

  test "an issue with no counted events is last seen when created" do
    issue = issues_page["issues"].first

    assert_equal 0, issue["events_count"]
    assert_equal @issue.created_at.iso8601, issue["last_seen_at"]
    assert_equal @issue.created_at.iso8601, issue["first_seen_at"]
  end

  test "issues list is paginated 25 per page" do
    25.times { create(:issue, project: @project) }

    first = issues_page
    second = issues_page(page: 2)

    assert_equal 25, first["issues"].size
    assert_equal 1, second["issues"].size
    assert_equal({ "current_page" => 1, "total_pages" => 2, "total_count" => 26 }, first["pagination"])
    assert_equal 2, second["pagination"]["current_page"]
    assert_empty first["issues"].map { |i| i["id"] } & second["issues"].map { |i| i["id"] }
  end

  test "pagination counts only the issues that match the filters" do
    other_project = create(:project, organization: @organization)
    30.times { create(:issue, project: other_project) }
    3.times { create(:issue, project: @project, status: :resolved) }

    page = issues_page(project_id: @project.id, status: "unresolved")

    assert_equal [ @issue.id ], page["issues"].map { |i| i["id"] }
    assert_equal({ "current_page" => 1, "total_pages" => 1, "total_count" => 1 }, page["pagination"])
  end

  test "issues list sorts by last seen by default and by first seen on request" do
    @issue.update!(first_seen_at: Time.utc(2026, 1, 1), last_seen_at: Time.utc(2026, 1, 9))
    newer = create(:issue, project: @project, first_seen_at: Time.utc(2026, 1, 5), last_seen_at: Time.utc(2026, 1, 6))
    unseen = create(:issue, project: @project, created_at: Time.utc(2026, 1, 7))

    assert_equal [ @issue.id, unseen.id, newer.id ], issues_page["issues"].map { |i| i["id"] }
    assert_equal "last_seen", issues_page["filters"]["sort"]
    assert_equal [ unseen.id, newer.id, @issue.id ], issues_page(sort: "first_seen")["issues"].map { |i| i["id"] }
  end

  private

  def issues_page(params = {})
    get issues_url(org_slug: @organization.slug), params: params,
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    JSON.parse(response.body)["props"]
  end
end
