# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "minitest/mock"

class IntegrationNotifierTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActiveSupport::Testing::TimeHelpers

  setup do
    @organization = Organization.create!(name: "Test Org", slug: "test-org")
    @user = User.create!(email: "test@example.com", name: "Test User", password: "password", password_confirmation: "password")
    @organization.users << @user
    @project = @organization.projects.create!(name: "Test Project")
    @integration = @organization.integrations.create!(
      provider: "slack",
      name: "Slack",
      settings: { "webhook_url" => "http://example.com" },
      active: true
    )
    # Ensure defaults logic holds: default is true for both rules
  end

  test "notifies when a new issue is created" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue = @project.issues.create!(title: "New Issue", kind: "error", status: 0)

      assert_difference -> { Notification.count }, 1 do
        assert_enqueued_with(job: IntegrationNotificationProcessorJob, args: [ @integration.id ]) do
          IntegrationNotifier.notify(issue)
        end
      end
    end
  end

  test "does not notify new issue if rule checks unchecked" do
    @integration.settings["notify_on_new_issue"] = "0"
    @integration.save!

    issue = @project.issues.create!(title: "New Issue", kind: "error", status: 0)

    Notifiers::SlackNotifier.stub :new, ->(*args) { raise "Should not be called" } do
      IntegrationNotifier.notify(issue)
    end
    assert true # Verify no exception was raised
  end

  test "notifies when event threshold is reached" do
    @integration.settings["notify_on_new_issue"] = "0"
    @integration.save!

    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)
    issue.reload # Clear dirty tracking so id_previously_changed? returns false

    notifier_instance = Minitest::Mock.new
    notifier_instance.expect :call, nil

    issue.stub :events, (Class.new { def newer_than(_); self; end; def count; 10; end }.new) do
      Notifiers::SlackNotifier.stub :new, ->(*args, **kwargs) { notifier_instance } do
        IntegrationNotifier.notify(issue)
      end
    end

    assert notifier_instance.verify
  end

  test "does not notify threshold if rule unchecked" do
    @integration.settings["notify_on_new_issue"] = "0"
    @integration.settings["notify_on_event_threshold"] = "0"
    @integration.save!

    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)
    issue.reload

    Notifiers::SlackNotifier.stub :new, ->(*args) { raise "Should not be called" } do
      IntegrationNotifier.notify(issue)
    end
    assert true # Verify no exception was raised
  end

  test "does not notify below threshold" do
    @integration.settings["notify_on_new_issue"] = "0"
    @integration.save!

    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)
    issue.reload

    issue.stub :events, (Class.new { def newer_than(_); self; end; def count; 5; end }.new) do
      Notifiers::SlackNotifier.stub :new, ->(*args) { raise "Notification triggered unexpectedly" } do
        IntegrationNotifier.notify(issue)
      end
    end
    assert true # Verify no exception was raised
  end

  test "does not notify above threshold" do
    @integration.settings["notify_on_new_issue"] = "0"
    @integration.save!

    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)
    issue.reload

    issue.stub :events, (Class.new { def newer_than(_); self; end; def count; 11; end }.new) do
      Notifiers::SlackNotifier.stub :new, ->(*args) { raise "Notification triggered unexpectedly" } do
        IntegrationNotifier.notify(issue)
      end
    end
    assert true # Verify no exception was raised
  end

  test "respects configurable threshold" do
    @integration.settings["notify_on_new_issue"] = "0"
    @integration.settings["event_threshold"] = "5"
    @integration.save!

    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)
    issue.reload

    notifier_instance = Minitest::Mock.new
    notifier_instance.expect :call, nil

    issue.stub :events, (Class.new { def newer_than(_); self; end; def count; 5; end }.new) do
      Notifiers::SlackNotifier.stub :new, ->(*args, **kwargs) { notifier_instance } do
        IntegrationNotifier.notify(issue)
      end
    end

    assert notifier_instance.verify
  end

  test "notifies when issue assignment updated if enabled" do
    @integration.settings["notify_on_assignment"] = "1"
    @integration.save!

    issue = @project.issues.create!(title: "Assignable", kind: "error")
    previous_assignee = @organization.organization_users.find_by!(user: @user)
    new_user = User.create!(email: "assignee@example.com", name: "Assignee", password: "password", password_confirmation: "password")
    @organization.users << new_user
    new_assignee = @organization.organization_users.find_by!(user: new_user)

    notifier_instance = Minitest::Mock.new
    notifier_instance.expect :call, nil

    Notifiers::SlackNotifier.stub :new, ->(*args, **kwargs) { notifier_instance } do
      IntegrationNotifier.notify_assignment(issue, previous_assignee_id: previous_assignee.id, new_assignee_id: new_assignee.id)
    end

    assert notifier_instance.verify
  end

  test "does not notify on assignment if disabled" do
    @integration.settings["notify_on_assignment"] = "0"
    @integration.save!

    issue = @project.issues.create!(title: "Assignable", kind: "error")

    Notifiers::SlackNotifier.stub :new, ->(*args, **kwargs) { raise "Notification triggered unexpectedly" } do
      IntegrationNotifier.notify_assignment(issue, previous_assignee_id: nil, new_assignee_id: nil)
    end
    assert true
  end

  # --- deliver_batch tests ---

  test "deliver_batch delivers issues to notifier and marks rows sent" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue = @project.issues.create!(title: "Batch Issue", kind: "error")
      row = Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue.id },
        status: :pending
      )

      notifier_instance = Minitest::Mock.new
      notifier_instance.expect :call, nil

      Notifiers::SlackNotifier.stub :new, ->(integration, first_issue, notification:) {
        assert_equal @integration, integration
        assert_equal issue, first_issue
        assert_equal "issue_created_batch", notification[:event]
        assert_equal [issue], notification[:issues]
        notifier_instance
      } do
        IntegrationNotifier.deliver_batch(@integration, [row.id])
      end

      assert notifier_instance.verify
      assert_equal "sent", row.reload.status
      assert_not_nil row.sent_at
    end
  end

  test "deliver_batch marks rows sent with error when no issues found" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      row = Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => -1 },
        status: :pending
      )

      IntegrationNotifier.deliver_batch(@integration, [row.id])

      row.reload
      assert_equal "sent", row.status
      assert_equal "Issues missing", row.error_message
    end
  end

  test "deliver_batch marks rows failed on notifier error" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue = @project.issues.create!(title: "Failing Issue", kind: "error")
      row = Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue.id },
        status: :pending
      )

      Notifiers::SlackNotifier.stub :new, ->(*args, **kwargs) { raise "Slack API down" } do
        assert_raises(RuntimeError, "Slack API down") do
          IntegrationNotifier.deliver_batch(@integration, [row.id])
        end
      end

      row.reload
      assert_equal "failed", row.status
      assert_equal "Slack API down", row.error_message
    end
  end

  test "deliver_batch handles multiple notification rows" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue1 = @project.issues.create!(title: "Issue 1", kind: "error")
      issue2 = @project.issues.create!(title: "Issue 2", kind: "error")

      row1 = Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue1.id },
        status: :pending,
        created_at: 2.minutes.ago
      )
      row2 = Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue2.id },
        status: :pending,
        created_at: 1.minute.ago
      )

      notifier_instance = Minitest::Mock.new
      notifier_instance.expect :call, nil

      Notifiers::SlackNotifier.stub :new, ->(integration, first_issue, notification:) {
        assert_equal 2, notification[:issues].length
        assert_equal issue1, notification[:issues].first
        assert_equal issue2, notification[:issues].last
        notifier_instance
      } do
        IntegrationNotifier.deliver_batch(@integration, [row1.id, row2.id])
      end

      assert notifier_instance.verify
      assert_equal "sent", row1.reload.status
      assert_equal "sent", row2.reload.status
    end
  end

  test "deliver_batch marks rows processing before delivery" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue = @project.issues.create!(title: "Processing Issue", kind: "error")
      row = Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue.id },
        status: :pending
      )

      observed_status = nil
      Notifiers::SlackNotifier.stub :new, ->(*args, **kwargs) {
        observed_status = row.reload.status
        stub_notifier = Minitest::Mock.new
        stub_notifier.expect :call, nil
        stub_notifier
      } do
        IntegrationNotifier.deliver_batch(@integration, [row.id])
      end

      assert_equal "processing", observed_status
      assert_equal "sent", row.reload.status
    end
  end

  test "deliver_batch marks rows sent with error for unresolvable provider" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue = @project.issues.create!(title: "No Notifier Issue", kind: "error")
      row = Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue.id },
        status: :pending
      )

      IntegrationNotifier.stub :notifier_for, ->(_provider) { nil } do
        IntegrationNotifier.deliver_batch(@integration, [row.id])
      end

      row.reload
      assert_equal "sent", row.status
      assert_equal "Notifier unavailable", row.error_message
    end
  end

  test "notify_if_conditions_met schedules processor job for new issues" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue = @project.issues.create!(title: "New Issue", kind: "error", status: 0)

      assert_enqueued_with(job: IntegrationNotificationProcessorJob, args: [@integration.id]) do
        IntegrationNotifier.notify(issue)
      end
    end
  end
end
