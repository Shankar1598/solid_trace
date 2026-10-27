# frozen_string_literal: true

module McpTools
  # One trace across the Organization's Projects, as the trace page shows it,
  # summarised by TraceView so that a trace of thousands of Spans fits.
  class GetTrace < Base
    description "Answers a trace, every Span, Log and Event sharing its id across the Organization's Projects, summarised: " \
                "transactions, its Transactions (each segment's Project, name and duration); spans, its Spans with " \
                "repeats in one parent collapsed into one row (count, and durations and self times as totals), the most " \
                "self time first, each with its parent, depth, Project, op, description, start from the trace's " \
                "(offset_us) and critical when it's on the critical path, the Spans the request's end waited on; " \
                "critical_path, the critical rows' span ids in start order; left_out, the Transactions, critical path " \
                "rows, rows and Spans not given, as the three lists share #{TraceView::SUMMARY_ROWS} rows; issues, its Issues with their Events in it, for get_issue (project, issue, " \
                "event); and trace_logs, the query_logs arguments that read its Logs in each Project with any. " \
                "sampled is false for a trace whose SDK sent no Transaction: give time, such as its Event's or Log's, " \
                "to find its Logs, or they're looked for around its first Event. event_store says whether EventStore " \
                "answered. get_issue's trace_id and a Log's trace_id are trace ids."
    read_only!
    takes_organization
    argument :trace, { type: "string", description: "The trace id: 32 hex digits." }, required: true
    argument :time, { type: "string", description: "When the trace happened, as an ISO 8601 time, such as its Event's or Log's time." }

    def run
      trace_id = arguments[:trace].to_s.downcase
      raise ToolError, "trace must be a trace id, 32 hex digits." unless trace_id.match?(/\A\h{32}\z/)

      trace = TraceView.new(organization: organization, trace_id: trace_id, time: time)
      json(trace.summary, link: links.trace(organization, trace_id, time: time))
    end

    private

    def time
      return if arguments[:time].blank?

      @time ||= Time.iso8601(arguments[:time].to_s)
    rescue ArgumentError
      raise ToolError, "time must be an ISO 8601 time, such as an Event's or a Log's time."
    end
  end
end
