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

    # The title holds the group's Log values, which Slack's top-level text
    # would read as mrkdwn: a mention or a link a Log wrote.
    test "Slack: Log values in the title are sent as written, never as a mention or a link" do
      group = { "service" => "<!channel>", "level" => "<https://evil.example|login> & more" }.to_json
      deliver(@slack, row(IntegrationNotification::MONITOR_FIRING, group, threshold_payload(120.0)))

      assert_requested(:post, SLACK_URL) do |request|
        body = JSON.parse(request.body)
        assert_equal "🔥 Firing: Checkout errors (service=&lt;!channel&gt;, level=&lt;https://evil.example|login&gt; &amp; more)",
          body["text"]
        assert_equal "🔥 Firing: Checkout errors (service=<!channel>, level=<https://evil.example|login> & more)",
          body["blocks"].first.dig("text", "text"), "the header is plain_text, which Slack shows as written"
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

    test "the text says when a group fired for no data, has data again, has no row any more or stopped being compared" do
      texts = {
        firing_no_data: [ IntegrationNotification::MONITOR_FIRING, "", threshold_payload(nil) ],
        data_again: [ IntegrationNotification::MONITOR_RESOLVED, "", threshold_payload(4.0).merge("no_data" => true) ],
        absent: [ IntegrationNotification::MONITOR_RESOLVED, GROUP, threshold_payload(nil).merge("absent" => true) ],
        kind_changed: [ IntegrationNotification::MONITOR_RESOLVED, GROUP,
          threshold_payload(120.0).except("from", "to").merge("kind_changed" => true) ],
      }.transform_values { |kind, group, payload| MonitorMessage.build(row(kind, group, payload)).text }

      assert_equal "No data over the last 15 minutes.", texts[:firing_no_data]
      assert_equal "There is data again over the last 15 minutes: count_ was 4, not at or above the threshold of 100.",
        texts[:data_again]
      assert_equal "No rows for this group over the last 15 minutes, so it is back to ok.", texts[:absent]
      assert_equal "The Monitor was changed to match Logs, so it no longer compares a number with the threshold.",
        texts[:kind_changed]
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
    # Comparing the change
    # -------------------------------------------------------------------------

    test "Slack: a change trigger gives the number, its Baseline and the change, and links to both ranges" do
      deliver(@slack, row(IntegrationNotification::MONITOR_FIRING, GROUP, change_payload(120.0, 60.0)))

      assert_requested(:post, SLACK_URL) do |request|
        body = JSON.parse(request.body)
        _, text, fields, actions = body["blocks"]
        now, before = actions["elements"]
        assert_equal "count_ was 120 over the last 15 minutes, up 100% from 60 the 15 minutes before.", text.dig("text", "text")
        assert_equal [ "*Project:*\nShop", "*Group:*\nservice=api, level=error", "*Value:*\n120", "*Baseline:*\n60",
                       "*Change:*\n+100%", "*Threshold:*\nrises 50%", "*Range:*\nlast 15 minutes" ], fields["fields"].map { |f| f["text"] }
        assert_equal [ "Open in the Logs explorer", "Open the Baseline" ], [ now, before ].map { |button| button.dig("text", "text") }
        assert_equal [ "2026-10-17T11:44:00Z", "2026-10-17T11:59:00Z" ], Rack::Utils.parse_query(URI(now["url"]).query).values_at("from", "to")
        assert_equal [ @monitor.query, "2026-10-17T11:29:00Z", "2026-10-17T11:44:00Z" ],
          Rack::Utils.parse_query(URI(before["url"]).query).values_at("q", "from", "to")
        true
      end
    end

    test "a change reads up from 0, down by a percent, and a resolve gives the change it came back to" do
      texts = {
        from_zero: [ IntegrationNotification::MONITOR_FIRING, change_payload(5.0, 0.0) ],
        falls: [ IntegrationNotification::MONITOR_FIRING, change_payload(30.0, 60.0).merge("operator" => "falls") ],
        resolved: [ IntegrationNotification::MONITOR_RESOLVED, change_payload(70.0, 60.0) ],
        steady: [ IntegrationNotification::MONITOR_RESOLVED, change_payload(0.0, 0.0).merge("operator" => "changes") ],
      }.transform_values { |kind, payload| MonitorMessage.build(row(kind, GROUP, payload)) }

      assert_equal "count_ was 5 over the last 15 minutes, up from 0 the 15 minutes before.", texts[:from_zero].text
      assert_equal "up from 0", texts[:from_zero].fields.to_h["Change"]
      assert_equal "count_ was 30 over the last 15 minutes, down 50% from 60 the 15 minutes before.", texts[:falls].text
      assert_equal "falls 50%", texts[:falls].fields.to_h["Threshold"]
      assert_equal "count_ was 70 over the last 15 minutes, up 16.7% from 60 the 15 minutes before, " \
        "no longer a rise of 50% or more.", texts[:resolved].text
      assert_equal "count_ was 0 over the last 15 minutes, no change from 0 the 15 minutes before, " \
        "no longer a change of 50% or more.", texts[:steady].text
    end

    test "a resolve after switching between the value and the change says so" do
      to_change = MonitorMessage.build(row(IntegrationNotification::MONITOR_RESOLVED, GROUP,
        threshold_payload(120.0).except("from", "to").merge("compare_changed" => true)))
      to_value = MonitorMessage.build(row(IntegrationNotification::MONITOR_RESOLVED, GROUP,
        change_payload(120.0, 60.0).except("from", "to", "baseline_from", "baseline_to").merge("compare_changed" => true)))

      assert_equal "The Monitor now compares the change from a Baseline instead of the value, so its groups start again.",
        to_change.text
      assert_equal "The Monitor now compares the value instead of the change from a Baseline, so its groups start again.",
        to_value.text
      assert_equal [ "120", "60", "+100%", "rises 50%" ], to_value.fields.to_h.values_at("Value", "Baseline", "Change", "Threshold")
    end

    test "PagerDuty: a change trigger keeps one link and the group's dedup key, and gives the Baseline and change" do
      deliver(@pagerduty, row(IntegrationNotification::MONITOR_FIRING, GROUP, change_payload(120.0, 60.0)))

      assert_requested(:post, PAGERDUTY_URL) do |request|
        event = JSON.parse(request.body)
        assert_equal "solid-trace-monitor-#{@monitor.id}-#{GROUP}", event["dedup_key"]
        assert_equal [ "120", "60", "+100%", "rises 50%" ],
          event.dig("payload", "custom_details").values_at("Value", "Baseline", "Change", "Threshold")
        assert_equal 1, event["links"].size
        true
      end
    end

    test "Email: a change trigger links to both ranges" do
      message = MonitorMessage.build(row(IntegrationNotification::MONITOR_FIRING, GROUP, change_payload(120.0, 60.0)))
      mail = MonitorMailer.notify(message.to_h, [ "ops@example.com" ])

      text = mail.text_part.body.to_s
      assert_match "Baseline: 60", text
      assert_match %r{Open in the Logs explorer: http://[^/]+/acme/projects/shop/logs\?.*from=2026-10-17T11%3A44}, text
      assert_match %r{Open the Baseline in the Logs explorer: http://[^/]+/acme/projects/shop/logs\?.*from=2026-10-17T11%3A29}, text
      assert_match "Open the Baseline in the Logs explorer", mail.html_part.body.to_s
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

    # A run comparing the change from the range just before: rises 50%.
    def change_payload(value, baseline)
      change = baseline.zero? ? (value.zero? ? 0.0 : nil) : (value - baseline) / baseline.abs * 100
      threshold_payload(value).merge("compare" => "change", "operator" => "rises", "threshold" => 50.0,
        "baseline" => baseline, "change" => change,
        "baseline_from" => "2026-10-17T11:29:00Z", "baseline_to" => "2026-10-17T11:44:00Z")
    end

    def row(kind, group_key, payload)
      Notification.new(integration: @slack, monitor: @monitor, group_key: group_key, kind: kind, payload: payload)
    end
  end
end
