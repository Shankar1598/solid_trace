# frozen_string_literal: true

require "test_helper"

class LogMonitorTest < ActiveSupport::TestCase
  include MonitorTestHelper
  include ActiveSupport::Testing::TimeHelpers

  BATCH = "0199f0a0-0000-7000-8000-000000000001"

  setup do
    @organization = create(:organization, slug: "acme")
    @project = create(:project, organization: @organization, name: "Shop")
    stub_monitor_query_check(@project, ingest_cursor: BATCH)
  end

  # ---------------------------------------------------------------------------
  # Scoping: everything a Monitor names belongs to its Organization
  # ---------------------------------------------------------------------------

  test "a Monitor's Project must belong to its Organization" do
    other = create(:project, organization: create(:organization))
    monitor = build(:monitor, project: other, organization: @organization)

    assert_not monitor.valid?
    assert_includes monitor.errors[:project], "must belong to the Monitor's Organization"
    assert_not_requested(:post, %r{/logs/})
  end

  test "a Monitor's Integrations must belong to its Organization" do
    ours = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/x" })
    theirs = create(:organization).integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/y" })

    monitor = build(:monitor, project: @project, integrations: [ ours, theirs ])
    assert_not monitor.save
    link = monitor.monitor_integrations.find { |each| each.integration == theirs }
    assert_includes link.errors[:integration], "must belong to the Monitor's Organization"
    assert_equal 0, LogMonitor.count

    monitor.integrations = [ ours ]
    assert monitor.save, monitor.errors.full_messages.to_sentence
    assert_equal [ ours ], monitor.reload.integrations
  end

  test "Integrations linked to a saved Monitor, however they are linked, must belong to its Organization" do
    ours = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/x" })
    theirs = create(:organization).integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/y" })
    monitor = create(:monitor, project: @project, integrations: [ ours ])

    assert_raises(ActiveRecord::RecordInvalid) { monitor.integrations = [ ours, theirs ] }
    assert_raises(ActiveRecord::RecordInvalid) { monitor.integrations << theirs }
    assert_raises(ActiveRecord::RecordInvalid) { MonitorIntegration.create!(monitor: monitor, integration: theirs) }
    assert_raises(ActiveRecord::RecordInvalid) { monitor.update!(integration_ids: [ theirs.id ]) }

    assert_equal [ ours ], monitor.reload.integrations
  end

  test "an Organization's and a Project's Monitors are theirs alone" do
    ours = create(:monitor, project: @project)
    other_project = create(:project, organization: create(:organization))
    stub_monitor_query_check(other_project)
    theirs = create(:monitor, project: other_project)

    assert_equal [ ours ], @organization.monitors.to_a
    assert_equal [ ours ], @project.monitors.to_a
    assert_equal [ theirs ], other_project.organization.monitors.to_a
  end

  # ---------------------------------------------------------------------------
  # What each kind needs
  # ---------------------------------------------------------------------------

  test "a Threshold monitor needs an operator and a threshold, and a Match monitor neither" do
    monitor = build(:monitor, project: @project, operator: "=", threshold: nil)
    assert_not monitor.valid?
    assert_includes monitor.errors[:operator], "must be >, >=, < or <="
    assert_includes monitor.errors[:threshold], "is not a number"

    assert build(:monitor, :match, project: @project).valid?
  end

  test "the kind is threshold or match, and the interval and range are whole minutes in bounds" do
    monitor = build(:monitor, project: @project, kind: "anomaly", interval_minutes: 0, range_minutes: 7 * 24 * 60 + 1)

    assert_not monitor.valid?
    assert monitor.errors.key?(:kind)
    assert monitor.errors.key?(:interval_minutes)
    assert monitor.errors.key?(:range_minutes)
  end

  # ---------------------------------------------------------------------------
  # The Query check on save
  # ---------------------------------------------------------------------------

  test "saving a Threshold monitor dry-runs its Query over its range, with the run timeout" do
    travel_to Time.utc(2026, 10, 17, 12, 0, 0) do
      create(:monitor, project: @project, range_minutes: 15)
    end

    assert_requested(:post, "#{EventStore.base_url}/api/#{@project.id}/logs/query") do |request|
      body = JSON.parse(request.body)
      body["from"] == "2026-10-17T11:44:00.000Z" && body["to"] == "2026-10-17T11:59:00.000Z" &&
        body["timeout_seconds"] == LogMonitor::RUN_TIMEOUT_SECONDS
    end
  end

  test "EventStore's mistakes in the Query are errors on the Query, with where each starts" do
    stub_request(:post, %r{/logs/validate}).to_return(status: 200, body: {
      errors: [ { message: "unknown function cuont", line: 3, column: 13 } ], stages: [],
    }.to_json)

    monitor = build(:monitor, project: @project)

    assert_not monitor.valid?
    assert_equal [ "line 3, column 13: unknown function cuont" ], monitor.errors[:query]
  end

  test "a Threshold monitor's Query must end in a summarize" do
    monitor = build(:monitor, project: @project, query: "['shop']\n| summarize count() by service\n| take 5")

    assert_not monitor.valid?
    assert_equal [ MonitorQueryCheck::SUMMARIZE ], monitor.errors[:query]
  end

  test "a Threshold monitor's Query must give exactly one number per group" do
    stub_monitor_query_check(@project, columns: [
      { "name" => "service", "type" => "string" },
      { "name" => "count_", "type" => "number", "agg" => "count" },
      { "name" => "avg_duration", "type" => "number", "agg" => "avg" }
    ])

    monitor = build(:monitor, project: @project)

    assert_not monitor.valid?
    assert_match "gives 2 (count_, avg_duration)", monitor.errors[:query].sole
  end

  test "a Threshold monitor's Query can't group by a time bin" do
    stub_monitor_query_check(@project, columns: [
      { "name" => "time", "type" => "datetime", "bin_seconds" => 300 },
      { "name" => "count_", "type" => "number", "agg" => "count" }
    ])

    monitor = build(:monitor, project: @project)

    assert_not monitor.valid?
    assert_match "can't group by a time bin (time)", monitor.errors[:query].sole
  end

  test "a Threshold monitor's Query can end in a summarize of a peak rate per group" do
    stub_monitor_query_check(@project, columns: [
      { "name" => "shop", "type" => "string" },
      { "name" => "peak_rate_", "type" => "number", "agg" => "peak_rate" }
    ])

    monitor = build(:monitor, project: @project,
      query: "['shop']\n| where path == \"/v2/search\"\n| summarize peak_rate() by shop")

    assert monitor.valid?, monitor.errors.full_messages.to_sentence
  end

  test "a Threshold monitor's check says when EventStore is unavailable or the dry run times out" do
    stub_request(:post, %r{/logs/query}).to_return(status: 502, body: "bad gateway")
    monitor = build(:monitor, project: @project)
    assert_not monitor.valid?
    assert_equal [ "could not be checked, as EventStore is unavailable. Try again shortly." ], monitor.errors[:query]

    stub_request(:post, %r{/logs/query}).to_return(status: 504, body: {
      errors: [ { message: "the run was stopped after 30 s, its timeout: try a shorter time range" } ],
    }.to_json)
    assert_not monitor.valid?
    assert_equal [ "took longer than 30 s to run: the run was stopped after 30 s, its timeout: try a shorter time range" ],
      monitor.errors[:query]
  end

  test "saving a Match monitor arms it at the newest Log batch" do
    monitor = create(:monitor, :match, project: @project)

    assert_equal BATCH, monitor.reload.ingest_cursor
    assert_requested(:post, "#{EventStore.base_url}/api/#{@project.id}/logs/query",
      body: hash_including("after_batch" => "", "timeout_seconds" => LogMonitor::RUN_TIMEOUT_SECONDS))
  end

  test "a Match monitor's Query must return Logs" do
    stub_request(:post, %r{/logs/query}).to_return(status: 400, body: {
      errors: [ { message: "only a Query that returns Logs can read the Logs ingested after a batch, and this one returns a table" } ],
    }.to_json)

    monitor = build(:monitor, :match, project: @project, query: "['shop'] | summarize count()")

    assert_not monitor.valid?
    assert_match "only a Query that returns Logs", monitor.errors[:query].sole
    assert_nil monitor.ingest_cursor
  end

  test "a Monitor's Query can't name a Dashboard filter, whichever its kind, and runs nothing" do
    stub_monitor_query_check(@project, parameters: %w[service q])
    refusal = [ "can't name a filter. Filters ($service, $q) work only on Dashboard Charts" ]

    threshold = build(:monitor, project: @project, query: "['shop']\n| where service == $service\n| search $q\n| summarize count()")
    match = build(:monitor, :match, project: @project, query: "['shop'] | where service == $service | search $q")

    assert_not threshold.valid?
    assert_equal refusal, threshold.errors[:query]
    assert_not match.valid?
    assert_equal refusal, match.errors[:query]
    assert_nil match.ingest_cursor
    assert_not_requested(:post, %r{/logs/query})
  end

  test "renaming a Monitor asks EventStore nothing; changing its Query checks it again" do
    monitor = create(:monitor, project: @project)
    WebMock::RequestRegistry.instance.reset!

    monitor.update!(name: "Renamed", threshold: 20)
    assert_not_requested(:post, %r{/logs/})

    monitor.update!(query: "['shop']\n| summarize count() by service")
    assert_requested(:post, %r{/logs/validate}, times: 1)
  end

  test "a Match monitor keeps its ingest_cursor on an edit and arms again when turned back on" do
    monitor = create(:monitor, :match, project: @project)
    monitor.update_columns(ingest_cursor: "0199f0a0-0000-7000-8000-000000000000")

    monitor.update!(query: "['shop']\n| where level == 'fatal'")
    assert_equal "0199f0a0-0000-7000-8000-000000000000", monitor.reload.ingest_cursor

    monitor.update!(disabled_at: Time.current)
    monitor.update!(disabled_at: nil)
    assert_equal BATCH, monitor.reload.ingest_cursor
  end

  # ---------------------------------------------------------------------------
  # Schedule
  # ---------------------------------------------------------------------------

  test "a Monitor is due at its next run, give or take the tick, and never while turned off" do
    now = Time.utc(2026, 10, 17, 12, 0, 0)
    monitor = create(:monitor, project: @project)
    assert monitor.due?(now), "a new Monitor runs at the next tick"

    monitor.update_columns(next_run_at: now + 20.seconds)
    assert monitor.due?(now)
    monitor.update_columns(next_run_at: now + 40.seconds)
    assert_not monitor.due?(now)

    monitor.update!(interval_minutes: 5)
    assert_nil monitor.next_run_at
    monitor.update!(disabled_at: now)
    assert_not monitor.due?(now)
  end

  # ---------------------------------------------------------------------------
  # Deleting
  # ---------------------------------------------------------------------------

  test "deleting a Monitor deletes its states, runs and links, not its Integrations" do
    integration = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/x" })
    monitor = create(:monitor, project: @project, integrations: [ integration ])
    monitor.states.create!(group_key: "", status: "firing", since: Time.current, last_value: 12)
    monitor.runs.create!(ran_at: Time.current, status: "ok", firing: 1)

    monitor.destroy!

    assert_equal 0, MonitorState.count + MonitorRun.count + MonitorIntegration.count
    assert Integration.exists?(integration.id)
  end

  test "deleting an Integration unlinks it from its Monitors" do
    integration = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/x" })
    monitor = create(:monitor, project: @project, integrations: [ integration ])

    integration.destroy!

    assert_empty monitor.reload.integrations
  end

  test "deleting a Project or an Organization deletes its Monitors" do
    create(:monitor, project: @project)
    other = create(:project, organization: @organization)
    stub_monitor_query_check(other)
    create(:monitor, project: other)

    @project.destroy!
    assert_equal 1, LogMonitor.count

    @organization.destroy!
    assert_equal 0, LogMonitor.count
  end
end
