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
                     "a Dashboard filter ($name), which only Dashboard Charts fill in, is not saved. Giving it makes the " \
                     "Monitor's language query.",
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

    # The sql argument, listed only while SQL queries are on, and what the
    # tools' descriptions say of it then.
    SQL = {
      type: "string",
      description: "A SQL query (ClickHouse SQL) that the Monitor runs in place of a Query. Only a threshold Monitor takes it. " \
                   "Use query first. Use sql only when the Monitor needs a join, a subquery, a window, or a read of the " \
                   "events or spans tables. The SQL reads the logs, events and spans tables of the Monitor's Project, as " \
                   "run_sql does. The range is set outside the SQL. Each run reads range_minutes that end 60 seconds before " \
                   "the run, so the SQL needs no time condition. Its table must be the group columns and one number column. " \
                   "For example: SELECT service, count() AS errors FROM logs WHERE level = 'error' GROUP BY service. The " \
                   "columns other than the number column name each group. EventStore runs the SQL before the Monitor is " \
                   "saved. SQL that does not run is not saved. A table without exactly one number column is not saved. " \
                   "Give sql or query, not both. Giving sql makes the Monitor's language sql.",
    }.freeze
    SQL_NOTE = "A threshold Monitor that needs a join, a subquery, a window, or the events or spans tables takes sql in " \
               "place of query."

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
      def takes_monitor(required: true)
        takes_organization
        argument :monitor, MONITOR, required: required
      end

      # The Monitor's fields, or only those of only, with sql after query,
      # listed only while SQL queries are on.
      def takes_monitor_fields(required: [], only: MONITOR_FIELDS.keys, note: SQL_NOTE)
        MONITOR_FIELDS.slice(*only).each do |name, schema|
          argument name, schema, required: required.include?(name)
          sql_argument :sql, SQL, note: note if name == :query
        end
      end
    end

    private

    # The Monitor the monitor argument names, in #organization.
    def monitor
      @monitor ||= organization.monitors.find(arguments[:monitor])
    end

    # The fields given in the arguments, as the Monitor takes them, with the
    # Project when one is named. query or sql sets the Monitor's language,
    # and its text.
    def monitor_attributes
      raise ToolError, "Give query or sql, not both: a Monitor runs one of them." if arguments.key?(:query) && arguments.key?(:sql)

      attributes = arguments.slice(*ATTRIBUTES)
      attributes[:language] = "query" if arguments.key?(:query)
      attributes.merge!(language: "sql", query: arguments[:sql]) if arguments.key?(:sql)
      attributes[:project] = project if arguments.key?(:project)
      attributes
    end

    # Refuses a new Monitor without its text: query, or sql while SQL queries
    # are on.
    def needs_text!
      return if arguments.key?(:query) || arguments.key?(:sql)

      raise ToolError, InstanceSetting.current.sql_queries_enabled? ? "Give query, or sql for a Monitor that needs SQL." : "Give query."
    end

    # The changes the arguments make to monitor, as #monitor_attributes
    # gives them. A new project points a Query's source at it, as the
    # Monitor form does, unless a query or sql is given; a SQL Monitor's
    # SQL reads the new Project's tables as it is.
    def monitor_changes(monitor)
      changes = monitor_attributes
      if arguments.key?(:project) && !arguments.key?(:query) && !arguments.key?(:sql) && monitor.language_query?
        changes[:query] = with_source(monitor.query, project.slug)
      end
      changes
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
