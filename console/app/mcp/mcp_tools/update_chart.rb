# frozen_string_literal: true

module McpTools
  # Changes a Chart: its Project, title, Query, viz, options or place.
  class UpdateChart < Base
    include DashboardTool

    description "Changes a Chart on a Dashboard: its project, title, query, viz, options or place on the grid (x, y, w, h). " \
                "Only the fields given change."
    writes!
    takes_chart
    takes_chart_fields

    def run
      changes = given(CHART_FIELDS.keys)
      changes[:project] = project if arguments.key?(:project)
      changes[:options] = chart.options.merge(changes[:options].stringify_keys) if changes[:options].is_a?(Hash)
      raise ToolError, "Give at least one of project, #{CHART_FIELDS.keys.join(', ')} to change." if changes.empty?

      chart.update!(changes)
      json(chart_json(chart), link: links.dashboard(dashboard))
    end
  end
end
