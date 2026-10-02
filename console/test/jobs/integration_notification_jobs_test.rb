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

  test "the processor job delivers pending notifications through the module" do
    issue = create(:issue, project: @project)
    Notification.enqueue!(integration: @slack, kind: "issue_created", payload: { "issue_id" => issue.id })

    IntegrationNotificationProcessorJob.perform_now(@slack.id)

    assert_requested(:post, SLACK_URL, times: 1)
    assert Notification.sole.status_sent?
  end
end
