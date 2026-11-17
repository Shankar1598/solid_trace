# frozen_string_literal: true

module McpTools
  # Adds a Chart to a Dashboard, beside the last row of its Section (or of no
  # Section) when there is space, else under every Chart there, unless a
  # place is given, as the Dashboard page's Add chart does.
  class AddChart < Base
    include DashboardTool

    description "Adds a Chart to a Dashboard: a Query on one of the Organization's Projects, drawn as viz. " \
                "It goes in section_id's Section, or in no Section: beside the last row's Charts when there is space, " \
                "else under every Chart there, unless x or y is given. " \
                "get_chart_data shows what it draws. " \
                "#{READING_LESS}"
    writes!
    takes_dashboard
    takes_chart_fields required: %i[project title query]

    def run
      added = dashboard.charts.new(given(CHART_FIELDS.keys).merge(project: project))
      if !arguments.key?(:x) && !arguments.key?(:y)
        added.x, added.y = dashboard.place(added.section_id, added.w)
      elsif !arguments.key?(:y)
        added.y = dashboard.bottom(added.section_id)
      end
      save_chart!(added)

      json(chart_json(added), link: links.dashboard(dashboard))
    end
  end
end
