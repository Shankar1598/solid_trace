# frozen_string_literal: true

module McpTools
  # A Dashboard with its filters and Charts.
  class GetDashboard < Base
    include DashboardTool

    description "Answers a Dashboard with its filters and Charts. Each filter's name, label, kind, multiple and options, " \
                "as update_dashboard takes them; a Chart's Query names a filter as $name. Each Chart's id, title, Project, " \
                "Query, viz, options and place on the grid (x, y, w, h). get_chart_data runs a Chart's Query, with values " \
                "for the filters it names."
    read_only!
    takes_dashboard

    def run
      json(dashboard_json(dashboard), link: links.dashboard(dashboard))
    end
  end
end
