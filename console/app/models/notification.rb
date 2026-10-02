# frozen_string_literal: true

# A Notification is one row in the Integration notification outbox: a message
# of a given +kind+ owed to an Integration. The IntegrationNotification module
# writes, delivers and closes these rows.
class Notification < ApplicationRecord
  belongs_to :integration

  serialize :payload, coder: MessagePackCoder

  enum :status, {
    pending: 0,
    processing: 1,
    sent: 2,
    failed: 3,
    skipped: 4,
  }, prefix: true

  def self.enqueue!(integration:, kind:, payload: {})
    create!(
      integration: integration,
      kind: kind,
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
