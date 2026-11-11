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

  test "Issue links are to the Console URL saved when the email is sent" do
    organization = Organization.create!(name: "Test Org", slug: "test-org")
    project = organization.projects.create!(name: "Test Project")
    issue = project.issues.create!(title: "Test Issue", kind: "error")
    assert_match %r{http://example\.com/test-org/projects/}, IssueMailer.notify(issue, [ "a@example.com" ]).text_part.body.to_s

    InstanceSetting.current.update!(console_url: "https://solid-trace.example.ts.net")

    [ IssueMailer.notify(issue, [ "a@example.com" ]), IssueMailer.batch_notify([ issue ], [ "a@example.com" ]),
      IssueMailer.assignment_updated(issue, [ "a@example.com" ], previous_assignee_name: nil, new_assignee_name: "Ada") ].each do |mail|
      [ mail.text_part, mail.html_part ].each do |part|
        assert_match %r{https://solid-trace\.example\.ts\.net/test-org/projects/}, part.body.to_s, mail.subject
        assert_no_match %r{example\.com/test-org}, part.body.to_s.gsub("solid-trace.example.ts.net", ""), mail.subject
      end
    end
  end
end
