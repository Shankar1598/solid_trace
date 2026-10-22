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
  # Rows EventStore reads for the Project (ADR 0001).
  has_many :log_transform_versions, dependent: :delete_all

  before_validation :generate_slug, on: :create

  validates :name, presence: true
  validate :slug_reachable, on: :create
  # Whole days; nil means the instance default (InstanceSetting).
  validates :log_retention_days, numericality: { only_integer: true, greater_than: 0 }, allow_nil: true
  validate :log_file_order_sorts

  # The Log file order of a Project whose log_file_order is nil. EventStore
  # has the same default (storage.DefaultLogFileOrder).
  DEFAULT_LOG_FILE_ORDER = %w[service file].freeze
  MAX_LOG_FILE_ORDER = 3
  # The built-in Fields a Log file order can name. Any other name is an
  # Attribute, as the Query language reads it, unless it's another built-in
  # Field or the time alias _time: time and id always end the order, and
  # message, trace_id and span_id are near unique, so they group nothing.
  LOG_FILE_ORDER_BUILT_INS = %w[service environment level].freeze
  UNSORTED_FIELDS = %w[time _time id message trace_id span_id].freeze
  # Ingest drops an Attribute whose name is longer (maxNameBytes).
  MAX_ATTRIBUTE_NAME_BYTES = 256

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

  # log_file_order is nil for the default, or a JSON array of up to three
  # Field names, which EventStore sorts each hour's Log file by.
  def log_file_order_sorts
    order = log_file_order
    return if order.nil?
    unless order.is_a?(Array) && order.all?(String)
      return errors.add(:log_file_order, "must be a list of Field names")
    end

    if order.size > MAX_LOG_FILE_ORDER
      errors.add(:log_file_order, "can have at most #{MAX_LOG_FILE_ORDER} Fields")
    elsif (field = order.find { |name| UNSORTED_FIELDS.include?(name) })
      errors.add(:log_file_order, "can't name #{field}: only #{LOG_FILE_ORDER_BUILT_INS.join(', ')} and Attributes sort a Log file")
    elsif (name = order.find { |n| !attribute_name?(n) && !LOG_FILE_ORDER_BUILT_INS.include?(n) })
      errors.add(:log_file_order, "names #{name.inspect}, which no Log can have as an Attribute")
    elsif (field = order.detect { |n| order.count(n) > 1 })
      errors.add(:log_file_order, "names #{field} twice")
    end
  end

  # A name a Log can have as an Attribute: ingest keeps names up to
  # MAX_ATTRIBUTE_NAME_BYTES, and EventStore can't make a column of one with
  # a NUL or invalid UTF-8. A Query reads any such name, quoted as ['name'].
  def attribute_name?(name)
    !name.empty? && name.bytesize <= MAX_ATTRIBUTE_NAME_BYTES && name.valid_encoding? && !name.include?("\u0000")
  end

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
