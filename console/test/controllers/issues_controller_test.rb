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

  test "an issue page shows its latest Event and a page of its Events" do
    fingerprint = create(:issue_fingerprint, issue: @issue)
    event = event_attrs("evt-1", fingerprint)
    stub_request(:get, %r{/api/#{@project.id}/events/context}).to_return(body: { event: event, prev_uuid: "evt-0" }.to_json)
    stub_request(:get, %r{/api/#{@project.id}/events\?}).to_return(body: [ event ].to_json)
    stub_request(:get, %r{/api/#{@project.id}/events/evt-1}).to_return(body: { message: "boom" }.to_json)

    props = issue_page
    assert_equal "evt-1", props["event"]["id"]
    assert_equal "evt-0", props["prev_event_id"]
    assert_equal [ "evt-1" ], props["events_list"].map { |e| e["id"] }
    assert_nil props["events_unavailable"]
  end

  test "an issue page says why when EventStore can't read Events, instead of showing none" do
    create(:issue_fingerprint, issue: @issue)
    why = "Events can't be read now: the Event archive in the bucket hasn't been listed yet; EventStore tries again every minute"
    stub_request(:get, %r{/api/#{@project.id}/events}).to_return(status: 503, body: { error: why }.to_json)

    props = issue_page
    assert_equal why, props["events_unavailable"]
    assert_nil props["event"]
    assert_empty props["events_list"]
  end

  test "an issue page says when EventStore can't be reached" do
    create(:issue_fingerprint, issue: @issue)
    stub_request(:get, %r{/api/#{@project.id}/events}).to_raise(Errno::ECONNREFUSED)

    assert_equal "Events can't be read now: EventStore can't be reached.", issue_page["events_unavailable"]
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

  test "the issues list resolves and unresolves the issues ticked" do
    other = create(:issue, project: @project)
    untouched = create(:issue, project: @project)

    patch status_issues_url(org_slug: @organization.slug), params: { ids: [ @issue.id, other.id ], status: "resolved" },
      headers: { "Referer" => issues_url(org_slug: @organization.slug, range: "30d") }

    assert_redirected_to issues_url(org_slug: @organization.slug, range: "30d")
    assert_equal "2 Issues resolved", flash[:notice]
    assert_equal %w[resolved resolved unresolved], [ @issue, other, untouched ].map { |i| i.reload.status }

    patch status_issues_url(org_slug: @organization.slug), params: { ids: [ other.id ], status: "unresolved" }
    assert_equal "1 Issue unresolved", flash[:notice]
    assert_equal "unresolved", other.reload.status
  end

  test "the issues list changes only its own organization's issues" do
    stranger = create(:issue, project: create(:project))

    patch status_issues_url(org_slug: @organization.slug), params: { ids: [ stranger.id ], status: "resolved" }

    assert_equal "0 Issues resolved", flash[:notice]
    assert_equal "unresolved", stranger.reload.status
  end

  test "the issues list refuses a status issues don't have" do
    patch status_issues_url(org_slug: @organization.slug), params: { ids: [ @issue.id ], status: "ignored" }

    assert_response :bad_request
    assert_equal "unresolved", @issue.reload.status
  end

  test "issues list serves counts from the issue without calling EventStore" do
    travel_to Time.utc(2026, 1, 10)
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
    travel_to Time.utc(2026, 1, 10)
    @issue.update!(first_seen_at: Time.utc(2026, 1, 1), last_seen_at: Time.utc(2026, 1, 9))
    newer = create(:issue, project: @project, first_seen_at: Time.utc(2026, 1, 5), last_seen_at: Time.utc(2026, 1, 6))
    unseen = create(:issue, project: @project, created_at: Time.utc(2026, 1, 7))

    assert_equal [ @issue.id, unseen.id, newer.id ], issues_page["issues"].map { |i| i["id"] }
    assert_equal "last_seen", issues_page["filters"]["sort"]
    assert_equal [ unseen.id, newer.id, @issue.id ], issues_page(sort: "first_seen")["issues"].map { |i| i["id"] }
  end

  test "issues list keeps the issues seen in the last 14 days by default" do
    travel_to Time.utc(2026, 1, 31)
    @issue.update!(last_seen_at: Time.utc(2026, 1, 10))
    recent = create(:issue, project: @project, last_seen_at: Time.utc(2026, 1, 25))
    unseen = create(:issue, project: @project, created_at: Time.utc(2026, 1, 30))

    page = issues_page

    assert_equal [ unseen.id, recent.id ], page["issues"].map { |i| i["id"] }
    assert_equal 2, page["pagination"]["total_count"]
    assert_equal "14d", page["filters"]["range"]
  end

  test "issues list keeps the issues seen in the range asked for" do
    travel_to Time.utc(2026, 1, 31)
    @issue.update!(last_seen_at: Time.utc(2026, 1, 10))
    today = create(:issue, project: @project, last_seen_at: Time.utc(2026, 1, 30, 12))
    create(:issue, project: @project, last_seen_at: Time.utc(2026, 1, 25))

    assert_equal [ today.id ], issues_page(range: "24h")["issues"].map { |i| i["id"] }
    assert_equal "24h", issues_page(range: "24h")["filters"]["range"]
    assert_equal 3, issues_page(range: "30d")["pagination"]["total_count"]
    assert_equal "14d", issues_page(range: "1y")["filters"]["range"]
  end

  private

  def issues_page(params = {})
    get issues_url(org_slug: @organization.slug), params: params,
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    JSON.parse(response.body)["props"]
  end

  def issue_page
    get project_issue_url(@project, @issue, org_slug: @organization.slug),
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    JSON.parse(response.body)["props"]
  end

  def event_attrs(uuid, fingerprint)
    {
      uuid: uuid, project_id: @project.id, issue_fingerprint_id: fingerprint.id,
      environment: "production", timestamp: Time.current.iso8601, tags: {},
    }
  end
end
