# frozen_string_literal: true

module McpTools
  # Runs a Chart's saved Query over a range, as the Dashboard page does, and
  # answers the table it draws.
  class GetChartData < Base
    include DashboardTool

    description "Runs a Chart's saved Query on its Project over a range and answers the table the Chart draws: " \
                "#{Table::SHAPE}."
    read_only!
    takes_chart
    takes_range
    argument :range, RANGE.merge(description: RANGE[:description].sub("1h when", "The Dashboard's default_range when"))

    def run
      answer = ChartRuns.run_chart(organization.projects.find(chart.project_id), chart.query, from: time_range.from, to: time_range.to)
      table = answer["table"]

      json({ chart: chart.id, title: chart.title, viz: chart.viz,
             from: table["from"] || time_range.from.utc.iso8601(3), to: table["to"] || time_range.to.utc.iso8601(3),
             **Table.compact(table) },
        link: links.dashboard(dashboard, range: time_range))
    end

    private

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
