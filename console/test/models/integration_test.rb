# frozen_string_literal: true

require "test_helper"

class IntegrationTest < ActiveSupport::TestCase
  setup do
    @organization = create(:organization)
  end

  test "an Integration with no saved rule settings uses the defaults the New page shows" do
    integration = @organization.integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.com/x" })

    assert integration.notify_on_new_issue
    assert_not integration.notify_on_assignment
    assert integration.notify_on_event_threshold
    assert_equal 10, integration.event_threshold
    assert_equal 5, integration.time_window_minutes
  end

  # Console POSTs to a member's webhook URL, so outside development and
  # test it must be https, never a plain-http host Console reaches.
  test "outside development and test, a webhook URL must be https" do
    build = ->(webhook_url) { @organization.integrations.build(provider: "slack", settings: { "webhook_url" => webhook_url }) }

    Rails.stub(:env, ActiveSupport::EnvironmentInquirer.new("production")) do
      [ "http://127.0.0.1:4100/api/1/logs/query", "ftp://hooks.example.com/x", "https://", "not a url" ].each do |webhook_url|
        integration = build.call(webhook_url)
        assert_not integration.valid?, webhook_url
        assert_equal [ "must be an https:// URL" ], integration.errors[:"settings.webhook_url"]
      end
      assert build.call("https://hooks.slack.com/services/T00/B00/XXX").valid?
    end
    assert build.call("http://127.0.0.1:9999/slack").valid?, "a local stub's URL is allowed in development and test"
  end

  test "saved rule settings are read from form values and JSON booleans alike" do
    on = { "notify_on_new_issue" => "1", "notify_on_assignment" => true, "notify_on_event_threshold" => "true" }
    off = { "notify_on_new_issue" => "0", "notify_on_assignment" => false, "notify_on_event_threshold" => "false" }

    on_integration = @organization.integrations.build(provider: "slack", settings: on)
    off_integration = @organization.integrations.build(provider: "slack", settings: off)

    assert [ on_integration.notify_on_new_issue, on_integration.notify_on_assignment, on_integration.notify_on_event_threshold ].all?
    assert [ off_integration.notify_on_new_issue, off_integration.notify_on_assignment, off_integration.notify_on_event_threshold ].none?
  end

  test "rule_settings resolves every rule setting, saved or default" do
    integration = @organization.integrations.build(provider: "slack", settings: { "event_threshold" => "20" })

    assert_equal(
      {
        "notify_on_new_issue" => true,
        "notify_on_assignment" => false,
        "notify_on_event_threshold" => true,
        "event_threshold" => 20,
        "time_window_minutes" => 5,
      },
      integration.rule_settings
    )
  end
end
