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

  test "a tabular Query answers EventStore's table" do
    table = {
      columns: [ { name: "service", type: "dynamic" }, { name: "count_", type: "number" } ],
      rows: [ [ "api", 12 ], [ "web", 3 ] ],
      truncated: false,
    }
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
      .with(body: hash_including(query: "summarize count() by service"))
      .to_return(status: 200, body: { table: table }.to_json)

    get query_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "summarize count() by service", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :success
    assert_equal [ [ "api", 12 ], [ "web", 3 ] ], response.parsed_body["table"]["rows"]
    assert_equal [ "service", "count_" ], response.parsed_body["table"]["columns"].map { |c| c["name"] }
    assert_equal false, response.parsed_body["table"]["truncated"]
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

  test "validate answers a Query's chips from EventStore" do
    chips = { filters: [ { field: "http.status", op: "!=", value: 500 } ], search: "boom" }
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/validate")
      .with(body: { query: "where ['http.status'] != 500 | search \"boom\"" }.to_json)
      .to_return(status: 200, body: { errors: [], chips: chips }.to_json)

    get validate_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "where ['http.status'] != 500 | search \"boom\"" }

    assert_response :success
    assert_equal [], response.parsed_body["errors"]
    assert_equal({ "field" => "http.status", "op" => "!=", "value" => 500 }, response.parsed_body["chips"]["filters"].first)
    assert_equal "boom", response.parsed_body["chips"]["search"]
  end

  test "validate answers where each stage is and whether the Builder can show it" do
    stages = [
      { line: 1, column: 1, end_line: 1, end_column: 21, filter: true, builder: false },
      { line: 1, column: 24, end_line: 1, end_column: 31, filter: false, builder: false },
    ]
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/validate")
      .to_return(status: 200, body: { errors: [], chips: nil, stages: stages }.to_json)

    get validate_project_logs_url(@project, org_slug: @organization.slug), params: { query: "where duration > 500 | count" }

    assert_response :success
    assert_nil response.parsed_body["chips"]
    assert_equal [ true, false ], response.parsed_body["stages"].map { |s| s["filter"] }
  end

  test "someone outside the Organization can't validate a Query on its Logs" do
    sign_in_as(@outsider)
    validate = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/validate")

    get validate_project_logs_url(@project, org_slug: @organization.slug), params: { query: "" }

    assert_response :not_found
    assert_not_requested validate
  end

  test "validate answers 502 when EventStore can't" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/validate").to_timeout

    get validate_project_logs_url(@project, org_slug: @organization.slug), params: { query: "" }

    assert_response :bad_gateway
    assert response.parsed_body["error"].present?
  end

  test "the histogram counts the Project's Logs over the range and bucket it was given" do
    levels = { trace: 0, debug: 0, info: 4, warn: 1, error: 2, fatal: 0, none: 1 }
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/histogram")
      .with(body: {
        query: "search \"boom\"",
        from: "2026-10-03T09:00:00.000Z",
        to: "2026-10-03T10:00:00.000Z",
        bucket_seconds: 60,
      }.to_json)
      .to_return(status: 200, body: {
        bucket_seconds: 60,
        buckets: [ { time: "2026-10-03T09:00:00Z", count: 8, levels: levels } ],
        count: 8,
        levels: levels,
      }.to_json)

    get histogram_project_logs_url(@project, org_slug: @organization.slug), params: {
      query: "search \"boom\"",
      from: "2026-10-03T11:00:00+02:00",
      to: "2026-10-03T10:00:00Z",
      bucket_seconds: "60",
    }

    assert_response :success
    assert_equal 8, response.parsed_body["count"]
    assert_equal 2, response.parsed_body["levels"]["error"]
    assert_equal [ 8 ], response.parsed_body["buckets"].map { |bucket| bucket["count"] }
  end

  test "someone outside the Organization can't count its Logs" do
    sign_in_as(@outsider)
    histogram = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/histogram")

    get histogram_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z", bucket_seconds: "60" }

    assert_response :not_found
    assert_not_requested histogram
  end

  test "a histogram of a Query with mistakes answers with EventStore's errors" do
    errors = [ { message: "unknown stage \"nope\"", line: 1, column: 1, end_line: 1, end_column: 5 } ]
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/histogram")
      .to_return(status: 400, body: { errors: errors }.to_json)

    get histogram_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "nope", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z", bucket_seconds: "60" }

    assert_response :bad_request
    assert_equal "unknown stage \"nope\"", response.parsed_body["errors"].first["message"]
  end

  test "a histogram's range or bucket that can't be read is refused before EventStore" do
    histogram = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/histogram")

    get histogram_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "last hour", to: "2026-10-03T10:00:00Z", bucket_seconds: "60" }
    assert_response :bad_request
    assert response.parsed_body["error"].present?

    get histogram_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z", bucket_seconds: "a minute" }
    assert_response :bad_request
    assert response.parsed_body["error"].present?

    assert_not_requested histogram
  end

  test "the histogram answers 502 when EventStore can't" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/histogram").to_timeout

    get histogram_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z", bucket_seconds: "60" }

    assert_response :bad_gateway
    assert response.parsed_body["error"].present?
  end

  test "one Log is fetched by its id, near its own time" do
    id = "0199a8b2-6a1e-7c3d-9f00-1234567890ab"
    stub_request(:get, "#{@base_url}/api/#{@project.id}/logs/#{id}")
      .with(query: { time: "2026-10-03T09:15:00.123Z" })
      .to_return(status: 200, body: { id: id, message: "boom", time: "2026-10-03T09:15:00.123Z", attributes: {} }.to_json)

    get project_log_url(@project, id, org_slug: @organization.slug), params: { time: "2026-10-03T09:15:00.123Z" }

    assert_response :success
    assert_equal "boom", response.parsed_body["message"]
  end

  test "a Log EventStore doesn't have isn't found" do
    id = "0199a8b2-6a1e-7c3d-9f00-1234567890ab"
    stub_request(:get, "#{@base_url}/api/#{@project.id}/logs/#{id}").to_return(status: 404)

    get project_log_url(@project, id, org_slug: @organization.slug)

    assert_response :not_found
  end

  test "a Log id that isn't a UUID isn't found, without asking EventStore" do
    fetch = stub_request(:get, %r{#{@base_url}/api/#{@project.id}/logs/.*})

    get project_log_url(@project, "nope", org_slug: @organization.slug)

    assert_response :not_found
    assert_not_requested fetch
  end

  test "someone outside the Organization can't fetch one of its Logs" do
    sign_in_as(@outsider)
    fetch = stub_request(:get, %r{#{@base_url}/api/#{@project.id}/logs/.*})

    get project_log_url(@project, "0199a8b2-6a1e-7c3d-9f00-1234567890ab", org_slug: @organization.slug)

    assert_response :not_found
    assert_not_requested fetch
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
