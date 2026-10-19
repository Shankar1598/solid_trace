# frozen_string_literal: true

class Project < ApplicationRecord
  belongs_to :organization
  has_many :issues, dependent: :destroy
  has_many :issue_fingerprints, dependent: :destroy
  has_many :project_keys, dependent: :destroy
  has_many :dashboard_charts, dependent: :destroy
  has_many :monitors, class_name: "LogMonitor", dependent: :destroy
  # Rows EventStore writes for the Project (ADR 0001).
  has_one :issue_counter, class_name: "ProjectIssueCounter", dependent: :delete
  has_one :seen_cursor, class_name: "ProjectSeenCursor", dependent: :delete

  before_validation :generate_slug, on: :create

  validates :name, presence: true
  validate :slug_reachable, on: :create
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
  # reached at ingest_url (as the Logs snippets build it). The route is
  # key-only: ingest finds the Project from the key it is sent with.
  def logs_url(ingest_url)
    uri = URI(ingest_url)
    uri.path = "#{uri.path.sub(%r{/+\z}, '')}/api/logs"
    uri.to_s
  end

  def platform
    "ruby"
  end

  private

  def generate_slug
    self.slug ||= name.to_s.parameterize
  end

  # A Project is found by its slug in its Organization, and its page is
  # /:org_slug/projects/:slug, where new is the New Project page. The slug is
  # made from the name, so the name carries the error. Projects made before
  # this check keep their slugs.
  def slug_reachable
    return if name.blank?

    if slug.blank?
      errors.add(:name, "needs Latin letters or digits, since the Project's slug is made from them")
    elsif slug == "new"
      errors.add(:name, "gives the slug new, which is the New Project page's address")
    elsif Project.exists?(organization_id: organization_id, slug: slug)
      errors.add(:name, "gives the slug #{slug}, which another of this Organization's Projects has")
    end
  end
end
