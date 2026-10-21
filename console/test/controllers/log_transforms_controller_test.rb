# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# A Project's Log transform page: any member reads the program, its versions
# and running counts, and checks and previews a draft; only admins save and
# restore.
# EventStore is stubbed.
class LogTransformsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  ERROR = {
    "message" => "unexpected end of program", "code" => 203, "start" => 9, "end" => 10,
    "text" => "error[E203]: syntax error\n  ┌─ :1:10\n  │\n1 │ .status =\n  │          ^ unexpected end of program",
  }.freeze

  setup do
    @organization = create(:organization)
    @admin = create(:user, name: "Ada")
    create(:organization_user, organization: @organization, user: @admin, role: :admin)
    @member = create(:user, name: "Bob")
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @project = create(:project, organization: @organization)

    @check_url = "#{EventStore.base_url}/api/#{@project.id}/logs/transform/check"
    @stats = { "version_id" => nil, "processed" => 0, "dropped" => 0, "failed" => 0, "average_micros" => 0 }
    stub_request(:get, "#{EventStore.base_url}/api/#{@project.id}/logs/transform/stats")
      .to_return { { status: 200, body: @stats.to_json } }

    sign_in_as(@member)
  end

  test "the page gives a member the program, its versions and running counts, read-only" do
    old = travel_to(Time.utc(2026, 10, 20, 9)) { @project.log_transform_versions.create!(program: ".a = 1", user: @admin) }
    current = travel_to(Time.utc(2026, 10, 21, 9)) { @project.log_transform_versions.create!(program: ".a = 2", user: @admin) }
    @stats = { "version_id" => current.id, "processed" => 120, "dropped" => 3, "failed" => 2, "average_micros" => 14 }

    page = log_transform_page

    assert_equal ".a = 2", page["program"]
    assert_equal false, page["can_manage"]
    assert_equal [ current.id, old.id ], page["versions"].map { |v| v["id"] }
    assert_equal({ "id" => current.id, "program" => ".a = 2", "saved_by" => "Ada", "saved_at" => "2026-10-21T09:00:00Z" }, page["versions"].first)
    assert_equal @stats, page["stats"]
  end

  test "the page lists the newest 20 versions, and a version whose saver is gone" do
    21.times { |n| @project.log_transform_versions.create!(program: ".n = #{n}", user: n.zero? ? nil : @admin) }

    versions = log_transform_page["versions"]

    assert_equal 20, versions.size
    assert_equal ".n = 20", versions.first["program"]
    assert_equal ".n = 1", versions.last["program"]
    @project.log_transform_versions.find_by!(program: ".n = 1").update_columns(user_id: nil)
    assert_nil log_transform_page["versions"].last["saved_by"]
  end

  test "the page opens without running counts when EventStore can't give them" do
    stub_request(:get, "#{EventStore.base_url}/api/#{@project.id}/logs/transform/stats").to_raise(Errno::ECONNREFUSED)

    page = log_transform_page

    assert_nil page["stats"]
    assert_equal "", page["program"]
  end

  test "an admin saves a program, which becomes the current version" do
    check = stub_request(:post, @check_url).with(body: { program: ".status = 1" })
      .to_return(status: 200, body: { errors: [], warnings: [] }.to_json)
    sign_in_as(@admin)

    patch project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".status = 1" }

    assert_redirected_to project_log_transform_url(@project, org_slug: @organization.slug)
    assert_equal "Log transform saved. It applies to Logs processed from now on; stored Logs don't change.", flash[:notice]
    assert_requested check
    assert_equal ".status = 1", @project.log_transform.program
    assert_equal @admin, @project.log_transform.user
  end

  test "a program that doesn't compile is refused with its errors" do
    @project.log_transform_versions.create!(program: ".a = 1", user: @admin)
    stub_request(:post, @check_url).to_return(status: 200, body: { errors: [ ERROR ], warnings: [] }.to_json)
    sign_in_as(@admin)

    patch project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".status =" },
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }

    assert_response :success
    page = JSON.parse(response.body)["props"]
    assert_equal({ "program" => [ ERROR["text"] ] }, page["errors"])
    assert_equal ".a = 1", page["program"]
    assert_equal 1, @project.log_transform_versions.count
  end

  test "a member's save is refused" do
    check = stub_request(:post, @check_url)

    patch project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".status = 1" }

    assert_response :redirect
    assert_equal "You are not authorized to perform this action.", flash[:alert]
    assert_not_requested check
    assert_nil @project.log_transform
  end

  test "an admin restores an earlier version, which saves it again as the newest" do
    old = @project.log_transform_versions.create!(program: ".a = 1", user: @member)
    @project.log_transform_versions.create!(program: ".a = 2", user: @admin)
    stub_request(:post, @check_url).with(body: { program: ".a = 1" }).to_return(status: 200, body: { errors: [], warnings: [] }.to_json)
    sign_in_as(@admin)

    post restore_project_log_transform_url(@project, org_slug: @organization.slug), params: { version_id: old.id }

    assert_redirected_to project_log_transform_url(@project, org_slug: @organization.slug)
    assert_equal 3, @project.log_transform_versions.count
    assert_equal ".a = 1", @project.log_transform.program
    assert_equal @admin, @project.log_transform.user
  end

  test "a member's restore is refused, and another Project's version can't be restored" do
    old = @project.log_transform_versions.create!(program: ".a = 1", user: @admin)
    @project.log_transform_versions.create!(program: ".a = 2", user: @admin)

    post restore_project_log_transform_url(@project, org_slug: @organization.slug), params: { version_id: old.id }

    assert_equal "You are not authorized to perform this action.", flash[:alert]
    assert_equal ".a = 2", @project.log_transform.program

    theirs = create(:project, organization: @organization).log_transform_versions.create!(program: ".b = 1")
    sign_in_as(@admin)
    post restore_project_log_transform_url(@project, org_slug: @organization.slug), params: { version_id: theirs.id }
    assert_response :not_found
    assert_equal 2, @project.log_transform_versions.count
  end

  test "a save while EventStore is unavailable saves nothing and says so" do
    stub_request(:post, @check_url).to_raise(Errno::ECONNREFUSED)
    sign_in_as(@admin)

    patch project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".status = 1" }

    assert_redirected_to project_log_transform_url(@project, org_slug: @organization.slug)
    assert_equal "EventStore is unavailable, so the program couldn't be checked. Try again shortly.", flash[:alert]
    assert_nil @project.log_transform
  end

  test "any member checks a draft, getting EventStore's errors and warnings" do
    answer = { "errors" => [ ERROR ], "warnings" => [] }
    stub_request(:post, @check_url).with(body: { program: ".status =" }).to_return(status: 200, body: answer.to_json)

    post check_project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".status =" }, as: :json

    assert_response :success
    assert_equal answer, response.parsed_body
  end

  test "a check while EventStore is unavailable answers 502" do
    stub_request(:post, @check_url).to_raise(Errno::ECONNREFUSED)

    post check_project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".a = 1" }, as: :json

    assert_response :bad_gateway
    assert_equal "EventStore is unavailable. Try again shortly.", response.parsed_body["error"]
  end

  test "any member previews a draft on the recent arrivals, getting each Log before and after" do
    log = { "id" => "0192", "time" => "2026-10-21T10:00:00Z", "message" => "GET /health", "attributes" => {} }
    answer = { "results" => [ { "before" => log, "dropped" => true } ], "kept" => 0, "dropped" => 1, "failed" => 0 }
    preview = stub_request(:post, preview_url).with(body: { program: "abort" }).to_return(status: 200, body: answer.to_json)

    post preview_project_log_transform_url(@project, org_slug: @organization.slug), params: { program: "abort" }, as: :json

    assert_response :success
    assert_equal answer, response.parsed_body
    assert_requested preview
  end

  test "a preview runs on the Logs pasted, as they were pasted" do
    logs = [ { "message" => "one", "tags" => [ "a", nil ], "n" => 1.5 }, { "message" => "two", "nested" => { "ok" => false } } ]
    answer = { "results" => [], "kept" => 2, "dropped" => 0, "failed" => 0 }
    preview = stub_request(:post, preview_url).with(body: { program: ".a = 1", logs: logs }).to_return(status: 200, body: answer.to_json)

    post preview_project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".a = 1", logs: logs }, as: :json

    assert_response :success
    assert_requested preview
  end

  test "a preview of a draft that doesn't compile answers the check's errors" do
    answer = { "errors" => [ ERROR ], "warnings" => [] }
    stub_request(:post, preview_url).to_return(status: 200, body: answer.to_json)

    post preview_project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".status =" }, as: :json

    assert_response :success
    assert_equal answer, response.parsed_body
  end

  test "a preview EventStore refuses answers 400 with why, and 502 when it is unavailable" do
    stub_request(:post, preview_url).to_return(status: 400, body: { error: "a preview takes at most 200 Logs" }.to_json)
    post preview_project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".a = 1", logs: [ {} ] }, as: :json
    assert_response :bad_request
    assert_equal "a preview takes at most 200 Logs", response.parsed_body["error"]

    stub_request(:post, preview_url).to_raise(Errno::ECONNREFUSED)
    post preview_project_log_transform_url(@project, org_slug: @organization.slug), params: { program: ".a = 1" }, as: :json
    assert_response :bad_gateway
    assert_equal "EventStore is unavailable. Try again shortly.", response.parsed_body["error"]
  end

  private

  def preview_url
    "#{EventStore.base_url}/api/#{@project.id}/logs/transform/preview"
  end

  def log_transform_page
    get project_log_transform_url(@project, org_slug: @organization.slug),
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "LogTransform/Show", JSON.parse(response.body)["component"]
    JSON.parse(response.body)["props"]
  end
end
