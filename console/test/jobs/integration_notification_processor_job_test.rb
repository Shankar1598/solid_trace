# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"
require "minitest/mock"

class IntegrationNotificationProcessorJobTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActiveSupport::Testing::TimeHelpers

  setup do
    @organization = Organization.create!(name: "Test Org", slug: "test-org")
    @project = @organization.projects.create!(name: "Test Project")
    @integration = @organization.integrations.create!(
      provider: "email",
      name: "Email",
      settings: { "recipients" => "test@example.com" },
      active: true
    )
  end

  test "processes all pending rows" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue = @project.issues.create!(title: "Issue", kind: "error")

      Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue.id },
        status: :pending
      )

      notifier_instance = Minitest::Mock.new
      notifier_instance.expect :call, nil

      Notifiers::EmailNotifier.stub :new, ->(integration, issue, notification:) {
        notifier_instance
      } do
        IntegrationNotificationProcessorJob.perform_now(@integration.id)
      end

      assert notifier_instance.verify
      assert_equal "sent", Notification.last.status

      # Verifies a sent record exists at current time
      assert Notification.where(integration: @integration, status: :sent, sent_at: Time.current).exists?
    end
  end

  test "reschedules if rate limited" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      # Simulate recent send 30 seconds ago via a sent record
      Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: {},
        status: :sent,
        sent_at: 30.seconds.ago,
        created_at: 30.seconds.ago
      )

      issue = @project.issues.create!(title: "Issue", kind: "error")
      Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue.id },
        status: :pending
      )

      assert_enqueued_with(job: IntegrationNotificationProcessorJob, args: [ @integration.id ]) do
        IntegrationNotificationProcessorJob.perform_now(@integration.id)
      end

      # Should NOT have processed the pending one
      pending_record = Notification.where(status: :pending).last
      assert_equal "pending", pending_record.status
    end
  end

  test "processes if rate limit expired" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      # Simulate send 61 seconds ago
      Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: {},
        status: :sent,
        sent_at: 61.seconds.ago,
        created_at: 61.seconds.ago
      )

      issue = @project.issues.create!(title: "Issue", kind: "error")
      Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue.id },
        status: :pending
      )

      notifier_instance = Minitest::Mock.new
      notifier_instance.expect :call, nil

      Notifiers::EmailNotifier.stub :new, ->(*args) { notifier_instance } do
        IntegrationNotificationProcessorJob.perform_now(@integration.id)
      end

      assert notifier_instance.verify
      assert_equal "sent", Notification.last.status
      assert Notification.where(integration: @integration, status: :sent, sent_at: Time.current).exists?
    end
  end

  test "schedules tail job if new items arrive during processing" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      issue = @project.issues.create!(title: "Issue", kind: "error")

      # Item 1: Already pending
      item1 = Notification.create!(
        integration: @integration,
        event_type: "issue_created",
        payload: { "issue_id" => issue.id },
        status: :pending
      )

      notifier_instance = Minitest::Mock.new
      notifier_instance.expect :call, nil

      # Mock notifier to simulate new item arriving DURING processing
      Notifiers::EmailNotifier.stub :new, ->(*args) {
        # Create Item 2 while processing Item 1
        Notification.create!(
          integration: @integration, # Same integration
          event_type: "issue_created",
          payload: { "issue_id" => issue.id }, # Same issue for simplicity
          status: :pending
        )
        notifier_instance
      } do
        assert_enqueued_with(job: IntegrationNotificationProcessorJob, args: [ @integration.id ]) do
          IntegrationNotificationProcessorJob.perform_now(@integration.id)
        end
      end

      assert notifier_instance.verify

      # Item 1 should be sent
      assert_equal "sent", item1.reload.status

      # Item 2 should be pending (will be picked up by tail job)
      item2 = Notification.last
      assert_equal "pending", item2.status
      assert_not_equal item1.id, item2.id
    end
  end

  test "marks rows as sent when integration is inactive" do
    @integration.update!(active: false)

    issue = @project.issues.create!(title: "Test Issue", kind: "error")
    Notification.create!(
      integration: @integration,
      event_type: "issue_created",
      payload: { "issue_id" => issue.id },
      status: :pending
    )

    IntegrationNotificationProcessorJob.perform_now(@integration.id)

    queue_record = Notification.last
    assert_equal "sent", queue_record.status
    assert_equal "Integration inactive", queue_record.error_message
  end
end
