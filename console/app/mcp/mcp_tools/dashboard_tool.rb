# frozen_string_literal: true

module McpTools
  # What the Dashboard, Section and Chart tools share: the dashboard, section
  # and chart arguments, the records they name in the Organization, and a
  # Dashboard and a Chart as the tools answer them. Any member of the Organization can see,
  # make, change and delete its Dashboards, as in the UI.
  module DashboardTool
    extend ActiveSupport::Concern

    DASHBOARD = { type: "integer", description: "The Dashboard's id. list_dashboards lists them." }.freeze
    CHART = { type: "integer", description: "The Chart's id, on the Dashboard. get_dashboard lists them." }.freeze
    SECTION = { type: "integer", description: "The Section's id, on the Dashboard. get_dashboard lists them." }.freeze
    SECTION_POSITION = {
      type: "integer",
      description: "Where the Section goes among the Dashboard's Sections, from 0 at the top. The others move to make room, " \
                   "and a position past the last puts it last.",
    }.freeze

    # A Dashboard filter as the Dashboard keeps it, which get_dashboard
    # answers and the Dashboard validates.
    FILTER = {
      type: "object",
      properties: {
        name: {
          type: "string",
          description: "What a Query writes after $ to use the filter, as $service: lowercase letters, digits and _, starting with " \
                       "a letter or _, at most 40 characters, unique on the Dashboard. " \
                       "Keep a saved filter's name, since Charts name it.",
        },
        label: { type: "string", description: "What the filter bar shows, at most #{Dashboard::MAX_FILTER_LABEL} characters. The name when empty." },
        kind: {
          type: "string", enum: %w[select search],
          description: "select: a pick from options. search: a text box, for search $q or message contains $q.",
        },
        multiple: { type: "boolean", description: "Whether a select takes several values, false when not given. Not for a search." },
        options: {
          type: "object",
          description: "A select's options, in one of two shapes; not for a search. A fixed list, " \
                       "{\"from\": \"list\", \"items\": [{\"label\": \"Errors\", \"value\": \"error\"}, {\"value\": \"warn\"}]}: " \
                       "1 to #{Dashboard::MAX_OPTIONS} items, each value not empty and unique, label and value at most " \
                       "#{Dashboard::MAX_OPTION_TEXT} characters, an empty label being the value. Or a field's values, " \
                       "{\"from\": \"field\", \"project_id\": 3, \"field\": \"service\"}: the 100 most common values of a field other " \
                       "than time, in a Project of the Organization (list_projects gives the ids), over the range the Dashboard shows.",
          properties: {
            from: { type: "string", enum: %w[list field] },
            items: { type: "array", items: { type: "object", properties: { label: { type: "string" }, value: { type: "string" } } } },
            project_id: { type: "integer" },
            field: { type: "string" },
          },
        },
        default: {
          type: %w[string array], items: { type: "string" },
          description: "The values the filter has when the Dashboard opens, until someone picks others: a string, or a list of strings. " \
                       "One value for a search, or for a select that isn't multiple; a fixed list's values must be its options' values. " \
                       "None, which is All, when not given; \"\" or [] removes the default. " \
                       "For an on/off filter, give a select the options hide and show, and the default hide. " \
                       "Then a Chart's Query where $known_issues == \"show\" or not(store_id in (1, 2)) leaves those Logs out with hide, " \
                       "and keeps them with show or All.",
        },
      },
    }.freeze

    DASHBOARD_FIELDS = {
      name: { type: "string", description: "The Dashboard's name, at most 200 characters." },
      description: { type: "string", description: "What the Dashboard shows, at most 2000 characters." },
      default_range: {
        type: "string",
        description: "The range the Dashboard shows until someone picks another: a span back from now such as 15m, 1h, 24h or 7d. 1h when made.",
      },
      auto_refresh: { type: "string", enum: Dashboard::AUTO_REFRESH, description: "How often the open page runs its Charts again. 15s when made." },
      filters: {
        type: "array",
        items: FILTER,
        description: "The Dashboard's filters, the controls of the bar under its header, in the bar's order, at most " \
                     "#{Dashboard::MAX_FILTERS}. A Chart uses a filter by naming it in its Query as $name, such as " \
                     "where service == $service, and runs it with the values picked, or as All, as if the comparison weren't there, " \
                     "when none is. The Dashboard opens with each filter on its default values, or on All when it has none. " \
                     "query_language_reference's Dashboard filters says where $name can go. The list given replaces " \
                     "the Dashboard's filters whole, so give every filter to keep; [] removes them all. None when made.",
      },
    }.freeze

    CHART_FIELDS = {
      title: { type: "string", description: "The Chart's title, at most 200 characters. Optional for a Text block." },
      text: {
        type: "string",
        description: "A Text block's Markdown, at most #{DashboardChart::MAX_TEXT} characters: headings, lists, bold, code, links " \
                     "and tables. The page shows no raw HTML and no images (an image shows as its link), and opens links in a new tab. " \
                     "Only for viz text, and needed for it.",
      },
      query: {
        type: "string",
        description: "The Query the Chart runs on its Project over the Dashboard's range. It must answer a table, so end it with " \
                     "summarize, count or project, such as: where level == \"error\" | summarize count() by bin_auto(time). " \
                     "It uses the Dashboard's filters by naming them as $name, such as where service == $service. " \
                     "A Query with mistakes, or naming a filter the Dashboard lacks, is not saved.",
      },
      viz: {
        type: "string",
        enum: DashboardChart::VIZES,
        description: "How the table is drawn. line and area need the last summarize's time bins, such as by bin_auto(time); " \
                     "bar needs a summarize with no time bin; stat needs a table of one row and one number; table draws any table. " \
                     "text is a Text block: a note in Markdown (the text field) with no project, no query and no options, " \
                     "which runs nothing and does not change with the range or the filters. " \
                     "A Chart does not change into a Text block, nor a Text block into a Chart. line when made.",
      },
      options: {
        type: "object",
        description: "How values are shown. Only the keys given change; an empty unit or a null clears one.",
        properties: {
          stacked: { type: %w[boolean null], description: "Stack a line, area or bar Chart's series." },
          unit: { type: "string", description: "A short unit shown with the values, such as ms, at most 16 characters." },
          decimals: { type: %w[integer null], description: "How many decimals to show, 0 to #{DashboardChart::MAX_DECIMALS}." },
          thresholds: {
            type: %w[array null],
            description: "Limits a line or area Chart draws as horizontal lines across its whole range, such as an SLO or a capacity: " \
                         "at most #{DashboardChart::MAX_THRESHOLDS}, in the order given. A threshold line is a limit, not a series: " \
                         "the Query's by does not split it, it is not in the legend, it does not take one of the 8 series a Chart draws, " \
                         "and the value axis reaches it. It sends nothing; a Monitor does that. Not for bar, stat or table, " \
                         "so give null with a change to one of those. The list given replaces the Chart's; [] or null removes them all.",
            items: {
              type: "object",
              properties: {
                value: { type: "number", description: "Where the line is, in the Chart's unit." },
                label: {
                  type: "string",
                  description: "What the line shows at its right end, at most #{DashboardChart::MAX_THRESHOLD_LABEL} characters. " \
                               "The value with the Chart's unit when not given.",
                },
                severity: {
                  type: "string", enum: DashboardChart::SEVERITIES,
                  description: "The line's colour: warn or critical. A muted line when not given.",
                },
              },
              required: %w[value],
            },
          },
        },
      },
      x: { type: "integer", description: "The column the Chart starts at, from 0, on a grid #{DashboardChart::COLUMNS} columns wide. 0 when added." },
      y: { type: "integer", description: "The row the Chart starts at, from 0. Under every Chart when added." },
      w: { type: "integer", description: "How many columns the Chart spans, 1 to #{DashboardChart::COLUMNS}; x + w at most #{DashboardChart::COLUMNS}. 6 when added." },
      h: { type: "integer", description: "How many rows the Chart spans, 1 to #{DashboardChart::MAX_HEIGHT}. 4 when added." },
      section_id: {
        type: %w[integer null],
        description: "The id of the Dashboard's Section the Chart is in, which get_dashboard lists; null for no Section. " \
                     "The page shows the Charts in no Section first, then each Section with its Charts on a grid of its own, " \
                     "so x and y are the place in the Section. A Chart put in a Section goes under its Charts unless y is given. " \
                     "No Section when added.",
      },
    }.freeze

    # The sql argument of add_chart and update_chart, listed only while SQL
    # queries are on, and what the tools' descriptions say of it then.
    CHART_SQL = {
      type: "string",
      description: "A SQL query (ClickHouse SQL) that the Chart runs in place of a Query. Use query first. Use sql only when the " \
                   "Chart needs a join, a subquery, a window, or a read of the events or spans tables. The SQL reads the logs, " \
                   "events and spans tables of the Chart's Project, as run_sql does, over the Dashboard's range, so it needs no " \
                   "time condition. It names a Dashboard filter as a ClickHouse query parameter, and writes its All case: a select " \
                   "as (empty({service:Array(String)}) OR service IN {service:Array(String)}), where All is []; a search as " \
                   "({q:String} = '' OR positionCaseInsensitive(message, {q:String}) > 0), where All is '' and several values " \
                   "are joined by a space; or {name:Nullable(String)}, as String with All NULL. No other type names a filter. " \
                   "line and area need a time column first; bar needs no time column; stat needs one row of one number. " \
                   "EventStore runs the SQL over no time before the Chart is saved: SQL that does not run, or that names a " \
                   "filter the Dashboard lacks, is not saved. Give sql or query, not both. Giving sql makes the Chart's language sql.",
    }.freeze
    CHART_SQL_NOTE = "A Chart that needs a join, a subquery, a window, or the events or spans tables takes sql in place of query."

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

      # And the section argument, which #section reads.
      def takes_section
        takes_dashboard
        argument :section, SECTION, required: true
      end

      def takes_dashboard_fields(required: [])
        DASHBOARD_FIELDS.each { |name, schema| argument name, schema, required: required.include?(name) }
      end

      # A Chart's project and fields. The project is a slug in the
      # Organization, which #project reads.
      def takes_chart_fields(required: [])
        argument :project, { type: "string", description: "The slug of the Project the Chart's Query runs on. list_projects lists them." },
          required: required.include?(:project)
        CHART_FIELDS.each do |name, schema|
          argument name, schema, required: required.include?(name)
          sql_argument :sql, CHART_SQL, note: CHART_SQL_NOTE if name == :query
        end
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

    # The Section the section argument names, on #dashboard.
    def section
      @section ||= dashboard.sections.find(arguments[:section])
    end

    # The fields of names given in the arguments, as the record takes them.
    def given(names)
      arguments.slice(*names)
    end

    # The Chart fields given, with query or sql as the Chart's text and
    # language.
    def chart_fields
      raise ToolError, "Give query or sql, not both: a Chart runs one of them." if arguments.key?(:query) && arguments.key?(:sql)

      fields = given(CHART_FIELDS.keys)
      fields[:language] = "query" if arguments.key?(:query)
      fields.merge!(language: "sql", query: arguments[:sql]) if arguments.key?(:sql)
      fields
    end

    # Saves a Chart whose fields are valid. A new Chart, or one whose Query,
    # language or Project changed, is saved only when EventStore finds no
    # mistakes in its Query on its Project, and the Query names only the
    # Dashboard's filters, as the chart editor checks before it saves. The
    # check compiles the Query and runs nothing. A SQL Chart's SQL is run
    # over no time instead, which reads no rows, with its filters as All:
    # its answer names the filters it uses. A Text block has no Query to
    # check.
    def save_chart!(chart)
      chart.validate!
      if !chart.text? && (chart.new_record? || chart.query_changed? || chart.language_changed? || chart.project_id_changed?)
        names = chart.language_sql? ? sql_filter_names(chart) : query_filter_names(chart)
        unknown = names - dashboard.filter_names
        if unknown.any?
          mark = chart.language_sql? ? "" : "$"
          attribute = chart.language_sql? ? :sql : :query
          chart.errors.add(attribute, "names #{unknown.map { |name| "#{mark}#{name}" }.to_sentence}, which #{not_its_filters(unknown)}")
          raise ActiveRecord::RecordInvalid, chart
        end
      end
      chart.save!
    end

    # The filters a Chart's Query names, which validate finds.
    def query_filter_names(chart)
      validation = EventStore.validate_logs_query(project_id: chart.project_id, query: chart.query)
      if Array(validation["errors"]).any?
        EventStore.mistakes(validation).each { |mistake| chart.errors.add(:query, mistake) }
        raise ActiveRecord::RecordInvalid, chart
      end
      Array(validation["parameters"])
    end

    # The filters a SQL Chart's SQL names, from a run over no time.
    def sql_filter_names(chart)
      now = Time.current
      answer = EventStore.run_logs_sql(project_id: chart.project_id, sql: chart.query, from: now, to: now,
                                       timeout_seconds: EventStoreProxy::SQL_TIMEOUT_SECONDS)
      Array(answer["parameters"])
    rescue EventStore::Rejected => e
      chart.errors.add(:sql, "can't run: #{EventStore.error_text(e.body)}")
      raise ActiveRecord::RecordInvalid, chart
    end

    # What a refusal of names the Dashboard has no filters of says of them,
    # listing its filters: "isn't one of this Dashboard's filters (level, q)".
    def not_its_filters(names)
      "#{names.one? ? "isn't one" : "aren't any"} of this Dashboard's filters (#{dashboard.filter_names.join(', ').presence || 'it has none'})"
    end

    # A Dashboard with its Sections and Charts, in the order the page reads
    # them.
    def dashboard_json(dashboard)
      charts = dashboard.charts.includes(:project).to_a
      DashboardSerializer.new(dashboard, chart_count: charts.size).as_json.merge(
        sections: dashboard.sections.map { |section| section_json(section) },
        charts: charts.map { |chart| chart_json(chart) },
      )
    end

    def section_json(section)
      DashboardSectionSerializer.new(section).as_json
    end

    # A Chart, with the slug of its Project, which the tools take; a Text
    # block has none.
    def chart_json(chart)
      DashboardChartSerializer.new(chart).as_json.merge(project: chart.project&.slug)
    end
  end
end
