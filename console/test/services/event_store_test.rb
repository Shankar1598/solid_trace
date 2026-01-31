# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

class EventStoreTest < ActiveSupport::TestCase
  setup do
    @base_url = "http://localhost:4000"
    @previous_url = ENV["EVENT_STORE_URL"]
    ENV["EVENT_STORE_URL"] = @base_url
  end

  teardown do
    ENV["EVENT_STORE_URL"] = @previous_url
  end

  test "get_event returns parsed response" do
    stub_request(:get, "#{@base_url}/api/events/evt-123")
      .to_return(status: 200, body: { uuid: "evt-123" }.to_json, headers: { "Content-Type" => "application/json" })

    response = EventStore.get_event("evt-123")

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
