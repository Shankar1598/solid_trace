# frozen_string_literal: true

module McpTools
  # An Organization's Projects, as its Project pickers list them.
  class ListProjects < Base
    description "Lists an Organization's Projects: the slug other tools take, the name, and " \
                "log_retention_days, how many days back its Logs go."
    read_only!
    takes_organization

    def run
      listed = ProjectOptionSerializer.list(organization.projects.order(:name))

      json({ organization: organization.slug, projects: listed },
        link: links.projects(organization))
    end
  end
end
