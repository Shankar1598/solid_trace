# frozen_string_literal: true

require "test_helper"

class IssueMailerTest < ActionMailer::TestCase
  test "notify" do
    organization = Organization.create!(name: "Test Org", slug: "test-org")
    project = organization.projects.create!(name: "Test Project")
    issue = project.issues.create!(title: "Test Issue", kind: "error")
    recipients = [ "test@example.com" ]

    mail = IssueMailer.notify(issue, recipients)

    assert_equal "[Garnet] New Issue in Test Project: Test Issue", mail.subject
    assert_equal recipients, mail.to
    assert_match "Test Issue", mail.body.encoded
  end
end
