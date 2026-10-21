# frozen_string_literal: true

module McpTools
  # Runs SQL on a Project's Logs, as the explorer's SQL mode does. The server
  # lists it only while SQL queries are on, and it only reads its Project's
  # views: EventStore refuses any SQL but one SELECT of them. EventStore's
  # notice, on SQL that matches message slowly, is passed on.
  class RunSql < Base
    description <<~TEXT.squish
      Runs a DuckDB SQL SELECT on the logs view, which holds a Project's Logs over a range, and answers the table:
      #{Table::SHAPE}. The view's columns are id, time, level, message, service,
      environment, trace_id, span_id and the Attributes by type, read as attrs_string['name'], attrs_number['name'],
      attrs_bool['name'] and attrs_json['name']. An events view holds the Project's Events over the range. SQL is
      read-only, and reads only those two views: anything but one SELECT, and any table, file, URL or table function
      but range, generate_series and unnest, is refused. Match text in message with contains(lower(message), 'x') or
      starts_with, not ILIKE, which is several times slower; an answer that matched slowly carries a notice saying
      what to write instead. query_logs does most things more simply.
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
             **Table.compact(answer["table"] || {}), **answer.slice("notice").symbolize_keys },
        link: links.explorer(project, range: time_range, sql: arguments[:sql].to_s))
    end
  end
end
