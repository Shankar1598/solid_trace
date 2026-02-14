# frozen_string_literal: true

require "test_helper"

class IssueMailerTest < ActionMailer::TestCase
  test "notify" do
    organization = Organization.create!(name: "Test Org", slug: "test-org")
    project = organization.projects.create!(name: "Test Project")
    issue = project.issues.create!(title: "Test Issue", kind: "error")
    recipients = [ "test@example.com" ]

    mail = IssueMailer.notify(issue, recipients)

    assert_equal "[SolidTrace] New Issue in Test Project: Test Issue", mail.subject
    assert_equal recipients, mail.to
    assert_match "Test Issue", mail.body.encoded
  end

  test "batch_notify" do
    organization = Organization.create!(name: "Test Org", slug: "test-org")
    project = organization.projects.create!(name: "Test Project")
    issues = [
      project.issues.create!(title: "Issue One", kind: "error"),
      project.issues.create!(title: "Issue Two", kind: "error"),
    ]
    recipients = [ "test@example.com" ]

    mail = IssueMailer.batch_notify(issues, recipients)

    assert_equal "[SolidTrace] 2 New Issues Detected", mail.subject
    assert_equal recipients, mail.to
    assert_match "Issue One", mail.body.encoded
  end
end
