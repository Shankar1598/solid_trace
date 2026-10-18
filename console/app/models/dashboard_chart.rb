# frozen_string_literal: true

# One chart of a Dashboard: a saved Query on a Project of the Dashboard's
# Organization, drawn as a line, area, bar, stat or table, at a place on the
# Dashboard's grid of COLUMNS columns.
class DashboardChart < ApplicationRecord
  VIZES = %w[line area bar stat table].freeze
  COLUMNS = 12
  MAX_HEIGHT = 24
  MAX_DECIMALS = 6

  belongs_to :dashboard, touch: true
  belongs_to :project

  before_validation :normalize_options

  validates :title, presence: true, length: { maximum: 200 }
  validates :query, presence: true, length: { maximum: 20_000 }
  validates :viz, inclusion: { in: VIZES, message: "must be line, area, bar, stat or table" }
  validates :x, :y, numericality: { only_integer: true, greater_than_or_equal_to: 0 }
  validates :w, numericality: { only_integer: true, in: 1..COLUMNS }
  validates :h, numericality: { only_integer: true, in: 1..MAX_HEIGHT }
  validate :fits_the_grid
  validate :project_of_the_dashboards_organization
  validate :options_known

  private

  # options holds stacked (true or false), unit (a short label such as ms)
  # and decimals (0 to MAX_DECIMALS), each only when set; other keys go.
  def normalize_options
    given = options.is_a?(Hash) ? options.stringify_keys : {}
    stacked, unit, @decimals_given = given.values_at("stacked", "unit", "decimals")
    self.options = {
      "stacked" => stacked.to_s == "" ? nil : ActiveModel::Type::Boolean.new.cast(stacked),
      "unit" => unit.to_s.strip.presence,
      "decimals" => @decimals_given.to_s == "" ? nil : Integer(@decimals_given.to_s, 10, exception: false),
    }.compact
  end

  def options_known
    errors.add(:options, "unit must be at most 16 characters") if options["unit"].to_s.length > 16
    return if @decimals_given.to_s == "" || (0..MAX_DECIMALS).cover?(options["decimals"])

    errors.add(:options, "decimals must be a whole number from 0 to #{MAX_DECIMALS}")
  end

  def fits_the_grid
    return unless x.is_a?(Integer) && w.is_a?(Integer)

    errors.add(:w, "must fit in #{COLUMNS} columns from x") if x + w > COLUMNS
  end

  def project_of_the_dashboards_organization
    return unless project && dashboard

    errors.add(:project, "must belong to the Dashboard's Organization") unless project.organization_id == dashboard.organization_id
  end
end
