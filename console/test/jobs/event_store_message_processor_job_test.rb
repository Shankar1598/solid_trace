# frozen_string_literal: true

require "test_helper"
require "active_job/test_helper"

class EventStoreMessageProcessorJobTest < ActiveSupport::TestCase
  include ActiveJob::TestHelper

  setup do
    @organization = create(:organization)
    @user = create(:user)
    @organization.users << @user
    @project = create(:project, organization: @organization)
    @issue = create(:issue, project: @project)
  end

  test "processes issue_created messages and enqueues notification" do
    message = EventStoreMessage.create!(
      message_type: "issue_created",
      payload: { "issue_ids" => [ @issue.id ] },
      status: :pending
    )

    assert_enqueued_with(job: IntegrationNotificationJob, args: [ @issue, true ]) do
      EventStoreMessageProcessorJob.perform_now
    end

    message.reload
    assert message.status_processed?
    assert_not_nil message.processed_at
  end

  test "processes issue_received_event messages and enqueues notification" do
    message = EventStoreMessage.create!(
      message_type: "issue_received_event",
      payload: { "issue_ids" => [ @issue.id ] },
      status: :pending
    )

    assert_enqueued_with(job: IntegrationNotificationJob, args: [ @issue, false ]) do
      EventStoreMessageProcessorJob.perform_now
    end

    message.reload
    assert message.status_processed?
    assert_not_nil message.processed_at
  end

  test "marks message failed when processing raises" do
    message = EventStoreMessage.create!(
      message_type: "issue_created",
      payload: { "issue_ids" => [ @issue.id ] },
      status: :pending
    )

    IntegrationNotificationJob.stub :perform_later, ->(*) { raise StandardError, "boom" } do
      EventStoreMessageProcessorJob.perform_now
    end

    message.reload
    assert message.status_failed?
    assert_match "boom", message.error_message
  end
end
