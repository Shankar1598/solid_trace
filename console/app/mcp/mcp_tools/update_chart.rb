# frozen_string_literal: true

module McpTools
  # Changes a Chart: its Project, title, Query, viz, options, place or
  # Section. A new Project without a new Query points the Query's source at
  # it, as the chart editor does. A Chart moved to another Section goes under
  # its Charts unless a row is given.
  class UpdateChart < Base
    include DashboardTool

    description "Changes a Chart on a Dashboard: its project, title, query, viz, options, place on the grid (x, y, w, h) " \
                "or Section (section_id, null for no Section), or a Text block's text, title, place or Section. " \
                "Only the fields given change."
    writes!
    takes_chart
    takes_chart_fields

    def run
      changes = chart_fields
      if arguments.key?(:project)
        changes[:project] = project
        # A SQL Chart's SQL reads the new Project's tables as it is.
        changes[:query] = with_source(chart.query, project.slug) unless changes.key?(:query) || chart.query.nil? || chart.language_sql?
      end
      changes[:options] = chart.options.merge(changes[:options].stringify_keys) if changes[:options].is_a?(Hash)
      if changes.empty?
        listed = [ :project, *CHART_FIELDS.keys ]
        listed.insert(listed.index(:query) + 1, :sql) if InstanceSetting.current.sql_queries_enabled?
        raise ToolError, "Give at least one of #{listed.join(', ')} to change."
      end

      chart.assign_attributes(changes)
      chart.y = dashboard.bottom(chart.section_id) if chart.section_id_changed? && !changes.key?(:y)
      save_chart!(chart)
      json(chart_json(chart), link: links.dashboard(dashboard))
    end
  end
end
