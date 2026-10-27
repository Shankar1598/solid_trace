# frozen_string_literal: true

# A Project as the explorer's and Dashboards' Project pickers know it.
# log_retention_days is how far back its Logs go, its own Retention or the
# instance's default, so the time-range picker can turn away longer ranges;
# trace_retention_days is the same for its Spans.
# description is what it holds, for agents over MCP to pick the Project to query.
class ProjectOptionSerializer
  def self.list(projects)
    settings = InstanceSetting.current
    projects.map { |project| new(project, settings: settings).as_json }
  end

  def initialize(project, settings: nil)
    @project = project
    @settings = settings
  end

  def as_json(*)
    {
      id: @project.id,
      name: @project.name,
      slug: @project.slug,
      description: @project.description,
      log_retention_days: @project.log_retention_days || settings.default_log_retention_days,
      trace_retention_days: @project.trace_retention_days || settings.default_trace_retention_days,
    }
  end

  def to_json(*)
    as_json.to_json
  end

  private

  def settings
    @settings ||= InstanceSetting.current
  end
end
