# frozen_string_literal: true

module McpTools
  # A Dashboard with its Charts.
  class GetDashboard < Base
    include DashboardTool

    description "Answers a Dashboard with its Charts: each Chart's id, title, Project, Query, viz, options and place " \
                "on the grid (x, y, w, h). get_chart_data runs a Chart's Query."
    read_only!
    takes_dashboard

    def run
      json(dashboard_json(dashboard), link: links.dashboard(dashboard))
    end
  end
end
