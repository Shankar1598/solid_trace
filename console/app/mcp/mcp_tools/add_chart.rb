# frozen_string_literal: true

module McpTools
  # Adds a Chart to a Dashboard, under every Chart unless a row is given, as
  # the Dashboard page's Add chart does.
  class AddChart < Base
    include DashboardTool

    description "Adds a Chart to a Dashboard: a Query on one of the Organization's Projects, drawn as viz. " \
                "It goes under every Chart unless y is given. get_chart_data shows what it draws. " \
                "#{READING_LESS}"
    writes!
    takes_dashboard
    takes_chart_fields required: %i[project title query]

    def run
      added = dashboard.charts.new(given(CHART_FIELDS.keys).merge(project: project))
      added.y = dashboard.bottom unless arguments.key?(:y)
      save_chart!(added)

      json(chart_json(added), link: links.dashboard(dashboard))
    end
  end
end
