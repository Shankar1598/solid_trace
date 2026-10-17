# frozen_string_literal: true

module McpTools
  # What the Dashboard and Chart tools share: the dashboard and chart
  # arguments, the records they name in the Organization, and a Dashboard and
  # a Chart as the tools answer them. Any member of the Organization can see,
  # make, change and delete its Dashboards, as in the UI.
  module DashboardTool
    extend ActiveSupport::Concern

    DASHBOARD = { type: "integer", description: "The Dashboard's id. list_dashboards lists them." }.freeze
    CHART = { type: "integer", description: "The Chart's id, on the Dashboard. get_dashboard lists them." }.freeze

    DASHBOARD_FIELDS = {
      name: { type: "string", description: "The Dashboard's name, at most 200 characters." },
      description: { type: "string", description: "What the Dashboard shows, at most 2000 characters." },
      default_range: {
        type: "string",
        description: "The range the Dashboard shows until someone picks another: a span back from now such as 15m, 1h, 24h or 7d. 1h when made.",
      },
      auto_refresh: { type: "string", enum: Dashboard::AUTO_REFRESH, description: "How often the open page runs its Charts again. off when made." },
    }.freeze

    CHART_FIELDS = {
      title: { type: "string", description: "The Chart's title, at most 200 characters." },
      query: {
        type: "string",
        description: "The Query the Chart runs on its Project over the Dashboard's range. It must answer a table, so end it with " \
                     "summarize, count or project, such as: where level == \"error\" | summarize count() by bin_auto(time). " \
                     "A Query with mistakes is not saved.",
      },
      viz: {
        type: "string",
        enum: DashboardChart::VIZES,
        description: "How the table is drawn. line and area need the last summarize's time bins, such as by bin_auto(time); " \
                     "bar needs a summarize with no time bin; stat needs a table of one row and one number; table draws any table. line when made.",
      },
      options: {
        type: "object",
        description: "How values are shown. Only the keys given change; an empty unit or a null clears one.",
        properties: {
          stacked: { type: %w[boolean null], description: "Stack a line, area or bar Chart's series." },
          unit: { type: "string", description: "A short unit shown with the values, such as ms, at most 16 characters." },
          decimals: { type: %w[integer null], description: "How many decimals to show, 0 to #{DashboardChart::MAX_DECIMALS}." },
        },
      },
      x: { type: "integer", description: "The column the Chart starts at, from 0, on a grid #{DashboardChart::COLUMNS} columns wide. 0 when added." },
      y: { type: "integer", description: "The row the Chart starts at, from 0. Under every Chart when added." },
      w: { type: "integer", description: "How many columns the Chart spans, 1 to #{DashboardChart::COLUMNS}; x + w at most #{DashboardChart::COLUMNS}. 6 when added." },
      h: { type: "integer", description: "How many rows the Chart spans, 1 to #{DashboardChart::MAX_HEIGHT}. 4 when added." },
    }.freeze

    class_methods do
      # The organization and dashboard arguments, which #dashboard reads.
      def takes_dashboard
        takes_organization
        argument :dashboard, DASHBOARD, required: true
      end

      # And the chart argument, which #chart reads.
      def takes_chart
        takes_dashboard
        argument :chart, CHART, required: true
      end

      def takes_dashboard_fields(required: [])
        DASHBOARD_FIELDS.each { |name, schema| argument name, schema, required: required.include?(name) }
      end

      # A Chart's project and fields. The project is a slug in the
      # Organization, which #project reads.
      def takes_chart_fields(required: [])
        argument :project, { type: "string", description: "The slug of the Project the Chart's Query runs on. list_projects lists them." },
          required: required.include?(:project)
        CHART_FIELDS.each { |name, schema| argument name, schema, required: required.include?(name) }
      end
    end

    private

    # The Dashboard the dashboard argument names, in #organization.
    def dashboard
      @dashboard ||= organization.dashboards.find(arguments[:dashboard])
    end

    # The Chart the chart argument names, on #dashboard.
    def chart
      @chart ||= dashboard.charts.find(arguments[:chart])
    end

    # The fields of names given in the arguments, as the record takes them.
    def given(names)
      arguments.slice(*names)
    end

    # Saves a Chart whose fields are valid. A new Chart, or one whose Query or
    # Project changed, is saved only when EventStore finds no mistakes in its
    # Query on its Project, as the chart editor checks before it saves. The
    # check compiles the Query and runs nothing.
    def save_chart!(chart)
      chart.validate!
      if chart.new_record? || chart.query_changed? || chart.project_id_changed?
        validation = EventStore.validate_logs_query(project_id: chart.project_id, query: chart.query)
        if Array(validation["errors"]).any?
          EventStore.mistakes(validation).each { |mistake| chart.errors.add(:query, mistake) }
          raise ActiveRecord::RecordInvalid, chart
        end
      end
      chart.save!
    end

    # A Dashboard with its Charts, in the order the page reads them.
    def dashboard_json(dashboard)
      charts = dashboard.charts.includes(:project).to_a
      DashboardSerializer.new(dashboard, chart_count: charts.size).as_json.merge(charts: charts.map { |chart| chart_json(chart) })
    end

    # A Chart, with the slug of its Project, which the tools take.
    def chart_json(chart)
      DashboardChartSerializer.new(chart).as_json.merge(project: chart.project.slug)
    end
  end
end
