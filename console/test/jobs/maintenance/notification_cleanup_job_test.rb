# frozen_string_literal: true

require "test_helper"

module Maintenance
  class NotificationCleanupJobTest < ActiveSupport::TestCase
    include ActiveSupport::Testing::TimeHelpers

    setup do
      organization = create(:organization)
      @issue = create(:issue, project: create(:project, organization: organization))
      @integration = organization.integrations.create!(
        provider: "slack",
        settings: { "webhook_url" => "https://hooks.slack.com/services/T00/B00/XXX" },
        active: true
      )
      travel_to Time.zone.parse("2026-10-02 12:00:00")
    end

    teardown do
      travel_back
    end

    test "deletes sent and skipped rows older than one day and keeps newer ones" do
      old_sent = row(:sent, age: 25.hours)
      old_skipped = row(:skipped, age: 25.hours)
      new_sent = row(:sent, age: 23.hours)
      new_skipped = row(:skipped, age: 23.hours)

      NotificationCleanupJob.perform_now

      assert_deleted old_sent, old_skipped
      assert_kept new_sent, new_skipped
    end

    test "deletes failed rows at the attempt cap older than seven days and keeps everything else" do
      old_capped = row(:failed, age: 8.days, attempts: Notification::MAX_ATTEMPTS)
      new_capped = row(:failed, age: 6.days, attempts: Notification::MAX_ATTEMPTS)
      old_retryable = row(:failed, age: 8.days, attempts: Notification::MAX_ATTEMPTS - 1)
      old_pending = row(:pending, age: 8.days)
      old_processing = row(:processing, age: 8.days)

      NotificationCleanupJob.perform_now

      assert_deleted old_capped
      assert_kept new_capped, old_retryable, old_pending, old_processing
    end

    private

    def row(status, age:, attempts: 0)
      at = Time.current - age
      Notification.create!(
        integration: @integration,
        issue: @issue,
        kind: "issue_created",
        status: status,
        attempts: attempts,
        sent_at: (at if status == :sent),
        created_at: at,
        updated_at: at
      )
    end

    def assert_deleted(*rows)
      rows.each { |r| assert_not Notification.exists?(r.id), "expected #{r.status} row #{r.id} to be deleted" }
    end

    def assert_kept(*rows)
      rows.each { |r| assert Notification.exists?(r.id), "expected #{r.status} row #{r.id} to be kept" }
    end
  end
end
