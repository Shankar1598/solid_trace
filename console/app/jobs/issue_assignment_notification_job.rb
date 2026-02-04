# frozen_string_literal: true

class IssueAssignmentNotificationJob < ApplicationJob
  queue_as :default

  retry_on Net::OpenTimeout, Net::ReadTimeout, wait: :exponentially_longer, attempts: 3

  def perform(issue, previous_assignee_id, new_assignee_id)
    IntegrationNotifier.notify_assignment(
      issue,
      previous_assignee_id: previous_assignee_id,
      new_assignee_id: new_assignee_id
    )
  end
end
