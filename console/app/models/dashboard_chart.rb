# frozen_string_literal: true

# One chart of a Dashboard: a saved Query, or a SQL query, on a Project of
# the Dashboard's Organization, drawn as a line, area, bar, stat or table, at
# a place on the Dashboard's grid of COLUMNS columns. language says which; the
# text is in query either way, as a Monitor keeps it. SQL names a Dashboard
# filter as a ClickHouse query parameter, {region:Array(String)} (ADR 0025).
# A chart in a Section has its place on that Section's grid; a chart in no
# Section, on the grid above the Sections.
# A chart of viz text is a Text block: Markdown in text, with no Project, no
# Query, no language and an optional title. It runs nothing.
class DashboardChart < ApplicationRecord
  VIZES = %w[line area bar stat table text].freeze
  MAX_TEXT = 10_000
  COLUMNS = 12
  MAX_HEIGHT = 24
  MAX_DECIMALS = 6
  MAX_THRESHOLDS = 5
  MAX_THRESHOLD_LABEL = 40
  SEVERITIES = %w[warn critical].freeze
  # The vizes that draw a time series over a value axis, where a threshold
  # line can go.
  THRESHOLD_VIZES = %w[line area].freeze

  enum :language, { query: "query", sql: "sql" }, prefix: true, validate: { allow_nil: true }

  belongs_to :dashboard, touch: true
  belongs_to :project, optional: true
  belongs_to :section, class_name: "DashboardSection", optional: true, inverse_of: :charts

  before_validation :normalize_options
  # A Text block has no language; a Chart given none runs a Query.
  before_validation { self.language = text? ? nil : language || "query" }

  validates :title, presence: true, unless: :text?
  validates :title, length: { maximum: 200 }
  validates :project, presence: { message: "must exist" }, unless: :text?
  validates :query, presence: true, length: { maximum: 20_000 }, unless: :text?
  validates :text, presence: true, length: { maximum: MAX_TEXT }, if: :text?
  validates :viz, inclusion: { in: VIZES, message: "must be line, area, bar, stat, table or text" }
  validate :text_block_fields
  validate :viz_kind_kept, on: :update
  validates :x, :y, numericality: { only_integer: true, greater_than_or_equal_to: 0 }
  validates :w, numericality: { only_integer: true, in: 1..COLUMNS }
  validates :h, numericality: { only_integer: true, in: 1..MAX_HEIGHT }
  validate :fits_the_grid
  validate :project_of_the_dashboards_organization
  validate :section_of_the_dashboard, if: :section_id_changed?
  validate :options_known
  validate :sql_queries_on, if: :language_sql?

  # Whether the chart is a Text block.
  def text?
    viz == "text"
  end

  private

  # A Text block has no Project, Query or options; a chart that runs a Query
  # has no text.
  def text_block_fields
    unless text?
      errors.add(:text, "is only for a Text block") unless text.nil?
      return
    end

    errors.add(:project, "must be empty for a Text block") unless project.nil?
    errors.add(:query, "must be empty for a Text block") unless query.nil?
    errors.add(:options, "are not for a Text block") if options.present?
  end

  # A chart does not change into a Text block, nor a Text block into a
  # chart: the two keep different fields.
  def viz_kind_kept
    return unless viz_changed? && (viz == "text") != (viz_was == "text")

    errors.add(:viz, "can not change between text and a Chart's viz: delete it and add a new one")
  end

  # A SQL Chart is saved only while SQL queries are on, when it is new or
  # its SQL or language changes. While they are off, a saved one keeps its
  # SQL, and its place and size can change.
  def sql_queries_on
    return unless new_record? || will_save_change_to_query? || will_save_change_to_language?
    return if InstanceSetting.current.sql_queries_enabled?

    errors.add(:base, LogMonitor::SQL_QUERIES_OFF)
  end

  # options holds stacked (true or false), unit (a short label such as ms),
  # decimals (0 to MAX_DECIMALS) and thresholds, each only when set; other
  # keys go.
  def normalize_options
    given = options.is_a?(Hash) ? options.stringify_keys : {}
    stacked, unit, @decimals_given, thresholds = given.values_at("stacked", "unit", "decimals", "thresholds")
    self.options = {
      "stacked" => stacked.to_s == "" ? nil : ActiveModel::Type::Boolean.new.cast(stacked),
      "unit" => unit.to_s.strip.presence,
      "decimals" => @decimals_given.to_s == "" ? nil : Integer(@decimals_given.to_s, 10, exception: false),
      "thresholds" => normalize_thresholds(thresholds),
    }.compact
  end

  # Each threshold line is {value, label, severity}, label and severity only
  # when given, in the order given; an empty list is none. A value written
  # as text, as a form sends it, is read as a number. What isn't a list or
  # a threshold, and a value that isn't a number, are kept for
  # thresholds_known to refuse.
  def normalize_thresholds(thresholds)
    return nil if thresholds.blank?
    return thresholds unless thresholds.is_a?(Array)

    thresholds.map do |threshold|
      next threshold unless threshold.is_a?(Hash)

      given = threshold.stringify_keys
      value = given["value"]
      value = Float(value.strip, exception: false) || value if value.is_a?(String)
      { "value" => value, "label" => given["label"].to_s.strip.presence, "severity" => given["severity"].to_s.strip.presence }.compact
    end
  end

  def options_known
    errors.add(:options, "unit must be at most 16 characters") if options["unit"].to_s.length > 16
    thresholds_known(options["thresholds"]) if options.key?("thresholds")
    return if @decimals_given.to_s == "" || (0..MAX_DECIMALS).cover?(options["decimals"])

    errors.add(:options, "decimals must be a whole number from 0 to #{MAX_DECIMALS}")
  end

  # Errors name a threshold by its place in the list, from 0, as
  # thresholds[1].value.
  def thresholds_known(thresholds)
    return errors.add(:options, "thresholds must be a list") unless thresholds.is_a?(Array)
    return errors.add(:options, "thresholds are for line and area charts") unless THRESHOLD_VIZES.include?(viz)
    return errors.add(:options, "thresholds are at most #{MAX_THRESHOLDS}") if thresholds.size > MAX_THRESHOLDS

    thresholds.each_with_index do |threshold, i|
      at = "thresholds[#{i}]"
      next errors.add(:options, "#{at} must be an object") unless threshold.is_a?(Hash)

      errors.add(:options, "#{at}.value must be a number") unless threshold["value"].is_a?(Numeric)
      errors.add(:options, "#{at}.label is too long (at most #{MAX_THRESHOLD_LABEL} characters)") if threshold["label"].to_s.length > MAX_THRESHOLD_LABEL
      errors.add(:options, "#{at}.severity must be warn or critical") if threshold.key?("severity") && !SEVERITIES.include?(threshold["severity"])
    end
  end

  def fits_the_grid
    return unless x.is_a?(Integer) && w.is_a?(Integer)

    errors.add(:w, "must fit in #{COLUMNS} columns from x") if x + w > COLUMNS
  end

  def project_of_the_dashboards_organization
    return unless project && dashboard

    errors.add(:project, "must belong to the Dashboard's Organization") unless project.organization_id == dashboard.organization_id
  end

  # A section_id that names no Section, or another Dashboard's, is refused.
  def section_of_the_dashboard
    return if section_id.nil?
    return if section && section.dashboard_id == dashboard_id

    errors.add(:section, "must be a Section of the Chart's Dashboard")
  end
end
