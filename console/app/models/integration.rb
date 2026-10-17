# frozen_string_literal: true

class Integration < ApplicationRecord
  PROVIDERS = %w[email slack pagerduty].freeze

  # Default notification thresholds
  DEFAULT_EVENT_THRESHOLD = 10
  DEFAULT_TIME_WINDOW_MINUTES = 5

  belongs_to :organization
  has_many :monitor_integrations, dependent: :delete_all

  validates :provider, presence: true, inclusion: { in: PROVIDERS }
  validates :name, presence: true
  validate :webhook_url_is_https, unless: -> { Rails.env.local? }
  before_validation :set_name

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

  # Notification rules that apply when an Integration has no saved setting.
  # The New and Edit pages start from these through rule_settings.
  RULE_DEFAULTS = {
    "notify_on_new_issue" => true,
    "notify_on_assignment" => false,
    "notify_on_event_threshold" => true,
  }.freeze

  # Notification rule settings
  def notify_on_new_issue
    rule_enabled?("notify_on_new_issue")
  end

  def notify_on_event_threshold
    rule_enabled?("notify_on_event_threshold")
  end

  def notify_on_assignment
    rule_enabled?("notify_on_assignment")
  end

  def event_threshold
    val = (settings || {})["event_threshold"].to_i
    val > 0 ? val : DEFAULT_EVENT_THRESHOLD
  end

  def time_window_minutes
    val = (settings || {})["time_window_minutes"].to_i
    val > 0 ? val : DEFAULT_TIME_WINDOW_MINUTES
  end

  # Every notification rule setting as the rules apply it, saved or default.
  def rule_settings
    {
      "notify_on_new_issue" => notify_on_new_issue,
      "notify_on_assignment" => notify_on_assignment,
      "notify_on_event_threshold" => notify_on_event_threshold,
      "event_threshold" => event_threshold,
      "time_window_minutes" => time_window_minutes,
    }
  end

  private

  # Accepts the form's "1"/"0" and JSON true/false alike.
  def rule_enabled?(key)
    val = (settings || {})[key]
    val.nil? ? RULE_DEFAULTS.fetch(key) : ActiveModel::Type::Boolean.new.cast(val)
  end

  def set_name
    self.name = provider.titleize if name.blank?
  end

  # Console POSTs to a webhook URL over https only (Notifiers::HttpDelivery),
  # so a member can't point it at a plain-http host it reaches. Development
  # and test allow http for local stubs.
  def webhook_url_is_https
    return if webhook_url.blank? || https_url?(webhook_url)

    errors.add(:"settings.webhook_url", "must be an https:// URL")
  end

  def https_url?(url)
    uri = URI.parse(url.to_s)
    uri.is_a?(URI::HTTPS) && uri.host.present?
  rescue URI::InvalidURIError
    false
  end
end
