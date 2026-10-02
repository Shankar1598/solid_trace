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
    assert rows.all? { |row| row.issue_id == issue.id && row.payload == {} }

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

    late_row = Notification.find_by!(issue_id: late_issue.id)
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
  # Deliver pending notifications: failed deliveries and bounded retries
  # ---------------------------------------------------------------------------

  {
    "a Slack 500" => -> { stub_request(:post, SLACK_URL).to_return(status: 500, body: "internal error") },
    "a refused connection" => -> { stub_request(:post, SLACK_URL).to_raise(Errno::ECONNREFUSED) },
    "a timeout" => -> { stub_request(:post, SLACK_URL).to_timeout },
  }.each do |failure, stub_failure|
    test "#{failure} is recorded as failed and retried by later ticks until the attempt cap" do
      instance_exec(&stub_failure)
      IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)

      IntegrationNotification.deliver_pending(@slack.id)

      row = Notification.sole
      assert row.status_failed?
      assert row.error_message.present?
      assert_equal 1, row.attempts
      assert_enqueued_with(
        job: IntegrationNotificationProcessorJob,
        args: [ @slack.id ],
        at: Time.current + IntegrationNotification::RATE_LIMIT_WINDOW
      )

      4.times do
        travel IntegrationNotification::RATE_LIMIT_WINDOW
        IntegrationNotification.deliver_pending(@slack.id)
      end

      assert_requested(:post, SLACK_URL, times: Notification::MAX_ATTEMPTS)
      assert_equal Notification::MAX_ATTEMPTS, row.reload.attempts
      assert row.status_failed?
    end
  end

  test "a failed row at the attempt cap is not picked up again" do
    stub_request(:post, SLACK_URL).to_return(status: 500, body: "internal error")
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    Notification.update_all(status: Notification.statuses[:failed], attempts: Notification::MAX_ATTEMPTS)
    clear_enqueued_jobs

    assert_no_enqueued_jobs do
      IntegrationNotification.deliver_pending(@slack.id)
    end

    assert_not_requested(:post, SLACK_URL)
    assert Notification.sole.status_failed?
    assert_equal Notification::MAX_ATTEMPTS, Notification.sole.attempts
  end

  test "a failed batch message marks every row in the batch failed" do
    stub_request(:post, SLACK_URL).to_return(status: 500, body: "internal error")
    2.times do
      IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    end

    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 1)
    assert Notification.all.all?(&:status_failed?)
    assert_equal [ 1, 1 ], Notification.pluck(:attempts)
  end

  test "a successful retry records the row as sent" do
    stub_request(:post, SLACK_URL).to_return({ status: 500, body: "internal error" }, { status: 200, body: "ok" })
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)

    IntegrationNotification.deliver_pending(@slack.id)
    travel IntegrationNotification::RATE_LIMIT_WINDOW
    IntegrationNotification.deliver_pending(@slack.id)

    row = Notification.sole
    assert row.status_sent?
    assert_equal 2, row.attempts
  end

  # ---------------------------------------------------------------------------
  # Deliver pending notifications: rows that cannot be delivered are skipped
  # ---------------------------------------------------------------------------

  test "rows for an inactive Integration are skipped without delivery and not retried" do
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    @slack.update!(active: false)
    clear_enqueued_jobs

    assert_no_enqueued_jobs do
      IntegrationNotification.deliver_pending(@slack.id)
    end

    assert_not_requested(:post, SLACK_URL)
    assert_skipped Notification.sole, "Integration inactive"
  end

  test "rows for a missing Integration are skipped" do
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    ActiveRecord::Base.connection.disable_referential_integrity { @slack.delete }

    IntegrationNotification.deliver_pending(@slack.id)

    assert_skipped Notification.sole, "Integration missing"
  end

  test "a row whose Issue is missing is skipped while the rest of the batch is sent" do
    kept = create(:issue, project: @project, title: "Kept")
    removed = create(:issue, project: @project, title: "Removed")
    [ kept, removed ].each { |issue| IntegrationNotification.issue_received_event(issue, newly_created: true) }
    removed.destroy!

    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 1) do |request|
      JSON.parse(request.body)["text"] == "🚨 1 New Issues Detected"
    end
    rows = Notification.all.index_by(&:issue_id)
    assert rows[kept.id].status_sent?
    assert_skipped rows[removed.id], "Issue missing"
  end

  {
    "slack" => { "webhook_url" => "" },
    "pagerduty" => { "routing_key" => "" },
    "email" => { "recipients" => "" },
  }.each do |provider, settings|
    test "rows for a #{provider} Integration that is not configured are skipped and not retried" do
      integration = @organization.integrations.create!(
        provider: provider,
        settings: settings.merge("notify_on_event_threshold" => "0"),
        active: true
      )
      IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
      clear_enqueued_jobs

      assert_no_enqueued_jobs do
        assert_no_enqueued_emails do
          IntegrationNotification.deliver_pending(integration.id)
        end
      end

      row = Notification.find_by!(integration: integration)
      assert row.status_skipped?
      assert row.error_message.present?
      assert_not_requested(:post, "https://events.pagerduty.com/v2/enqueue")
    end
  end

  # ---------------------------------------------------------------------------
  # Issue received an Event: the threshold rule
  # ---------------------------------------------------------------------------

  test "the threshold fires when the windowed count lands exactly on the threshold" do
    issue = threshold_issue
    count_request = stub_event_count(3)

    IntegrationNotification.issue_received_event(issue, newly_created: false)

    assert_equal [ [ "threshold_reached", issue.id, {} ] ], Notification.pluck(:kind, :issue_id, :payload)
    assert_requested(count_request, times: 1)
    assert_enqueued_with(
      job: IntegrationNotificationProcessorJob,
      args: [ @slack.id ],
      at: Time.current + IntegrationNotification::INITIAL_DELAY
    )
    assert_not_requested(:post, SLACK_URL)
  end

  test "the threshold fires when a burst overshoots it" do
    issue = threshold_issue
    stub_event_count(7)

    IntegrationNotification.issue_received_event(issue, newly_created: false)

    assert_equal [ "threshold_reached" ], Notification.pluck(:kind)
  end

  test "below the threshold no row is written" do
    issue = threshold_issue
    stub_event_count(2)

    IntegrationNotification.issue_received_event(issue, newly_created: false)

    assert_equal 0, Notification.count
  end

  test "the count request carries the window as newer_than and the Issue's fingerprints" do
    issue = threshold_issue
    fingerprint = issue.issue_fingerprints.sole
    count_request = stub_request(:get, %r{/api/#{@project.id}/events/count})
      .with(query: { "newer_than" => 5.minutes.ago.iso8601, "fingerprint_ids" => fingerprint.id.to_s })
      .to_return(status: 200, body: { count: 3 }.to_json)

    IntegrationNotification.issue_received_event(issue, newly_created: false)

    assert_requested(count_request, times: 1)
  end

  test "a second crossing within the window writes no new row and it fires again after the window" do
    issue = threshold_issue
    stub_event_count(4)

    IntegrationNotification.issue_received_event(issue, newly_created: false)
    travel 4.minutes
    IntegrationNotification.issue_received_event(issue, newly_created: false)

    assert_equal 1, Notification.count

    travel 1.minute + 1.second
    IntegrationNotification.issue_received_event(issue, newly_created: false)

    assert_equal [ "threshold_reached", "threshold_reached" ], Notification.pluck(:kind)
  end

  test "a threshold row for another Issue does not hold back this one" do
    first = threshold_issue
    second = create(:issue, project: @project)
    create(:issue_fingerprint, issue: second)
    stub_event_count(3)

    IntegrationNotification.issue_received_event(first, newly_created: false)
    IntegrationNotification.issue_received_event(second, newly_created: false)

    assert_equal [ first.id, second.id ], Notification.order(:id).pluck(:issue_id)
  end

  test "an Integration with no saved threshold settings uses the default rule: on, 10 Events in 5 minutes" do
    @slack.update!(settings: { "webhook_url" => SLACK_URL })
    issue = create(:issue, project: @project)
    create(:issue_fingerprint, issue: issue)
    count_request = stub_request(:get, %r{/api/#{@project.id}/events/count})
      .with(query: hash_including("newer_than" => 5.minutes.ago.iso8601))
      .to_return(status: 200, body: { count: 10 }.to_json)

    IntegrationNotification.issue_received_event(issue, newly_created: false)

    assert_equal [ "threshold_reached" ], Notification.pluck(:kind)
    assert_requested(count_request, times: 1)
  end

  test "with the threshold rule off no row is written and EventStore is not asked" do
    issue = threshold_issue
    @slack.update!(settings: @slack.settings.merge("notify_on_event_threshold" => "0"))
    count_request = stub_event_count(3)

    IntegrationNotification.issue_received_event(issue, newly_created: false)

    assert_equal 0, Notification.count
    assert_not_requested(count_request)
  end

  test "a new Issue crossing the threshold with both rules on yields both rows" do
    issue = threshold_issue
    stub_event_count(3)

    IntegrationNotification.issue_received_event(issue, newly_created: true)

    assert_equal [ "issue_created", "threshold_reached" ], Notification.order(:id).pluck(:kind)
  end

  test "with EventStore unavailable the check raises and writes nothing, not even the issue created row" do
    issue = threshold_issue
    stub_request(:get, %r{/api/#{@project.id}/events/count}).to_return(status: 503, body: "down")

    assert_raises(EventStore::Unavailable) do
      IntegrationNotification.issue_received_event(issue, newly_created: true)
    end

    assert_equal 0, Notification.count
    assert_no_enqueued_jobs(only: IntegrationNotificationProcessorJob)
  end

  test "with EventStore unreachable the check raises" do
    issue = threshold_issue
    stub_request(:get, %r{/api/#{@project.id}/events/count}).to_raise(Errno::ECONNREFUSED)

    assert_raises(EventStore::Unavailable) do
      IntegrationNotification.issue_received_event(issue, newly_created: false)
    end
  end

  test "threshold rows are delivered one message per row in the same tick as the batch" do
    spiking = threshold_issue(title: "Spiking")
    also_spiking = create(:issue, project: @project, title: "Also spiking")
    create(:issue_fingerprint, issue: also_spiking)
    stub_event_count(3)
    IntegrationNotification.issue_received_event(spiking, newly_created: true)
    IntegrationNotification.issue_received_event(also_spiking, newly_created: false)

    IntegrationNotification.deliver_pending(@slack.id)

    texts = []
    assert_requested(:post, SLACK_URL, times: 3) { |request| texts << JSON.parse(request.body)["text"] }
    assert_equal(
      [ "🚨 1 New Issues Detected", "📈 Event threshold reached: Spiking", "📈 Event threshold reached: Also spiking" ].sort,
      texts.sort
    )
    assert Notification.all.all?(&:status_sent?)

    travel 30.seconds
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 3)
  end

  # ---------------------------------------------------------------------------
  # Issue assignment changed: the assignment rule
  # ---------------------------------------------------------------------------

  test "an assignment change writes one assignment changed row per active Integration and schedules the tick" do
    enable_assignment_rule
    email = @organization.integrations.create!(
      provider: "email",
      settings: { "recipients" => "ops@example.com", "notify_on_assignment" => "1" },
      active: true
    )
    @organization.integrations.create!(
      provider: "slack", settings: { "webhook_url" => SLACK_URL, "notify_on_assignment" => "1" }, active: false
    )
    issue = create(:issue, project: @project)
    previous = member("Ada")
    current = member("Grace")

    IntegrationNotification.issue_assignment_changed(issue, previous_assignee_id: previous.id, new_assignee_id: current.id)

    rows = Notification.order(:integration_id)
    assert_equal [ @slack.id, email.id ].sort, rows.map(&:integration_id)
    assert rows.all? { |row| row.kind == "assignment_changed" && row.status_pending? }
    assert rows.all? { |row| row.issue_id == issue.id }
    assert_equal(
      { "previous_assignee_id" => previous.id, "new_assignee_id" => current.id },
      rows.first.payload
    )
    [ @slack, email ].each do |integration|
      assert_enqueued_with(
        job: IntegrationNotificationProcessorJob,
        args: [ integration.id ],
        at: Time.current + IntegrationNotification::INITIAL_DELAY
      )
    end
    assert_not_requested(:post, SLACK_URL)
  end

  test "with the assignment rule off no row is written" do
    issue = create(:issue, project: @project)

    IntegrationNotification.issue_assignment_changed(issue, previous_assignee_id: nil, new_assignee_id: member("Grace").id)

    assert_equal 0, Notification.count
    assert_no_enqueued_jobs(only: IntegrationNotificationProcessorJob)
  end

  test "changing the assignee on the Issue writes the row through the module" do
    enable_assignment_rule
    issue = create(:issue, project: @project)
    current = member("Grace")

    issue.update!(assignee: current)

    assert_equal(
      [ [ "assignment_changed", issue.id, { "previous_assignee_id" => nil, "new_assignee_id" => current.id } ] ],
      Notification.pluck(:kind, :issue_id, :payload)
    )
  end

  test "an assignment change is delivered with both assignee names" do
    enable_assignment_rule
    issue = create(:issue, project: @project, title: "Assignable")
    IntegrationNotification.issue_assignment_changed(issue, previous_assignee_id: member("Ada").id, new_assignee_id: member("Grace").id)

    IntegrationNotification.deliver_pending(@slack.id)

    assert_assignment_message "Ada", "Grace"
    assert Notification.sole.status_sent?
  end

  test "an archived previous assignee is named in the delivered message" do
    enable_assignment_rule
    issue = create(:issue, project: @project)
    previous = member("Ada")
    IntegrationNotification.issue_assignment_changed(issue, previous_assignee_id: previous.id, new_assignee_id: member("Grace").id)
    previous.discard!

    IntegrationNotification.deliver_pending(@slack.id)

    assert_assignment_message "Ada", "Grace"
  end

  test "unassigning an Issue is delivered as To: Unassigned" do
    enable_assignment_rule
    issue = create(:issue, project: @project)
    IntegrationNotification.issue_assignment_changed(issue, previous_assignee_id: member("Ada").id, new_assignee_id: nil)

    IntegrationNotification.deliver_pending(@slack.id)

    assert_assignment_message "Ada", "Unassigned"
  end

  test "assignment rows are delivered one message per row and count towards the shared rate limit" do
    enable_assignment_rule
    first = create(:issue, project: @project, title: "First")
    second = create(:issue, project: @project, title: "Second")
    IntegrationNotification.issue_received_event(create(:issue, project: @project), newly_created: true)
    [ first, second ].each do |issue|
      IntegrationNotification.issue_assignment_changed(issue, previous_assignee_id: nil, new_assignee_id: member("Grace #{issue.id}").id)
    end

    IntegrationNotification.deliver_pending(@slack.id)

    texts = []
    assert_requested(:post, SLACK_URL, times: 3) { |request| texts << JSON.parse(request.body)["text"] }
    assert_equal 2, texts.count { |text| text.start_with?("👤 Issue assignment updated") }

    travel 30.seconds
    IntegrationNotification.issue_assignment_changed(first, previous_assignee_id: nil, new_assignee_id: nil)
    clear_enqueued_jobs
    IntegrationNotification.deliver_pending(@slack.id)

    assert_requested(:post, SLACK_URL, times: 3)
    assert_equal 1, Notification.status_pending.count
    assert_enqueued_with(job: IntegrationNotificationProcessorJob, args: [ @slack.id ], at: Time.current + 30.seconds)
  end

  private

  # An existing Issue with a fingerprint, and the Slack threshold rule on at
  # three Events in five minutes.
  def threshold_issue(title: "Spiking")
    @slack.update!(settings: @slack.settings.merge(
      "notify_on_event_threshold" => "1", "event_threshold" => "3", "time_window_minutes" => "5"
    ))
    create(:issue, project: @project, title: title).tap { |issue| create(:issue_fingerprint, issue: issue) }
  end

  def stub_event_count(count)
    stub_request(:get, %r{/api/#{@project.id}/events/count}).to_return(status: 200, body: { count: count }.to_json)
  end

  def enable_assignment_rule
    @slack.update!(settings: @slack.settings.merge("notify_on_assignment" => "1"))
  end

  def member(name)
    create(:organization_user, organization: @organization, user: create(:user, name: name))
  end

  def assert_assignment_message(from, to)
    assert_requested(:post, SLACK_URL, times: 1) do |request|
      fields = JSON.parse(request.body)["blocks"].last["fields"].map { |field| field["text"] }
      fields.include?("*From:*\n#{from}") && fields.include?("*To:*\n#{to}")
    end
  end

  def assert_skipped(row, reason)
    row.reload
    assert row.status_skipped?, "expected skipped, was #{row.status}"
    assert_equal reason, row.error_message
    assert_nil row.sent_at
  end
end
