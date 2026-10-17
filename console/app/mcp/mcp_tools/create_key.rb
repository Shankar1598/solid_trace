# frozen_string_literal: true

module McpTools
  # Gives a Project another key, as the Project page's Generate New Key does.
  # For the Organization's admins.
  class CreateKey < Base
    include ProjectKeyTool

    description "Gives a Project another key, with its own DSN, so a sender can move to it before the old one is " \
                "rotated or deleted. Only an Organization admin can."
    writes!
    takes_project

    def run
      made = project.project_keys.build
      authorize!(made, :create?, "make a Project's keys")
      made.save!

      key_answer(made)
    end
  end
end
