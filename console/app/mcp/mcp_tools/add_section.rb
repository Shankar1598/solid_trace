# frozen_string_literal: true

module McpTools
  # Adds a Section to a Dashboard, last unless a position is given, as the
  # Dashboard page's Add section does.
  class AddSection < Base
    include DashboardTool

    description "Adds a Section to a Dashboard: a title over a group of its Charts, which the page shows under the Charts " \
                "in no Section, and a viewer can fold. It goes last unless position is given, and has no Charts: add_chart " \
                "and update_chart take its id as section_id. A Dashboard has at most #{Dashboard::MAX_SECTIONS} Sections."
    writes!
    takes_dashboard
    argument :title, { type: "string", description: "The Section's title, at most #{DashboardSection::MAX_TITLE} characters." }, required: true
    argument :position, SECTION_POSITION

    def run
      added = dashboard.sections.new(title: arguments[:title])
      added.save_at!(arguments[:position])

      json(section_json(added), link: links.dashboard(dashboard))
    end
  end
end
