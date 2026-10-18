# frozen_string_literal: true

# Per-Issue trigger: an Issue received an Event. It writes the rows that need
# nothing from EventStore, then hands the event threshold check to its own
# job, so EventStore being unable to count Events never holds back a
# new-Issue message.
class IntegrationNotificationJob < ApplicationJob
  queue_as :default

  def perform(issue, newly_created)
    IntegrationNotification.issue_received_event(issue, newly_created: newly_created)
    EventThresholdJob.perform_later(issue)
  end
end
