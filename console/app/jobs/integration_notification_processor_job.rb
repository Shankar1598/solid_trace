# frozen_string_literal: true

class IntegrationNotificationProcessorJob < ApplicationJob
  queue_as :default

  limits_concurrency to: 1,
    key: ->(integration_id, *_args) { integration_id },
    duration: 2.minutes,
    on_conflict: :discard

  def perform(integration_id)
    cutoff_at = Time.current
    # Fetch pending rows first to mark them if integration issues exist
    rows = Notification.pending_for(integration_id, cutoff_at)
    row_ids = rows.pluck(:id)
    return if row_ids.empty?

    integration = Integration.find_by(id: integration_id)
    if integration.nil?
      Notification.mark_rows(row_ids, :sent, "Integration missing")
      return
    end

    unless integration.active
      Notification.mark_rows(row_ids, :sent, "Integration inactive")
      return
    end

    # Rate Limit Check
    last_sent_at = last_notification_sent_at(integration)
    if rate_limited?(last_sent_at)
      # Reschedule for when the window opens
      delay = (last_sent_at + 1.minute) - Time.current
      self.class.set(wait: delay).perform_later(integration_id) if delay > 0
      return
    end

    IntegrationNotifier.deliver_batch(integration, row_ids)

    # Tail Check: Schedule follow-up if more items arrived during processing
    if Notification.where(integration_id: integration_id, status: [ :pending, :failed ]).exists?
      self.class.set(wait: Notification::GROUPING::DEBOUNCE_WINDOW).perform_later(integration_id)
    end
  rescue StandardError => e
    # Optionally handle error
    raise e
  end

  private

  def last_notification_sent_at(integration)
    Notification
      .where(integration_id: integration.id, status: :sent)
      .maximum(:sent_at)
  end

  def rate_limited?(last_sent_at)
    return false unless last_sent_at

    Time.current < last_sent_at + 1.minute
  end
end
