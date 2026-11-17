# frozen_string_literal: true

class DashboardSerializer
  # chart_count saves counting each Dashboard's charts when a list has
  # counted them together. With its charts, it also has its Sections, in
  # their order.
  def initialize(dashboard, include_charts: false, chart_count: nil)
    @dashboard = dashboard
    @include_charts = include_charts
    @chart_count = chart_count
  end

  def as_json(*)
    result = {
      id: @dashboard.id,
      name: @dashboard.name,
      description: @dashboard.description,
      default_range: @dashboard.default_range,
      auto_refresh: @dashboard.auto_refresh,
      filters: @dashboard.filters,
      chart_count: @chart_count || @dashboard.charts.size,
      created_by: @dashboard.created_by&.name,
      created_at: @dashboard.created_at.iso8601,
      updated_at: @dashboard.updated_at.iso8601,
    }
    if @include_charts
      result[:sections] = @dashboard.sections.map { |section| DashboardSectionSerializer.new(section).as_json }
      result[:charts] = @dashboard.charts.map { |chart| DashboardChartSerializer.new(chart).as_json }
    end
    result
  end

  def to_json(*)
    as_json.to_json
  end
end
