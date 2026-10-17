# frozen_string_literal: true

module McpTools
  # Deletes a Monitor, which resolves its firing groups first.
  class DeleteMonitor < Base
    include MonitorTool

    description "Deletes a Monitor with its groups and runs. Its firing groups are resolved first, which tells its Integrations."
    destructive!(open_world: true)
    takes_monitor

    def run
      monitor.destroy!

      text("Deleted Monitor #{monitor.id}, #{monitor.name}.", link: links.monitors(organization))
    end
  end
end
