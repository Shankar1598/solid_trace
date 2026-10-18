# frozen_string_literal: true

module McpTools
  # Deletes a Dashboard and its Charts.
  class DeleteDashboard < Base
    include DashboardTool

    description "Deletes a Dashboard and all its Charts."
    destructive!
    takes_dashboard

    def run
      charts = dashboard.charts.size
      dashboard.destroy!

      text("Deleted Dashboard #{dashboard.id}, #{dashboard.name}, and its #{charts} #{'Chart'.pluralize(charts)}.",
        link: links.dashboards(organization))
    end
  end
end
