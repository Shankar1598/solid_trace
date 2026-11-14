# frozen_string_literal: true

class Project < ApplicationRecord
  # The DuckDB release's Log file order. Every Project's Logs are sorted by
  # Service and file now, but the column stays so that a rollback to that
  # release still works, until the old Log file path goes (ticket 12 of
  # .scratch/clickhouse).
  self.ignored_columns += [ "log_file_order" ]

  belongs_to :organization
  has_many :issues, dependent: :destroy
  has_many :issue_fingerprints, dependent: :destroy
  has_many :project_keys, dependent: :destroy
  has_many :dashboard_charts, dependent: :destroy
  has_many :monitors, class_name: "LogMonitor", dependent: :destroy
  # Rows EventStore writes for the Project (ADR 0001).
  has_one :issue_counter, class_name: "ProjectIssueCounter", dependent: :delete
  has_one :seen_cursor, class_name: "ProjectSeenCursor", dependent: :delete
  # Rows EventStore reads for the Project (ADR 0001).
  has_many :log_transform_versions, dependent: :delete_all
  # The Agent Project's confirms of the capture notice.
  has_many :capture_confirmations, dependent: :delete_all

  # A captured repository: host/workspace/name, or host/workspace/* for each
  # repository of a workspace. The host is in lower case, as `capture`
  # normalises each remote's host before it matches.
  CAPTURED_REPOSITORY = %r{\A[a-z0-9]([a-z0-9.-]*[a-z0-9])?/[A-Za-z0-9_.-]+/([A-Za-z0-9_.-]+|\*)\z}

  before_validation :generate_slug, on: :create
  normalizes :description, with: ->(description) { description.strip.presence }
  # Each entry without spaces or a .git ending, its host in lower case, and
  # each entry once.
  normalizes :captured_repositories, with: ->(entries) {
    Array(entries).map { |entry| Project.normalise_captured_repository(entry) }.reject(&:empty?).uniq
  }

  validate :captured_repositories_well_formed

  validates :name, presence: true
  validate :slug_reachable, on: :create
  # Whole days; nil means the instance default (InstanceSetting).
  validates :log_retention_days, numericality: { only_integer: true, greater_than: 0 }, allow_nil: true
  # Trace Retention, in whole days; nil means the instance default.
  validates :trace_retention_days, numericality: { only_integer: true, greater_than: 0 }, allow_nil: true
  # What the Project holds, for agents over MCP to pick the Project to query.
  validates :description, length: { maximum: 1000 }

  # A new Project with its first key, so SDKs and shippers can send to it at
  # once, as Console makes one.
  def self.build_with_key(attributes = {})
    new(attributes).tap { |project| project.project_keys.build }
  end

  def self.normalise_captured_repository(entry)
    host, rest = entry.to_s.strip.split("/", 2)
    [ host.to_s.downcase, rest ].compact.join("/").delete_suffix(".git")
  end

  def to_param
    slug
  end

  # Where shippers and curl send this Project's Logs as JSON, on ingest
  # reached at ingest_url (as the Logs snippets build it). The route is
  # key-only: ingest finds the Project from the key it is sent with.
  # logsRoute in app/frontend/lib/logs/getStarted.ts builds the same path.
  def logs_url(ingest_url)
    uri = URI(ingest_url)
    uri.path = "#{uri.path.sub(%r{/+\z}, '')}/api/logs"
    uri.to_s
  end

  def platform
    "ruby"
  end

  # The Project's current Log transform: its newest version, or nil before
  # one is saved.
  def log_transform
    log_transform_versions.newest_first.first
  end

  private

  def generate_slug
    self.slug ||= name.to_s.parameterize
  end

  def captured_repositories_well_formed
    if !agent? && captured_repositories.any?
      errors.add(:captured_repositories, "are a setting of the Agent Project only")
      return
    end

    bad = captured_repositories.reject { |entry| entry.match?(CAPTURED_REPOSITORY) && entry.split("/").none? { |part| part.in?([ ".", ".." ]) } }
    return if bad.empty?

    errors.add(:captured_repositories, "must each be host/workspace/name or host/workspace/*, not #{bad.to_sentence}")
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
