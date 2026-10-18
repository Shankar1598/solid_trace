# frozen_string_literal: true

class DashboardChartSerializer
  def initialize(chart)
    @chart = chart
  end

  def as_json(*)
    {
      id: @chart.id,
      dashboard_id: @chart.dashboard_id,
      project_id: @chart.project_id,
      title: @chart.title,
      query: @chart.query,
      viz: @chart.viz,
      options: @chart.options,
      x: @chart.x,
      y: @chart.y,
      w: @chart.w,
      h: @chart.h,
      updated_at: @chart.updated_at.iso8601(6),
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
