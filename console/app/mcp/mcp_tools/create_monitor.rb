# frozen_string_literal: true

module McpTools
  # Makes a Monitor with its Integrations, as the New Monitor form does.
  class CreateMonitor < Base
    include MonitorTool

    description "Makes a Monitor on one of the Organization's Projects. It is on, and runs every interval_minutes, telling " \
                "the Integrations of integration_ids what it finds. A threshold Monitor needs operator and threshold; one comparing " \
                "the change from its Baseline also needs compare change and baseline, with rises, falls or changes and a percent. " \
                "EventStore checks the Query before it's saved. run_monitor runs it now. #{READING_LESS}"
    writes!
    takes_project
    takes_monitor_fields required: %i[name kind query]

    def run
      made = organization.monitors.new
      save_monitor!(made, monitor_attributes)

      json(monitor_json(made), link: links.monitor(made))
    end
  end
end
