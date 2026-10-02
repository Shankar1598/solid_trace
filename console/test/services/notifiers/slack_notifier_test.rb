# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

module Notifiers
  class SlackNotifierTest < ActiveSupport::TestCase
    setup do
      @organization = create(:organization)
      @project = create(:project, organization: @organization)
      @issue = create(:issue, project: @project, title: "Test Error", culprit: "app/models/user.rb:42")

      @integration = @organization.integrations.create!(
        provider: "slack",
        name: "Slack Alerts",
        settings: { "webhook_url" => "https://hooks.slack.com/services/T00/B00/XXX" },
        active: true
      )

      stub_request(:post, "https://hooks.slack.com/services/T00/B00/XXX")
        .to_return(status: 200, body: "ok")
    end

    # -----------------------------------------------------------------------
    # call — HTTP delivery and its reported outcome
    # -----------------------------------------------------------------------

    test "posts JSON to the configured webhook URL and reports success on a 2xx response" do
      response = SlackNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call

      assert_equal "200", response.code
      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        assert_includes body["text"], "New Issue"
        true
      end
    end

    test "raises NotConfigured and does not post when webhook_url is blank" do
      @integration.settings["webhook_url"] = ""
      @integration.save!

      error = assert_raises(NotConfigured) do
        SlackNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
      end

      assert_equal "No webhook URL", error.message
      assert_not_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX")
    end

    test "reports a non-2xx response as a failure with the provider's error" do
      stub_request(:post, "https://hooks.slack.com/services/T00/B00/XXX").to_return(status: 500, body: "internal error")

      error = assert_raises(DeliveryFailed) do
        SlackNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
      end

      assert_equal "HTTP 500: internal error", error.message
    end

    test "reports a connection error as a failure" do
      stub_request(:post, "https://hooks.slack.com/services/T00/B00/XXX").to_raise(Errno::ECONNREFUSED)

      error = assert_raises(DeliveryFailed) do
        SlackNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
      end

      assert_match "Connection refused", error.message
    end

    test "reports a timeout as a failure" do
      stub_request(:post, "https://hooks.slack.com/services/T00/B00/XXX").to_timeout

      error = assert_raises(DeliveryFailed) do
        SlackNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
      end

      assert_match "Timeout", error.message
    end

    # -----------------------------------------------------------------------
    # Payload: issue_created
    # -----------------------------------------------------------------------

    test "threshold_reached payload lists the Issue fields" do
      notifier = SlackNotifier.new(@integration, kind: "threshold_reached", issues: [ @issue ])
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        fields_block = body["blocks"].find { |b| b["type"] == "section" && b["fields"] }
        field_texts = fields_block["fields"].map { |f| f["text"] }

        assert field_texts.any? { |t| t.include?(@issue.title) }, "Expected title in fields"
        assert field_texts.any? { |t| t.include?(@issue.kind) }, "Expected kind in fields"
        assert field_texts.any? { |t| t.include?(@issue.culprit) }, "Expected culprit in fields"
        assert field_texts.any? { |t| t.include?(@project.name) }, "Expected project name in fields"
        true
      end
    end

    # -----------------------------------------------------------------------
    # Payload: event_threshold_reached
    # -----------------------------------------------------------------------

    test "threshold_reached payload has correct header" do
      notifier = SlackNotifier.new(@integration, kind: "threshold_reached", issues: [ @issue ])
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        assert_includes body["text"], "threshold"
        header = body["blocks"].find { |b| b["type"] == "header" }
        assert_includes header["text"]["text"], "Threshold"
        true
      end
    end

    # -----------------------------------------------------------------------
    # Payload: issue_assignment_updated
    # -----------------------------------------------------------------------

    test "assignment payload includes previous and new assignee names" do
      notifier = SlackNotifier.new(
        @integration,
        kind: "assignment_changed",
        issues: [ @issue ],
        previous_assignee_name: "Alice",
        new_assignee_name: "Bob"
      )
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        fields_block = body["blocks"].find { |b| b["type"] == "section" && b["fields"] }
        field_texts = fields_block["fields"].map { |f| f["text"] }

        assert field_texts.any? { |t| t.include?("Alice") }, "Expected previous assignee"
        assert field_texts.any? { |t| t.include?("Bob") }, "Expected new assignee"
        true
      end
    end

    test "assignment payload shows Unassigned when names are nil" do
      notifier = SlackNotifier.new(
        @integration,
        kind: "assignment_changed",
        issues: [ @issue ],
        previous_assignee_name: nil,
        new_assignee_name: nil
      )
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        fields_block = body["blocks"].find { |b| b["type"] == "section" && b["fields"] }
        field_texts = fields_block["fields"].map { |f| f["text"] }

        unassigned_count = field_texts.count { |t| t.include?("Unassigned") }
        assert_equal 2, unassigned_count, "Expected both From and To to show Unassigned"
        true
      end
    end

    # -----------------------------------------------------------------------
    # Payload: issue_created (batched)
    # -----------------------------------------------------------------------

    test "batch payload lists multiple issues" do
      issue2 = create(:issue, project: @project, title: "Second Error")
      issue3 = create(:issue, project: @project, title: "Third Error")

      notifier = SlackNotifier.new(@integration, kind: "issue_created", issues: [ @issue, issue2, issue3 ])
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        assert_includes body["text"], "3 New Issues"

        section = body["blocks"].find { |b| b["type"] == "section" && b.dig("text", "type") == "mrkdwn" }
        text = section["text"]["text"]
        assert_includes text, "Test Error"
        assert_includes text, "Second Error"
        assert_includes text, "Third Error"
        true
      end
    end

    test "batch payload truncates to 10 issues and shows remainder" do
      issues = 12.times.map { |i| create(:issue, project: @project, title: "Issue #{i}") }

      notifier = SlackNotifier.new(@integration, kind: "issue_created", issues: issues)
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        section = body["blocks"].find { |b| b["type"] == "section" && b.dig("text", "type") == "mrkdwn" }
        text = section["text"]["text"]
        assert_includes text, "and 2 more"
        true
      end
    end

    test "a single new Issue is sent as a batch of one with a link to it" do
      SlackNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        assert_includes body["text"], "1 New Issues"
        section = body["blocks"].find { |b| b["type"] == "section" && b.dig("text", "type") == "mrkdwn" }
        assert_includes section["text"]["text"], "/issues/#{@issue.to_param}|##{@issue.number} Test Error>"
        true
      end
    end

    # -----------------------------------------------------------------------
    # Unknown kind
    # -----------------------------------------------------------------------

    test "an unknown kind raises and posts nothing" do
      assert_raises(ArgumentError) do
        SlackNotifier.new(@integration, kind: "issue_notification", issues: [ @issue ]).call
      end
      assert_not_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX")
    end
  end
end
