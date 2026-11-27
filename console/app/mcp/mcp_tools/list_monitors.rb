# frozen_string_literal: true

module McpTools
  # Lists an Organization's Monitors, as its Monitors page does.
  class ListMonitors < Base
    include MonitorTool

    description "Lists the Organization's Monitors by name: kind, language (query or sql), Project, whether it's on, status (disabled, firing, held, error or ok), " \
                "how many groups are firing and the first #{FIRING_LISTED} of them, its last run and error, and held_since, " \
                "when its runs started to be held while the Project's Logs are not complete. " \
                "get_monitor gives a Monitor's Query or SQL, groups and runs."
    read_only!
    takes_organization

    def run
      monitors = organization.monitors.includes(:project, :states, :integrations).order(:name)

      json({ organization: organization.slug, monitors: monitors.map { |m| summary(m) } }, link: links.monitors(organization))
    end

    private

    def summary(monitor)
      firing = monitor.states.select(&:status_firing?).sort_by(&:group_key).first(FIRING_LISTED)

      monitor_json(monitor).slice(:id, :name, :kind, :language, :project, :enabled, :status, :firing_count, :last_run_at, :last_error,
        :held_since)
        .merge(firing_groups: firing.map(&:group))
    end
  end
end
