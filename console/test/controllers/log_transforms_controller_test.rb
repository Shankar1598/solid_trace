# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# A Project's Log transform page: any member reads the program, its versions
# and running counts, and checks a draft; only admins save and restore.
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

  private

  def log_transform_page
    get project_log_transform_url(@project, org_slug: @organization.slug),
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "LogTransform/Show", JSON.parse(response.body)["component"]
    JSON.parse(response.body)["props"]
  end
end
