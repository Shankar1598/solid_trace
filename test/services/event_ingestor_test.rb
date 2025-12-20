# frozen_string_literal: true

require "test_helper"

class EventIngestorTest < ActiveSupport::TestCase
  setup do
    @project = create(:project)
  end

  test "call should create issue and event" do
    data = {
      "message" => "Test Error",
      "level" => "error",
      "event_id" => "12345",
      "culprit" => "test_culprit",
      "environment" => "production",
      "exception" => { "values" => [ { "type" => "Error", "value" => "Something went wrong" } ] },
    }

    assert_difference -> { Issue.count } => 1, -> { Event.count } => 1 do
      result = EventIngestor.new(@project, data).call
      assert_not_nil result[:issue_id]
      assert_equal Event.last.id, result[:event_id]
    end

    issue = Issue.last
    assert_equal "Test Error", issue.title
    assert_equal "error", issue.kind
    assert_equal "test_culprit", issue.culprit
  end

  test "call should group events into existing issue" do
    data = {
      "message" => "Test Error",
      "level" => "error",
      "culprit" => "test_culprit",
    }

    # First event
    EventIngestor.new(@project, data).call

    # Second event (same fingerprint logic)
    assert_no_difference "Issue.count" do
      assert_difference "Event.count", 1 do
        EventIngestor.new(@project, data).call
      end
    end
  end

  test "call should determine kind correctly" do
    # CSP
    data_csp = { "csp-report" => { "blocked-uri" => "google.com" }, "level" => "info" }
    EventIngestor.new(@project, data_csp).call
    assert_equal "csp", Issue.last.kind

    # Default
    data_default = { "level" => "info" }
    EventIngestor.new(@project, data_default).call
    assert_equal "default", Issue.last.kind
  end
end
