# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "webmock/minitest"

# The outbox for Monitors: monitor_changed writes rows, and the tick delivers
# them apart from Issue rows, as an ordered chain per group of a Monitor.
# Observes only Notification rows, outbound HTTP (WebMock) and scheduled jobs.
class IntegrationNotificationMonitorTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActiveSupport::Testing::TimeHelpers
  include MonitorTestHelper

  SLACK_URL = "https://hooks.slack.com/services/T00/B00/MONITOR"
  FIRING = IntegrationNotification::MONITOR_FIRING
  RESOLVED = IntegrationNotification::MONITOR_RESOLVED

  setup do
    travel_to Time.utc(2026, 10, 17, 12, 0, 0)
    @organization = create(:organization, slug: "acme")
    @project = create(:project, organization: @organization, name: "Shop")
    @slack = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => SLACK_URL })
    stub_monitor_query_check(@project)
    @monitor = create(:monitor, project: @project, name: "Checkout errors", integrations: [ @slack ])
    stub_request(:post, SLACK_URL).to_return(status: 200, body: "ok")
  end

  teardown do
    travel_back
  end

  test "a change writes one row per active Integration of the Monitor and schedules its tick" do
    inactive = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => SLACK_URL }, active: false)
    @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => SLACK_URL }) # not the Monitor's
    @monitor.integrations << inactive

    assert_difference -> { Notification.count }, 1 do
      change(FIRING, value: 12)
    end

    row = Notification.sole
    assert_equal [ @slack.id, @monitor.id, "", FIRING, nil ], [ row.integration_id, row.monitor_id, row.group_key, row.kind, row.issue_id ]
    assert row.status_pending?
    assert_enqueued_with(job: IntegrationNotificationProcessorJob, args: [ @slack.id ],
      at: Time.current + IntegrationNotification::INITIAL_DELAY)
  end

  test "Issue rows and Monitor rows due together are delivered apart, each as its own message" do
    issue = create(:issue, project: @project, title: "Boom")
    IntegrationNotification.issue_received_event(issue, newly_created: true)
    change(FIRING, value: 12)

    IntegrationNotification.deliver_pending(@slack.id)

    texts = []
    assert_requested(:post, SLACK_URL, times: 2) { |request| texts << JSON.parse(request.body)["text"] }
    assert_equal [ "🚨 1 New Issues Detected", "🔥 Firing: Checkout errors" ], texts
    assert Notification.all.all?(&:status_sent?)
  end

  test "a group's rows go in order, and a failed trigger holds back the resolve until it is sent" do
    change(FIRING, value: 12)
    change(RESOLVED, value: 3)
    stub_request(:post, SLACK_URL).to_return(status: 500, body: "down")

    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 1)
    firing, resolved = Notification.order(:id).to_a
    assert firing.reload.status_failed?
    assert_equal 1, firing.attempts
    assert resolved.reload.status_pending?, "the resolve waits behind the failed trigger"
    assert_equal 0, resolved.attempts
    assert_enqueued_with(job: IntegrationNotificationProcessorJob, args: [ @slack.id ],
      at: Time.current + IntegrationNotification::RATE_LIMIT_WINDOW)

    WebMock::RequestRegistry.instance.reset!
    stub_request(:post, SLACK_URL).to_return(status: 200, body: "ok")
    travel IntegrationNotification::RATE_LIMIT_WINDOW
    IntegrationNotification.deliver_pending(@slack.id)

    texts = []
    assert_requested(:post, SLACK_URL, times: 2) { |request| texts << JSON.parse(request.body)["text"] }
    assert_equal [ "🔥 Firing: Checkout errors", "✅ Resolved: Checkout errors" ], texts
    assert firing.reload.status_sent?
    assert resolved.reload.status_sent?
  end

  test "a failure stops only its own group's chain" do
    change(FIRING, value: 12, group_key: { service: "api" }.to_json)
    change(RESOLVED, value: 3, group_key: { service: "api" }.to_json)
    change(FIRING, value: 40, group_key: { service: "web" }.to_json)
    stub_request(:post, SLACK_URL).to_return do |request|
      JSON.parse(request.body)["text"].include?("service=api") ? { status: 500, body: "down" } : { status: 200, body: "ok" }
    end

    IntegrationNotification.deliver_pending(@slack.id)

    statuses = Notification.order(:id).map(&:status)
    assert_equal %w[failed pending sent], statuses
  end

  test "rows whose Monitor was deleted are skipped" do
    change(FIRING, value: 12)
    @monitor.destroy!

    IntegrationNotification.deliver_pending(@slack.id)

    row = Notification.sole
    assert row.status_skipped?
    assert_equal "Monitor missing", row.error_message
    assert_not_requested(:post, SLACK_URL)
  end

  test "rows for an Integration that is not configured are skipped and the chain goes on" do
    @slack.update!(settings: { "webhook_url" => "" })
    change(FIRING, value: 12)
    change(RESOLVED, value: 3)

    IntegrationNotification.deliver_pending(@slack.id)

    assert Notification.all.all?(&:status_skipped?)
    assert_equal [ "No webhook URL" ], Notification.distinct.pluck(:error_message)
  end

  test "an unknown kind is refused" do
    assert_raises(ArgumentError) { change("monitor_exploded", value: 1) }
  end

  private

  def change(kind, value:, group_key: "")
    IntegrationNotification.monitor_changed(@monitor, kind: kind, group_key: group_key, payload: {
      "value" => value, "column" => "count_", "threshold" => 10.0, "operator" => ">",
      "from" => "2026-10-17T11:54:00Z", "to" => "2026-10-17T11:59:00Z",
    })
  end
end
