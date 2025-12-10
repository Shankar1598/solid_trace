# frozen_string_literal: true

class Integration < ApplicationRecord
  PROVIDERS = %w[email slack pagerduty].freeze

  # Default notification thresholds
  DEFAULT_EVENT_THRESHOLD = 10
  DEFAULT_TIME_WINDOW_MINUTES = 5

  belongs_to :organization

  validates :provider, presence: true, inclusion: { in: PROVIDERS }
  validates :name, presence: true

  scope :active, -> { where(active: true) }
  scope :by_provider, ->(provider) { where(provider: provider) }

  # Provider-specific settings
  def webhook_url
    (settings || {})["webhook_url"]
  end

  def routing_key
    (settings || {})["routing_key"]
  end

  def severity
    (settings || {})["severity"] || "error"
  end

  def recipients
    val = (settings || {})["recipients"]
    val.to_s.split(",").map(&:strip).reject(&:empty?)
  end

  # Notification rule settings
  def notify_on_new_issue
    val = (settings || {})["notify_on_new_issue"]
    val.nil? ? true : (val.to_s == "1" || val == true)
  end

  def notify_on_event_threshold
    val = (settings || {})["notify_on_event_threshold"]
    val.nil? ? true : (val.to_s == "1" || val == true)
  end

  def event_threshold
    val = (settings || {})["event_threshold"].to_i
    val > 0 ? val : DEFAULT_EVENT_THRESHOLD
  end

  def time_window_minutes
    val = (settings || {})["time_window_minutes"].to_i
    val > 0 ? val : DEFAULT_TIME_WINDOW_MINUTES
  end
end
