# frozen_string_literal: true

require "test_helper"
require "sentry/test_helper"

# What Console sends its own Sentry (the Meta Project, docs/traces.md): its
# Issues unless a failure would only loop back to the Meta Organization, and
# the Transactions its traces_sampler keeps.
class ConsoleSentryTest < ActiveSupport::TestCase
  include Sentry::TestHelper

  setup do
    ConsoleSentry.reset
    setup_sentry_test
    @meta = create(:organization, name: "SolidTrace")
    InstanceSetting.current.update!(meta_organization: @meta)
    @meta_issue = create(:issue, project: create(:project, organization: @meta))
    @issue = create(:issue, project: create(:project))
  end

  teardown do
    teardown_sentry_test
    ConsoleSentry.reset
  end

  test "a notification job that fails for a Meta Organization Issue sends no Event, as it would loop; for another Issue it does" do
    failing(IntegrationNotification, :issue_received_event) do
      run_failing { IntegrationNotificationJob.perform_now(@meta_issue, true) }
      assert_empty sentry_events

      run_failing { IntegrationNotificationJob.perform_now(@issue, true) }
      assert_equal [ "notification down (RuntimeError)" ], sentry_events.map { |event| event.exception.values.first.value }
    end
  end

  test "the event threshold check and an Integration's delivery tick, failing for the Meta Organization, send no Event" do
    failing(IntegrationNotification, :check_event_threshold) do
      run_failing { EventThresholdJob.perform_now(@meta_issue) }
      assert_empty sentry_events

      run_failing { EventThresholdJob.perform_now(@issue) }
      assert_equal 1, sentry_events.size
    end

    meta_slack = @meta.integrations.create!(provider: "slack", settings: { "webhook_url" => "https://hooks.slack.test/a" })
    slack = @issue.project.organization.integrations.create!(provider: "slack",
      settings: { "webhook_url" => "https://hooks.slack.test/b" })
    failing(IntegrationNotification, :deliver_pending) do
      run_failing { IntegrationNotificationProcessorJob.perform_now(meta_slack.id) }
      assert_equal 1, sentry_events.size

      run_failing { IntegrationNotificationProcessorJob.perform_now(slack.id) }
      assert_equal 2, sentry_events.size
    end
  end

  private

  def failing(receiver, method, &block)
    receiver.stub(method, ->(*, **) { raise "notification down" }, &block)
  end

  def run_failing
    assert_raises(RuntimeError) { yield }
  end
end
