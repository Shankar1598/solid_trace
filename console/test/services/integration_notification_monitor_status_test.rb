# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "webmock/minitest"

# The Monitor status messages: Monitors run through MonitorEvaluation with
# EventStore stubbed (WebMock), MonitorStatusJob runs as the recurring task
# does, and the tick delivers. Observes the Notification rows, what the
# Monitor status Integration and the Monitors' own Integration receive, and
# the Monitors list's facts.
class IntegrationNotificationMonitorStatusTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActiveSupport::Testing::TimeHelpers
  include MonitorTestHelper

  STATUS_URL = "https://hooks.slack.com/services/T00/B00/STATUS"
  OWN_URL = "https://hooks.slack.com/services/T00/B00/OWN"
  NOT_RUNNING = IntegrationNotification::MONITORS_NOT_RUNNING
  RUNNING_AGAIN = IntegrationNotification::MONITORS_RUNNING_AGAIN

  setup do
    travel_to Time.utc(2026, 10, 17, 12, 0, 0)
    @organization = create(:organization, slug: "acme")
    @project = create(:project, organization: @organization, name: "Shop")
    @own = @organization.integrations.create!(provider: "slack", name: "Own", settings: { "webhook_url" => OWN_URL })
    @status = @organization.integrations.create!(provider: "slack", name: "Status", settings: { "webhook_url" => STATUS_URL })
    @organization.update!(monitor_status_integration: @status)
    stub_monitor_query_check(@project)
    stub_request(:post, STATUS_URL).to_return(status: 200, body: "ok")
    stub_request(:post, OWN_URL).to_return(status: 200, body: "ok")
  end

  teardown do
    travel_back
  end

  test "30 Monitors that all fail for 2 minutes send one message that says all 30, with the first error once" do
    monitors = Array.new(30) { |i| monitor("Monitor #{i}") }
    eventstore_down(@project)

    3.times do |minute|
      travel 1.minute unless minute.zero?
      monitors.each { |m| run_monitor(m) }
      MonitorStatusJob.perform_now
    end

    row = Notification.sole
    assert_equal NOT_RUNNING, row.kind
    assert row.payload["all"]
    assert_equal 30, row.payload["monitors"].size
    deliver

    body = slack_bodies(STATUS_URL).sole
    assert_equal "🚨 All 30 Monitors are not running", body["text"]
    assert_equal 1, body.to_json.scan("EventStore connection error").size, "the first error is given once"
    assert_not_requested(:post, OWN_URL)
  end

  test "one Monitor that fails 3 runs while the others pass sends one message that names it" do
    other = create(:project, organization: @organization, name: "Search")
    stub_monitor_query_check(other)
    failing = monitor("Search errors", project: other)
    passing = Array.new(3) { |i| monitor("Shop #{i}") }
    eventstore_down(other)

    3.times do |minute|
      travel 1.minute unless minute.zero?
      [ failing, *passing ].each { |m| run_monitor(m) }
      MonitorStatusJob.perform_now
    end

    row = Notification.sole
    assert_not row.payload["all"]
    assert_equal [ "Search errors" ], row.payload["monitors"].map { |m| m["name"] }
    deliver
    body = slack_bodies(STATUS_URL).sole
    assert_equal "🚨 Search errors is not running", body["text"]
    assert_includes body.to_json, "EventStore connection error"
    assert_includes body.to_json, "/acme/monitors/#{failing.id}"
  end

  test "a Monitor not running for less than 2 minutes sends nothing" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 1.minute + 59.seconds
    run_monitor(m)

    MonitorStatusJob.perform_now

    assert_equal 0, Notification.count
    assert_not m.reload.not_running_notified
  end

  test "the job run two times in the same minute writes one message" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    run_monitor(m)

    MonitorStatusJob.perform_now
    MonitorStatusJob.perform_now

    assert_equal 1, Notification.count
    assert m.reload.not_running_notified
    assert_enqueued_jobs 1, only: IntegrationNotificationProcessorJob
  end

  test "with no Monitor status Integration there is no message, and the Monitor still shows not running" do
    @organization.update!(monitor_status_integration: nil)
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 3.minutes
    run_monitor(m)

    MonitorStatusJob.perform_now

    assert_equal 0, Notification.count
    assert_equal (Time.current - 3.minutes).iso8601, MonitorSerializer.new(m.reload).as_json[:not_running_since]
  end

  test "an inactive Monitor status Integration gets no message" do
    @status.update!(active: false)
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 3.minutes

    MonitorStatusJob.perform_now

    assert_equal 0, Notification.count
    assert_not m.reload.not_running_notified
  end

  test "Monitors that run again send one message, and PagerDuty resolves the incident that not running opened" do
    pagerduty = @organization.integrations.create!(provider: "pagerduty", settings: { "routing_key" => "R" })
    @organization.update!(monitor_status_integration: pagerduty)
    pagerduty_url = "https://events.pagerduty.com/v2/enqueue"
    stub_request(:post, pagerduty_url).to_return(status: 202, body: "{}")
    monitors = [ monitor("A"), monitor("B") ]
    eventstore_down(@project)
    monitors.each { |m| run_monitor(m) }
    travel 2.minutes
    MonitorStatusJob.perform_now

    travel 1.minute
    eventstore_up(@project)
    monitors.each { |m| run_monitor(m) }
    MonitorStatusJob.perform_now
    MonitorStatusJob.perform_now

    assert_equal [ NOT_RUNNING, RUNNING_AGAIN ], Notification.order(:id).pluck(:kind)
    assert Notification.order(:id).last.payload["all"]
    deliver(pagerduty)
    travel 1.minute
    deliver(pagerduty)

    events = []
    assert_requested(:post, pagerduty_url, times: 2) { |request| events << JSON.parse(request.body) }
    assert_equal %w[trigger resolve], events.map { |event| event["event_action"] }
    assert_equal [ "solid-trace-monitors-not-running-#{@organization.id}" ] * 2, events.map { |event| event["dedup_key"] }
    assert_equal "All 2 Monitors are not running", events.first.dig("payload", "summary")
    assert_equal [ false, false ], monitors.map { |m| m.reload.not_running_notified }
  end

  test "running again while another Monitor still does not run says so, and keeps the PagerDuty incident open" do
    other = create(:project, organization: @organization, name: "Search")
    stub_monitor_query_check(other)
    a = monitor("A")
    b = monitor("B", project: other)
    eventstore_down(@project)
    eventstore_down(other)
    [ a, b ].each { |m| run_monitor(m) }
    travel 2.minutes
    MonitorStatusJob.perform_now

    eventstore_up(@project)
    run_monitor(a)
    MonitorStatusJob.perform_now

    row = Notification.find_by!(kind: RUNNING_AGAIN)
    message = Notifiers::MonitorStatusMessage.build(row)
    assert_equal "A is running again", message.title
    assert_equal "Their runs compare again. 1 Monitor is still not running.", message.text
    assert_equal "trigger", message.action
  end

  test "a not running Monitor that is turned off is in the running again message, as turned off" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    MonitorStatusJob.perform_now

    m.reload.update!(disabled_at: Time.current)
    assert_nil m.not_running_since
    MonitorStatusJob.perform_now

    row = Notification.find_by!(kind: RUNNING_AGAIN)
    assert_equal [ true ], row.payload["monitors"].map { |fact| fact["turned_off"] }
    assert_equal "Checkout errors was turned off", Notifiers::MonitorStatusMessage.build(row).title
  end

  test "a deleted Monitor leaves the set with no message" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    MonitorStatusJob.perform_now

    m.destroy!
    MonitorStatusJob.perform_now

    assert_equal [ NOT_RUNNING ], Notification.pluck(:kind)
  end

  test "a hold that reaches 10 minutes is told at once, with why the runs are held" do
    m = monitor("Checkout errors")
    stub_request(:post, query_url(@project)).with(body: hash_including("from", "to"))
      .to_return(status: 200, body: { incomplete: { reason: "log_processing_behind", behind_seconds: 900 } }.to_json)
    10.times do |minute|
      travel 1.minute unless minute.zero?
      run_monitor(m)
      MonitorStatusJob.perform_now
    end
    assert_equal 0, Notification.count, "a hold is not a failure until it is 10 minutes old"

    travel 1.minute
    run_monitor(m)
    MonitorStatusJob.perform_now

    row = Notification.sole
    assert_equal "Held: Waiting for Log processing: it is 15 minutes behind.", row.payload["monitors"].sole["error"]
  end

  test "running again waits behind a not running message that failed, so it never goes first" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    MonitorStatusJob.perform_now
    eventstore_up(@project)
    run_monitor(m)
    MonitorStatusJob.perform_now
    stub_request(:post, STATUS_URL).to_return(status: 500, body: "down")

    deliver

    assert_requested(:post, STATUS_URL, times: 1)
    not_running, running_again = Notification.order(:id).to_a
    assert not_running.reload.status_failed?
    assert running_again.reload.status_pending?
    assert not_running.retried?
  end

  test "an email Monitor status Integration gets a mail with the Monitors and a link to the list" do
    email = @organization.integrations.create!(provider: "email", settings: { "recipients" => "ops@example.com" })
    @organization.update!(monitor_status_integration: email)
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    MonitorStatusJob.perform_now

    perform_enqueued_jobs { deliver(email) }

    mail = ActionMailer::Base.deliveries.last
    assert_equal "[SolidTrace] Checkout errors is not running", mail.subject
    assert_includes mail.text_part.body.to_s, "Open the Monitors: http://example.com/acme/monitors"
    assert_includes mail.text_part.body.to_s, "Checkout errors (Shop), since 2026-10-17 12:00 UTC"
  end

  private

  def monitor(name, project: @project)
    create(:monitor, project: project, name: name, integrations: [ @own ]).tap { WebMock::RequestRegistry.instance.reset! }
  end

  def run_monitor(monitor)
    MonitorEvaluation.run(monitor.reload, Time.current)
  end

  def query_url(project)
    "#{EventStore.base_url}/api/#{project.id}/logs/query"
  end

  def eventstore_down(project)
    stub_request(:post, query_url(project)).to_raise(Errno::ECONNREFUSED)
  end

  def eventstore_up(project)
    stub_request(:post, query_url(project)).with(body: hash_including("from", "to"))
      .to_return(status: 200, body: { table: { columns: [ COUNT_COLUMN ], rows: [] } }.to_json)
  end

  def deliver(integration = @status)
    IntegrationNotification.deliver_pending(integration.id)
  end

  def slack_bodies(url)
    bodies = []
    assert_requested(:post, url, at_least_times: 1) { |request| bodies << JSON.parse(request.body) }
    bodies
  end
end
