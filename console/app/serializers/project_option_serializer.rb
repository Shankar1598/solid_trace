# frozen_string_literal: true

# A Project as the explorer's and Dashboards' Project pickers know it.
# log_retention_days is how far back its Logs go, its own Retention or the
# instance's default, so the time-range picker can turn away longer ranges.
# With include_log_file_order, it also has the Log file order in force.
class ProjectOptionSerializer
  def self.list(projects, include_log_file_order: false)
    default = InstanceSetting.current.default_log_retention_days
    projects.map do |project|
      new(project, default_log_retention_days: default, include_log_file_order: include_log_file_order).as_json
    end
  end

  def initialize(project, default_log_retention_days: nil, include_log_file_order: false)
    @project = project
    @default_log_retention_days = default_log_retention_days
    @include_log_file_order = include_log_file_order
  end

  def as_json(*)
    json = {
      id: @project.id,
      name: @project.name,
      slug: @project.slug,
      log_retention_days: @project.log_retention_days || @default_log_retention_days || InstanceSetting.current.default_log_retention_days,
    }
    json[:log_file_order] = @project.log_file_order_in_force if @include_log_file_order
    json
  end

  def to_json(*)
    as_json.to_json
  end
end
