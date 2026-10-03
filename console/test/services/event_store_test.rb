# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

class EventStoreTest < ActiveSupport::TestCase
  setup do
    @base_url = "http://127.0.0.1:4100"
    @previous_url = ENV["EVENT_STORE_URL"]
    ENV["EVENT_STORE_URL"] = @base_url
  end

  teardown do
    ENV["EVENT_STORE_URL"] = @previous_url
  end

  test "get_event requests the event under its project" do
    stub_request(:get, "#{@base_url}/api/123/events/evt-123")
      .to_return(status: 200, body: { uuid: "evt-123" }.to_json, headers: { "Content-Type" => "application/json" })

    response = EventStore.get_event(project_id: 123, event_uuid: "evt-123")

    assert_equal "evt-123", response["uuid"]
  end

  test "query_events returns default on not found" do
    stub_request(:get, "#{@base_url}/api/123/events")
      .to_return(status: 404, body: "")

    response = EventStore.query_events(project_id: 123, params: {})

    assert_equal [], response
  end

  test "count_events returns count" do
    stub_request(:get, "#{@base_url}/api/123/events/count")
      .to_return(status: 200, body: { count: 7 }.to_json, headers: { "Content-Type" => "application/json" })

    count = EventStore.count_events(project_id: 123, params: {})

    assert_equal 7, count
  end

  test "query_logs runs a Query over a Project's time range" do
    stub_request(:post, "#{@base_url}/api/123/logs/query")
      .with(body: {
        query: "where level == \"error\"",
        from: "2026-10-03T09:00:00.000Z",
        to: "2026-10-03T10:00:00.000Z",
        cursor: "abc",
      }.to_json)
      .to_return(status: 200, body: { logs: [ { id: "log-1" } ], next_cursor: "def" }.to_json)

    page = EventStore.query_logs(
      project_id: 123,
      query: "where level == \"error\"",
      from: Time.utc(2026, 10, 3, 9),
      to: Time.utc(2026, 10, 3, 10),
      cursor: "abc"
    )

    assert_equal [ "log-1" ], page["logs"].map { |log| log["id"] }
    assert_equal "def", page["next_cursor"]
  end

  test "query_logs raises Rejected with EventStore's errors for a Query with mistakes" do
    errors = [ { message: "unknown stage", line: 1, column: 1, end_line: 1, end_column: 4 } ]
    stub_request(:post, "#{@base_url}/api/123/logs/query")
      .to_return(status: 400, body: { errors: errors }.to_json)

    error = assert_raises(EventStore::Rejected) do
      EventStore.query_logs(project_id: 123, query: "nope", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))
    end

    assert_equal "unknown stage", error.body["errors"].first["message"]
  end

  test "query_logs raises Unavailable when EventStore fails" do
    stub_request(:post, "#{@base_url}/api/123/logs/query").to_return(status: 500, body: { error: "Internal error" }.to_json)

    assert_raises(EventStore::Unavailable) do
      EventStore.query_logs(project_id: 123, query: "", from: Time.utc(2026, 10, 3, 9), to: Time.utc(2026, 10, 3, 10))
    end
  end

  test "validate_logs_query returns errors and chips" do
    stub_request(:post, "#{@base_url}/api/123/logs/validate")
      .with(body: { query: "search \"boom\"" }.to_json)
      .to_return(status: 200, body: { errors: [], chips: { filters: [], search: "boom" } }.to_json)

    result = EventStore.validate_logs_query(project_id: 123, query: "search \"boom\"")

    assert_equal [], result["errors"]
    assert_equal "boom", result["chips"]["search"]
  end

  test "get_log fetches one Log by id near its time" do
    stub_request(:get, "#{@base_url}/api/123/logs/0199a0b0-0000-7000-8000-000000000001")
      .with(query: { time: "2026-10-03T09:30:00.123Z" })
      .to_return(status: 200, body: { id: "0199a0b0-0000-7000-8000-000000000001" }.to_json)

    log = EventStore.get_log(project_id: 123, log_id: "0199a0b0-0000-7000-8000-000000000001", time: "2026-10-03T09:30:00.123Z")

    assert_equal "0199a0b0-0000-7000-8000-000000000001", log["id"]
  end

  test "get_log returns nil for a Log that isn't there" do
    stub_request(:get, "#{@base_url}/api/123/logs/0199a0b0-0000-7000-8000-000000000001")
      .to_return(status: 404, body: { error: "Log not found" }.to_json)

    assert_nil EventStore.get_log(project_id: 123, log_id: "0199a0b0-0000-7000-8000-000000000001")
  end

  test "all builds event objects" do
    issue = create(:issue)
    fingerprint = create(:issue_fingerprint, issue: issue)

    response = [
      {
        "uuid" => "evt-1",
        "project_id" => issue.project_id,
        "issue_fingerprint_id" => fingerprint.id,
        "timestamp" => Time.current.iso8601,
        "environment" => "production",
        "tags" => { "env" => "prod" }
      }
    ]

    EventStore.stub :query_events, response do
      events = EventStore.new(issue).all

      assert_equal 1, events.length
      assert_instance_of Event, events.first
      assert_equal "evt-1", events.first.uuid
      assert_equal issue.project_id, events.first.project_id
    end
  end

  test "get_event_with_context returns nil when no fingerprints" do
    issue = build(:issue)

    result = EventStore.new(issue).get_event_with_context

    assert_nil result
  end
end
