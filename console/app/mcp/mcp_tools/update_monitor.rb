# frozen_string_literal: true

module McpTools
  # Changes a Monitor and its Integrations, as the Edit Monitor form does,
  # and turns it off and on, as its page does.
  class UpdateMonitor < Base
    include MonitorTool

    description "Changes a Monitor. Only the fields given change. A new project points the Query's source at it, as the " \
                "Monitor form does, unless a query is given. integration_ids replaces the Integrations it tells. enabled false " \
                "turns it off, which resolves its firing groups; enabled true turns it on, which checks its Query again, and a " \
                "Query that no longer runs keeps it off."
    writes!(open_world: true)
    takes_monitor
    argument :project, { type: "string", description: "The slug of the Project the Monitor's Query runs on. list_projects lists them." }
    takes_monitor_fields
    argument :enabled, { type: "boolean", description: "Whether the Monitor is on and runs." }

    CHANGES = [ :project, :enabled, *MONITOR_FIELDS.keys ].freeze

    def run
      raise ToolError, "Give at least one of #{CHANGES.join(', ')} to change." unless arguments.keys.intersect?(CHANGES)

      turning_on = switches? && arguments[:enabled]
      changes = monitor_attributes
      changes[:query] = with_source(monitor.query, project.slug) if arguments.key?(:project) && !arguments.key?(:query)
      changes[:disabled_at] = (turning_on ? nil : Time.current) if switches?
      save_monitor!(monitor, changes, refused: turning_on ? "Not saved, so the Monitor stays off" : "Not saved")

      json(monitor_json(monitor), link: links.monitor(monitor))
    end

    private

    # Whether enabled turns the Monitor off or on, rather than leaving it be.
    def switches?
      arguments.key?(:enabled) && arguments[:enabled] != monitor.enabled?
    end
  end
end
