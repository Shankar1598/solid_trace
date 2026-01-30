# frozen_string_literal: true

require "test_helper"
require "minitest/mock"

module Notifiers
  class EmailNotifierTest < ActiveSupport::TestCase
    include ActionMailer::TestHelper

    setup do
      @organization = Organization.create!(name: "Test Org", slug: "test-org")
      @project = @organization.projects.create!(name: "Test Project")
      @issue = @project.issues.create!(title: "Test Issue", kind: "error")

      @integration = @organization.integrations.create!(
        provider: "email",
        name: "Email Alerts",
        settings: { "recipients" => "test@example.com, other@example.com" },
        active: true
      )
    end

    test "delivers email to configured recipients" do
      # assert_enqueued_email_with matches on method and args
      assert_enqueued_with(job: ActionMailer::MailDeliveryJob) do
        Notifiers::EmailNotifier.new(@integration, @issue).call
      end
    end

    test "does nothing if recipients are empty" do
      @integration.settings["recipients"] = ""
      @integration.save!

      assert_no_enqueued_emails do
        Notifiers::EmailNotifier.new(@integration, @issue).call
      end
    end
  end
end
