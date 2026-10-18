# frozen_string_literal: true

module McpTools
  # Lists an Organization's Monitors, as its Monitors page does.
  class ListMonitors < Base
    include MonitorTool

    description "Lists the Organization's Monitors by name: kind, Project, whether it's on, status (disabled, firing, error or ok), " \
                "how many groups are firing and the first #{FIRING_LISTED} of them, and its last run and error. " \
                "get_monitor gives a Monitor's Query, groups and runs."
    read_only!
    takes_organization

    def run
      monitors = organization.monitors.includes(:project, :states, :integrations).order(:name)

      json({ organization: organization.slug, monitors: monitors.map { |m| summary(m) } }, link: links.monitors(organization))
    end

    private

    def summary(monitor)
      firing = monitor.states.select(&:status_firing?).sort_by(&:group_key).first(FIRING_LISTED)

      monitor_json(monitor).slice(:id, :name, :kind, :project, :enabled, :status, :firing_count, :last_run_at, :last_error)
        .merge(firing_groups: firing.map(&:group))
    end
  end
end
