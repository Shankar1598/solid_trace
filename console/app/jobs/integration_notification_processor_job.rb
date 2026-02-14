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
      mark_rows(row_ids, :sent, "Integration missing")
      return
    end

    unless integration.active
      mark_rows(row_ids, :sent, "Integration inactive")
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

    process_notifications(integration, row_ids)

    # Mark as sent (implicit in process_notifications) and no need to update integration

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

  def process_notifications(integration, row_ids)
    Notification.where(id: row_ids).update_all(
      status: Notification.statuses.fetch("processing"),
      processing_at: Time.current,
      updated_at: Time.current
    )

    notifications = Notification.where(id: row_ids).order(:created_at)
    issue_ids = notifications.map { |n| n.payload["issue_id"] }.compact.uniq
    issues = Issue.where(id: issue_ids).includes(:project).index_by(&:id)
    ordered_issues = notifications.map { |n| issues[n.payload["issue_id"]] }.compact

    if ordered_issues.any?
      notifier_class = IntegrationNotifier.notifier_for(integration.provider)
      if notifier_class
        notification_payload = { event: "issue_created_batch", issues: ordered_issues }
        notifier_class.new(integration, ordered_issues.first, notification: notification_payload).call
        mark_rows(row_ids, :sent)
      else
        mark_rows(row_ids, :sent, "Notifier unavailable")
      end
    else
      mark_rows(row_ids, :sent, "Issues missing")
    end
  rescue StandardError => e
    mark_rows(row_ids, :failed, e.message)
    raise e
  end

  def mark_rows(row_ids, status, error_message = nil)
    attrs = {
      status: Notification.statuses.fetch(status.to_s),
      updated_at: Time.current,
    }
    attrs[:sent_at] = Time.current if status.to_s == "sent"
    attrs[:error_message] = error_message if error_message

    Notification.where(id: row_ids).update_all(attrs)
  end
end
