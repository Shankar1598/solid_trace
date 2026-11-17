# frozen_string_literal: true

class DashboardSectionSerializer
  def initialize(section)
    @section = section
  end

  def as_json(*)
    {
      id: @section.id,
      dashboard_id: @section.dashboard_id,
      title: @section.title,
      position: @section.position,
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
