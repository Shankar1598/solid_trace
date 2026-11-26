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
    button = body["blocks"].find { |block| block[:type] == "actions" || block["type"] == "actions" }["elements"].sole
    assert_equal [ "Open the Monitors", "http://example.com/acme/monitors" ], [ button.dig("text", "text"), button["url"] ]
    assert_not_requested(:post, OWN_URL)
  end

  test "Slack lists as many whole Monitor lines as fit, then how many more" do
    names = Array.new(30) { |i| "Monitor #{i} #{'with a long name ' * 6}".strip }
    facts = Notifiers::MonitorStatusMessage.facts(@organization, enabled: 40, all: false, monitors: names.each_with_index.map do |name, i|
      Notifiers::MonitorStatusMessage::Listed.new(id: i + 1, name: name, project: "Shop", since: "2026-10-17T11:58:00Z",
        error: "EventStore connection error: #{'refused ' * 10}".strip)
    end)
    row = Notification.create!(integration: @status, kind: NOT_RUNNING, payload: Notifiers::MonitorStatusMessage.payload(facts))

    deliver

    section = slack_bodies(STATUS_URL).sole["blocks"].last(2).first.dig("text", "text")
    lines = section.split("\n")
    assert_operator section.length, :<=, Notifiers::SlackNotifier::MAX_SECTION
    assert_match(/\A• and (\d+) more\z/, lines.last)
    shown = lines[...-1]
    assert_equal 30 - shown.size, lines.last[/\d+/].to_i
    assert shown.each_with_index.all? { |line, i| line.start_with?("• #{names[i]} (Shop), since") && line.end_with?("/acme/monitors/#{i + 1}") },
      "each line is whole"
    assert row.reload.status_sent?
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
    assert_nil told(m)
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
    assert_equal @status.id, told(m)
    perform_enqueued_jobs(only: IntegrationNotificationProcessorJob)
    assert_requested(:post, STATUS_URL, times: 1)
  end

  test "each job run schedules the tick again while a status row waits, so a tick that was not scheduled is" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    MonitorStatusJob.perform_now
    clear_enqueued_jobs

    travel 1.minute
    MonitorStatusJob.perform_now

    assert_enqueued_with(job: IntegrationNotificationProcessorJob, args: [ @status.id ])
    perform_enqueued_jobs(only: IntegrationNotificationProcessorJob)
    assert Notification.sole.status_sent?
    clear_enqueued_jobs

    travel 1.minute
    MonitorStatusJob.perform_now
    assert_no_enqueued_jobs(only: IntegrationNotificationProcessorJob)
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
    assert_nil told(m)
  end

  test "Monitors that run again send one message, and PagerDuty resolves the incident that not running opened" do
    pagerduty = pagerduty_status
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

    events = pagerduty_events
    assert_equal %w[trigger resolve], events.map { |event| event["event_action"] }
    assert_equal [ "solid-trace-monitors-not-running-#{@organization.id}" ] * 2, events.map { |event| event["dedup_key"] }
    assert_equal "All 2 Monitors are not running", events.first.dig("payload", "summary")
    assert_equal [ { "href" => "http://example.com/acme/monitors", "text" => "Open the Monitors" } ], events.first["links"]
    assert_equal [ nil, nil ], monitors.map { |m| told(m) }
  end

  test "running again while another Monitor still does not run says so, sends no PagerDuty event, and the last one back resolves" do
    pagerduty = pagerduty_status
    other = create(:project, organization: @organization, name: "Search")
    stub_monitor_query_check(other)
    a = monitor("A")
    b = monitor("B", project: other)
    eventstore_down(@project)
    eventstore_down(other)
    [ a, b ].each { |m| run_monitor(m) }
    travel 2.minutes
    MonitorStatusJob.perform_now
    deliver(pagerduty)

    travel 1.minute
    eventstore_up(@project)
    run_monitor(a)
    MonitorStatusJob.perform_now
    travel 1.minute
    deliver(pagerduty)

    row = Notification.find_by!(kind: RUNNING_AGAIN)
    message = Notifiers::MonitorStatusMessage.build(row)
    assert_equal "A is running again", message.title
    assert_equal "Their runs compare again. 1 Monitor is still not running.", message.text
    assert row.reload.status_skipped?, "PagerDuty gets no event for good news while the incident is still open"
    assert_equal %w[trigger], pagerduty_events.map { |event| event["event_action"] }

    travel 1.minute
    eventstore_up(other)
    run_monitor(b)
    MonitorStatusJob.perform_now
    travel 1.minute
    deliver(pagerduty)

    assert_equal %w[trigger resolve], pagerduty_events.map { |event| event["event_action"] }
  end

  test "a told Monitor that is turned off keeps not running until the job, then is in the running again message, as turned off" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    MonitorStatusJob.perform_now

    m.reload.update!(disabled_at: Time.current)
    assert_equal Time.current - 2.minutes, m.not_running_since
    MonitorStatusJob.perform_now

    row = Notification.find_by!(kind: RUNNING_AGAIN)
    assert_equal [ "turned_off" ], Notifiers::MonitorStatusMessage.read(row.payload).monitors.map(&:left)
    assert_equal "Checkout errors was turned off", Notifiers::MonitorStatusMessage.build(row).title
    assert_nil m.reload.not_running_since
    assert_nil told(m)
  end

  test "a told Monitor turned off and on again before the job is not running again until a run compares" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    MonitorStatusJob.perform_now

    m.reload.update!(disabled_at: Time.current)
    eventstore_up(@project)
    m.reload.update!(disabled_at: nil)
    MonitorStatusJob.perform_now
    assert_equal [ NOT_RUNNING ], Notification.pluck(:kind), "no false running again"
    assert_equal Time.current - 2.minutes, m.reload.not_running_since

    travel 1.minute
    run_monitor(m)
    MonitorStatusJob.perform_now
    assert_equal [ NOT_RUNNING, RUNNING_AGAIN ], Notification.order(:id).pluck(:kind)
    assert_equal "Checkout errors is running again", Notifiers::MonitorStatusMessage.build(Notification.order(:id).last).title
  end

  test "a deleted Monitor leaves the set with no message while another told one still does not run" do
    a = monitor("A")
    b = monitor("B")
    eventstore_down(@project)
    [ a, b ].each { |m| run_monitor(m) }
    travel 2.minutes
    MonitorStatusJob.perform_now

    a.destroy!
    MonitorStatusJob.perform_now

    assert_equal [ NOT_RUNNING ], Notification.pluck(:kind)
  end

  test "when every told Monitor is deleted, a running again lists them as deleted and resolves the PagerDuty incident" do
    pagerduty = pagerduty_status
    a = monitor("A")
    b = monitor("B")
    eventstore_down(@project)
    [ a, b ].each { |m| run_monitor(m) }
    travel 2.minutes
    MonitorStatusJob.perform_now
    deliver(pagerduty)

    travel 1.minute
    a.destroy!
    b.destroy!
    MonitorStatusJob.perform_now
    deliver(pagerduty)

    row = Notification.find_by!(kind: RUNNING_AGAIN)
    message = Notifiers::MonitorStatusMessage.build(row)
    assert_equal "B was deleted", message.title
    assert_equal "1 Monitor was deleted, so it does not run.", message.text
    assert_equal %w[trigger resolve], pagerduty_events.map { |event| event["event_action"] }
  end

  test "a changed Monitor status Integration is told about the Monitors that still do not run, and the old one hears no more" do
    other_url = "https://hooks.slack.com/services/T00/B00/OTHER"
    stub_request(:post, other_url).to_return(status: 200, body: "ok")
    other = @organization.integrations.create!(provider: "slack", name: "Other", settings: { "webhook_url" => other_url })
    still = monitor("Still failing")
    recovers = monitor("Recovers", project: second_project)
    eventstore_down(@project)
    eventstore_down(recovers.project)
    [ still, recovers ].each { |m| run_monitor(m) }
    travel 2.minutes
    MonitorStatusJob.perform_now

    @organization.update!(monitor_status_integration: other)
    travel 1.minute
    eventstore_up(recovers.project)
    [ still, recovers ].each { |m| run_monitor(m) }
    MonitorStatusJob.perform_now

    rows = Notification.order(:id).map { |row| [ row.integration_id, row.kind, monitor_names(row) ] }
    assert_equal [ [ @status.id, NOT_RUNNING, [ "Still failing", "Recovers" ] ], [ other.id, NOT_RUNNING, [ "Still failing" ] ] ], rows
    assert_equal [ other.id, nil ], [ told(still), told(recovers) ]
  end

  test "a deleted Monitor status Integration, or None, forgets what was told, and a new one is told about the Monitors that still do not run" do
    m = monitor("Checkout errors")
    eventstore_down(@project)
    run_monitor(m)
    travel 2.minutes
    MonitorStatusJob.perform_now
    assert_equal @status.id, told(m)

    @status.destroy!
    assert_nil told(m)
    assert_nil @organization.reload.monitor_status_integration_id

    other = @organization.integrations.create!(provider: "slack", name: "Other", settings: { "webhook_url" => OWN_URL })
    @organization.update!(monitor_status_integration: other)
    travel 1.minute
    MonitorStatusJob.perform_now
    assert_equal other.id, told(m)

    @organization.update!(monitor_status_integration: nil)
    MonitorStatusJob.perform_now
    assert_nil told(m), "with None, every mark is cleared"

    newest = @organization.integrations.create!(provider: "slack", name: "Newest", settings: { "webhook_url" => OWN_URL })
    @organization.update!(monitor_status_integration: newest)
    MonitorStatusJob.perform_now
    assert_equal [ other.id, newest.id ], Notification.where(kind: NOT_RUNNING).order(:id).pluck(:integration_id)
    assert_equal 0, Notification.where(kind: RUNNING_AGAIN).count
  end

  test "a not running message that ends skipped, refused or exhausted is sent again while the Monitor still does not run" do
    { skipped: 0.minutes, refused: 0.minutes, failed: Notification::MONITOR_RETRY_FOR + 1.minute }.each do |outcome, wait|
      Notification.delete_all
      eventstore_up(@project)
      m = monitor("Checkout errors #{outcome}")
      eventstore_down(@project)
      run_monitor(m)
      travel 2.minutes
      MonitorStatusJob.perform_now
      Notification.sole.update_columns(status: Notification.statuses.fetch(outcome.to_s))

      travel 1.minute
      MonitorStatusJob.perform_now
      if wait.positive?
        assert_equal 1, Notification.count, "a failed row is still retried"
        travel wait
        MonitorStatusJob.perform_now
      end

      rows = Notification.order(:id).to_a
      assert_equal [ NOT_RUNNING, NOT_RUNNING ], rows.map(&:kind), "after #{outcome}"
      assert_equal [ m.name ], monitor_names(rows.last)
      assert_equal @status.id, told(m)
      MonitorStatusJob.perform_now
      assert_equal 2, Notification.count, "the newer row was not lost, so nothing more"
      m.destroy!
    end
  end

  test "another Organization's Monitors that fail are not in the message, nor in its counts" do
    other_org = create(:organization, slug: "other")
    other_project = create(:project, organization: other_org, name: "Other shop")
    stub_monitor_query_check(other_project)
    other_status = other_org.integrations.create!(provider: "slack", name: "Their status", settings: { "webhook_url" => OWN_URL })
    other_org.update!(monitor_status_integration: other_status)
    theirs = Array.new(3) { |i| create(:monitor, project: other_project, name: "Theirs #{i}") }
    ours = [ monitor("Ours"), monitor("Ours too") ]
    eventstore_down(@project)
    eventstore_down(other_project)
    [ *ours, *theirs ].each { |m| run_monitor(m) }
    travel 2.minutes
    MonitorStatusJob.perform_now

    row = Notification.find_by!(integration: @status)
    facts = Notifiers::MonitorStatusMessage.read(row.payload)
    assert_equal [ "Ours", "Ours too" ], facts.monitors.map(&:name)
    assert_equal [ 2, true ], [ facts.enabled, facts.all ]
    assert_equal [ "Not running", "2 of 2 Monitors that are on" ], Notifiers::MonitorStatusMessage.build(row).fields.last
    assert_equal 3, Notifiers::MonitorStatusMessage.read(Notification.find_by!(integration: other_status).payload).monitors.size
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

  def told(monitor)
    LogMonitor.find(monitor.id).not_running_told_integration_id
  end

  def monitor_names(row)
    Notifiers::MonitorStatusMessage.read(row.payload).monitors.map(&:name)
  end

  def second_project
    create(:project, organization: @organization, name: "Search").tap { |project| stub_monitor_query_check(project) }
  end

  PAGERDUTY_URL = "https://events.pagerduty.com/v2/enqueue"

  def pagerduty_status
    stub_request(:post, PAGERDUTY_URL).to_return(status: 202, body: "{}")
    @organization.integrations.create!(provider: "pagerduty", settings: { "routing_key" => "R" })
      .tap { |pagerduty| @organization.update!(monitor_status_integration: pagerduty) }
  end

  def pagerduty_events
    events = []
    WebMock::RequestRegistry.instance.requested_signatures.hash.each do |signature, times|
      next unless signature.uri.host == "events.pagerduty.com"

      times.times { events << JSON.parse(signature.body) }
    end
    events
  end

  def slack_bodies(url)
    bodies = []
    assert_requested(:post, url, at_least_times: 1) { |request| bodies << JSON.parse(request.body) }
    bodies
  end
end
