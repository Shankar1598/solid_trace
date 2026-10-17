# frozen_string_literal: true

module McpTools
  # Deletes a Chart from a Dashboard.
  class DeleteChart < Base
    include DashboardTool

    description "Deletes a Chart from a Dashboard."
    destructive!
    takes_chart

    def run
      chart.destroy!

      text("Deleted Chart #{chart.id}, #{chart.title}, from Dashboard #{dashboard.name}.", link: links.dashboard(dashboard))
    end
  end
end
