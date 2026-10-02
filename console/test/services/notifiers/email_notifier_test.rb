# frozen_string_literal: true

require "test_helper"

module Notifiers
  class EmailNotifierTest < ActiveSupport::TestCase
    include ActionMailer::TestHelper

    RECIPIENTS = [ "test@example.com", "other@example.com" ].freeze

    setup do
      @organization = Organization.create!(name: "Test Org", slug: "test-org")
      @project = @organization.projects.create!(name: "Test Project")
      @issue = @project.issues.create!(title: "Test Issue", kind: "error")

      @integration = @organization.integrations.create!(
        provider: "email",
        name: "Email Alerts",
        settings: { "recipients" => RECIPIENTS.join(", ") },
        active: true
      )
    end

    test "issue_created hands the batch mail to the mailer" do
      other = @project.issues.create!(title: "Other Issue", kind: "error")

      assert_enqueued_email_with IssueMailer, :batch_notify, args: [ [ @issue, other ], RECIPIENTS ] do
        EmailNotifier.new(@integration, kind: "issue_created", issues: [ @issue, other ]).call
      end
    end

    test "threshold_reached hands the single-Issue mail to the mailer" do
      assert_enqueued_email_with IssueMailer, :notify, args: [ @issue, RECIPIENTS ] do
        EmailNotifier.new(@integration, kind: "threshold_reached", issues: [ @issue ]).call
      end
    end

    test "assignment_changed hands the assignment mail to the mailer" do
      assert_enqueued_email_with IssueMailer, :assignment_updated,
        args: [ @issue, RECIPIENTS, { previous_assignee_name: "Alice", new_assignee_name: "Bob" } ] do
        EmailNotifier.new(
          @integration,
          kind: "assignment_changed",
          issues: [ @issue ],
          previous_assignee_name: "Alice",
          new_assignee_name: "Bob"
        ).call
      end
    end

    test "raises NotConfigured and hands nothing to the mailer when recipients are empty" do
      @integration.settings["recipients"] = ""
      @integration.save!

      assert_no_enqueued_emails do
        error = assert_raises(NotConfigured) do
          EmailNotifier.new(@integration, kind: "issue_created", issues: [ @issue ]).call
        end
        assert_equal "No recipients", error.message
      end
    end
  end
end
