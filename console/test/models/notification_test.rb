# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"

class NotificationTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper
  include ActiveSupport::Testing::TimeHelpers

  setup do
    @organization = Organization.create!(name: "Test Org", slug: "test-org")
    @project = @organization.projects.create!(name: "Test Project")
    @issue = @project.issues.create!(title: "Test Issue", kind: "error")
    @integration = @organization.integrations.create!(
      provider: "email",
      name: "Email",
      settings: { "recipients" => "test@example.com" },
      active: true
    )
  end

  test "enqueue creates queue row without scheduling a job" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      assert_difference -> { Notification.count }, 1 do
        assert_no_enqueued_jobs do
          Notification.enqueue!(
            integration: @integration,
            issue: @issue,
            kind: "issue_created"
          )
        end
      end

      queue_record = Notification.last
      assert_equal "issue_created", queue_record.kind
      assert_equal @issue.id, queue_record.issue_id
      assert_equal "pending", queue_record.status
    end
  end

  test "pending_for returns pending and failed records before cutoff" do
    travel_to Time.zone.parse("2026-02-07 12:00:00") do
      record1 = Notification.create!(
        integration: @integration,
        issue: @issue,
        kind: "issue_created",
        status: :pending,
        created_at: Time.current
      )
      record2 = Notification.create!(
        integration: @integration,
        issue: @issue,
        kind: "issue_created",
        status: :failed,
        created_at: Time.current + 30.seconds
      )
      _record3 = Notification.create!(
        integration: @integration,
        issue: @issue,
        kind: "issue_created",
        status: :pending,
        created_at: Time.current + 90.seconds
      )

      cutoff = Time.current + 1.minute
      results = Notification.pending_for(@integration.id, cutoff)

      assert_equal [ record1.id, record2.id ], results.pluck(:id)
    end
  end
end
