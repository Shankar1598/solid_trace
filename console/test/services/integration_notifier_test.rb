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
end
