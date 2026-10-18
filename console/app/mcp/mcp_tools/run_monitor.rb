# frozen_string_literal: true

module McpTools
  # Run now: runs a Monitor at once, as its schedule would, while the call
  # waits, and says what the run found, as the Monitor's page does.
  class RunMonitor < Base
    include MonitorTool

    description "Runs a Monitor now, as its schedule would, and says what the run found: how many groups are firing, or how " \
                "many Logs matched. Like a scheduled run, it tells the Monitor's Integrations what changed. get_monitor shows the run."
    writes!(open_world: true)
    takes_monitor

    def run
      raise ToolError, "Turn the Monitor on to run it: update_monitor with enabled true." unless monitor.enabled?

      ran = MonitorEvaluation.run(monitor, Time.current)
      raise ToolError, "The Monitor is running now: get_monitor shows that run when it ends." if ran.nil?
      raise ToolError, "The run failed: #{ran.error}" if ran.status_error?

      text("Ran the Monitor: #{ran.found}.", link: links.monitor(monitor))
    end
  end
end
