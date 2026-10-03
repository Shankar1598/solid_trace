# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

class LogsControllerTest < ActionDispatch::IntegrationTest
  include IntegrationTestHelper

  setup do
    @base_url = "http://127.0.0.1:4100"
    @previous_url = ENV["EVENT_STORE_URL"]
    ENV["EVENT_STORE_URL"] = @base_url

    @organization = create(:organization)
    @member = create(:user)
    create(:organization_user, organization: @organization, user: @member, role: :member)
    @project = create(:project, organization: @organization, name: "Storefront")

    @outsider = create(:user)
    create(:organization_user, organization: create(:organization), user: @outsider, role: :admin)

    sign_in_as(@member)
  end

  teardown do
    ENV["EVENT_STORE_URL"] = @previous_url
  end

  test "an Organization member opens a Project's Logs explorer" do
    other = create(:project, organization: @organization, name: "Backoffice")

    props = explorer_page

    assert_equal @project.slug, props["project"]["slug"]
    assert_equal [ "Backoffice", "Storefront" ], props["projects"].map { |p| p["name"] }.sort
    assert_includes props["projects"].map { |p| p["slug"] }, other.slug
  end

  test "someone outside the Organization can't open its Logs explorer" do
    sign_in_as(@outsider)

    get project_logs_url(@project, org_slug: @organization.slug)

    assert_response :not_found
  end

  test "someone outside the Organization can't run a Query on its Logs" do
    sign_in_as(@outsider)
    query = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")

    get query_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :not_found
    assert_not_requested query
  end

  test "a Project of another Organization isn't found under this one" do
    foreign = create(:project, organization: create(:organization))

    get project_logs_url(foreign, org_slug: @organization.slug)

    assert_response :not_found
  end

  test "a Query runs over the Project and time range it was given" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
      .with(body: {
        query: "search \"boom\"",
        from: "2026-10-03T09:00:00.000Z",
        to: "2026-10-03T10:30:00.250Z",
        cursor: "older",
      }.to_json)
      .to_return(status: 200, body: { logs: [ { id: "log-1", message: "boom" } ], next_cursor: "older-still" }.to_json)

    get query_project_logs_url(@project, org_slug: @organization.slug), params: {
      query: "search \"boom\"",
      from: "2026-10-03T11:00:00+02:00",
      to: "2026-10-03T10:30:00.250Z",
      cursor: "older",
    }

    assert_response :success
    assert_equal [ "boom" ], response.parsed_body["logs"].map { |log| log["message"] }
    assert_equal "older-still", response.parsed_body["next_cursor"]
  end

  test "a Query with mistakes answers with EventStore's errors" do
    errors = [ { message: "unknown stage \"nope\"", line: 1, column: 1, end_line: 1, end_column: 5 } ]
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
      .to_return(status: 400, body: { errors: errors }.to_json)

    get query_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "nope", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :bad_request
    assert_equal "unknown stage \"nope\"", response.parsed_body["errors"].first["message"]
  end

  test "a time range that isn't two times is refused before EventStore" do
    query = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")

    get query_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "last hour", to: "2026-10-03T10:00:00Z" }

    assert_response :bad_request
    assert response.parsed_body["error"].present?
    assert_not_requested query
  end

  test "a Query answers 502 when EventStore can't" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_timeout

    get query_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :bad_gateway
    assert response.parsed_body["error"].present?
  end

  test "the Logs entry opens the newest Project's Logs" do
    newest = create(:project, organization: @organization, created_at: 1.minute.from_now)

    get logs_url(org_slug: @organization.slug)

    assert_redirected_to project_logs_url(newest, org_slug: @organization.slug)
  end

  test "the Logs entry asks for a Project when the Organization has none" do
    @project.destroy!

    get logs_url(org_slug: @organization.slug)

    assert_redirected_to projects_url(org_slug: @organization.slug)
  end

  private

  def explorer_page
    get project_logs_url(@project, org_slug: @organization.slug),
      headers: { "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest }
    assert_response :success
    assert_equal "Logs/Index", JSON.parse(response.body)["component"]
    JSON.parse(response.body)["props"]
  end
end
