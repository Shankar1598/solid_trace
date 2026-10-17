# frozen_string_literal: true

module McpTools
  # One of a Project's Logs, whole, as the explorer's inspector shows it.
  class GetLog < Base
    description "Answers one of a Project's Logs whole: its time, level, message, service, environment, trace_id, " \
                "span_id and every Attribute, as EventStore keeps them. query_logs' lines give a Log's id and time."
    read_only!
    takes_project
    argument :log, { type: "string", description: "The Log's id, from a query_logs line." }, required: true
    argument :time, {
      type: "string",
      description: "The Log's own time, from the same line, as an ISO 8601 time. EventStore then looks only in that hour; " \
                   "without it, in every file of the Project, which takes longer.",
    }

    def run
      log = EventStore.get_log(project_id: project.id, log_id: arguments[:log].to_s, time: time&.utc&.iso8601(9))
      unless log
        raise ToolError, "No Log #{arguments[:log]} in #{project.slug}#{' in the hour of time' if time}. " \
                         "query_logs gives Logs' ids and times."
      end

      json(log, link: link(log))
    end

    private

    def time
      return if arguments[:time].blank?

      @time ||= Time.iso8601(arguments[:time].to_s)
    rescue ArgumentError
      raise ToolError, "time must be an ISO 8601 time, such as the Log's time in its query_logs line."
    end

    # The explorer over the second of the Log's time, which shows it among
    # the few Logs of that second.
    def link(log)
      second = Time.iso8601(log["time"].to_s).utc.floor
      links.explorer(project, range: TimeRange.new(from: second, to: second + 1.second))
    rescue ArgumentError
      links.explorer(project, range: TimeRange.parse)
    end
  end
end
