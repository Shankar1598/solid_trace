# frozen_string_literal: true

# A page of charts that belongs to an Organization. Each chart runs a saved
# Query on one of the Organization's Projects, over the range the page picks:
# default_range until a member picks another. Its filters are the controls of
# the bar under its header, whose values the Charts that name them in their
# Queries ($name) run with (ADR 0014).
class Dashboard < ApplicationRecord
  AUTO_REFRESH = %w[off 15s 30s 1m 5m].freeze
  # A span back from now, as the explorer's links hold a range: 15m, 1h, 7d.
  SPAN = /\A0*[1-9]\d*[mhd]\z/
  MAX_FILTERS = 20
  # What a Query writes after $.
  FILTER_NAME = /\A[a-z_][a-z0-9_]{0,39}\z/
  MAX_FILTER_LABEL = 100
  MAX_OPTIONS = 200
  MAX_OPTION_TEXT = 200
  MAX_FIELD_NAME = 200

  belongs_to :organization
  belongs_to :created_by, class_name: "User", optional: true
  has_many :charts, -> { order(:y, :x, :id) }, class_name: "DashboardChart", inverse_of: :dashboard, dependent: :destroy

  validates :name, presence: true, length: { maximum: 200 }
  validates :description, length: { maximum: 2000 }
  validates :default_range, format: { with: SPAN, message: "must be a span such as 15m, 1h or 7d" }
  validates :auto_refresh, inclusion: { in: AUTO_REFRESH, message: "must be off, 15s, 30s, 1m or 5m" }
  before_validation :normalize_filters
  validate :filters_valid

  # The row under every chart, where a new chart goes.
  def bottom
    charts.maximum(Arel.sql("y + h")) || 0
  end

  def filter_names
    filters.map { |filter| filter["name"] }
  end

  # The values of its filters in given, {name => value or [values]}, as
  # EventStore's parameters, in the filters' order. Names it lacks and empty
  # values are left out, so EventStore runs those filters as All.
  def parameters(given)
    given = given.to_h.stringify_keys
    filter_names.each_with_object({}) do |name, parameters|
      values = Array(given[name]).map(&:to_s).reject(&:empty?)
      parameters[name] = values if values.any?
    end
  end

  private

  # Each filter is {name, label, kind} and, for a select, multiple and
  # options; an empty label is the name, and other keys go. What isn't a
  # filter, and a search's multiple or options, are kept for filters_valid to
  # refuse.
  def normalize_filters
    self.filters = filters.map { |filter| normalize_filter(filter) } if filters.is_a?(Array)
  end

  def normalize_filter(filter)
    return filter unless filter.is_a?(Hash)

    given = filter.stringify_keys
    name = given["name"].to_s.strip
    normalized = { "name" => name, "label" => given["label"].to_s.strip.presence || name, "kind" => given["kind"].to_s }
    multiple = ActiveModel::Type::Boolean.new.cast(given["multiple"]) || false
    if normalized["kind"] == "select"
      normalized.merge("multiple" => multiple, "options" => normalize_filter_options(given["options"]))
    else
      normalized["multiple"] = multiple if multiple
      normalized["options"] = given["options"] unless given["options"].nil?
      normalized
    end
  end

  # A fixed list is {from: "list", items: [{label, value}]}, an empty label
  # being the value. A field's values are {from: "field", project_id, field}:
  # that field's most common values in that Project.
  def normalize_filter_options(options)
    return options unless options.is_a?(Hash)

    given = options.stringify_keys
    case given["from"]
    when "list"
      items = given["items"]
      items = items.map { |item| item.is_a?(Hash) ? normalize_option(item.stringify_keys) : item } if items.is_a?(Array)
      { "from" => "list", "items" => items }
    when "field"
      project_id = Integer(given["project_id"].to_s, 10, exception: false) || given["project_id"]
      { "from" => "field", "project_id" => project_id, "field" => given["field"].to_s.strip }
    else options
    end
  end

  def normalize_option(item)
    value = item["value"].to_s.strip
    { "label" => item["label"].to_s.strip.presence || value, "value" => value }
  end

  # Errors name the filter by its place in the list, from 0, as
  # filters[1].name.
  def filters_valid
    return errors.add(:filters, "must be a list") unless filters.is_a?(Array)
    return errors.add(:filters, "are at most #{MAX_FILTERS}") if filters.size > MAX_FILTERS

    names = Set.new
    filters.each_with_index do |filter, i|
      at = "filters[#{i}]"
      next errors.add(at.to_sym, "must be an object") unless filter.is_a?(Hash)

      if !FILTER_NAME.match?(filter["name"])
        errors.add(:"#{at}.name", "must be lowercase letters, digits and _, starting with a letter or _, at most 40 characters")
      elsif !names.add?(filter["name"])
        errors.add(:"#{at}.name", "is already a filter's name")
      end
      errors.add(:"#{at}.label", "is too long (at most #{MAX_FILTER_LABEL} characters)") if filter["label"].length > MAX_FILTER_LABEL
      case filter["kind"]
      when "select" then filter_options_valid(filter["options"], "#{at}.options")
      when "search"
        errors.add(:"#{at}.multiple", "is for selects only") if filter.key?("multiple")
        errors.add(:"#{at}.options", "are for selects only") if filter.key?("options")
      else errors.add(:"#{at}.kind", "must be select or search")
      end
    end
  end

  def filter_options_valid(options, at)
    return errors.add(at.to_sym, "can't be blank") if options.nil?
    return field_options_valid(options, at) if options.is_a?(Hash) && options["from"] == "field"
    return errors.add(at.to_sym, "must be a fixed list or a field") unless options.is_a?(Hash) && options["from"] == "list"

    items = options["items"]
    return errors.add(at.to_sym, "must have 1 to #{MAX_OPTIONS} options") unless items.is_a?(Array) && items.size.between?(1, MAX_OPTIONS)

    values = Set.new
    items.each_with_index do |item, j|
      next errors.add(:"#{at}[#{j}]", "must be an object") unless item.is_a?(Hash)

      errors.add(:"#{at}[#{j}].label", "is too long (at most #{MAX_OPTION_TEXT} characters)") if item["label"].length > MAX_OPTION_TEXT
      if item["value"].empty?
        errors.add(:"#{at}[#{j}].value", "can't be blank")
      elsif item["value"].length > MAX_OPTION_TEXT
        errors.add(:"#{at}[#{j}].value", "is too long (at most #{MAX_OPTION_TEXT} characters)")
      elsif !values.add?(item["value"])
        errors.add(:"#{at}[#{j}].value", "is already an option's value")
      end
    end
  end

  def field_options_valid(options, at)
    field = options["field"]
    if field.empty?
      errors.add(:"#{at}.field", "can't be blank")
    elsif field.length > MAX_FIELD_NAME
      errors.add(:"#{at}.field", "is too long (at most #{MAX_FIELD_NAME} characters)")
    elsif field == "time"
      errors.add(:"#{at}.field", "can't be time")
    end
    project_id = options["project_id"]
    return if project_id.is_a?(Integer) && organization&.projects&.exists?(id: project_id)

    errors.add(:"#{at}.project_id", "must be a Project of the Dashboard's Organization")
  end
end
