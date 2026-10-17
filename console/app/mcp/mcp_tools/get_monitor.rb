# frozen_string_literal: true

module McpTools
  # A Monitor with its groups, last runs and Integrations, as its page shows
  # it.
  class GetMonitor < Base
    include MonitorTool

    description "Gives a Monitor: its Project, Query, kind and settings, its status, each group's state (firing first, at most " \
                "#{STATES_SHOWN}), its last #{RUNS_SHOWN} runs, newest first, and the Integrations it tells."
    read_only!
    takes_monitor

    def run
      json(monitor_json(monitor, details: true), link: links.monitor(monitor))
    end
  end
end
