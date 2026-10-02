# frozen_string_literal: true

# Per-Issue trigger: an Issue received an Event. It stays a job because the
# Integration notification rules may call EventStore over HTTP.
class IntegrationNotificationJob < ApplicationJob
  queue_as :default

  EVENT_STORE_ATTEMPTS = 5

  # One trigger at a time per Issue, so two Events arriving together cannot
  # both pass the threshold dedup check and write two threshold reached rows.
  limits_concurrency to: 1, key: ->(issue, *_args) { issue }, duration: 2.minutes

  # The threshold rule needs EventStore's windowed count. While EventStore is
  # unavailable the check is retried with backoff rather than counting zero.
  retry_on EventStore::Unavailable, wait: :polynomially_longer, attempts: EVENT_STORE_ATTEMPTS do |job, error|
    issue = job.arguments.first
    Rails.logger.error("Gave up notifying Integrations about Issue #{issue.try(:id)}: #{error.message}")
  end

  def perform(issue, newly_created)
    IntegrationNotification.issue_received_event(issue, newly_created: newly_created)
  end
end
