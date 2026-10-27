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

  test "the traces sampler never keeps the every-second message job or a health check, and otherwise uses the rate" do
    tracing_at(0.05)

    assert_not transaction("EventStoreMessageProcessorJob", op: "queue.active_job").sampled
    assert_not transaction("/up", op: "http.server", env: Rack::MockRequest.env_for("/up")).sampled
    assert_equal 0.05, transaction("LogsController#index", op: "http.server",
      env: Rack::MockRequest.env_for("/acme/logs")).effective_sample_rate
    assert_equal 0.05, transaction("MonitorSchedulerJob", op: "queue.active_job").effective_sample_rate
  end

  test "the traces sampler follows the decision of a trace sampled upstream" do
    tracing_at(0.05)

    assert transaction("LogsController#index", op: "http.server", parent_sampled: true).sampled
    tracing_at(1.0)
    assert_not transaction("LogsController#index", op: "http.server", parent_sampled: false).sampled
  end

  private

  # Sentry configured as config/initializers/sentry.rb does with
  # SENTRY_TRACES_SAMPLE_RATE=rate.
  def tracing_at(rate)
    Sentry.configuration.traces_sample_rate = rate
    Sentry.configuration.traces_sampler = ->(context) { ConsoleSentry.traces_sample_rate(context, rate) }
  end

  def transaction(name, op:, env: nil, **options)
    Sentry.start_transaction(name: name, op: op, custom_sampling_context: env ? { env: env } : {}, **options)
  end

  def failing(receiver, method, &block)
    receiver.stub(method, ->(*, **) { raise "notification down" }, &block)
  end

  def run_failing
    assert_raises(RuntimeError) { yield }
  end
end
