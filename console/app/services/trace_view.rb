# frozen_string_literal: true

# One trace as a user sees it: its Spans across their Organization's
# Projects, nested by parent for the trace page's waterfall, with repeats
# collapsed and the critical path marked; its Logs; and its Issues. Made here
# once, so the page (#to_h) and the MCP server's get_trace (#summary) can't
# drift.
#
# EventStore is sent the Organization's Project ids only, and a Span, Log or
# Event of any other Project is left out, so another Organization's trace
# shows nothing. A trace its SDK didn't sample has no Spans: EventStore then
# reads its Logs around time, the hint a link gives (an Event's or a Log's
# time), or else around its first Event. When EventStore can't answer, there
# is nothing of it and event_store says why.
class TraceView
  # How long Console waits for EventStore's trace, in seconds.
  TIMEOUT_SECONDS = 10
  # The most rows #summary gives, its Transactions, critical path and Spans
  # together, so that a large trace fits an agent's context.
  SUMMARY_ROWS = 200
  # How much of a description #summary gives, and of a message the page
  # does.
  SUMMARY_DESCRIPTION = 120
  LOG_MESSAGE = 500

  attr_reader :organization, :user, :trace_id, :time

  def initialize(organization:, user:, trace_id:, time: nil)
    @organization = organization
    @user = user
    @trace_id = trace_id.to_s.downcase
    @time = time
  end

  # The trace page's props.
  def to_h
    {
      trace_id: trace_id, time: time&.utc&.iso8601, event_store: event_store, sampled: sampled,
      duration_us: duration_us, span_count: received.size, spans: spans,
      logs: logs, more_logs: answer&.dig("more_logs") || false, logs_links: logs_links,
      issues: issues,
    }
  end

  # The trace for an agent: its Transactions; the critical path; its rows by
  # self time, the most first; its Issues; and the query_logs arguments that
  # read its Logs. The Transactions, the critical path and the rows share
  # one budget of rows: the Transactions take at most a tenth of it, the
  # critical path a quarter, and the rows the rest, and left_out counts what
  # each didn't give.
  def summary(rows: SUMMARY_ROWS)
    kept_transactions = transactions.first(rows / 10)
    path = spans.select { |row| row["critical"] }
    kept_path = path.first(rows / 4)
    kept = spans.sort_by { |row| [ -row["self_time_us"], row["offset_us"] ] }
      .first(rows - kept_transactions.size - kept_path.size)
    {
      trace_id: trace_id, event_store: event_store, sampled: sampled, duration_us: duration_us,
      span_count: received.size, transactions: kept_transactions,
      spans: kept.map { |row| summary_row(row) },
      left_out: {
        transactions: transactions.size - kept_transactions.size, critical_path: path.size - kept_path.size,
        rows: spans.size - kept.size, spans: received.size - kept.sum { |row| row["count"] },
      },
      critical_path: kept_path.map { |row| row["span_id"] },
      issues: issues.map do |issue|
        { project: issue[:project][:slug], issue: issue[:number], title: issue[:title], events: issue[:events], event: issue[:event] }
      end,
      logs: { count: logs.size, more: answer&.dig("more_logs") || false },
      trace_logs: trace_logs,
    }
  end

  # Whether EventStore answered, and if not, why.
  def event_store
    answer
    @unavailable ? { available: false, error: @unavailable } : { available: true }
  end

  # Whether the trace has Spans: nil when EventStore didn't answer.
  def sampled
    answer && received.any?
  end

  # The trace's rows in waterfall order: each after its parent, a parent's
  # children in start order, with its depth and its start from the trace's,
  # in microseconds. Siblings of one group, a query repeated in a loop, are
  # one row, its count how many, its durations and self times their totals,
  # its children all theirs. critical marks the rows on the critical path.
  # A Span whose parent isn't in the trace starts a tree of its own.
  def spans
    @spans ||= begin
      ids = received.to_set { |s| s["span_id"] }
      roots = received.reject { |s| ids.include?(s["parent_span_id"]) }
      rows = []
      walk = lambda do |members, depth|
        rows << row(members, depth)
        siblings(members.flat_map { |member| children[member["span_id"]] || [] }).each { |group| walk.call(group, depth + 1) }
      end
      siblings(roots).each { |group| walk.call(group, 0) }
      rows
    end
  end

  # From the trace's first start to its last end, in microseconds.
  def duration_us
    return 0 if received.empty?

    received.map { |s| end_us(s) }.max - first_us
  end

  # The trace's Logs in time order, each linked to the explorer over its
  # second among the trace's Logs.
  def logs
    @logs ||= Array(answer&.dig("logs")).filter_map do |log|
      project = projects[log["project_id"]]
      next unless project

      second = Time.iso8601(log["time"]).utc.floor
      {
        id: log["id"], time: log["time"], level: log["level"], service: log["service"],
        message: log["message"].to_s.truncate(LOG_MESSAGE), project: project_ref(project),
        url: explorer_path(project, second, second + 1.second),
      }
    end
  end

  # The trace's Issues, the one with its earliest Event first, each with how
  # many of its Events are in the trace, linked to its first.
  def issues
    @issues ||= begin
      groups = Array(answer&.dig("issues")).select { |group| projects.key?(group["project_id"]) }
      fingerprints = IssueFingerprint.where(id: groups.map { |group| group["issue_fingerprint_id"] }, project_id: projects.keys)
        .includes(:issue).index_by(&:id)
      found = {}
      groups.each do |group|
        issue = fingerprints[group["issue_fingerprint_id"]]&.issue
        next unless issue

        events = Array(group["events"])
        if (seen = found[issue.id])
          seen[:events] += events.size
        else
          found[issue.id] = {
            id: issue.id, number: issue.number, title: issue.title, status: issue.status,
            project: project_ref(projects.fetch(issue.project_id)), events: events.size,
            event: events.first&.dig("uuid"), url: issue_path(issue, events.first&.dig("uuid")),
          }
        end
      end
      found.values
    end
  end

  private

  def row(members, depth)
    first = members.first
    first.slice(*%w[span_id parent_span_id segment_id is_segment transaction op description group repeats
      status sample_rate environment release server_name attributes])
      .merge(
        "project" => project_ref(projects.fetch(first["project_id"])), "depth" => depth, "start" => first["start"],
        "offset_us" => start_us(first) - first_us, "count" => members.size,
        "duration_us" => members.sum { |s| s["duration_us"].to_i }, "self_time_us" => members.sum { |s| s["self_time_us"].to_i },
        "span_us" => members.map { |s| end_us(s) }.max - start_us(first),
        "critical" => members.any? { |s| critical.include?(s) },
      )
  end

  # Spans in start order, those of one group together as one list where the
  # first of them was.
  def siblings(spans)
    spans.sort_by { |s| start_us(s) }.group_by { |s| s["group"].present? ? [ :group, s["group"] ] : [ :span, s["span_id"] ] }.values
  end

  # A row as #summary gives it, short: its Transaction is its segment's, a
  # status only when it isn't ok, and critical only when it is.
  def summary_row(row)
    {
      span_id: row["span_id"], parent_span_id: row["parent_span_id"].presence, depth: row["depth"],
      project: row["project"][:slug], op: row["op"], description: row["description"].to_s.truncate(SUMMARY_DESCRIPTION),
      status: (row["status"] unless [ "", "ok" ].include?(row["status"].to_s)),
      count: row["count"], offset_us: row["offset_us"], duration_us: row["duration_us"],
      self_time_us: row["self_time_us"], critical: (true if row["critical"]),
    }.compact
  end

  # The trace's Transactions, each segment's Project, name and duration.
  def transactions
    received.select { |s| s["is_segment"] }.map do |s|
      { span_id: s["span_id"], project: projects.fetch(s["project_id"]).slug, transaction: s["transaction"], duration_us: s["duration_us"] }
    end
  end

  # The Spans the trace's end waited on: from its end back, the Span that
  # ended last, then the one that ended last by the time that one began, and
  # so on, and within each of those its children the same way. A Span still
  # running when the next one on the path began isn't on it.
  def critical
    @critical ||= begin
      marked = Set.new.compare_by_identity
      ids = received.to_set { |s| s["span_id"] }
      mark = lambda do |spans, until_us|
        cursor = until_us
        spans.sort_by { |s| -end_us(s) }.each do |span|
          next unless end_us(span) <= cursor

          marked << span
          mark.call(children[span["span_id"]] || [], end_us(span))
          cursor = start_us(span)
        end
      end
      mark.call(received.reject { |s| ids.include?(s["parent_span_id"]) }, received.map { |s| end_us(s) }.max.to_i + 1)
      marked
    end
  end

  def children
    @children ||= received.group_by { |s| s["parent_span_id"] }
  end

  def project_ref(project)
    { id: project.id, name: project.name, slug: project.slug }
  end

  # The query_logs arguments that read the trace's Logs, for each Project
  # with any, as get_issue's trace_logs are.
  def trace_logs
    return [] unless logs_window

    from, to = logs_window
    logs.map { |log| log[:project][:slug] }.uniq.map do |slug|
      {
        tool: "query_logs",
        arguments: {
          organization: organization.slug, project: slug, query: trace_query,
          from: from.utc.iso8601(3), to: to.utc.iso8601(3),
        },
      }
    end
  end

  # The explorer on each Project with the trace's Logs, over the time they
  # were read from.
  def logs_links
    return [] unless logs_window

    logs.map { |log| log[:project] }.uniq.map do |ref|
      { project: ref, url: explorer_path(projects.fetch(ref[:id]), *logs_window) }
    end
  end

  # When EventStore read the trace's Logs from and to, or nil.
  def logs_window
    from, to = answer&.values_at("logs_from", "logs_to")
    [ Time.iso8601(from), Time.iso8601(to) ] if from && to
  end

  def trace_query
    "where trace_id == \"#{trace_id}\""
  end

  def explorer_path(project, from, to)
    routes.project_logs_path(project, org_slug: organization.slug, from: from.utc.iso8601(3), to: to.utc.iso8601(3),
      mode: "query", q: trace_query)
  end

  def issue_path(issue, event)
    routes.project_issue_path(projects.fetch(issue.project_id), issue, org_slug: organization.slug, event_id: event)
  end

  def routes
    Rails.application.routes.url_helpers
  end

  # The answered Spans of the Organization's Projects, in start order.
  def received
    @received ||= Array(answer&.dig("spans")).select { |s| projects.key?(s["project_id"]) }
  end

  def first_us
    @first_us ||= received.map { |s| start_us(s) }.min
  end

  def start_us(span)
    span["_start_us"] ||= begin
      t = Time.iso8601(span["start"])
      t.to_i * 1_000_000 + t.usec
    end
  end

  def end_us(span)
    start_us(span) + span["duration_us"].to_i
  end

  # The Organization's Projects, by id.
  def projects
    @projects ||= organization.projects.order(:created_at, :id).index_by(&:id)
  end

  # EventStore's answer, or nil when the Organization has no Projects or
  # EventStore can't answer (@unavailable says why).
  def answer
    return @answer if defined?(@answer)

    @answer = projects.empty? ? nil : EventStore.trace!(project_ids: projects.keys, trace_id: trace_id, time: time, read_timeout: TIMEOUT_SECONDS)
  rescue EventStore::Unavailable, EventStore::Rejected => e
    Rails.logger.error("Trace: #{e.message}")
    @unavailable = "EventStore can't read the trace: #{e.message}"
    @answer = nil
  end
end
