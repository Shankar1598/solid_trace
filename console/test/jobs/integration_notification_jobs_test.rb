# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "webmock/minitest"

# The notification jobs are thin triggers: each only calls the Integration
# notification module. Behaviour is covered by IntegrationNotificationTest.
class IntegrationNotificationJobsTest < ActiveJob::TestCase
  SLACK_URL = "https://hooks.slack.com/services/T00/B00/XXX"

  setup do
    @organization = create(:organization)
    @project = create(:project, organization: @organization)
    @slack = @organization.integrations.create!(
      provider: "slack",
      settings: { "webhook_url" => SLACK_URL, "notify_on_event_threshold" => "0" },
      active: true
    )
    stub_request(:post, SLACK_URL).to_return(status: 200, body: "ok")
  end

  test "the per-Issue trigger job reports the Event to the module" do
    issue = create(:issue, project: @project)

    IntegrationNotificationJob.perform_now(issue, true)

    assert_equal [ [ "issue_created", issue.id ] ], Notification.pluck(:kind, :issue_id)
  end

  test "a new Issue is announced while EventStore can't count Events, and the event threshold check is retried apart" do
    @slack.update!(settings: @slack.settings.merge("notify_on_event_threshold" => "1"))
    issue = create(:issue, project: @project)
    create(:issue_fingerprint, issue: issue)
    stub_request(:get, %r{/api/#{@project.id}/events/count}).to_return(status: 503, body: "down")

    IntegrationNotificationJob.perform_now(issue, true)

    assert_equal [ "issue_created" ], Notification.pluck(:kind)
    assert_enqueued_jobs 1, only: EventThresholdJob

    logged = capture_log do
      assert_nothing_raised do
        perform_enqueued_jobs(only: EventThresholdJob) until enqueued_jobs.none? { |job| job[:job] == EventThresholdJob }
      end
    end

    assert_requested(:get, %r{/events/count}, times: EventThresholdJob::EVENT_STORE_ATTEMPTS)
    assert_match "EventStore error: 503", logged
    assert_equal [ "issue_created" ], Notification.pluck(:kind)
  end

  test "the event threshold check retries with backoff while EventStore is unavailable" do
    @slack.update!(settings: @slack.settings.merge("notify_on_event_threshold" => "1"))
    issue = create(:issue, project: @project)
    create(:issue_fingerprint, issue: issue)
    stub_request(:get, %r{/api/#{@project.id}/events/count}).to_return(status: 503, body: "down")

    EventThresholdJob.perform_now(issue)

    assert_enqueued_jobs 1, only: EventThresholdJob
    assert enqueued_jobs.last[:at].present?, "expected the retry to wait"
    assert_equal 0, Notification.count
  end

  test "the processor job delivers pending notifications through the module" do
    issue = create(:issue, project: @project)
    Notification.enqueue!(integration: @slack, issue: issue, kind: "issue_created")

    IntegrationNotificationProcessorJob.perform_now(@slack.id)

    assert_requested(:post, SLACK_URL, times: 1)
    assert Notification.sole.status_sent?
  end

  private

  def capture_log
    output = StringIO.new
    original = Rails.logger
    Rails.logger = ActiveSupport::Logger.new(output)
    yield
    output.string
  ensure
    Rails.logger = original
  end
end
