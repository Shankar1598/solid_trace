# frozen_string_literal: true

module McpTools
  # An Organization's Dashboards, as its Dashboards page lists them.
  class ListDashboards < Base
    include DashboardTool

    description "Lists an Organization's Dashboards by name: the id other tools take, the description, " \
                "the range each shows by default and how many Charts it has. get_dashboard answers one with its Charts."
    read_only!
    takes_organization

    def run
      dashboards = organization.dashboards.includes(:created_by).order(:name)
      counts = DashboardChart.where(dashboard_id: dashboards.map(&:id)).group(:dashboard_id).count

      json({ organization: organization.slug,
             dashboards: dashboards.map { |d| DashboardSerializer.new(d, chart_count: counts.fetch(d.id, 0)).as_json } },
        link: links.dashboards(organization))
    end
  end
end
