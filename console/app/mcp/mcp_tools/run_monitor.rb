# frozen_string_literal: true

module McpTools
  # Run now: runs a Monitor at once, as its schedule would, while the call
  # waits, and says what the run found, as the Monitor's page does. With
  # preview, it reads what a threshold Monitor's run now would, as the
  # Monitor form's preview does (MonitorEvaluation.preview), for a Monitor
  # not saved yet or a saved one with changes, and saves and tells nothing.
  class RunMonitor < Base
    include MonitorTool

    # The fields a preview takes: those a threshold Monitor's run reads.
    PREVIEW_FIELDS = %i[query range_minutes compare baseline minimum operator threshold notify_by_group alert_on_no_data].freeze

    description "Runs a Monitor now, as its schedule would, and says what the run found: how many groups are firing, or how " \
                "many Logs matched, and comparing the change, each group's number, Baseline and change. Like a scheduled run, " \
                "it tells the Monitor's Integrations what changed, or it is held while the Project's Logs are not complete, " \
                "and says why. get_monitor shows the run. With preview true, it saves nothing, tells no Integration and is " \
                "never held: it reads what a threshold Monitor's run now would and gives each group's " \
                "number, Baseline and change, and whether it would fire, before you save it. Give project and the fields " \
                "create_monitor would take for a new Monitor, or monitor and the fields to change for a saved one."
    writes!(open_world: true)
    takes_monitor(required: false)
    argument :preview, {
      type: "boolean",
      description: "true previews the Monitor the other arguments give, saving nothing and telling no Integration.",
    }
    argument :project, {
      type: "string",
      description: "With preview, the slug of the Project a new Monitor runs on, or a saved Monitor's new Project. " \
                   "list_projects lists them.",
    }
    takes_monitor_fields only: PREVIEW_FIELDS, note: "A preview takes sql in place of query, as create_monitor does."

    PREVIEW_ARGUMENTS = [ :project, *PREVIEW_FIELDS, :sql ].freeze

    def run
      return preview if arguments[:preview]
      raise ToolError, "Give monitor to run it, or preview true to preview a Monitor's settings." unless arguments.key?(:monitor) &&
        !arguments.keys.intersect?(PREVIEW_ARGUMENTS)
      raise ToolError, "Turn the Monitor on to run it: update_monitor with enabled true." unless monitor.enabled?

      ran = MonitorEvaluation.run(monitor, Time.current)
      raise ToolError, "The Monitor is running now: get_monitor shows that run when it ends." if ran.nil?
      raise ToolError, "The run failed: #{ran.error}" if ran.status_error?
      return text("The run is held, so it compared nothing: #{ran.held_reason}", link: links.monitor(monitor)) if ran.status_held?

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

    # What a run now would find for the Monitor previewed: the ranges it
    # reads, how many groups would fire, and each group, firing first.
    def preview
      previewed = previewed_monitor
      found = ChartRuns.hold { MonitorEvaluation.preview(previewed, Time.current) }
      groups = found.groups.sort_by { |group| [ group[:firing] ? 0 : 1, group[:group].to_json ] }.map do |group|
        next group unless previewed.compares_the_change?

        group.merge(change: LogMonitor.change_fact(group[:value], group[:baseline]))
      end
      baseline_from, baseline_to = found.baseline_range&.map { |at| at.utc.iso8601 }
      answer = { from: found.from.utc.iso8601, to: found.to.utc.iso8601, baseline_from: baseline_from, baseline_to: baseline_to,
                 firing: groups.count { |group| group[:firing] }, groups: groups.first(STATES_SHOWN) }.compact
      answer[:more_groups] = "#{groups.size - STATES_SHOWN} more groups." if groups.size > STATES_SHOWN
      json(answer, link: previewed.persisted? ? links.monitor(previewed) : links.monitors(organization))
    rescue MonitorEvaluation::Unusable => e
      raise ToolError, e.message
    end

    # The Monitor monitor names with the changes given, or a new threshold
    # Monitor on project, not saved. A rule saving would refuse is refused
    # before EventStore is asked.
    def previewed_monitor
      previewed = arguments.key?(:monitor) ? monitor : new_monitor
      previewed.assign_attributes(monitor_changes(previewed))
      unless previewed.kind_threshold?
        raise ToolError, "Only a threshold Monitor previews: a match Monitor's run reads the Logs ingested since its last run."
      end

      problems = previewed.rule_problems
      raise ToolError, "Saving would refuse it: #{problems.join('. ')}." if problems.any?

      previewed
    end

    def new_monitor
      raise ToolError, "Give project for a new Monitor, or monitor for a saved one." unless arguments.key?(:project)
      needs_text!

      organization.monitors.new(kind: "threshold")
    end
  end
end
