# frozen_string_literal: true

module McpTools
  # An Organization's Projects, as its Project pickers list them.
  class ListProjects < Base
    description "Lists an Organization's Projects: the slug other tools take, the name, " \
                "log_retention_days, how many days back its Logs go, and log_file_order, the Fields its Log files are " \
                "sorted by before time ([] is time only), so a Query filtered on them reads less. Only an admin " \
                "changes the order, on the Project's settings page."
    read_only!
    takes_organization

    def run
      listed = ProjectOptionSerializer.list(organization.projects.order(:name), include_log_file_order: true)

      json({ organization: organization.slug, projects: listed },
        link: links.projects(organization))
    end
  end
end
