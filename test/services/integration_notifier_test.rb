# frozen_string_literal: true

require "test_helper"
require "minitest/mock"

class IntegrationNotifierTest < ActiveSupport::TestCase
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
    notifier_instance = Minitest::Mock.new
    notifier_instance.expect :call, nil

    Notifiers::SlackNotifier.stub :new, ->(integration, issue) { notifier_instance } do
      issue = @project.issues.create!(title: "New Issue", kind: "error", status: 0)
      IntegrationNotifier.notify(issue)
    end

    assert notifier_instance.verify
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
    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue_fingerprint = issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)

    # Create 10 events (default threshold is 10)
    10.times { |i| e = issue_fingerprint.events.build(environment: "production"); e.build_event_payload(payload: {}); e.save! }

    notifier_instance = Minitest::Mock.new
    notifier_instance.expect :call, nil

    Notifiers::SlackNotifier.stub :new, ->(integration, issue) { notifier_instance } do
      IntegrationNotifier.notify(issue)
    end

    assert notifier_instance.verify
  end

  test "does not notify threshold if rule unchecked" do
    @integration.settings["notify_on_event_threshold"] = "0"
    @integration.save!

    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue_fingerprint = issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)

    # Create 10 events
    10.times { |i| e = issue_fingerprint.events.build(environment: "production"); e.build_event_payload(payload: {}); e.save! }

    Notifiers::SlackNotifier.stub :new, ->(*args) { raise "Should not be called" } do
      IntegrationNotifier.notify(issue)
    end
    assert true # Verify no exception was raised
  end

  test "does not notify below threshold" do
    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue_fingerprint = issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)

    # Create 5 events
    5.times { |i| e = issue_fingerprint.events.build(environment: "production"); e.build_event_payload(payload: {}); e.save! }

    Notifiers::SlackNotifier.stub :new, ->(*args) { raise "Notification triggered unexpectedly" } do
      IntegrationNotifier.notify(issue)
    end
    assert true # Verify no exception was raised
  end

  test "does not notify above threshold" do
    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue_fingerprint = issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)

    # Create 11 events
    11.times { |i| e = issue_fingerprint.events.build(environment: "production"); e.build_event_payload(payload: {}); e.save! }

    Notifiers::SlackNotifier.stub :new, ->(*args) { raise "Notification triggered unexpectedly" } do
      IntegrationNotifier.notify(issue)
    end
    assert true # Verify no exception was raised
  end

  test "respects configurable threshold" do
    @integration.settings["event_threshold"] = "5"
    @integration.save!

    issue = @project.issues.create!(title: "Existing Issue", kind: "error")
    issue_fingerprint = issue.issue_fingerprints.create!(fingerprint: "test-fingerprint", project: @project)

    # Create 5 events
    5.times { |i| e = issue_fingerprint.events.build(environment: "production"); e.build_event_payload(payload: {}); e.save! }

    notifier_instance = Minitest::Mock.new
    notifier_instance.expect :call, nil

    Notifiers::SlackNotifier.stub :new, ->(integration, issue) { notifier_instance } do
      IntegrationNotifier.notify(issue)
    end

    assert notifier_instance.verify
  end
end
