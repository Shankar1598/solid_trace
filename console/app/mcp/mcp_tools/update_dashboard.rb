# frozen_string_literal: true

module McpTools
  # Changes a Dashboard's name, description, default range, auto-refresh or
  # filters.
  class UpdateDashboard < Base
    include DashboardTool

    description "Changes a Dashboard's name, description, default_range, auto_refresh or filters. Only the fields given " \
                "change, and filters replaces the whole list: get_dashboard lists them. add_chart, update_chart and delete_chart " \
                "change its Charts."
    writes!
    takes_dashboard
    takes_dashboard_fields

    def run
      changes = given(DASHBOARD_FIELDS.keys)
      raise ToolError, "Give at least one of #{DASHBOARD_FIELDS.keys.join(', ')} to change." if changes.empty?

      dashboard.update!(changes)
      json(dashboard_json(dashboard), link: links.dashboard(dashboard))
    end
  end
end
