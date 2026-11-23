# frozen_string_literal: true

module McpTools
  # A Dashboard with its filters, Sections and Charts.
  class GetDashboard < Base
    include DashboardTool

    description "Answers a Dashboard with its filters, Sections and Charts. Each filter's name, label, kind, multiple and options, " \
                "as update_dashboard takes them; a Chart's Query names a filter as $name. Each Section's id, title and position, " \
                "in the page's order. Each Chart's id, title, Project, Query, viz, options (stacked, unit, decimals and thresholds, " \
                "each only when set, as add_chart takes them), section_id (null for no Section) and " \
                "place on the grid of its Section (x, y, w, h). A Text block (viz text) comes with its Markdown as text, " \
                "and with no Project and no Query (both null); a Chart has text null. get_chart_data runs a Chart's Query, " \
                "with values for the filters it names."
    read_only!
    takes_dashboard

    def run
      json(dashboard_json(dashboard), link: links.dashboard(dashboard))
    end
  end
end
