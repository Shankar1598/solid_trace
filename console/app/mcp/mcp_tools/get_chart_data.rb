# frozen_string_literal: true

module McpTools
  # Runs a Chart's saved Query over a range, as the Dashboard page does, and
  # answers the table it draws.
  class GetChartData < Base
    include DashboardTool

    description "Runs a Chart's saved Query on its Project over a range and answers the table the Chart draws: " \
                "#{Table::SHAPE}. With filters, it runs as the Dashboard page does with those values picked in its filter bar; " \
                "the answer's filters are the values it ran with, {name: [values]}, and a filter not in it ran as All."
    read_only!
    takes_chart
    takes_range
    argument :range, RANGE.merge(description: RANGE[:description].sub("1h when", "The Dashboard's default_range when"))
    argument :filters, {
      type: "object",
      additionalProperties: { type: %w[string array], items: { type: "string" } },
      description: "Values for the Dashboard's filters by name, as get_dashboard lists them: a string, or a list of strings " \
                   "for several, such as {\"service\": [\"api\", \"web\"], \"q\": \"timeout\"}. Where the Chart's Query names a " \
                   "filter as $name, it runs with these values; a filter left out, or given \"\" or [], runs as All, " \
                   "as if its comparisons weren't there.",
    }

    def run
      values = filter_values
      answer = ChartRuns.run_chart(organization.projects.find(chart.project_id), chart.query,
        from: time_range.from, to: time_range.to, parameters: values)
      table = answer["table"]

      json({ chart: chart.id, title: chart.title, viz: chart.viz,
             from: table["from"] || time_range.from.utc.iso8601(3), to: table["to"] || time_range.to.utc.iso8601(3),
             filters: values, **Table.compact(table) },
        link: links.dashboard(dashboard, range: time_range, values: values))
    end

    private

    # The filters argument as EventStore's parameters. A name the Dashboard
    # lacks is refused rather than run as All, as it is likely a slip.
    def filter_values
      given = (arguments[:filters] || {}).transform_keys(&:to_s)
      unknown = given.keys - dashboard.filter_names
      raise ToolError, "#{unknown.to_sentence} #{not_its_filters(unknown)}." if unknown.any?

      dashboard.parameters(given)
    end

    # The range given, or the Dashboard's default range, as the page first
    # shows it.
    def time_range
      @time_range ||= begin
        span = arguments[:range].presence || (dashboard.default_range unless arguments[:from].present? || arguments[:to].present?)
        TimeRange.parse(range: span, from: arguments[:from], to: arguments[:to])
      end
    end
  end
end
