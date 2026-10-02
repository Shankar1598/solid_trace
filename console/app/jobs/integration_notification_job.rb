# frozen_string_literal: true

# Per-Issue trigger: an Issue received an Event. It stays a job because the
# Integration notification rules may call EventStore over HTTP.
class IntegrationNotificationJob < ApplicationJob
  queue_as :default

  def perform(issue, newly_created)
    IntegrationNotification.issue_received_event(issue, newly_created: newly_created)
  end
end
