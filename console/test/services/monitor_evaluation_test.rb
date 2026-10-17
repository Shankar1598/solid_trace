# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "webmock/minitest"

# Runs Monitors through MonitorEvaluation.run with EventStore stubbed
# (WebMock) and the clock fixed, and observes what a member and an
# Integration would: the Monitor, its states and runs, and the
# Notifications written.
class MonitorEvaluationTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActiveSupport::Testing::TimeHelpers
  include MonitorTestHelper

  NOW = Time.utc(2026, 10, 17, 12, 0, 0)
  CURSOR = "0199f0a0-0000-7000-8000-000000000001"
  NEXT_CURSOR = "0199f0a0-0000-7000-8000-000000000002"
  NEWEST_CURSOR = "0199f0a0-0000-7000-8000-000000000003"
  SLACK_URL = "https://hooks.slack.com/services/T00/B00/MONITOR"
  SERVICE = { "name" => "service", "type" => "string" }.freeze
  TIMEOUT = "the run was stopped after 30 s, its timeout: too many Logs were ingested since the last run to read in time"

  setup do
    travel_to NOW
    @organization = create(:organization, slug: "acme")
    @project = create(:project, organization: @organization, name: "Shop")
    @slack = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => SLACK_URL })
    stub_monitor_query_check(@project, columns: [ SERVICE, COUNT_COLUMN ], ingest_cursor: CURSOR)
    @query_url = "#{EventStore.base_url}/api/#{@project.id}/logs/query"
  end

  teardown do
    travel_back
  end

  # ---------------------------------------------------------------------------
  # Threshold: transitions
  # ---------------------------------------------------------------------------

  test "a Threshold run reads [now - range - 60 s, now - 60 s) with the run timeout" do
    monitor = threshold_monitor(range_minutes: 15)
    answer_table([ [ 3 ] ])

    evaluate(monitor)

    assert_requested(:post, @query_url, times: 1) do |request|
      body = JSON.parse(request.body)
      body["query"] == monitor.query && body["from"] == "2026-10-17T11:44:00.000Z" &&
        body["to"] == "2026-10-17T11:59:00.000Z" && body["timeout_seconds"] == LogMonitor::RUN_TIMEOUT_SECONDS
    end
  end

  test "ok to firing sends firing once, staying firing sends nothing, and firing to ok sends resolved" do
    monitor = threshold_monitor

    answer_table([ [ 12 ] ])
    evaluate(monitor)
    assert_equal [ [ "monitor_firing", "", 12 ] ], notifications
    assert_state monitor, "", "firing", 12, since: NOW

    travel 1.minute
    answer_table([ [ 40 ] ])
    evaluate(monitor)
    assert_equal 1, Notification.count, "a group that stays firing sends nothing more"
    assert_state monitor, "", "firing", 40, since: NOW

    travel 1.minute
    answer_table([ [ 4 ] ])
    evaluate(monitor)
    assert_equal [ [ "monitor_firing", "", 12 ], [ "monitor_resolved", "", 4 ] ], notifications
    assert_state monitor, "", "ok", 4, since: NOW + 2.minutes

    resolved = Notification.order(:id).last.payload
    assert_equal({ "column" => "count_", "threshold" => 10.0, "operator" => ">", "value" => 4, "no_data" => false,
                   "from" => "2026-10-17T11:56:00Z", "to" => "2026-10-17T12:01:00Z" }, resolved)
    assert_equal [ 1, 1, 0 ], monitor.runs.reorder(:ran_at).pluck(:firing)
  end

  test "a number at the threshold fires by >= and not by >" do
    strict = threshold_monitor(operator: ">")
    inclusive = threshold_monitor(operator: ">=")
    answer_table([ [ 10 ] ])

    evaluate(strict)
    evaluate(inclusive)

    assert strict.states.sole.status_ok?
    assert inclusive.states.sole.status_firing?
  end

  test "a below Monitor fires on a small number" do
    monitor = threshold_monitor(operator: "<", threshold: 5)
    answer_table([ [ 2 ] ])

    evaluate(monitor)

    assert_equal [ [ "monitor_firing", "", 2 ] ], notifications
  end

  test "a firing run's Notification reaches Slack" do
    monitor = threshold_monitor(name: "Checkout errors")
    answer_table([ [ 12 ] ])
    stub_request(:post, SLACK_URL).to_return(status: 200, body: "ok")

    evaluate(monitor)
    travel IntegrationNotification::INITIAL_DELAY
    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL) { |request| JSON.parse(request.body)["text"] == "🔥 Firing: Checkout errors" }
    assert Notification.sole.status_sent?
  end

  # ---------------------------------------------------------------------------
  # Threshold: groups
  # ---------------------------------------------------------------------------

  test "notifying by group, each row is a group keyed by its other columns" do
    monitor = threshold_monitor(notify_by_group: true)
    api = { "service" => "api" }.to_json
    web = { "service" => "web" }.to_json

    answer_table([ [ "api", 20 ], [ "web", 5 ] ], columns: [ SERVICE, COUNT_COLUMN ])
    evaluate(monitor)
    assert_equal [ [ "monitor_firing", api, 20 ] ], notifications
    assert_state monitor, web, "ok", 5

    travel 1.minute
    answer_table([ [ "api", 3 ], [ "web", 30 ] ], columns: [ SERVICE, COUNT_COLUMN ])
    evaluate(monitor)
    assert_equal [ [ "monitor_firing", api, 20 ], [ "monitor_resolved", api, 3 ], [ "monitor_firing", web, 30 ] ],
      notifications
    assert_equal 1, monitor.runs.first.firing
  end

  test "not notifying by group, all rows are one group and the worst number decides" do
    above = threshold_monitor(operator: ">", threshold: 10)
    below = threshold_monitor(operator: "<", threshold: 10)
    answer_table([ [ "api", 20 ], [ "web", 5 ], [ "jobs", nil ] ], columns: [ SERVICE, COUNT_COLUMN ])

    evaluate(above)
    evaluate(below)

    assert_state above, "", "firing", 20
    assert_state below, "", "firing", 5
  end

  test "a Threshold monitor changed to a Match monitor resolves its firing groups and forgets its states" do
    monitor = threshold_monitor(notify_by_group: true)
    answer_table([ [ "api", 12 ], [ "web", 3 ] ], columns: [ SERVICE, COUNT_COLUMN ])
    evaluate(monitor)
    api = { service: "api" }.to_json
    assert_equal [ [ "monitor_firing", api, 12 ] ], notifications

    monitor.update!(kind: "match", query: "['#{@project.slug}']\n| where level == 'error'", operator: nil, threshold: nil)

    assert_empty monitor.states.reload
    assert_equal [ [ "monitor_firing", api, 12 ], [ "monitor_resolved", api, 12 ] ], notifications
    resolved = Notification.order(:id).last
    assert_equal [ ">", 10.0, true ], resolved.payload.values_at("operator", "threshold", "kind_changed")
    assert_equal "Resolved: #{monitor.name} (service=api)", Notifiers::MonitorMessage.build(resolved).title

    travel 1.minute
    answer_matched([], total: 0, ingest_cursor: NEXT_CURSOR)
    evaluate(monitor)
    assert_equal 2, Notification.count, "a Match run sends nothing about the old groups"
  end

  test "a firing group with no row resolves, and an ok group with no row is forgotten" do
    monitor = threshold_monitor(notify_by_group: true)
    api = { "service" => "api" }.to_json
    answer_table([ [ "api", 20 ], [ "web", 5 ] ], columns: [ SERVICE, COUNT_COLUMN ])
    evaluate(monitor)

    travel 1.minute
    answer_table([ [ "jobs", 1 ] ], columns: [ SERVICE, COUNT_COLUMN ])
    evaluate(monitor)

    assert_equal [ [ "monitor_firing", api, 20 ], [ "monitor_resolved", api, nil ] ], notifications
    assert Notification.order(:id).last.payload["absent"]
    assert_equal [ api, { "service" => "jobs" }.to_json ], monitor.states.order(:group_key).pluck(:group_key)
    assert monitor.states.all?(&:status_ok?)
  end

  # ---------------------------------------------------------------------------
  # Threshold: no data
  # ---------------------------------------------------------------------------

  test "no rows are ok and send nothing" do
    monitor = threshold_monitor
    answer_table([])

    evaluate(monitor)

    assert_equal 0, Notification.count
    assert_empty monitor.states
    assert monitor.runs.sole.status_ok?
  end

  test "with alert_on_no_data no rows fire the Monitor's one group, and data coming back resolves it" do
    monitor = threshold_monitor(alert_on_no_data: true, notify_by_group: true)
    answer_table([], columns: [ SERVICE, COUNT_COLUMN ])
    evaluate(monitor)

    assert_equal [ [ "monitor_firing", "", nil ] ], notifications
    assert Notification.sole.payload["no_data"]
    assert_state monitor, "", "firing", nil

    travel 1.minute
    answer_table([ [ "api", 2 ] ], columns: [ SERVICE, COUNT_COLUMN ])
    evaluate(monitor)

    assert_equal [ "monitor_resolved", "", nil ], notifications.last
    assert Notification.order(:id).last.payload["no_data"]
    assert_equal [ [ "", "ok" ], [ { "service" => "api" }.to_json, "ok" ] ],
      monitor.states.order(:group_key).pluck(:group_key, :status)
  end

  # ---------------------------------------------------------------------------
  # Threshold: EventStore down
  # ---------------------------------------------------------------------------

  test "EventStore down, answering 5xx or timing out records last_error, changes no state, and runs again at the next tick" do
    monitor = threshold_monitor(interval_minutes: 10)
    answer_table([ [ 12 ] ])
    evaluate(monitor)
    assert_equal NOW + 10.minutes, monitor.reload.next_run_at

    {
      -> { stub_request(:post, @query_url).to_raise(Errno::ECONNREFUSED) } => /EventStore connection error: .*Connection refused/,
      -> { stub_request(:post, @query_url).to_return(status: 500, body: "Internal error") } => /EventStore error: 500/,
      -> { stub_request(:post, @query_url).to_return(status: 504, body: timeout_body) } => /\Athe run was stopped after 30 s/,
    }.each do |stub, error|
      travel 10.minutes
      stub.call
      evaluate(monitor)

      monitor.reload
      assert_match error, monitor.last_error
      assert_equal Time.current, monitor.last_run_at
      assert_equal Time.current + 1.minute, monitor.next_run_at
      assert_state monitor, "", "firing", 12, since: NOW
      assert_equal 1, Notification.count
      assert monitor.runs.first.status_error?
      assert_match error, monitor.runs.first.error
    end

    travel 1.minute
    answer_table([ [ 15 ] ])
    evaluate(monitor)
    assert_nil monitor.reload.last_error
  end

  test "a Query EventStore refuses, or a table that isn't one number per group, is recorded as an error" do
    monitor = threshold_monitor
    stub_request(:post, @query_url).to_return(status: 400, body: { errors: [ { message: "unknown Project 'shop'" } ] }.to_json)
    evaluate(monitor)
    assert_equal "unknown Project 'shop'", monitor.reload.last_error

    answer_table([ [ 1, 2 ] ], columns: [ COUNT_COLUMN, { "name" => "avg", "type" => "number" } ])
    evaluate(monitor)
    assert_equal "The Query gives 2 numbers per group, not one to compare with the threshold.", monitor.reload.last_error

    stub_request(:post, @query_url).to_return(status: 200, body: { logs: [], next_cursor: nil }.to_json)
    evaluate(monitor)
    assert_match "The Query returns Logs, not a table", monitor.reload.last_error
    assert_equal 0, Notification.count + MonitorState.count
  end

  # ---------------------------------------------------------------------------
  # Match
  # ---------------------------------------------------------------------------

  test "a Match run reads after its cursor, keeps the new cursor and sends one message with the newest ten Logs" do
    monitor = match_monitor
    answer_matched(logs(10), total: 13, ingest_cursor: NEXT_CURSOR)

    evaluate(monitor)

    assert_requested(:post, @query_url) do |request|
      JSON.parse(request.body).values_at("after_batch", "limit", "timeout_seconds") ==
        [ CURSOR, MonitorEvaluation::MATCHED_LOGS, LogMonitor::RUN_TIMEOUT_SECONDS ]
    end
    assert_equal NEXT_CURSOR, monitor.reload.ingest_cursor
    row = Notification.sole
    assert_equal [ "monitor_matched", "" ], [ row.kind, row.group_key ]
    assert_equal 13, row.payload["total"]
    assert_equal 10, row.payload["logs"].size
    assert_equal [ "log-0", "2026-10-17T11:59:30Z", "error", "checkout", "payment declined 0" ],
      row.payload["logs"].first.values_at("id", "time", "level", "service", "message")
    assert_equal [ "2026-10-17T11:59:00Z", "2026-10-17T12:00:00Z" ], row.payload.values_at("from", "to"),
      "the link starts a minute before the cursor was kept, as the Logs it doesn't list are older"
    assert_equal 13, monitor.runs.sole.matched
  end

  test "a Match message links to the explorer over all the Logs it counts, not only the ten it lists" do
    monitor = match_monitor
    travel 15.minutes
    answer_matched(logs(10, newest: Time.current - 2.seconds), total: 200, ingest_cursor: NEXT_CURSOR)

    evaluate(monitor)

    assert_equal [ (NOW - LogMonitor::PROCESSING_LAG).iso8601, Time.current.iso8601 ], matched_range,
      "from a minute before the cursor was kept, when the Monitor was armed"

    first_run = Time.current
    travel 15.minutes
    answer_matched(logs(10, newest: Time.current - 2.seconds), total: 50, ingest_cursor: NEWEST_CURSOR)

    evaluate(monitor)

    assert_equal [ (first_run - LogMonitor::PROCESSING_LAG).iso8601, Time.current.iso8601 ], matched_range,
      "from a minute before the last run kept the cursor"

    travel 15.minutes
    late = logs(1, newest: NOW - 1.hour).map { |log| log.merge("id" => "late") }
    answer_matched(logs(9, newest: Time.current - 2.seconds) + late, total: 9, ingest_cursor: NEWEST_CURSOR)

    evaluate(monitor)

    assert_equal [ (NOW - 1.hour).iso8601, Time.current.iso8601 ], matched_range,
      "from the oldest Log it lists, when that came in late"
  end

  test "a Match run with no Logs moves its cursor and sends nothing" do
    monitor = match_monitor
    answer_matched([], total: 0, ingest_cursor: NEXT_CURSOR)

    evaluate(monitor)

    assert_equal NEXT_CURSOR, monitor.reload.ingest_cursor
    assert_equal 0, Notification.count
    assert_equal 0, monitor.runs.sole.matched
  end

  test "a Match monitor without a cursor arms itself and reads no Logs" do
    monitor = match_monitor
    monitor.update_columns(ingest_cursor: nil)
    answer_matched([], total: 0, ingest_cursor: NEWEST_CURSOR)

    evaluate(monitor)

    assert_requested(:post, @query_url, body: hash_including("after_batch" => ""))
    assert_equal NEWEST_CURSOR, monitor.reload.ingest_cursor
    assert monitor.runs.sole.status_ok?
    assert_not monitor.runs.sole.logs_skipped
  end

  test "a Match monitor EventStore can't arm records why, and shows the error" do
    monitor = match_monitor
    monitor.update_columns(ingest_cursor: nil)

    {
      -> { stub_request(:post, @query_url).to_raise(Errno::ECONNREFUSED) } => /\AEventStore connection error: .*Connection refused/,
      -> { stub_request(:post, @query_url).to_return(status: 500, body: "Internal error") } => /\AEventStore error: 500 - Internal error\z/,
      -> { stub_request(:post, @query_url).to_return(status: 504, body: timeout_body(TIMEOUT)) } => /\A#{Regexp.escape(TIMEOUT)}\z/,
      -> { stub_request(:post, @query_url).to_return(status: 400, body: { error: "unknown Project" }.to_json) } => /\Aunknown Project\z/,
    }.each do |stub, error|
      travel 1.minute
      stub.call
      evaluate(monitor)

      monitor.reload
      assert_nil monitor.ingest_cursor
      assert_match error, monitor.last_error
      assert monitor.runs.first.status_error?
      assert_match error, monitor.runs.first.error
      assert_equal "error", MonitorSerializer.new(monitor).as_json[:status]
    end
  end

  test "a third timeout whose starting again can't reach EventStore says why" do
    monitor = match_monitor
    stub_request(:post, @query_url).with(body: hash_including("after_batch" => CURSOR))
      .to_return(status: 504, body: timeout_body(TIMEOUT))
    stub_request(:post, @query_url).with(body: hash_including("after_batch" => ""))
      .to_raise(Errno::ECONNREFUSED)

    3.times { evaluate(monitor) }

    assert_equal [ CURSOR, 3 ], monitor.reload.values_at(:ingest_cursor, :timeouts_in_a_row)
    assert_match(/\A#{Regexp.escape(TIMEOUT)} Starting again from the newest Logs failed: EventStore connection error: .*Connection refused/,
      monitor.last_error)
  end

  test "EventStore down or refusing a Match run moves no cursor" do
    monitor = match_monitor
    stub_request(:post, @query_url).to_return(status: 502, body: "bad gateway")
    evaluate(monitor)
    stub_request(:post, @query_url).to_return(status: 400, body: { error: "after_batch must be a Log batch id" }.to_json)
    evaluate(monitor)

    monitor.reload
    assert_equal CURSOR, monitor.ingest_cursor
    assert_equal "after_batch must be a Log batch id", monitor.last_error
    assert_equal 0, monitor.timeouts_in_a_row
    assert_equal 0, Notification.count
  end

  test "after three timeouts in a row a Match monitor starts again from the newest Logs, and its next message says Logs were skipped" do
    monitor = match_monitor
    stub_request(:post, @query_url).with(body: hash_including("after_batch" => CURSOR))
      .to_return(status: 504, body: timeout_body(TIMEOUT))
    answer_matched([], total: 0, ingest_cursor: NEWEST_CURSOR, after_batch: "")

    2.times do
      evaluate(monitor)
      travel 1.minute
    end
    monitor.reload
    assert_equal [ CURSOR, 2, TIMEOUT ], [ monitor.ingest_cursor, monitor.timeouts_in_a_row, monitor.last_error ]
    assert_not_requested(:post, @query_url, body: hash_including("after_batch" => ""))

    evaluate(monitor)
    monitor.reload
    skipped_at = Time.current
    assert_equal [ NEWEST_CURSOR, 0, skipped_at ], [ monitor.ingest_cursor, monitor.timeouts_in_a_row, monitor.logs_skipped_at ]
    third = monitor.runs.first
    assert third.logs_skipped
    assert_match TIMEOUT, third.error
    assert_match "skipped those ingested since its last run", third.error
    assert_equal [ false, false, true ], monitor.runs.reorder(:ran_at).pluck(:logs_skipped)

    travel 1.minute
    answer_matched(logs(1, newest: Time.current - 2.seconds), total: 1, ingest_cursor: NEXT_CURSOR,
      after_batch: NEWEST_CURSOR)
    evaluate(monitor)

    assert_equal skipped_at.iso8601, Notification.sole.payload["logs_skipped_at"]
    assert_equal (skipped_at - LogMonitor::PROCESSING_LAG).iso8601, matched_range.first,
      "the link starts from where the Monitor started again"
    assert_nil monitor.reload.logs_skipped_at
    assert_nil monitor.last_error
  end

  test "a Match run that succeeds between timeouts starts the count again" do
    monitor = match_monitor
    stub_request(:post, @query_url).to_return(status: 504, body: timeout_body(TIMEOUT))
    2.times { evaluate(monitor) }
    answer_matched([], total: 0, ingest_cursor: CURSOR)
    evaluate(monitor)
    stub_request(:post, @query_url).to_return(status: 504, body: timeout_body(TIMEOUT))
    2.times { evaluate(monitor) }

    assert_equal 2, monitor.reload.timeouts_in_a_row
    assert_equal CURSOR, monitor.ingest_cursor
    assert_nil monitor.logs_skipped_at
  end

  test "when starting again fails, the next run starts again before reading" do
    monitor = match_monitor
    stub_request(:post, @query_url).to_return(status: 504, body: timeout_body(TIMEOUT))
    3.times { evaluate(monitor) }
    assert_equal [ CURSOR, 3 ], monitor.reload.values_at(:ingest_cursor, :timeouts_in_a_row)
    assert_match "#{TIMEOUT} Starting again from the newest Logs failed: #{TIMEOUT}", monitor.last_error

    answer_matched([], total: 0, ingest_cursor: NEWEST_CURSOR, after_batch: "")
    evaluate(monitor)

    assert_equal [ NEWEST_CURSOR, 0 ], monitor.reload.values_at(:ingest_cursor, :timeouts_in_a_row)
    assert monitor.runs.first.logs_skipped
    assert monitor.runs.first.status_ok?
  end

  private

  def evaluate(monitor)
    MonitorEvaluation.run(monitor.reload, Time.current)
  end

  # A Monitor telling Slack, saved with the requests its Query check made
  # forgotten.
  def threshold_monitor(**attributes)
    create(:monitor, project: @project, operator: ">", threshold: 10, integrations: [ @slack ], **attributes)
      .tap { WebMock::RequestRegistry.instance.reset! }
  end

  def match_monitor
    create(:monitor, :match, project: @project, integrations: [ @slack ]).tap do |monitor|
      assert_equal CURSOR, monitor.ingest_cursor
      WebMock::RequestRegistry.instance.reset!
    end
  end

  def answer_table(rows, columns: [ COUNT_COLUMN ])
    stub_request(:post, @query_url).with(body: hash_including("from", "to"))
      .to_return(status: 200, body: { table: { columns: columns, rows: rows, truncated: false } }.to_json)
  end

  def answer_matched(logs, total:, ingest_cursor:, after_batch: nil)
    body = after_batch ? hash_including("after_batch" => after_batch) : hash_including("after_batch")
    stub_request(:post, @query_url).with(body: body).to_return(status: 200, body: {
      logs: logs, total: total, ingest_cursor: ingest_cursor, next_cursor: nil,
    }.to_json)
  end

  # count Logs a second apart, newest first.
  def logs(count, newest: NOW - 30.seconds)
    count.times.map do |i|
      { "id" => "log-#{i}", "time" => (newest - i).iso8601, "level" => "error", "service" => "checkout",
        "message" => "payment declined #{i}", "attributes" => { "order" => i } }
    end
  end

  def timeout_body(message = "the run was stopped after 30 s, its timeout: try a shorter time range")
    { errors: [ { message: message } ] }.to_json
  end

  # The [from, to) the newest monitor_matched row links to.
  def matched_range
    Notification.where(kind: "monitor_matched").order(:id).last.payload.values_at("from", "to")
  end

  # Each Notification written, oldest first: kind, group and value.
  def notifications
    Notification.order(:id).map { |row| [ row.kind, row.group_key, row.payload["value"] ] }
  end

  def assert_state(monitor, group_key, status, value, since: nil)
    state = monitor.states.find_by!(group_key: group_key)
    assert_equal [ status, value ], [ state.status, state.last_value ]
    assert_equal since, state.since if since
  end
end
