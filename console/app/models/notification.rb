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

  # Delivery attempts after which a failed row is no longer picked up.
  MAX_ATTEMPTS = 5

  # Rows a tick still has to deliver: pending, or failed below the attempt cap.
  scope :deliverable, -> {
    where(status: :pending).or(where(status: :failed, attempts: ...MAX_ATTEMPTS))
  }

  # Failed rows at the attempt cap: their outcome is final.
  scope :exhausted, -> { where(status: :failed, attempts: MAX_ATTEMPTS..) }

  def self.enqueue!(integration:, kind:, payload: {})
    create!(
      integration: integration,
      kind: kind,
      payload: payload,
      status: :pending
    )
  end

  def self.pending_for(integration_id, cutoff_at = Time.current)
    deliverable.where(integration_id: integration_id)
      .where("created_at <= ?", cutoff_at)
      .order(:created_at)
  end

  # Bulk-update status for a set of Notification rows.
  # Automatically timestamps +sent_at+ and +processing_at+ when appropriate,
  # and replaces any earlier error with +error_message+.
  # Marking rows +processing+ starts a delivery attempt and counts it.
  def self.mark_rows(row_ids, status, error_message = nil)
    attrs = {
      status: statuses.fetch(status.to_s),
      updated_at: Time.current,
    }
    attrs[:sent_at] = Time.current if status.to_s == "sent"
    attrs[:processing_at] = Time.current if status.to_s == "processing"
    attrs[:error_message] = error_message

    rows = where(id: row_ids)
    rows.update_counters(attempts: 1) if status.to_s == "processing"
    rows.update_all(attrs)
  end
end
