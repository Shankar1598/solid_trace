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
                "With viz text it adds a Text block instead: a note in Markdown (text), with no project and no query, " \
                "placed as a Chart is. project, title and query are needed for every other viz. " \
                "#{READING_LESS}"
    writes!
    takes_dashboard
    takes_chart_fields

    def run
      fields = chart_fields
      if fields[:viz] == "text"
        fields[:project] = project if arguments.key?(:project)
      elsif %i[project title].all? { |name| arguments[name].present? } && fields[:query].present?
        fields[:project] = project
      else
        raise ToolError, "Give project, title and query for a Chart; only a Text block (viz text) goes without them."
      end
      added = dashboard.charts.new(fields)
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
