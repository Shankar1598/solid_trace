# frozen_string_literal: true

# One trace as a user sees it: its Spans across their Organization's
# Projects, nested by parent for the trace page's waterfall. Made here once,
# so the page and the MCP server's get_trace can't drift.
#
# EventStore is sent the Organization's Project ids only, and a Span of any
# other Project is left out, so another Organization's trace shows nothing.
# When EventStore can't answer, there are no Spans and event_store says why.
class TraceView
  # How long Console waits for EventStore's trace, in seconds.
  TIMEOUT_SECONDS = 10

  attr_reader :organization, :user, :trace_id

  def initialize(organization:, user:, trace_id:)
    @organization = organization
    @user = user
    @trace_id = trace_id.to_s.downcase
  end

  def to_h
    { trace_id: trace_id, event_store: event_store, duration_us: duration_us, spans: spans }
  end

  # Whether EventStore answered, and if not, why.
  def event_store
    answer
    @unavailable ? { available: false, error: @unavailable } : { available: true }
  end

  # The trace's Spans in waterfall order: each after its parent, a parent's
  # children in start order, with its depth and its start from the trace's,
  # in microseconds. A Span whose parent isn't in the trace starts a tree of
  # its own.
  def spans
    @spans ||= begin
      found = received
      children = found.group_by { |s| s["parent_span_id"] }
      ids = found.to_set { |s| s["span_id"] }
      roots = found.reject { |s| ids.include?(s["parent_span_id"]) }
      ordered = []
      walk = lambda do |span, depth|
        ordered << row(span, depth)
        (children[span["span_id"]] || []).each { |child| walk.call(child, depth + 1) unless child.equal?(span) }
      end
      roots.each { |root| walk.call(root, 0) }
      ordered
    end
  end

  # From the trace's first start to its last end, in microseconds.
  def duration_us
    return 0 if received.empty?

    received.map { |s| start_us(s) + s["duration_us"].to_i }.max - first_us
  end

  private

  def row(span, depth)
    span.slice(*%w[span_id parent_span_id segment_id is_segment transaction op description group repeats
      status duration_us self_time_us sample_rate environment release server_name attributes])
      .merge("project" => project_ref(projects.fetch(span["project_id"])), "depth" => depth,
        "start" => span["start"], "offset_us" => start_us(span) - first_us)
  end

  def project_ref(project)
    { id: project.id, name: project.name, slug: project.slug }
  end

  # The answered Spans of the Organization's Projects, in start order.
  def received
    @received ||= Array(answer&.dig("spans")).select { |s| projects.key?(s["project_id"]) }
  end

  def first_us
    @first_us ||= received.map { |s| start_us(s) }.min
  end

  def start_us(span)
    t = Time.iso8601(span["start"])
    t.to_i * 1_000_000 + t.usec
  end

  # The Organization's Projects, by id.
  def projects
    @projects ||= organization.projects.order(:created_at, :id).index_by(&:id)
  end

  # EventStore's answer, or nil when the Organization has no Projects or
  # EventStore can't answer (@unavailable says why).
  def answer
    return @answer if defined?(@answer)

    @answer = projects.empty? ? nil : EventStore.trace!(project_ids: projects.keys, trace_id: trace_id, read_timeout: TIMEOUT_SECONDS)
  rescue EventStore::Unavailable, EventStore::Rejected => e
    Rails.logger.error("Trace: #{e.message}")
    @unavailable = "EventStore can't read the trace: #{e.message}"
    @answer = nil
  end
end
