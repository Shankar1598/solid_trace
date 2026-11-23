# frozen_string_literal: true

module McpTools
  # Deletes a Section of a Dashboard. Its Charts stay on the Dashboard, in no
  # Section.
  class DeleteSection < Base
    include DashboardTool

    description "Deletes a Section of a Dashboard. Its Charts are not deleted: they stay on the Dashboard in no Section, " \
                "under the Charts there, in the same places to each other."
    destructive!
    takes_section

    def run
      charts = section.charts.size
      section.destroy!

      text("Deleted Section #{section.id}, #{section.title}, from Dashboard #{dashboard.name}. " \
           "Its #{charts} #{'Chart'.pluralize(charts)} #{charts == 1 ? 'is' : 'are'} now in no Section.", link: links.dashboard(dashboard))
    end
  end
end
