# frozen_string_literal: true

module McpTools
  # Runs SQL on a Project's Logs, as the explorer's SQL mode does. The server
  # lists it only while SQL queries are on. EventStore runs it as the
  # Project's SQL user, which sees only the Project's logs and events.
  class RunSql < Base
    description <<~TEXT.squish
      Runs a ClickHouse SQL SELECT on the logs table, which holds the Project's Logs over the range and no others, and
      answers the table: #{Table::SHAPE}. Its columns are id, time, level, message, service, environment, trace_id,
      span_id and file. attributes holds each string, number and true/false Attribute at its name, read with its
      type: attributes.user.:String, attributes.http.status.:Float64, attributes.cached.:Bool (NULL when a Log
      lacks it or holds another type). attrs_json holds array and object Attributes as JSON text, read as
      attrs_json['tags']. An events table holds the Project's Events over the range: uuid, issue_fingerprint_id,
      timestamp, environment, server_name, release, level and tags. The SQL runs as a read-only database user that
      sees only those two tables of this Project: other tables, system tables, files, URLs and setting changes are
      refused, and a query stops after 10 seconds. query_logs does most things more simply.
    TEXT
    read_only!
    needs_sql_queries!
    takes_project
    argument :sql, { type: "string", description: "The SQL, such as SELECT service, count(*) AS logs FROM logs GROUP BY service." },
      required: true
    takes_range

    def run
      answer = ChartRuns.hold do
        EventStore.run_logs_sql(
          project_id: project.id, sql: arguments[:sql].to_s, from: time_range.from, to: time_range.to,
          timeout_seconds: EventStoreProxy::SQL_TIMEOUT_SECONDS
        )
      end

      json({ project: project.slug, from: time_range.from.utc.iso8601(3), to: time_range.to.utc.iso8601(3),
             **Table.compact(answer["table"] || {}) },
        link: links.explorer(project, range: time_range, sql: arguments[:sql].to_s))
    end
  end
end
