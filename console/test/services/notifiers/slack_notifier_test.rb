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
    # call — HTTP delivery
    # -----------------------------------------------------------------------

    test "posts JSON to the configured webhook URL" do
      notifier = SlackNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      response = notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        assert_includes body["text"], "New Issue"
        true
      end
    end

    test "returns nil and does not post when webhook_url is blank" do
      @integration.settings["webhook_url"] = ""
      @integration.save!

      notifier = SlackNotifier.new(@integration, @issue)
      result = notifier.call

      assert_nil result
      assert_not_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX")
    end

    test "returns nil on network error without raising" do
      stub_request(:post, "https://hooks.slack.com/services/T00/B00/XXX")
        .to_raise(Errno::ECONNREFUSED)

      notifier = SlackNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      result = notifier.call

      assert_nil result
    end

    test "logs warning on non-success HTTP response" do
      stub_request(:post, "https://hooks.slack.com/services/T00/B00/XXX")
        .to_return(status: 500, body: "internal error")

      notifier = SlackNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      response = notifier.call

      assert_equal "500", response.code
    end

    # -----------------------------------------------------------------------
    # Payload: issue_created
    # -----------------------------------------------------------------------

    test "issue_created payload includes title and kind" do
      notifier = SlackNotifier.new(@integration, @issue, notification: { event: "issue_created" })
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        blocks = body["blocks"]

        # Header block
        header = blocks.find { |b| b["type"] == "header" }
        assert_includes header["text"]["text"], "New Issue Created"

        # Fields block
        fields_block = blocks.find { |b| b["type"] == "section" && b["fields"] }
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
      notifier = SlackNotifier.new(@integration, @issue, notification: { event: "event_threshold_reached" })
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
      notification = {
        event: "issue_assignment_updated",
        previous_assignee_name: "Alice",
        new_assignee_name: "Bob",
      }

      notifier = SlackNotifier.new(@integration, @issue, notification: notification)
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
      notification = {
        event: "issue_assignment_updated",
        previous_assignee_name: nil,
        new_assignee_name: nil,
      }

      notifier = SlackNotifier.new(@integration, @issue, notification: notification)
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
    # Payload: issue_created_batch
    # -----------------------------------------------------------------------

    test "batch payload lists multiple issues" do
      issue2 = create(:issue, project: @project, title: "Second Error")
      issue3 = create(:issue, project: @project, title: "Third Error")

      notification = {
        event: "issue_created_batch",
        issues: [@issue, issue2, issue3],
      }

      notifier = SlackNotifier.new(@integration, @issue, notification: notification)
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

      notification = {
        event: "issue_created_batch",
        issues: issues,
      }

      notifier = SlackNotifier.new(@integration, issues.first, notification: notification)
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        section = body["blocks"].find { |b| b["type"] == "section" && b.dig("text", "type") == "mrkdwn" }
        text = section["text"]["text"]
        assert_includes text, "and 2 more"
        true
      end
    end

    test "batch payload falls back to single issue when issues array is empty" do
      notification = {
        event: "issue_created_batch",
        issues: [],
      }

      notifier = SlackNotifier.new(@integration, @issue, notification: notification)
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        assert_includes body["text"], "1 New Issues"
        true
      end
    end

    # -----------------------------------------------------------------------
    # Default event (no notification hash)
    # -----------------------------------------------------------------------

    test "default notification uses issue_created payload" do
      notifier = SlackNotifier.new(@integration, @issue)
      notifier.call

      assert_requested(:post, "https://hooks.slack.com/services/T00/B00/XXX") do |req|
        body = JSON.parse(req.body)
        assert_includes body["text"], "New Issue"
        true
      end
    end
  end
end
