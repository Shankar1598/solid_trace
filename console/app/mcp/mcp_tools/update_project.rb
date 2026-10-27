# frozen_string_literal: true

module McpTools
  # Changes a Project's settings, as its Project page does: its description and
  # its Log and trace Retention. For the Organization's admins (ProjectPolicy).
  class UpdateProject < Base
    description "Changes a Project's description, which says what it holds so agents can tell which Project to query, " \
                "or how many days back its Logs and its traces' Spans are kept. Arguments left out stay as they are. Only an Organization admin can."
    writes!
    takes_project
    argument :description, {
      type: "string",
      description: "What the Project holds, such as the checkout service's errors and Logs. An empty string clears it.",
    }
    argument :log_retention_days, { type: "integer", description: "How many days back its Logs are kept." }
    argument :trace_retention_days, { type: "integer", description: "How many days back its traces' Spans are kept." }

    def run
      authorize!(project, :update?, "change a Project's settings")
      project.update!(arguments.slice(:description, :log_retention_days, :trace_retention_days))

      json({ organization: organization.slug, project: ProjectOptionSerializer.new(project).as_json },
        link: links.project(project))
    end
  end
end
