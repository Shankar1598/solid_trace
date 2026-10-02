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
    # call — HTTP delivery and its reported outcome
    # -----------------------------------------------------------------------

    test "posts JSON to the PagerDuty Events API and reports success on a 2xx response" do
      response = PagerdutyNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call

      assert_equal "202", response.code
      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        assert_equal "test-routing-key-123", body["routing_key"]
        assert_equal "trigger", body["event_action"]
        true
      end
    end

    test "raises NotConfigured and does not post when routing_key is blank" do
      @integration.settings["routing_key"] = ""
      @integration.save!

      error = assert_raises(NotConfigured) do
        PagerdutyNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
      end

      assert_equal "No routing key", error.message
      assert_not_requested(:post, EVENTS_API_URL)
    end

    test "reports a non-2xx response as a failure with the provider's error" do
      stub_request(:post, EVENTS_API_URL).to_return(status: 500, body: "internal error")

      error = assert_raises(DeliveryFailed) do
        PagerdutyNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
      end

      assert_equal "HTTP 500: internal error", error.message
    end

    test "reports a connection error as a failure" do
      stub_request(:post, EVENTS_API_URL).to_raise(Errno::ECONNREFUSED)

      error = assert_raises(DeliveryFailed) do
        PagerdutyNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
      end

      assert_match "Connection refused", error.message
    end

    test "reports a timeout as a failure" do
      stub_request(:post, EVENTS_API_URL).to_timeout

      error = assert_raises(DeliveryFailed) do
        PagerdutyNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
      end

      assert_match "Timeout", error.message
    end

    # -----------------------------------------------------------------------
    # Payload: one Issue (threshold_reached)
    # -----------------------------------------------------------------------

    test "threshold_reached payload includes the Issue summary, dedup_key and details" do
      notifier = PagerdutyNotifier.new(@integration, kind: "threshold_reached", issues: [ @issue ])
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        payload = body["payload"]

        assert_includes payload["summary"], "Event threshold reached"
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
      notifier = PagerdutyNotifier.new(@integration, kind: "threshold_reached", issues: [ @issue ])
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
      notifier = PagerdutyNotifier.new(
        @integration,
        kind: "assignment_changed",
        issues: [ @issue ],
        previous_assignee_name: "Alice",
        new_assignee_name: "Bob"
      )
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
    # Payload: issue_created (batched)
    # -----------------------------------------------------------------------

    test "batch payload lists multiple issues with correct count" do
      issue2 = create(:issue, project: @project, title: "Second Error")
      issue3 = create(:issue, project: @project, title: "Third Error")

      notifier = PagerdutyNotifier.new(@integration, kind: "issue_created", issues: [ @issue, issue2, issue3 ])
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

      notifier = PagerdutyNotifier.new(@integration, kind: "issue_created", issues: issues)
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

    test "an unknown kind raises and posts nothing" do
      assert_raises(ArgumentError) do
        PagerdutyNotifier.new(@integration, kind: "issue_notification", issues: [ @issue ]).call
      end
      assert_not_requested(:post, EVENTS_API_URL)
    end

    # -----------------------------------------------------------------------
    # Severity configuration
    # -----------------------------------------------------------------------

    test "uses configured severity from integration settings" do
      @integration.settings["severity"] = "warning"
      @integration.save!

      notifier = PagerdutyNotifier.new(@integration, kind: "issue_created", issues: [ @issue ])
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

      notifier = PagerdutyNotifier.new(@integration, kind: "issue_created", issues: [ @issue ])
      notifier.call

      assert_requested(:post, EVENTS_API_URL) do |req|
        body = JSON.parse(req.body)
        assert_equal "error", body["payload"]["severity"]
        true
      end
    end
  end
end
