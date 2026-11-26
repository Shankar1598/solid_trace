# frozen_string_literal: true

module McpTools
  # A Monitor with its groups, last runs and Integrations, as its page shows
  # it.
  class GetMonitor < Base
    include MonitorTool

    description "Gives a Monitor: its Project, its language (query or sql) and its Query or SQL, its kind and settings, its status " \
                "(disabled, firing, held, error or ok) and held_since, each group's state (firing first, at most " \
                "#{STATES_SHOWN}) with its last value and, comparing the change, its last Baseline, its last #{RUNS_SHOWN} " \
                "runs, newest first, and the Integrations it tells. A run's status is ok, error (with error) or held " \
                "(with held_reason): a threshold Monitor's run is held while the Project's Logs are not complete, such " \
                "as when Log processing is behind or after EventStore's restart. A held run compares nothing, changes no " \
                "group and tells no Integration, and the Monitor runs again in a minute."
    read_only!
    takes_monitor

    def run
      json(monitor_json(monitor, details: true), link: links.monitor(monitor))
    end
  end
end
