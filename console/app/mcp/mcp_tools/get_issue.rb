# frozen_string_literal: true

module McpTools
  # An Issue with one of its Events and its comments, as its page shows them.
  # The Event comes from EventStore, as IssuesController#load_events reads
  # it; while EventStore can't read Events, the Issue still answers, saying
  # why its Event is left out.
  class GetIssue < Base
    include IssueTool

    COMMENTS_SHOWN = 20
    # How far either side of the Event's time the query_logs hint reads its
    # trace's Logs.
    TRACE_LOGS_AROUND = 1.hour

    description "Gives an Issue and one of its Events, its latest unless event names another: the exception's type and value " \
                "and its top in-app frames, the frame that raised first, and the Event's tags and trace id. older_event and " \
                "newer_event are the ids of the Events either side, for event. When the Event has a trace id, trace_logs gives " \
                "the query_logs arguments that read that trace's Logs. The Issue's last #{COMMENTS_SHOWN} comments come last."
    read_only!
    takes_issue
    argument :event, { type: "string", description: "The id of one of the Issue's Events. Its latest when left out." }

    def run
      json(issue_json(issue).merge(event_answer, comments_answer), link: links.issue(issue, event: arguments[:event]))
    end

    private

    def event_answer
      context = issue.event_with_context!(arguments[:event])
      if context.nil?
        raise ToolError, "No Event #{arguments[:event]} in this Issue. get_issue without event gives its latest." if arguments[:event].present?

        return { event: nil }
      end

      event = EventSummary.compact(context[:event])
      { event: event, older_event: context[:prev_uuid], newer_event: context[:next_uuid], trace_logs: trace_logs(event) }.compact
    rescue EventStore::Unavailable => e
      Rails.logger.error(e.message)
      { event: nil, events_unavailable: EventStore.events_unavailable_text(e) }
    end

    # The query_logs arguments that read the Logs of the Event's trace, in
    # its Project, around its time.
    def trace_logs(event)
      return unless event[:trace_id] && event[:time]

      time = Time.iso8601(event[:time])
      quoted = event[:trace_id].gsub(/["\\]/) { |char| "\\#{char}" }
      {
        tool: "query_logs",
        arguments: {
          organization: organization.slug,
          project: project.slug,
          query: "where trace_id == \"#{quoted}\"",
          from: (time - TRACE_LOGS_AROUND).utc.iso8601(3),
          to: (time + TRACE_LOGS_AROUND).utc.iso8601(3),
        },
      }
    end

    def comments_answer
      comments = issue.comments.includes(:rich_text_content, :user).order(created_at: :desc, id: :desc)
        .limit(COMMENTS_SHOWN + 1).to_a
      answer = { comments: comments.first(COMMENTS_SHOWN).reverse.map { |comment| comment_json(comment) } }
      answer[:more_comments] = "Older comments are on the Issue's page." if comments.size > COMMENTS_SHOWN
      answer
    end
  end
end
