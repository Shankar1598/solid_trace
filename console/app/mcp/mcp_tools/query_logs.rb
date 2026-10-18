# frozen_string_literal: true

module McpTools
  # Runs a Query on a Project's Logs, as the Logs explorer does: a page of
  # Logs, one line each, with the cursor of the next page, or a table.
  class QueryLogs < Base
    DEFAULT_LIMIT = 50
    MAX_LIMIT = 200

    description <<~TEXT.squish
      Runs a Query on a Project's Logs over a range, and links to the Logs explorer showing it.
      A Query is stages joined by |, such as where level == "error" | summarize count() by bin_auto(time), service.
      Don't name the Project in it: the project argument picks it.
      where keeps the Logs a condition is true for: ==, !=, <, >, contains, startswith, in (...), and, or, not(...).
      search "text" looks in the message and string Attributes.
      A number only matches a number: status == 200 misses the string "200", so write toint(status) == 200 to match either.
      summarize count() by bin_auto(time) counts over time; summarize count() by service | sort by count_ desc | take 10
      gives the top Services; avg, sum, min, max, dcount and percentile(x, 95) work too.
      A Query of only where, search, take and sort by time desc answers Logs, newest first, one a line, limit at a time,
      with a cursor for older ones. Any other stage, such as summarize, project, extend, top, count or distinct,
      answers a table, of which you get #{Table::SHAPE}.
      list_fields lists the Project's Fields, and query_language_reference gives the whole language.
    TEXT
    read_only!
    takes_project
    argument :query, { type: "string", description: "The Query. Empty or left out, it reads every Log." }
    takes_range
    argument :limit, {
      type: "integer", minimum: 1, maximum: MAX_LIMIT,
      description: "How many Logs a page holds: #{DEFAULT_LIMIT} when left out, at most #{MAX_LIMIT}. A table ignores it.",
    }
    argument :cursor, {
      type: "string",
      description: "The cursor an answer gave, for the page of older Logs. Give the same query, from and to with it.",
    }

    def run
      answer = ChartRuns.hold do
        EventStore.query_logs(
          project_id: project.id, query: query, from: time_range.from, to: time_range.to,
          cursor: arguments[:cursor].presence, limit: arguments[:limit] || DEFAULT_LIMIT,
          timeout_seconds: EventStoreProxy::RUN_TIMEOUT_SECONDS
        )
      end
      link = links.explorer(project, range: time_range, query: query)
      answer["table"].is_a?(Hash) ? table_answer(answer["table"], link) : logs_answer(answer, link)
    end

    private

    def query
      arguments[:query].to_s
    end

    def table_answer(table, link)
      json({ project: project.slug, from: table["from"] || from, to: table["to"] || to, **Table.compact(table) }, link: link)
    end

    # A header, a line per Log and, when there are older Logs, how to read
    # them. EventStore may answer fewer Logs than the limit, or none, with a
    # cursor, when it stops reading at its scan budget.
    def logs_answer(answer, link)
      logs = Array(answer["logs"])
      cursor = answer["next_cursor"].presence
      lines = [ "#{logs.size} Logs of #{project.slug} from #{from} to #{to}, newest first. " \
                "A line is #{LogLine::PARTS}; get_log gives the whole Log." ]
      lines += logs.map { |log| LogLine.of(log) }
      if cursor
        lines << "Older Logs: call query_logs again with the same query, cursor #{JSON.generate(cursor)}, from #{from} and to #{to}."
      end
      text(lines.join("\n"), link: link)
    end

    def from
      time_range.from.utc.iso8601(3)
    end

    def to
      time_range.to.utc.iso8601(3)
    end
  end
end
