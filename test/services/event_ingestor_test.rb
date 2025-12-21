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

  test "call should reopen resolved issue when new event arrives" do
    data = {
      "message" => "Test Error",
      "culprit" => "test_culprit",
    }

    # First event creates issue
    result = EventIngestor.new(@project, data).call
    issue = Issue.find(result[:issue_id])

    # Resolve the issue
    issue.update!(status: :resolved)
    assert issue.reload.resolved?

    # Second event should reopen the issue
    EventIngestor.new(@project, data).call
    assert issue.reload.unresolved?
  end

  test "call should scope issues to project" do
    other_project = create(:project)
    data = {
      "message" => "Test Error",
      "culprit" => "test_culprit",
    }

    # Create in first project
    EventIngestor.new(@project, data).call

    # Should create new issue in second project even with same data
    assert_difference "Issue.count", 1 do
      result = EventIngestor.new(other_project, data).call
      issue = Issue.find(result[:issue_id])
      assert_equal other_project, issue.project
    end
  end

  test "call should create event_fingerprint linking event to issue" do
    data = {
      "message" => "Test Error",
      "culprit" => "test_culprit",
    }

    result = EventIngestor.new(@project, data).call

    event = Event.find(result[:event_id])
    issue = Issue.find(result[:issue_id])

    assert_not_nil event.event_fingerprint
    assert_equal issue, event.event_fingerprint.issue
    assert_equal issue, event.issue
  end
end
