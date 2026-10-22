# frozen_string_literal: true

module McpTools
  # A Project's Fields, the names a Query reads, as EventStore counts them.
  class ListFields < Base
    DEFAULT_SPAN = "24h"
    MAX_LIMIT = 500

    description "Lists a Project's Fields, the names a Query reads: the built-in fields with their types, then its " \
                "Attributes, most used first, each with its type (string, number or bool) and logs, how many Logs had it " \
                "on the UTC days the range touches. A name whose values have two types is listed once for each, and a " \
                "Query's value picks which it matches: status == 200 reads the numbers, status == \"200\" the strings. " \
                "The counts come from Logs as they arrive: a Log sent more than about 15 minutes after its time may not be counted, " \
                "and Attributes whose values are JSON, such as arrays, aren't listed. A Query still reads both."
    read_only!
    takes_project
    takes_range
    argument :range, RANGE.merge(description: RANGE[:description].sub("1h when", "#{DEFAULT_SPAN} when"))
    argument :limit, {
      type: "integer", minimum: 1, maximum: MAX_LIMIT,
      description: "How many Attributes to list: 100 when left out, at most #{MAX_LIMIT}.",
    }

    def run
      fields = EventStore.logs_fields(project_id: project.id, from: time_range.from, to: time_range.to, limit: arguments[:limit])
      attributes = Array(fields["attributes"])
      answer = { project: project.slug, from: time_range.from.utc.iso8601(3), to: time_range.to.utc.iso8601(3),
                 built_in: fields["built_in"], attributes: attributes }
      if fields["truncated"]
        answer[:more] = "More Attributes than these #{attributes.size}: give a larger limit, at most #{MAX_LIMIT}."
      end

      json(answer, link: links.explorer(project, range: time_range))
    end

    private

    def time_range
      @time_range ||= TimeRange.parse(
        range: arguments[:range].presence || (DEFAULT_SPAN unless arguments[:from].present? || arguments[:to].present?),
        from: arguments[:from], to: arguments[:to]
      )
    end
  end
end
