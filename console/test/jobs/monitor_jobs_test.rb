# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "webmock/minitest"

# The scheduler enqueues a run of each due, enabled Monitor every minute, and
# the run job evaluates a Monitor only while it is still due.
class MonitorJobsTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActiveSupport::Testing::TimeHelpers
  include MonitorTestHelper

  NOW = Time.utc(2026, 10, 17, 12, 0, 0)

  setup do
    travel_to NOW
    @project = create(:project)
    stub_monitor_query_check(@project)
    @query_url = "#{EventStore.base_url}/api/#{@project.id}/logs/query"
  end

  teardown do
    travel_back
  end

  test "the scheduler runs every minute in production" do
    file = Rails.root.join("config/recurring.yml")
    task = SolidQueue::Configuration.new.send(:config_from, file, env: "production").fetch(:monitor_scheduler)

    assert_equal({ class: "MonitorSchedulerJob", schedule: "every minute" }, task)
  end

  test "the scheduler enqueues each enabled Monitor that is due, give or take the tick" do
    new_monitor = create(:monitor, project: @project)
    due = create(:monitor, project: @project).tap { |m| m.update_columns(next_run_at: NOW - 1.minute) }
    almost_due = create(:monitor, project: @project).tap { |m| m.update_columns(next_run_at: NOW + 20.seconds) }
    create(:monitor, project: @project).tap { |m| m.update_columns(next_run_at: NOW + 4.minutes) }
    create(:monitor, project: @project, disabled_at: NOW)

    MonitorSchedulerJob.perform_now

    enqueued = enqueued_jobs.select { |job| job[:job] == MonitorEvaluationJob }.map { |job| job[:args].first }
    assert_equal [ new_monitor.id, due.id, almost_due.id ].sort, enqueued.sort
  end

  test "the run job evaluates a due Monitor and schedules its next run after its interval" do
    monitor = create(:monitor, project: @project, interval_minutes: 5)
    stub_request(:post, @query_url).to_return(status: 200, body: { table: { columns: [ COUNT_COLUMN ], rows: [ [ 2 ] ] } }.to_json)

    MonitorEvaluationJob.perform_now(monitor.id)

    monitor.reload
    assert_equal [ NOW, NOW + 5.minutes ], [ monitor.last_run_at, monitor.next_run_at ]
    assert_equal 1, monitor.runs.count
  end

  test "the run job skips a Monitor whose run is going, as one started by Run now" do
    monitor = create(:monitor, project: @project)
    monitor.update_columns(run_started_at: NOW - 5.seconds)
    WebMock::RequestRegistry.instance.reset!

    MonitorEvaluationJob.perform_now(monitor.id)

    assert_not_requested(:post, @query_url)
    assert_equal 0, MonitorRun.count
  end

  test "the run job skips a Monitor that is no longer due, turned off or deleted" do
    ran = create(:monitor, project: @project).tap { |m| m.update_columns(next_run_at: NOW + 1.minute) }
    off = create(:monitor, project: @project, disabled_at: NOW)
    WebMock::RequestRegistry.instance.reset!

    [ ran.id, off.id, 0 ].each { |id| MonitorEvaluationJob.perform_now(id) }

    assert_not_requested(:post, @query_url)
    assert_equal 0, MonitorRun.count
  end
end
