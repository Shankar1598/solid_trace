# frozen_string_literal: true

class Notification < ApplicationRecord
  belongs_to :integration

  serialize :payload, coder: MessagePackCoder

  module GROUPING
    INITIAL_DELAY = 10.seconds
    DEBOUNCE_WINDOW = 1.minute
  end

  enum :status, {
    pending: 0,
    processing: 1,
    sent: 2,
    failed: 3,
  }, prefix: true

  def self.enqueue!(integration:, event_type:, payload: {})
    create!(
      integration: integration,
      event_type: event_type,
      payload: payload,
      status: :pending
    )
  end

  def self.pending_for(integration_id, cutoff_at = Time.current)
    where(integration_id: integration_id, status: [ :pending, :failed ])
      .where("created_at <= ?", cutoff_at)
      .order(:created_at)
  end

  # Bulk-update status for a set of Notification rows.
  # Automatically timestamps +sent_at+ and +processing_at+ when appropriate.
  def self.mark_rows(row_ids, status, error_message = nil)
    attrs = {
      status: statuses.fetch(status.to_s),
      updated_at: Time.current,
    }
    attrs[:sent_at] = Time.current if status.to_s == "sent"
    attrs[:processing_at] = Time.current if status.to_s == "processing"
    attrs[:error_message] = error_message if error_message

    where(id: row_ids).update_all(attrs)
  end
end
