# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "webmock/minitest"

# Drives the Integration notification module through its three entry points and
# observes only the outside world: Notification rows, outbound HTTP (WebMock),
# mail handed to ActionMailer and scheduled jobs.
class IntegrationNotificationTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActionMailer::TestHelper
  include ActiveSupport::Testing::TimeHelpers

  SLACK_URL = "https://hooks.slack.com/services/T00/B00/XXX"

  setup do
    @organization = create(:organization, slug: "acme")
    @project = create(:project, organization: @organization, name: "Shop")
    @slack = @organization.integrations.create!(
      provider: "slack",
      settings: { "webhook_url" => SLACK_URL, "notify_on_event_threshold" => "0" },
      active: true
    )
    stub_request(:post, SLACK_URL).to_return(status: 200, body: "ok")
    travel_to Time.zone.parse("2026-10-02 12:00:00")
  end

  teardown do
    travel_back
  end

  # ---------------------------------------------------------------------------
  # Issue received an Event: the new-Issue rule
  # ---------------------------------------------------------------------------

  test "a new Issue writes one issue created row per active Integration and schedules the tick" do
    email = @organization.integrations.create!(
      provider: "email",
      settings: { "recipients" => "ops@example.com", "notify_on_event_threshold" => "0" },
      active: true
    )
    @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => SLACK_URL }, active: false)
    issue = create(:issue, project: @project)

    assert_difference -> { Notification.count }, 2 do
      IntegrationNotification.issue_received_event(issue, newly_created: true)
    end

    rows = Notification.order(:integration_id)
    assert_equal [ @slack.id, email.id ].sort, rows.map(&:integration_id)
    assert rows.all? { |row| row.kind == "issue_created" && row.status_pending? }
    assert rows.all? { |row| row.payload == { "issue_id" => issue.id } }

    [ @slack, email ].each do |integration|
      assert_enqueued_with(
        job: IntegrationNotificationProcessorJob,
        args: [ integration.id ],
        at: Time.current + IntegrationNotification::INITIAL_DELAY
      )
    end
    assert_not_requested(:post, SLACK_URL)
  end

  test "with the new-Issue rule off no issue created row is written" do
    @slack.update!(settings: @slack.settings.merge("notify_on_new_issue" => "0"))
    issue = create(:issue, project: @project)

    assert_no_difference -> { Notification.count } do
      assert_no_enqueued_jobs(only: IntegrationNotificationProcessorJob) do
        IntegrationNotification.issue_received_event(issue, newly_created: true)
      end
    end
  end

  test "an existing Issue receiving an Event writes no issue created row" do
    issue = create(:issue, project: @project)

    assert_no_difference -> { Notification.count } do
      IntegrationNotification.issue_received_event(issue, newly_created: false)
    end
  end

  # ---------------------------------------------------------------------------
  # Deliver pending notifications: batching
  # ---------------------------------------------------------------------------

  test "pending issue created rows are delivered as one batch message" do
    first = create(:issue, project: @project, title: "First failure")
    second = create(:issue, project: @project, title: "Second failure")
    IntegrationNotification.issue_received_event(first, newly_created: true)
    IntegrationNotification.issue_received_event(second, newly_created: true)

    travel IntegrationNotification::INITIAL_DELAY
    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 1) do |request|
      body = JSON.parse(request.body)
      text = body["blocks"].last.dig("text", "text")
      body["text"] == "🚨 2 New Issues Detected" &&
        text.index("First failure") < text.index("Second failure") &&
        text.include?("/acme/projects/#{@project.to_param}/issues/#{first.to_param}")
    end
    assert Notification.all.all?(&:status_sent?)
    assert Notification.all.all? { |row| row.sent_at == Time.current }
  end

  test "a batch lists at most ten Issues plus a count of the rest and a link to all Issues" do
    issues = 12.times.map { |i| create(:issue, project: @project, title: "Failure #{i}") }
    issues.each { |issue| IntegrationNotification.issue_received_event(issue, newly_created: true) }

    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 1) do |request|
      body = JSON.parse(request.body)
      lines = body["blocks"].last.dig("text", "text").split("\n")
      body["text"] == "🚨 12 New Issues Detected" &&
        lines.count { |line| line.start_with?("•") } == 10 &&
        lines.include?("_and 2 more_") &&
        lines.last.include?("/acme/issues|View all issues")
    end
  end

  test "an email Integration hands the batch to the mailer" do
    email = @organization.integrations.create!(
      provider: "email",
      settings: { "recipients" => "ops@example.com", "notify_on_event_threshold" => "0" },
      active: true
    )
    issue = create(:issue, project: @project)
    IntegrationNotification.issue_received_event(issue, newly_created: true)

    assert_enqueued_email_with IssueMailer, :batch_notify, args: [ [ issue ], [ "ops@example.com" ] ] do
      IntegrationNotification.deliver_pending(email.id)
    end
    assert Notification.where(integration: email).all?(&:status_sent?)
  end

  test "a tick with nothing pending sends nothing and schedules nothing" do
    assert_no_enqueued_jobs do
      IntegrationNotification.deliver_pending(@slack.id)
    end
    assert_not_requested(:post, SLACK_URL)
  end

  test "rows written while a tick delivers are left for a follow-up tick one window later" do
    issue = create(:issue, project: @project)
    late_issue = create(:issue, project: @project)
    IntegrationNotification.issue_received_event(issue, newly_created: true)
    stub_request(:post, SLACK_URL).to_return do
      IntegrationNotification.issue_received_event(late_issue, newly_created: true)
      { status: 200, body: "ok" }
    end

    IntegrationNotification.deliver_pending(@slack.id)

    late_row = Notification.all.find { |row| row.payload["issue_id"] == late_issue.id }
    assert late_row.status_pending?
    assert_enqueued_with(
      job: IntegrationNotificationProcessorJob,
      args: [ @slack.id ],
      at: Time.current + IntegrationNotification::RATE_LIMIT_WINDOW
    )
  end

  # ---------------------------------------------------------------------------
  # Deliver pending notifications: rate limit
  # ---------------------------------------------------------------------------

  test "a tick inside the rate-limit window reschedules itself for when the window opens" do
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    IntegrationNotification.deliver_pending(@slack.id)
    delivered_at = Time.current

    travel 30.seconds
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    clear_enqueued_jobs

    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 1)
    assert_equal 1, Notification.status_pending.count
    assert_enqueued_with(
      job: IntegrationNotificationProcessorJob,
      args: [ @slack.id ],
      at: delivered_at + IntegrationNotification::RATE_LIMIT_WINDOW
    )

    travel_to delivered_at + IntegrationNotification::RATE_LIMIT_WINDOW
    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 2)
    assert_equal 0, Notification.status_pending.count
  end

  # ---------------------------------------------------------------------------
  # Deliver pending notifications: Integration gone or paused (today's outcome)
  # ---------------------------------------------------------------------------

  test "rows for an inactive Integration are closed without delivery" do
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    @slack.update!(active: false)

    IntegrationNotification.deliver_pending(@slack.id)

    assert_not_requested(:post, SLACK_URL)
    row = Notification.sole
    assert row.status_sent?
    assert_equal "Integration inactive", row.error_message
  end

  # ---------------------------------------------------------------------------
  # Threshold and assignment: still sent straight from the trigger
  # ---------------------------------------------------------------------------

  test "an Issue reaching the Event threshold is still sent immediately" do
    @slack.update!(settings: @slack.settings.merge("notify_on_event_threshold" => "1", "event_threshold" => "3"))
    issue = create(:issue, project: @project, title: "Spiking")
    create(:issue_fingerprint, issue: issue)
    stub_request(:get, %r{/api/#{@project.id}/events/count})
      .to_return(status: 200, body: { count: 3 }.to_json)

    assert_no_difference -> { Notification.count } do
      IntegrationNotification.issue_received_event(issue, newly_created: false)
    end

    assert_requested(:post, SLACK_URL, times: 1) do |request|
      JSON.parse(request.body)["text"] == "📈 Event threshold reached: Spiking"
    end
  end

  test "a new Issue that also reaches the Event threshold gets only the issue created notification" do
    @slack.update!(settings: @slack.settings.merge("notify_on_event_threshold" => "1", "event_threshold" => "1"))
    issue = create(:issue, project: @project, title: "Brand new")
    create(:issue_fingerprint, issue: issue)
    count_request = stub_request(:get, %r{/api/#{@project.id}/events/count})
      .to_return(status: 200, body: { count: 1 }.to_json)

    IntegrationNotification.issue_received_event(issue, newly_created: true)

    assert_equal [ "issue_created" ], Notification.pluck(:kind)
    assert_not_requested(count_request)
    assert_not_requested(:post, SLACK_URL)
  end

  test "an assignment change is still sent immediately when the assignment rule is on" do
    @slack.update!(settings: @slack.settings.merge("notify_on_assignment" => "1"))
    issue = create(:issue, project: @project, title: "Assignable")
    previous = create(:organization_user, organization: @organization, user: create(:user, name: "Ada"))
    current = create(:organization_user, organization: @organization, user: create(:user, name: "Grace"))

    assert_no_difference -> { Notification.count } do
      IntegrationNotification.issue_assignment_changed(issue, previous_assignee_id: previous.id, new_assignee_id: current.id)
    end

    assert_requested(:post, SLACK_URL, times: 1) do |request|
      fields = JSON.parse(request.body)["blocks"].last["fields"].map { |field| field["text"] }
      fields.include?("*From:*\nAda") && fields.include?("*To:*\nGrace")
    end
  end

  test "an assignment change sends nothing when the assignment rule is off" do
    issue = create(:issue, project: @project)

    IntegrationNotification.issue_assignment_changed(issue, previous_assignee_id: nil, new_assignee_id: nil)

    assert_not_requested(:post, SLACK_URL)
    assert_equal 0, Notification.count
  end
end
