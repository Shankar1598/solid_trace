# frozen_string_literal: true

# A Notification is one row in the Integration notification outbox: a message
# of a given +kind+ owed to an Integration, about an Issue or about one group
# of a Monitor (+group_key+, "" for a Monitor of one group), never both. The
# IntegrationNotification module writes, delivers and closes these rows.
class Notification < ApplicationRecord
  belongs_to :integration
  belongs_to :issue, optional: true
  belongs_to :monitor, class_name: "LogMonitor", optional: true

  serialize :payload, coder: MessagePackCoder

  enum :status, {
    pending: 0,
    processing: 1,
    sent: 2,
    failed: 3,
    skipped: 4,
  }, prefix: true

  # Delivery attempts after which a failed Issue row is no longer picked up.
  MAX_ATTEMPTS = 5

  # How long a failed Monitor row is retried, at every tick. A group that
  # stays firing sends nothing more, so a trigger given up after a few
  # minutes' outage would lose the page for the whole incident, and the
  # rest of its group's chain waits behind it.
  MONITOR_RETRY_FOR = 6.hours

  # Rows a tick still has to deliver: pending, or failed and still retried.
  scope :deliverable, -> {
    where(status: :pending)
      .or(where(status: :failed, monitor_id: nil, attempts: ...MAX_ATTEMPTS))
      .or(where(status: :failed, created_at: MONITOR_RETRY_FOR.ago..).where.not(monitor_id: nil))
  }

  # Failed rows no longer retried: their outcome is final.
  scope :exhausted, -> {
    where(status: :failed, monitor_id: nil, attempts: MAX_ATTEMPTS..)
      .or(where(status: :failed, created_at: ...MONITOR_RETRY_FOR.ago).where.not(monitor_id: nil))
  }

  validate :one_subject

  def self.enqueue!(integration:, kind:, issue: nil, monitor: nil, group_key: nil, payload: {})
    create!(
      integration: integration,
      issue: issue,
      monitor: monitor,
      group_key: group_key,
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

  private

  def one_subject
    if issue_id.nil? == monitor_id.nil?
      errors.add(:base, "must be about either an Issue or a Monitor")
    elsif monitor_id && group_key.nil?
      errors.add(:group_key, "must be set for a Monitor")
    elsif issue_id && !group_key.nil?
      errors.add(:group_key, "must be empty for an Issue")
    end
  end
end
