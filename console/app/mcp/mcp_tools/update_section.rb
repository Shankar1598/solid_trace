# frozen_string_literal: true

module McpTools
  # Renames a Section, or moves it to another position among the Dashboard's
  # Sections.
  class UpdateSection < Base
    include DashboardTool

    description "Changes a Section of a Dashboard: its title, or its position among the Sections, which moves the others " \
                "to make room. Only the fields given change. get_dashboard lists the Sections."
    writes!
    takes_section
    argument :title, { type: "string", description: "The Section's title, at most #{DashboardSection::MAX_TITLE} characters." }
    argument :position, SECTION_POSITION

    def run
      raise ToolError, "Give at least one of title, position to change." unless arguments.key?(:title) || arguments.key?(:position)

      section.title = arguments[:title] if arguments.key?(:title)
      section.save_at!(arguments[:position])
      json(section_json(section.reload), link: links.dashboard(dashboard))
    end
  end
end
