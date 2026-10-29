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

      text([ "Ran the Monitor: #{ran.found}.", *changes ].join("\n"), link: links.monitor(monitor))
    end

    private

    # Comparing the change, each group as the run left it, firing first:
    # its number, Baseline and change.
    def changes
      return [] unless monitor.compares_the_change?

      states = monitor.states.reload.sort_by { |state| [ state.status_firing? ? 0 : 1, state.group_key ] }
      lines = states.first(STATES_SHOWN).map do |state|
        group = Notifiers::MonitorMessage.group_text(state.group_key)
        reading = Notifiers::MonitorMessage.reading_text(state.last_value, state.last_baseline)
        "- #{[ group.presence, reading ].compact.join(': ')}, #{state.status}"
      end
      lines << "- and #{states.size - STATES_SHOWN} more groups: get_monitor gives them." if states.size > STATES_SHOWN
      lines
    end
  end
end
