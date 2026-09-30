# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"

class EventStoreMessageProcessorJobTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper

  # Not a StandardError, so the job's rescue doesn't catch it, as with a killed process.
  class SimulatedCrash < Exception; end

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user
    @project = create(:project, organization: @organization)
    @issue = create(:issue, project: @project)
  end

  test "processes issue_created messages and enqueues notification" do
    message = create_message

    assert_enqueued_with(job: IntegrationNotificationJob, args: [ @issue, true ]) do
      EventStoreMessageProcessorJob.perform_now
    end

    message.reload
    assert message.status_processed?
    assert_not_nil message.processed_at
  end

  test "processes issue_received_event messages and enqueues notification" do
    message = create_message(message_type: "issue_received_event")

    assert_enqueued_with(job: IntegrationNotificationJob, args: [ @issue, false ]) do
      EventStoreMessageProcessorJob.perform_now
    end

    message.reload
    assert message.status_processed?
    assert_not_nil message.processed_at
  end

  test "marks message failed when processing raises" do
    message = create_message

    IntegrationNotificationJob.stub :perform_later, ->(*) { raise StandardError, "boom" } do
      EventStoreMessageProcessorJob.perform_now
    end

    message.reload
    assert message.status_failed?
    assert_match "boom", message.error_message
  end

  test "leaves the message status alone while handling it" do
    message = create_message

    seen_status = nil
    IntegrationNotificationJob.stub :perform_later, ->(*) { seen_status = message.reload.status } do
      EventStoreMessageProcessorJob.perform_now
    end

    assert_equal "pending", seen_status
  end

  test "a message interrupted by a crash is handled again on the next run" do
    message = create_message

    IntegrationNotificationJob.stub :perform_later, ->(*) { raise SimulatedCrash } do
      assert_raises(SimulatedCrash) { EventStoreMessageProcessorJob.perform_now }
    end

    message.reload
    assert message.status_pending?
    assert_equal 1, message.attempts

    assert_enqueued_with(job: IntegrationNotificationJob, args: [ @issue, true ]) do
      EventStoreMessageProcessorJob.perform_now
    end

    message.reload
    assert message.status_processed?
    assert_equal 2, message.attempts
  end

  test "a failed message interrupted by a crash stays failed and is retried" do
    message = create_message(status: :failed, attempts: 1)

    IntegrationNotificationJob.stub :perform_later, ->(*) { raise SimulatedCrash } do
      assert_raises(SimulatedCrash) { EventStoreMessageProcessorJob.perform_now }
    end

    message.reload
    assert message.status_failed?
    assert_equal 2, message.attempts

    EventStoreMessageProcessorJob.perform_now

    assert message.reload.status_processed?
  end

  test "does not handle a message that has reached 5 attempts" do
    message = create_message(status: :failed, attempts: 5)

    assert_no_enqueued_jobs(only: IntegrationNotificationJob) do
      EventStoreMessageProcessorJob.perform_now
    end

    message.reload
    assert message.status_failed?
    assert_equal 5, message.attempts
  end

  test "handles a message stuck in processing" do
    message = create_message(status: :processing, attempts: 1)

    assert_enqueued_with(job: IntegrationNotificationJob, args: [ @issue, true ]) do
      EventStoreMessageProcessorJob.perform_now
    end

    assert message.reload.status_processed?
  end

  test "only one run at a time, and a run that can't get the limit is discarded" do
    assert_equal 1, EventStoreMessageProcessorJob.concurrency_limit
    assert_equal :discard, EventStoreMessageProcessorJob.concurrency_on_conflict
    assert EventStoreMessageProcessorJob.new.concurrency_limited?
  end

  private

  def create_message(message_type: "issue_created", status: :pending, attempts: 0)
    EventStoreMessage.create!(
      message_type: message_type,
      payload: { "issue_ids" => [ @issue.id ] },
      status: status,
      attempts: attempts
    )
  end
end
