# frozen_string_literal: true

module McpTools
  # Makes a Dashboard in an Organization, by the user, as its Dashboards page
  # does.
  class CreateDashboard < Base
    include DashboardTool

    description "Makes a Dashboard in an Organization, with no Charts and the filters given. add_chart adds Charts."
    writes!
    takes_organization
    takes_dashboard_fields required: %i[name]

    def run
      made = organization.dashboards.create!(given(DASHBOARD_FIELDS.keys).merge(created_by: user))

      json(dashboard_json(made), link: links.dashboard(made))
    end
  end
end
