# frozen_string_literal: true

module McpTools
  # What the Monitor tools share: the monitor argument, the Monitor it names
  # in the Organization, a Monitor's fields, and a Monitor as the tools answer
  # it. Any member of the Organization can see, make, change, turn off and on,
  # run and delete its Monitors, as in the UI.
  module MonitorTool
    extend ActiveSupport::Concern

    MONITOR = { type: "integer", description: "The Monitor's id. list_monitors lists them." }.freeze

    MONITOR_FIELDS = {
      name: { type: "string", description: "The Monitor's name, at most 200 characters." },
      kind: {
        type: "string",
        enum: LogMonitor.kinds.keys,
        description: "threshold: the Query ends in a summarize of one number per row, such as | summarize count() by service. " \
                     "With notify_by_group, each row is a group that fires while its number passes the threshold and resolves " \
                     "when it no longer does; without it, the worst row's number is the Monitor's one group. " \
                     "match: the Query returns Logs, and each run sends the Logs ingested since the last run that match it.",
      },
      query: {
        type: "string",
        description: "The Query the Monitor runs on its Project. EventStore checks it as kind needs before it's saved: " \
                     "a Query it can't run, a threshold Query that doesn't end in a summarize of one number, or a Query naming " \
                     "a Dashboard filter ($name), which only Dashboard Charts fill in, is not saved.",
      },
      interval_minutes: { type: "integer", description: "How often the Monitor runs, in minutes, 1 to #{LogMonitor::MAX_INTERVAL_MINUTES}. 1 when made." },
      range_minutes: {
        type: "integer",
        description: "How far back a threshold Monitor's run reads, in minutes, 1 to #{LogMonitor::MAX_RANGE_MINUTES}, " \
                     "ending 60 seconds before the run. 5 when made.",
      },
      compare: {
        type: "string",
        enum: LogMonitor.compares.keys,
        description: "What a threshold Monitor compares with threshold. value: each group's number. change: each group's percent " \
                     "change from its Baseline, its number over the range of baseline, so the Query runs twice per run. Switching " \
                     "between them resolves the Monitor's firing groups. value when made.",
      },
      baseline: {
        type: "string",
        enum: LogMonitor::BASELINES,
        description: "Where a Monitor comparing the change finds each group's Baseline: previous, the range of range_minutes " \
                     "just before the one it reads; day or week, the same range 1 day or 1 week earlier, so daily or weekly " \
                     "cycles don't fire it. Needed with compare change. day can't take a range over 1 day, and range_minutes plus " \
                     "the Baseline's offset can't be longer than the Project's log_retention_days: saving refuses both. A group " \
                     "with no row in the Baseline rises from 0, which counts as the biggest rise; 0 then and now is no change. A " \
                     "group with no row now is ok, so falls misses a group that stops sending: alert_on_no_data, or a Query " \
                     "without groups, catches that.",
      },
      minimum: {
        type: %w[number null],
        description: "Comparing the change, keeps small numbers quiet: a group is compared only when the larger of its number " \
                     "and its Baseline, by size, is at least minimum, and is ok otherwise, so a drop from 1,000 to 0 still " \
                     "falls. Greater than 0; null for none, which compares every group. None when made.",
      },
      operator: {
        type: "string",
        enum: LogMonitor::OPERATORS + LogMonitor::CHANGE_OPERATORS,
        description: "How a threshold Monitor compares each group with threshold. Comparing the value (>, >=, <, <=): the group " \
                     "fires while number operator threshold holds. Comparing the change (rises, falls, changes): it fires while " \
                     "its change is at least threshold percent up, down, or either way.",
      },
      threshold: {
        type: "number",
        description: "The number a threshold Monitor compares its groups' numbers with, or comparing the change, a percent " \
                     "greater than 0, such as 50 for 50%.",
      },
      notify_by_group: {
        type: "boolean",
        description: "Whether each row of a threshold Monitor's table is its own group, named by its other columns, firing and " \
                     "resolving on its own. Otherwise the worst number of all rows is the Monitor's one group. false when made.",
      },
      alert_on_no_data: { type: "boolean", description: "Whether a threshold Monitor fires when its table has no rows. false when made." },
      integration_ids: {
        type: "array",
        items: { type: "integer" },
        description: "The ids of the Integrations the Monitor tells what it finds, which replace those it has. " \
                     "Give [] for none. list_integrations lists them.",
      },
    }.freeze

    # The fields the Monitor takes as attributes; integration_ids is saved
    # with them.
    ATTRIBUTES = (MONITOR_FIELDS.keys - [ :integration_ids ]).freeze

    # The firing groups list_monitors names per Monitor, the groups
    # get_monitor gives, and the runs it gives, newest first.
    FIRING_LISTED = 5
    STATES_SHOWN = 100
    RUNS_SHOWN = 10

    class_methods do
      # The organization and monitor arguments, which #monitor reads.
      def takes_monitor
        takes_organization
        argument :monitor, MONITOR, required: true
      end

      def takes_monitor_fields(required: [])
        MONITOR_FIELDS.each { |name, schema| argument name, schema, required: required.include?(name) }
      end
    end

    private

    # The Monitor the monitor argument names, in #organization.
    def monitor
      @monitor ||= organization.monitors.find(arguments[:monitor])
    end

    # The fields given in the arguments, as the Monitor takes them, with the
    # Project when one is named.
    def monitor_attributes
      attributes = arguments.slice(*ATTRIBUTES)
      attributes[:project] = project if arguments.key?(:project)
      attributes
    end

    # Saves monitor with attributes and the integration_ids given, as the
    # Monitor form does (LogMonitor#save_with), or raises saying why not,
    # after refused.
    def save_monitor!(monitor, attributes, refused: "Not saved")
      return if monitor.save_with(attributes, integration_ids: arguments[:integration_ids])

      raise ToolError, "#{refused}: #{monitor.errors.full_messages.join('; ').delete_suffix('.')}."
    end

    # A Monitor as its page knows it, with the slug of its Project, which the
    # tools take. details adds its groups, firing first, its last runs and its
    # Integrations.
    def monitor_json(monitor, details: false)
      json = MonitorSerializer.new(monitor, include_details: details).as_json.merge(project: monitor.project.slug)
      return json unless details

      states = json[:states]
      json[:states] = states.first(STATES_SHOWN)
      json[:more_states] = "#{states.size - STATES_SHOWN} more groups: the Monitor's page lists them." if states.size > STATES_SHOWN
      json[:runs] = json[:runs].first(RUNS_SHOWN)
      json
    end
  end
end
