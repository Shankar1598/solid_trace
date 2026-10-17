# frozen_string_literal: true

require "test_helper"
require "webmock/minitest"

# What each provider adapter sends for a Monitor's trigger, resolve and
# matched Logs, from the Notification row MonitorEvaluation wrote.
module Notifiers
  class MonitorMessagesTest < ActiveSupport::TestCase
    include ActionMailer::TestHelper
    include MonitorTestHelper

    SLACK_URL = "https://hooks.slack.com/services/T00/B00/MONITOR"
    PAGERDUTY_URL = "https://events.pagerduty.com/v2/enqueue"
    GROUP = { "service" => "api", "level" => "error" }.to_json

    setup do
      @organization = create(:organization, slug: "acme")
      @project = create(:project, organization: @organization, name: "Shop", slug: "shop")
      stub_monitor_query_check(@project)
      @monitor = create(:monitor, project: @project, name: "Checkout errors", operator: ">=", threshold: 100,
        range_minutes: 15, query: "['shop']\n| summarize count() by service, level")
      @slack = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => SLACK_URL })
      @pagerduty = @organization.integrations.create!(provider: "pagerduty",
        settings: { "routing_key" => "routing-123", "severity" => "critical" })
      @email = @organization.integrations.create!(provider: "email", settings: { "recipients" => "ops@example.com" })
      stub_request(:post, SLACK_URL).to_return(status: 200, body: "ok")
      stub_request(:post, PAGERDUTY_URL).to_return(status: 202, body: "{}")
    end

    # -------------------------------------------------------------------------
    # Slack
    # -------------------------------------------------------------------------

    test "Slack: a trigger shows the value against the threshold and links to the explorer over the range read" do
      deliver(@slack, firing)

      assert_requested(:post, SLACK_URL) do |request|
        body = JSON.parse(request.body)
        header, text, fields, actions = body["blocks"]
        url = actions["elements"].first["url"]
        params = Rack::Utils.parse_query(URI(url).query)

        assert_equal "🔥 Firing: Checkout errors (service=api, level=error)", body["text"]
        assert_equal body["text"], header.dig("text", "text")
        assert_equal "count_ was 120 over the last 15 minutes, at or above the threshold of 100.", text.dig("text", "text")
        assert_equal [ "*Project:*\nShop", "*Group:*\nservice=api, level=error", "*Value:*\n120",
                       "*Threshold:*\n&gt;= 100", "*Range:*\nlast 15 minutes" ], fields["fields"].map { |f| f["text"] }
        assert_equal "/acme/projects/shop/logs", URI(url).path
        assert_equal [ @monitor.query, "query", "2026-10-17T11:44:00Z", "2026-10-17T11:59:00Z" ],
          params.values_at("q", "mode", "from", "to")
        true
      end
    end

    test "Slack: a resolve says the group is back within the threshold" do
      deliver(@slack, resolved(value: 42.125))

      assert_requested(:post, SLACK_URL) do |request|
        body = JSON.parse(request.body)
        body["text"] == "✅ Resolved: Checkout errors (service=api, level=error)" &&
          body["blocks"][1].dig("text", "text") ==
            "count_ was 42.125 over the last 15 minutes, no longer at or above the threshold of 100."
      end
    end

    test "Slack: matched Logs are listed, at most ten, with how many more and that Logs were skipped" do
      deliver(@slack, matched(total: 13, logs_skipped_at: "2026-10-17T11:58:00Z"))

      assert_requested(:post, SLACK_URL) do |request|
        body = JSON.parse(request.body)
        lines = body["blocks"][3].dig("text", "text").split("\n")
        assert_equal "🔎 Checkout errors: 13 Logs matched", body["text"]
        assert_match "Some Logs were not checked: runs timed out 3 times in a row", body["blocks"][1].dig("text", "text")
        assert_equal 11, lines.size
        assert_equal "• 2026-10-17 11:59:50 UTC ERROR api payment &lt;declined&gt; for order 0", lines.first
        assert_equal "• and 3 more", lines.last
        true
      end
    end

    # -------------------------------------------------------------------------
    # PagerDuty
    # -------------------------------------------------------------------------

    test "PagerDuty: a trigger and its resolve share the group's dedup key" do
      deliver(@pagerduty, firing)
      deliver(@pagerduty, resolved(value: 3))

      events = []
      assert_requested(:post, PAGERDUTY_URL, times: 2) { |request| events << JSON.parse(request.body) }
      trigger, resolve = events
      key = "solid-trace-monitor-#{@monitor.id}-#{GROUP}"

      assert_equal [ "routing-123", "trigger", key ], trigger.values_at("routing_key", "event_action", "dedup_key")
      assert_equal "Firing: Checkout errors (service=api, level=error)", trigger.dig("payload", "summary")
      assert_equal [ "Shop", "critical" ], trigger["payload"].values_at("source", "severity")
      assert_equal ">= 100", trigger.dig("payload", "custom_details", "Threshold")
      assert_equal "120", trigger.dig("payload", "custom_details", "Value")
      assert trigger["links"].first["href"].include?("/acme/projects/shop/logs?")
      assert_equal({ "routing_key" => "routing-123", "event_action" => "resolve", "dedup_key" => key }, resolve)
    end

    test "PagerDuty: a Monitor of one group has a dedup key without a group, and a long group a digest" do
      assert_equal "solid-trace-monitor-7", MonitorMessage.dedup_key(7, "")
      long = { "path" => "/a" * 200 }.to_json
      assert_equal "solid-trace-monitor-7-#{Digest::SHA256.hexdigest(long)}", MonitorMessage.dedup_key(7, long)
    end

    test "PagerDuty: events go to PAGERDUTY_EVENTS_URL when it is set, over plain http too" do
      stub = stub_request(:post, "http://127.0.0.1:4555/v2/enqueue?run=1").to_return(status: 202, body: "{}")
      previous = ENV["PAGERDUTY_EVENTS_URL"]
      ENV["PAGERDUTY_EVENTS_URL"] = "http://127.0.0.1:4555/v2/enqueue?run=1"

      deliver(@pagerduty, firing)

      assert_requested(stub)
      assert_not_requested(:post, PAGERDUTY_URL)
    ensure
      ENV["PAGERDUTY_EVENTS_URL"] = previous
    end

    # -------------------------------------------------------------------------
    # Email
    # -------------------------------------------------------------------------

    test "Email: a trigger, a resolve and matched Logs each go to the mailer" do
      assert_enqueued_emails 3 do
        deliver(@email, firing)
        deliver(@email, resolved(value: 3))
        deliver(@email, matched(total: 2))
      end
    end

    test "Email: the mail carries the title, the value against the threshold and the link" do
      message = MonitorMessage.build(firing)
      mail = MonitorMailer.notify(message.to_h, [ "ops@example.com" ])

      assert_equal "[SolidTrace] Firing: Checkout errors (service=api, level=error)", mail.subject
      assert_equal [ "ops@example.com" ], mail.to
      text = mail.text_part.body.to_s
      assert_match "Threshold: >= 100", text
      assert_match "Value: 120", text
      assert_match %r{Open in the Logs explorer: http://[^/]+/acme/projects/shop/logs\?}, text
      assert_match "at or above the threshold of 100", mail.html_part.body.to_s
    end

    private

    def deliver(integration, row)
      adapter = IntegrationNotification::ADAPTERS.fetch(integration.provider).constantize
      adapter.new(integration, kind: row.kind, message: MonitorMessage.build(row)).call
    end

    def firing
      row(IntegrationNotification::MONITOR_FIRING, GROUP, threshold_payload(120.0))
    end

    def resolved(value:)
      row(IntegrationNotification::MONITOR_RESOLVED, GROUP, threshold_payload(value))
    end

    def matched(total:, logs_skipped_at: nil)
      logs = [ total, 10 ].min.times.map do |i|
        { "id" => "log-#{i}", "time" => (Time.utc(2026, 10, 17, 11, 59, 50) - i).iso8601,
          "level" => "error", "service" => "api", "message" => "payment <declined> for order #{i}" }
      end
      row(IntegrationNotification::MONITOR_MATCHED, "", {
        "logs" => logs, "total" => total, "from" => logs.last["time"], "to" => "2026-10-17T12:00:00Z",
        "logs_skipped_at" => logs_skipped_at,
      })
    end

    def threshold_payload(value)
      { "value" => value, "column" => "count_", "threshold" => 100.0, "operator" => ">=",
        "from" => "2026-10-17T11:44:00Z", "to" => "2026-10-17T11:59:00Z" }
    end

    def row(kind, group_key, payload)
      Notification.new(integration: @slack, monitor: @monitor, group_key: group_key, kind: kind, payload: payload)
    end
  end
end
