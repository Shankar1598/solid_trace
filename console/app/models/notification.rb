# frozen_string_literal: true

# A Notification is one row in the Integration notification outbox: a message
# of a given +kind+ owed to an Integration. It is about an Issue, or about one
# group of a Monitor (+group_key+, "" for a Monitor of one group), never both.
# A Monitor status row (IntegrationNotification::MONITOR_STATUS_KINDS) is
# about the Organization's Monitors, and has neither. The
# IntegrationNotification module writes, delivers and closes these rows.
#
# A row with an Issue is retried MAX_ATTEMPTS times. A row with no Issue is
# a Monitor row for delivery, and is retried for MONITOR_RETRY_FOR. A
# Monitor status row is one of these, with no monitor_id.
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
    # The receiver answered that it will never take the message (the
    # Factory provider's 4xx other than 429), so it is not retried.
    refused: 5,
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
      .or(where(status: :failed, attempts: ...MAX_ATTEMPTS).where.not(issue_id: nil))
      .or(where(status: :failed, issue_id: nil, created_at: MONITOR_RETRY_FOR.ago..))
  }

  # Failed rows no longer retried, and refused rows: their outcome is final.
  scope :exhausted, -> {
    where(status: :failed, attempts: MAX_ATTEMPTS..).where.not(issue_id: nil)
      .or(where(status: :failed, issue_id: nil, created_at: ...MONITOR_RETRY_FOR.ago))
      .or(where(status: :refused))
  }

  # Rows whose outcome is final with nothing sent: skipped, refused, or
  # exhausted.
  scope :undelivered, -> { exhausted.or(where(status: :skipped)) }

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

  # The Integration's last row that failed or was refused, which its page
  # shows with the reason. nil when there is none, or when a later delivery
  # was sent: the Integration then delivers again.
  def self.last_failure(integration)
    row = where(integration: integration, status: [ :failed, :refused ]).order(updated_at: :desc, id: :desc).first
    return if row.nil? || where(integration: integration, status: :sent).where("sent_at > ?", row.updated_at).exists?

    row
  end

  # Whether a later tick delivers this row again.
  def retried?
    self.class.deliverable.exists?(id: id)
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
    if IntegrationNotification::MONITOR_STATUS_KINDS.include?(kind)
      errors.add(:base, "must be about the Organization's Monitors") unless issue_id.nil? && monitor_id.nil? && group_key.nil?
    elsif issue_id.nil? == monitor_id.nil?
      errors.add(:base, "must be about either an Issue or a Monitor")
    elsif monitor_id && group_key.nil?
      errors.add(:group_key, "must be set for a Monitor")
    elsif issue_id && !group_key.nil?
      errors.add(:group_key, "must be empty for an Issue")
    end
  end
end
