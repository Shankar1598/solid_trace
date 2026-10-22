# frozen_string_literal: true

module McpTools
  # An Organization's Projects, as its Project pickers list them.
  class ListProjects < Base
    description "Lists an Organization's Projects: the slug other tools take, the name, " \
                "log_retention_days, how many days back its Logs go, and log_file_order, the Fields each hour's Log file " \
                "is sorted by before time, so a Query filtered on them reads less ([] is time only). Only an admin " \
                "changes the order, on the Project's settings page."
    read_only!
    takes_organization

    def run
      projects = organization.projects.order(:name)

      default = InstanceSetting.current.default_log_retention_days
      listed = projects.map do |project|
        ProjectOptionSerializer.new(project, default_log_retention_days: default).as_json
          .merge(log_file_order: project.log_file_order || Project::DEFAULT_LOG_FILE_ORDER)
      end

      json({ organization: organization.slug, projects: listed },
        link: links.projects(organization))
    end
  end
end
