# frozen_string_literal: true

class Project < ApplicationRecord
  belongs_to :organization
  has_many :issues, dependent: :destroy
  has_many :issue_fingerprints, dependent: :destroy
  has_many :project_keys, dependent: :destroy
  has_many :dashboard_charts, dependent: :destroy
  has_many :monitors, class_name: "LogMonitor", dependent: :destroy

  before_validation :generate_slug, on: :create

  # Whole days; nil means the instance default (InstanceSetting).
  validates :log_retention_days, numericality: { only_integer: true, greater_than: 0 }, allow_nil: true

  # A new Project with its first key, so SDKs and shippers can send to it at
  # once, as Console makes one.
  def self.build_with_key(attributes = {})
    new(attributes).tap { |project| project.project_keys.build }
  end

  def to_param
    slug
  end

  # Where shippers and curl send this Project's Logs as JSON, on ingest
  # reached at ingest_url (as the Logs snippets build it).
  def logs_url(ingest_url)
    uri = URI(ingest_url)
    uri.path = "#{uri.path.sub(%r{/+\z}, '')}/api/#{id}/logs"
    uri.to_s
  end

  def platform
    "ruby"
  end

  private

  def generate_slug
    self.slug ||= name.parameterize
  end
end
