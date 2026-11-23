# frozen_string_literal: true

# A page of charts that belongs to an Organization. Each chart runs a saved
# Query on one of the Organization's Projects, over the range the page picks:
# default_range until a member picks another. Its filters are the controls of
# the bar under its header, whose values the Charts that name them in their
# Queries ($name) run with (ADR 0014): a filter's default values until a
# member picks others. Its Sections group its charts under
# titles; a chart can also be in no Section.
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
  # A page's query string holds each filter's values under this and the
  # filter's name, the key repeated for several values:
  # ?range=1h&v_service=api&v_service=web. A filter missing from it has its
  # default values, and a key with only an empty value is All.
  FILTER_KEY_PREFIX = "v_"
  MAX_SECTIONS = 50

  belongs_to :organization
  belongs_to :created_by, class_name: "User", optional: true
  has_many :charts, -> { order(:y, :x, :id) }, class_name: "DashboardChart", inverse_of: :dashboard, dependent: :destroy
  has_many :sections, -> { order(:position, :id) }, class_name: "DashboardSection", inverse_of: :dashboard, dependent: :destroy

  validates :name, presence: true, length: { maximum: 200 }
  validates :description, length: { maximum: 2000 }
  validates :default_range, format: { with: SPAN, message: "must be a span such as 15m, 1h or 7d" }
  validates :auto_refresh, inclusion: { in: AUTO_REFRESH, message: "must be off, 15s, 30s, 1m or 5m" }
  before_validation :normalize_filters
  validate :filters_valid

  # The filter values a page's query string holds, {name => [values]}, for
  # any name. Rails' params keep only the last of a repeated key, so the
  # query string is read whole.
  def self.filter_values_in(query_string)
    Rack::Utils.parse_query(query_string).each_with_object({}) do |(key, values), found|
      found[key.delete_prefix(FILTER_KEY_PREFIX)] = Array(values) if key.start_with?(FILTER_KEY_PREFIX)
    end
  end

  # Filter values, {name => [values]}, as the keys of a page's query string.
  def self.filter_link_params(values)
    values.transform_keys { |name| "#{FILTER_KEY_PREFIX}#{name}" }
  end

  # The row under every chart of the Section of section_id, or of no Section
  # when it is nil: where a new chart in it goes.
  def bottom(section_id = nil)
    charts.where(section_id: section_id).maximum(Arel.sql("y + h")) || 0
  end

  # Where a new chart w columns wide goes in the Section of section_id, or in
  # no Section when it is nil, as [x, y]: beside the last row's charts when
  # there is space for it, else under every chart. The last row starts at the
  # largest y, so no chart is below it on the right.
  def place(section_id, w)
    group = charts.where(section_id: section_id).to_a
    return [ 0, 0 ] if group.empty?

    top = group.map(&:y).max
    right = group.select { |chart| chart.y + chart.h > top }.map { |chart| chart.x + chart.w }.max
    right + w.to_i <= DashboardChart::COLUMNS ? [ right, top ] : [ 0, bottom(section_id) ]
  end

  def filter_names
    filters.map { |filter| filter["name"] }
  end

  # The values of its filters in given, {name => value or [values]}, as
  # EventStore's parameters, in the filters' order. Names it lacks and empty
  # values are left out, so EventStore runs those filters as All. With
  # defaults, a filter whose name given lacks has its default values; one
  # given "" or [] is still All.
  def filter_values(given, defaults: false)
    given = given.to_h.stringify_keys
    filters.each_with_object({}) do |filter, found|
      name = filter["name"]
      values = (defaults && !given.key?(name) ? Array(filter["default"]) : Array(given[name])).map(&:to_s).reject(&:empty?)
      found[name] = values if values.any?
    end
  end

  # The values given for its filters, {name => value or [values]}, as a link
  # to its page holds them. The link leaves out a filter that given lacks, so
  # the page opens it on its default values. A filter with default values that
  # given sets to All gets [""], so the page opens it on All.
  def filter_link_values(given)
    given = given.to_h.stringify_keys
    filters.each_with_object({}) do |filter, link|
      name = filter["name"]
      next unless given.key?(name)

      values = Array(given[name]).map(&:to_s).reject(&:empty?)
      if values.any? then link[name] = values
      elsif filter["default"].present? then link[name] = [ "" ]
      end
    end
  end

  # The Project id and field of its filter name when that filter is a select
  # of a field's values, or nil.
  def field_select(name)
    options = filters.find { |filter| filter["name"] == name }&.dig("options")
    [ options["project_id"], options["field"] ] if options.is_a?(Hash) && options["from"] == "field"
  end

  private

  # Each filter is {name, label, kind} and, for a select, multiple and
  # options, and default when it has default values; an empty label is the
  # name, and other keys go. What isn't a filter, and a search's multiple or
  # options, are kept for filters_valid to refuse.
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
      normalized.merge!("multiple" => multiple, "options" => normalize_filter_options(given["options"]))
    else
      normalized["multiple"] = multiple if multiple
      normalized["options"] = given["options"] unless given["options"].nil?
    end
    default = normalize_default(given["default"])
    normalized["default"] = default unless default.nil? || default == []
    normalized
  end

  # Default values are a list of strings, from a string or a list; blank ones go.
  # Anything else is kept for filters_valid to refuse.
  def normalize_default(default)
    return default unless default.nil? || default.is_a?(String) || (default.is_a?(Array) && default.all?(String))

    Array(default).map(&:strip).reject(&:empty?)
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
      default_valid(filter, "#{at}.default") if filter.key?("default")
    end
  end

  # Default values: one for a search or a select of one value, those of a
  # fixed list's options, each at most MAX_OPTION_TEXT characters.
  def default_valid(filter, at)
    default = filter["default"]
    return errors.add(at.to_sym, "must be a string or a list of strings") unless default.is_a?(Array) && default.all?(String)

    takes_one = filter["kind"] != "select" || filter["multiple"] != true
    return errors.add(at.to_sym, "must be one value, as the filter takes one") if takes_one && default.size > 1
    return errors.add(at.to_sym, "are at most #{MAX_OPTIONS} values") if default.size > MAX_OPTIONS

    too_long = default.any? { |value| value.length > MAX_OPTION_TEXT }
    return errors.add(at.to_sym, "is too long (at most #{MAX_OPTION_TEXT} characters)") if too_long

    options = filter["options"]
    return unless filter["kind"] == "select" && options.is_a?(Hash) && options["from"] == "list" && options["items"].is_a?(Array)

    known = options["items"].filter_map { |item| item["value"] if item.is_a?(Hash) }
    unknown = default - known
    errors.add(at.to_sym, "must be the options' values, not #{unknown.map(&:inspect).to_sentence}") if unknown.any?
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
    elsif %w[time _time].include?(field)
      errors.add(:"#{at}.field", "can't be time")
    end
    project_id = options["project_id"]
    return if project_id.is_a?(Integer) && organization&.projects&.exists?(id: project_id)

    errors.add(:"#{at}.project_id", "must be a Project of the Dashboard's Organization")
  end
end
