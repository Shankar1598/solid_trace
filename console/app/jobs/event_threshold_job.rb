# frozen_string_literal: true

# The event threshold check for an Issue that received an Event. It is a job
# of its own because it reads EventStore's windowed Event count, and a retry
# then repeats only this check, never the new-Issue rows.
class EventThresholdJob < ApplicationJob
  queue_as :default

  EVENT_STORE_ATTEMPTS = 5

  # One check at a time per Issue, so two Events arriving together cannot
  # both pass the dedup check and write two threshold reached rows.
  limits_concurrency to: 1, key: ->(issue, *_args) { issue }, duration: 2.minutes

  # While EventStore is unavailable the check is retried with backoff rather
  # than counting zero. Giving up loses little: the Issue's next Event runs
  # the check again.
  retry_on EventStore::Unavailable, wait: :polynomially_longer, attempts: EVENT_STORE_ATTEMPTS do |job, error|
    issue = job.arguments.first
    Rails.logger.error("Gave up checking the event threshold of Issue #{issue.try(:id)}: #{error.message}")
  end

  def perform(issue)
    IntegrationNotification.check_event_threshold(issue)
  end
end
