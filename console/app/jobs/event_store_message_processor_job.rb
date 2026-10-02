# frozen_string_literal: true

class EventStoreMessageProcessorJob < ApplicationJob
  queue_as :default

  # Runs every second. A run that overlaps one still going is dropped, so no
  # two runs handle the same message. The lock expires after Solid Queue's
  # default duration, which also bounds the pause after a crashed run.
  limits_concurrency to: 1,
    key: "event_store_message_processor",
    on_conflict: :discard

  def perform
    EventStoreMessage.processable.find_each(batch_size: 100) do |message|
      process_message(message)
    end
  end

  private

  def process_message(message)
    # The status stays as it is while the message is handled, so a crash
    # leaves it processable for the next run.
    message.update_columns(attempts: message.attempts + 1)

    begin
      payload = message.payload
      case message.message_type
      when "issue_created"
        handle_issue_events(payload["issue_ids"], :issue_created)
      when "issue_received_event"
        handle_issue_events(payload["issue_ids"], :issue_received_event)
      end

      message.update_columns(status: :processed, processed_at: Time.current)
    rescue => e
      message.update_columns(status: :failed, error_message: e.message)
      Rails.logger.error("Failed to process event store message #{message.id}: #{e.message}")
    end
  end

  def handle_issue_events(issue_ids, notification_type)
    return if issue_ids.blank?

    issues = Issue.where(id: issue_ids).includes(:project)
    issues.each do |issue|
      # Trigger integration notification
      newly_created = notification_type == :issue_created
      IntegrationNotificationJob.perform_later(issue, newly_created)
    end
  end
end
