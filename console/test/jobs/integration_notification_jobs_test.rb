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

    assert_equal [ [ "issue_created", { "issue_id" => issue.id } ] ], Notification.pluck(:kind, :payload)
  end

  test "the per-Issue trigger job retries with backoff while EventStore is unavailable, then gives up with a logged error" do
    @slack.update!(settings: @slack.settings.merge("notify_on_event_threshold" => "1"))
    issue = create(:issue, project: @project)
    create(:issue_fingerprint, issue: issue)
    stub_request(:get, %r{/api/#{@project.id}/events/count}).to_return(status: 503, body: "down")

    IntegrationNotificationJob.perform_now(issue, false)

    assert_enqueued_jobs 1, only: IntegrationNotificationJob
    assert enqueued_jobs.last[:at].present?, "expected the retry to wait"

    logged = capture_log do
      assert_nothing_raised do
        perform_enqueued_jobs(only: IntegrationNotificationJob) until enqueued_jobs.none? { |job| job[:job] == IntegrationNotificationJob }
      end
    end

    assert_requested(:get, %r{/events/count}, times: IntegrationNotificationJob::EVENT_STORE_ATTEMPTS)
    assert_match "EventStore error: 503", logged
    assert_equal 0, Notification.count
  end

  test "the processor job delivers pending notifications through the module" do
    issue = create(:issue, project: @project)
    Notification.enqueue!(integration: @slack, kind: "issue_created", payload: { "issue_id" => issue.id })

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
