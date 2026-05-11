# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

module Notifiers
  class PagerdutyNotifierTest < ActiveSupport::TestCase
    EVENTS_API_URL = "https://events.pagerduty.com/v2/enqueue"

    setup do
      @organization = create(:organization)
      @project = create(:project, organization: @organization)
      @issue = create(:issue, project: @project, title: "Test Error", culprit: "app/models/user.rb:42")

      @integration = @organization.integrations.create!(
        provider: "pagerduty",
        name: "PagerDuty Alerts",
        settings: {
          "routing_key" => "test-routing-key-123",
          "severity" => "critical",
        },
        active: true
      )

      stub_request(:post, EVENTS_API_URL)
        .to_return(status: 202, body: { "status" => "success" }.to_json)
    end

    # -----------------------------------------------------------------------
    # call — HTTP delivery
    # -----------------------------------------------------------------------

    test "posts JSON to the PagerDuty Events API" do
      notifier = PagerdutyNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      response = notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        assert_equal "test-routing-key-123", body["routing_key"]
        assert_equal "trigger", body["event_action"]
        true
      end
    end

    test "returns nil and does not post when routing_key is blank" do
      @integration.settings["routing_key"] = ""
      @integration.save!

      notifier = PagerdutyNotifier.new(@integration, @issue)
      result = notifier.call

      assert_nil result
      assert_not_requested(:post, EVENTS_API_URL)
    end

    test "returns nil on network error without raising" do
      stub_request(:post, EVENTS_API_URL).to_raise(Errno::ECONNREFUSED)

      notifier = PagerdutyNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      result = notifier.call

      assert_nil result
    end

    test "logs warning on non-success HTTP response" do
      stub_request(:post, EVENTS_API_URL)
        .to_return(status: 429, body: "rate limited")

      notifier = PagerdutyNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      response = notifier.call

      assert_equal "429", response.code
    end

    # -----------------------------------------------------------------------
    # Payload: issue_created (default)
    # -----------------------------------------------------------------------

    test "issue_created payload includes correct summary and dedup_key" do
      notifier = PagerdutyNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        payload = body["payload"]

        assert_includes payload["summary"], "New issue"
        assert_includes payload["summary"], @issue.title
        assert_includes payload["summary"], @issue.kind.upcase
        assert_equal @project.name, payload["source"]
        assert_equal "critical", payload["severity"]
        assert_equal "solid-trace-issue-#{@issue.id}", body["dedup_key"]

        details = payload["custom_details"]
        assert_equal @issue.id, details["issue_id"]
        assert_equal @issue.number, details["issue_number"]
        assert_equal @issue.culprit, details["culprit"]
        assert_equal @project.name, details["project"]
        assert_equal @organization.name, details["organization"]
        true
      end
    end

    # -----------------------------------------------------------------------
    # Payload: event_threshold_reached
    # -----------------------------------------------------------------------

    test "threshold_reached payload has correct summary prefix" do
      notifier = PagerdutyNotifier.new(@integration, @issue, notification: { event: "event_threshold_reached" })
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        assert_includes body["payload"]["summary"], "Event threshold reached"
        true
      end
    end

    # -----------------------------------------------------------------------
    # Payload: issue_assignment_updated
    # -----------------------------------------------------------------------

    test "assignment payload includes assignee names in custom_details" do
      notification = {
        event: "issue_assignment_updated",
        previous_assignee_name: "Alice",
        new_assignee_name: "Bob",
      }

      notifier = PagerdutyNotifier.new(@integration, @issue, notification: notification)
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        details = body["payload"]["custom_details"]

        assert_includes body["payload"]["summary"], "Issue assignment updated"
        assert_equal "Alice", details["previous_assignee"]
        assert_equal "Bob", details["new_assignee"]
        true
      end
    end

    # -----------------------------------------------------------------------
    # Payload: issue_created_batch
    # -----------------------------------------------------------------------

    test "batch payload lists multiple issues with correct count" do
      issue2 = create(:issue, project: @project, title: "Second Error")
      issue3 = create(:issue, project: @project, title: "Third Error")

      notification = {
        event: "issue_created_batch",
        issues: [@issue, issue2, issue3],
      }

      notifier = PagerdutyNotifier.new(@integration, @issue, notification: notification)
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        payload = body["payload"]
        details = payload["custom_details"]

        assert_includes payload["summary"], "3 new issues"
        assert_equal 3, details["count"]
        assert_equal 3, details["issues"].length
        assert_equal @organization.name, payload["source"]

        issue_titles = details["issues"].map { |i| i["title"] }
        assert_includes issue_titles, "Test Error"
        assert_includes issue_titles, "Second Error"
        assert_includes issue_titles, "Third Error"

        assert_includes body["dedup_key"], "batch"
        true
      end
    end

    test "batch payload truncates to 10 issues and shows more_count" do
      issues = 12.times.map { |i| create(:issue, project: @project, title: "Issue #{i}") }

      notification = {
        event: "issue_created_batch",
        issues: issues,
      }

      notifier = PagerdutyNotifier.new(@integration, issues.first, notification: notification)
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        details = body["payload"]["custom_details"]

        assert_equal 12, details["count"]
        assert_equal 10, details["issues"].length
        assert_equal 2, details["more_count"]
        true
      end
    end

    test "batch payload falls back to single issue when issues array is empty" do
      notification = {
        event: "issue_created_batch",
        issues: [],
      }

      notifier = PagerdutyNotifier.new(@integration, @issue, notification: notification)
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        details = body["payload"]["custom_details"]

        assert_equal 1, details["count"]
        assert_equal 1, details["issues"].length
        true
      end
    end

    # -----------------------------------------------------------------------
    # Severity configuration
    # -----------------------------------------------------------------------

    test "uses configured severity from integration settings" do
      @integration.settings["severity"] = "warning"
      @integration.save!

      notifier = PagerdutyNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        assert_equal "warning", body["payload"]["severity"]
        true
      end
    end

    test "defaults severity to error when not configured" do
      @integration.settings.delete("severity")
      @integration.save!

      notifier = PagerdutyNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        assert_equal "error", body["payload"]["severity"]
        true
      end
    end
  end
end
