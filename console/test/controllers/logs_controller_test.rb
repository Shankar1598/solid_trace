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

  test "the explorer knows each Project's Log Retention, its own or the instance default" do
    InstanceSetting.current.update!(default_log_retention_days: 14)
    @project.update!(log_retention_days: 90)
    other = create(:project, organization: @organization, name: "Backoffice")

    props = explorer_page

    assert_equal 90, props["project"]["log_retention_days"]
    retention = props["projects"].to_h { |p| [ p["slug"], p["log_retention_days"] ] }
    assert_equal({ @project.slug => 90, other.slug => 14 }, retention)
  end

  test "the explorer has where to send the Project's Logs, for an empty Project's snippets" do
    key = create(:project_key, project: @project)

    assert_equal({ "url" => "http://www.example.com:4000", "key" => key.public_key }, explorer_page["ingest"])

    ENV["PUBLIC_INGEST_URL"] = "https://ingest.example.com/"
    assert_equal "https://ingest.example.com", explorer_page["ingest"]["url"]
  ensure
    ENV.delete("PUBLIC_INGEST_URL")
  end

  test "the explorer learns after it opens whether the Project has Logs in its Retention" do
    @project.update!(log_retention_days: 7)
    travel_to Time.utc(2026, 10, 17, 12, 0, 0) do
      newest = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
        .with(body: {
          query: "", from: "2026-10-10T12:00:00.000Z", to: "2026-10-17T13:00:00.000Z", limit: 1,
          timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS,
        }.to_json)
        .to_return(status: 200, body: { logs: [ { id: "log-1" } ], next_cursor: "older" }.to_json)

      assert_not explorer_page.key?("has_logs")
      assert_not_requested newest
      assert_equal true, explorer_page_prop("has_logs")

      newest.to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)
      assert_equal false, explorer_page_prop("has_logs")
    end
  end

  test "whether the Project has Logs is unknown while EventStore can't answer" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_timeout

    assert_nil explorer_page_prop("has_logs")
  end

  test "Send a test Log posts a sample Log to EventStore's ingest with the Project's key" do
    key = create(:project_key, project: @project)
    ENV["EVENT_STORE_INGEST_URL"] = "http://127.0.0.1:4999"
    sent = nil
    ingest = stub_request(:post, "http://127.0.0.1:4999/api/#{@project.id}/logs")
      .with(headers: { "Authorization" => "Bearer #{key.public_key}", "Content-Type" => "application/json" }) { |request| sent = JSON.parse(request.body) }
      .to_return(status: 200, body: { accepted: 1, dropped: 0, truncated: 0 }.to_json)

    travel_to Time.utc(2026, 10, 17, 12, 0, 0) do
      post test_project_logs_url(@project, org_slug: @organization.slug), as: :json
    end

    assert_response :created
    assert_requested ingest
    assert_equal 1, sent.size
    assert_equal "2026-10-17T12:00:00.000Z", sent.first["time"]
    assert_equal "info", sent.first["level"]
    assert_match(/test Log/, sent.first["message"])
    assert_kind_of Numeric, sent.first["duration_ms"]
    assert_equal "2026-10-17T12:00:00.000Z", response.parsed_body["time"]
  ensure
    ENV.delete("EVENT_STORE_INGEST_URL")
  end

  test "someone outside the Organization can't send a test Log to its Project" do
    create(:project_key, project: @project)
    ingest = stub_request(:post, %r{/api/#{@project.id}/logs\z})
    sign_in_as(@outsider)

    post test_project_logs_url(@project, org_slug: @organization.slug), as: :json

    assert_response :not_found
    assert_not_requested ingest
  end

  test "Send a test Log says so when EventStore is down" do
    create(:project_key, project: @project)
    stub_request(:post, %r{/api/#{@project.id}/logs\z}).to_raise(Errno::ECONNREFUSED)

    post test_project_logs_url(@project, org_slug: @organization.slug), as: :json

    assert_response :bad_gateway
    assert_equal "EventStore is unavailable. Try again shortly.", response.parsed_body["error"]
  end

  test "Send a test Log needs a key on the Project" do
    ingest = stub_request(:post, %r{/api/#{@project.id}/logs\z})

    post test_project_logs_url(@project, org_slug: @organization.slug), as: :json

    assert_response :unprocessable_content
    assert_equal "This Project has no key. Add one in its settings, then send a test Log.", response.parsed_body["error"]
    assert_not_requested ingest
  end

  test "someone outside the Organization can't open its Logs explorer" do
    sign_in_as(@outsider)

    get project_logs_url(@project, org_slug: @organization.slug)

    assert_response :not_found
  end

  test "the explorer's Query run is timed under logs#query with the request's id, while every read is" do
    InstanceSetting.current.update!(log_all_reads: true)
    output = StringIO.new
    previous_output = Rails.configuration.x.call_timings_output
    Rails.configuration.x.call_timings_output = output
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return(status: 200, body: { logs: [] }.to_json)

    get query_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :success
    line = JSON.parse(output.string.lines.sole)
    assert_equal [ "logs#query", response.request.request_id, @member.id ], line.values_at("origin", "request_id", "user_id")
  ensure
    Rails.configuration.x.call_timings_output = previous_output
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
        timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS,
        check_budget: true,
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

  test "the explorer's run and histogram answer EventStore's refusal of a Query over the budget, and run it as written with force" do
    refusal = { message: "Not run: this Query matches text …", narrower_query: { query: "where file == \"a.log\" | search \"boom\"" } }
    range = { query: "search \"boom\"", from: "2026-10-03T09:00:00Z", to: "2026-10-10T09:00:00Z" }
    [ [ "query", ->(params) { query_project_logs_url(@project, org_slug: @organization.slug, **params) } ],
      [ "histogram", ->(params) { histogram_project_logs_url(@project, org_slug: @organization.slug, **params) } ] ].each do |run, url|
      stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/#{run}").with(body: hash_including(check_budget: true))
        .to_return(status: 200, body: { refusal: refusal, elapsed_ms: 2 }.to_json)
      forced = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/#{run}").with(body: hash_including(check_budget: true, force: true))
        .to_return(status: 200, body: { logs: [], next_cursor: nil, buckets: [] }.to_json)

      get url.call(range)
      assert_response :success
      assert_equal "Not run: this Query matches text …", response.parsed_body.dig("refusal", "message"), run

      get url.call(range.merge(force: "1"))
      assert_response :success
      assert_requested forced
    end
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

  test "the explorer's run asks EventStore for a time chart's totals and answers them" do
    totals = { columns: [ { name: "level", type: "string" }, { name: "count_", type: "number", agg: "count" } ], rows: [ [ "info", 3 ] ], truncated: false }
    table = { columns: [], rows: [], truncated: false, totals: totals }
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query")
      .with(body: hash_including(query: "summarize count() by bin_auto(time), level", totals: true))
      .to_return(status: 200, body: { table: table }.to_json)

    get query_project_logs_url(@project, org_slug: @organization.slug), params: {
      query: "summarize count() by bin_auto(time), level", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z", totals: "1",
    }

    assert_response :success
    assert_equal [ [ "info", 3 ] ], response.parsed_body["table"]["totals"]["rows"]
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

  test "a run EventStore stops at its timeout answers 504 with EventStore's message" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    message = "the run was stopped after 30 s, its timeout: try a shorter time range"
    %w[query histogram].each do |run|
      stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/#{run}").to_return(status: 504, body: { errors: [ { message: message } ] }.to_json)
    end
    sql_message = "the run was stopped after 10 s, its timeout: try a shorter time range or a simpler query"
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/sql")
      .with { |request| JSON.parse(request.body)["timeout_seconds"] == 10 }
      .to_return(status: 504, body: { errors: [ { message: sql_message } ] }.to_json)
    range = { from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    get query_project_logs_url(@project, org_slug: @organization.slug), params: { query: "", **range }
    assert_response :gateway_timeout
    assert_equal message, response.parsed_body["error"]

    get histogram_project_logs_url(@project, org_slug: @organization.slug), params: { query: "", **range }
    assert_response :gateway_timeout
    assert_equal message, response.parsed_body["error"]

    post sql_project_logs_url(@project, org_slug: @organization.slug), params: { sql: "SELECT 1", **range }, as: :json
    assert_response :gateway_timeout
    assert_equal sql_message, response.parsed_body["error"]
  end

  test "the explorer's runs answer 503 at once while Console runs as many as it allows" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    runs = %w[query histogram sql].map { |run| stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/#{run}") }
    range = { from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    holding_every_chart_run do
      get query_project_logs_url(@project, org_slug: @organization.slug), params: { query: "", **range }
      assert_response :service_unavailable
      assert_match "busy", response.parsed_body["error"]

      get histogram_project_logs_url(@project, org_slug: @organization.slug), params: { query: "", **range }
      assert_response :service_unavailable

      post sql_project_logs_url(@project, org_slug: @organization.slug), params: { sql: "SELECT 1", **range }, as: :json
      assert_response :service_unavailable
    end

    runs.each { |run| assert_not_requested run }
  end

  test "an explorer run holds one of the runs Console allows until EventStore answers" do
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/query").to_return do
      holding_every_chart_run(ChartRuns.limit - 1) do
        assert_raises(ChartRuns::Busy) { ChartRuns.hold { flunk "ran past the limit" } }
      end
      { status: 200, body: { logs: [], next_cursor: nil }.to_json }
    end

    get query_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :success
    assert_equal :ran, ChartRuns.hold { :ran }
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
        timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS,
        check_budget: true,
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

  test "SQL mode is hidden while SQL queries are off" do
    InstanceSetting.current.update!(sql_queries_enabled: false)

    assert_equal false, explorer_page["sql_queries_enabled"]
  end

  test "SQL mode is offered on a fresh install" do
    assert_equal true, explorer_page["sql_queries_enabled"]
  end

  test "the readable SQL of a Query is refused while SQL queries are off" do
    InstanceSetting.current.update!(sql_queries_enabled: false)
    readable = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/readable-sql")

    get readable_sql_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :forbidden
    assert response.parsed_body["error"].present?
    assert_not_requested readable
  end

  test "SQL is refused while SQL queries are off" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    InstanceSetting.current.update!(sql_queries_enabled: false)
    sql = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/sql")

    post sql_project_logs_url(@project, org_slug: @organization.slug),
      params: { sql: "SELECT 1", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }, as: :json

    assert_response :forbidden
    assert response.parsed_body["error"].present?
    assert_not_requested sql
  end

  test "the readable SQL of a Query covers the Project and time range it was given" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/readable-sql")
      .with(body: { query: "search \"boom\"", from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z" }.to_json)
      .to_return(status: 200, body: { sql: "WITH stage1 AS (...)\nSELECT * FROM stage1" }.to_json)

    get readable_sql_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "search \"boom\"", from: "2026-10-03T11:00:00+02:00", to: "2026-10-03T10:00:00Z" }

    assert_response :success
    assert_equal "WITH stage1 AS (...)\nSELECT * FROM stage1", response.parsed_body["sql"]
  end

  test "the readable SQL of a Query with mistakes answers with EventStore's errors" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    errors = [ { message: "unknown stage \"nope\"", line: 1, column: 1, end_line: 1, end_column: 5 } ]
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/readable-sql")
      .to_return(status: 400, body: { errors: errors }.to_json)

    get readable_sql_project_logs_url(@project, org_slug: @organization.slug),
      params: { query: "nope", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :bad_request
    assert_equal "unknown stage \"nope\"", response.parsed_body["errors"].first["message"]
  end

  test "SQL runs over the Project and time range it was given, answering a table" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    table = { columns: [ { name: "n", type: "number" } ], rows: [ [ 3 ] ], truncated: false }
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/sql")
      .with(body: {
        sql: "SELECT count(*) AS n FROM logs", from: "2026-10-03T09:00:00.000Z", to: "2026-10-03T10:00:00.000Z",
        timeout_seconds: 10,
      }.to_json)
      .to_return(status: 200, body: { table: table }.to_json)

    post sql_project_logs_url(@project, org_slug: @organization.slug), params: {
      sql: "SELECT count(*) AS n FROM logs", from: "2026-10-03T11:00:00+02:00", to: "2026-10-03T10:00:00Z",
    }, as: :json

    assert_response :success
    assert_equal [ [ 3 ] ], response.parsed_body["table"]["rows"]
    assert_equal [ "n" ], response.parsed_body["table"]["columns"].map { |c| c["name"] }
  end

  test "failing SQL answers with ClickHouse's message" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    message = "Unknown expression identifier `nope` in scope SELECT nope FROM logs."
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/sql").to_return(status: 400, body: { error: message }.to_json)

    post sql_project_logs_url(@project, org_slug: @organization.slug),
      params: { sql: "SELECT nope FROM logs", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }, as: :json

    assert_response :bad_request
    assert_equal message, response.parsed_body["error"]
  end

  test "SQL can't be run from a link" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    sql = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/sql")

    get "#{project_logs_url(@project, org_slug: @organization.slug)}/sql",
      params: { sql: "SELECT 1", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }

    assert_response :not_found
    assert_not_requested sql
  end

  test "someone outside the Organization can't run SQL on its Logs" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    sign_in_as(@outsider)
    sql = stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/sql")

    post sql_project_logs_url(@project, org_slug: @organization.slug),
      params: { sql: "SELECT 1", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }, as: :json

    assert_response :not_found
    assert_not_requested sql
  end

  test "SQL answers 502 when EventStore can't" do
    InstanceSetting.current.update!(sql_queries_enabled: true)
    stub_request(:post, "#{@base_url}/api/#{@project.id}/logs/sql").to_timeout

    post sql_project_logs_url(@project, org_slug: @organization.slug),
      params: { sql: "SELECT 1", from: "2026-10-03T09:00:00Z", to: "2026-10-03T10:00:00Z" }, as: :json

    assert_response :bad_gateway
    assert response.parsed_body["error"].present?
  end

  test "the Logs entry opens the newest Project's Logs" do
    newest = create(:project, organization: @organization, created_at: 1.minute.from_now)

    get logs_url(org_slug: @organization.slug)

    assert_redirected_to project_logs_url(newest, org_slug: @organization.slug)
  end

  test "the Logs entry opens the Project whose Logs were opened last" do
    create(:project, organization: @organization, created_at: 1.minute.from_now)
    explorer_page

    get logs_url(org_slug: @organization.slug)

    assert_redirected_to project_logs_url(@project, org_slug: @organization.slug)
  end

  test "the Logs entry opens the newest Project once the last opened one is gone" do
    explorer_page
    @project.destroy!
    newest = create(:project, organization: @organization)

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

  # A prop the explorer loads after it opens, as Inertia asks for a deferred one.
  def explorer_page_prop(name)
    get project_logs_url(@project, org_slug: @organization.slug), headers: {
      "X-Inertia" => "true", "X-Inertia-Version" => ViteRuby.digest,
      "X-Inertia-Partial-Component" => "Logs/Index", "X-Inertia-Partial-Data" => name,
    }
    assert_response :success
    JSON.parse(response.body)["props"].fetch(name)
  end

  def holding_every_chart_run(left = ChartRuns.limit, &block)
    left.zero? ? yield : ChartRuns.hold { holding_every_chart_run(left - 1, &block) }
  end
end
