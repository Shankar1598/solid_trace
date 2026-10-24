# frozen_string_literal: true

# A Project as the explorer's and Dashboards' Project pickers know it.
# log_retention_days is how far back its Logs go, its own Retention or the
# instance's default, so the time-range picker can turn away longer ranges.
# description is what it holds, for agents over MCP to pick the Project to query.
class ProjectOptionSerializer
  def self.list(projects)
    default = InstanceSetting.current.default_log_retention_days
    projects.map { |project| new(project, default_log_retention_days: default).as_json }
  end

  def initialize(project, default_log_retention_days: nil)
    @project = project
    @default_log_retention_days = default_log_retention_days
  end

  def as_json(*)
    {
      id: @project.id,
      name: @project.name,
      slug: @project.slug,
      description: @project.description,
      log_retention_days: @project.log_retention_days || @default_log_retention_days || InstanceSetting.current.default_log_retention_days,
    }
  end

  def to_json(*)
    as_json.to_json
  end
end
