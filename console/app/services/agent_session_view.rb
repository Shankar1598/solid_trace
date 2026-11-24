# frozen_string_literal: true

# One Agent session of an Organization's Agent Project as a member sees it
# on the Agent session page: its header (the person or the Run, the
# repository, the folder, the parent Agent session, the model and its
# effort, the times, the tokens by kind, the estimated cost and the
# flags), its subagents, and one page of its timeline, of the main
# transcript or of one subagent's.
# Made here once, so that the page and the MCP server's get_agent_session
# cannot drift.
#
# Each step says whom it is from. In an engineer's own Agent session each
# person message is from that engineer. In a Run's Agent session the
# author comes from the Run's events (the factory's ticket 58); until then
# it has no name. Hook feedback and subagent prompts are "not from a
# person".
class AgentSessionView
  include EventStoreView

  # How long Console waits for EventStore, in seconds.
  TIMEOUT_SECONDS = 10
  # A cut or partial record cannot always be resumed (story 31).
  RESUME_FLAGS = %w[cut partial].freeze
  RESUME_WARNING = "The record of this Agent session is cut or partial, so a resume of this Agent session can fail."
  # The orders of the timeline: the oldest step first (EventStore's
  # default), or the newest first, whose first page is the end.
  ORDERS = %w[oldest newest].freeze
  STEP_KINDS = { "person" => "person", "assistant" => "agent", "tool" => "tool",
                 "not from a person" => "not from a person", "system" => "system" }.freeze

  attr_reader :organization, :id, :path, :cursor, :search, :limit, :order

  # path is the subagent transcript whose timeline the page gives, nil for
  # the main transcript; cursor, the next_cursor of the page before.
  # search, if given, keeps the steps whose text has it, with any case;
  # with no path, from all the Agent session's transcripts. limit is the
  # most steps on the page, EventStore's default (200) when nil. order is
  # one of ORDERS; another value is the oldest first.
  def initialize(organization:, id:, path: nil, cursor: nil, search: nil, limit: nil, order: nil)
    @organization = organization
    @id = id
    @path = path.presence
    @cursor = cursor.presence
    @search = search.to_s.strip.presence
    @limit = limit
    @order = ORDERS.include?(order.to_s) ? order.to_s : nil
  end

  def project
    organization.agent_project
  end

  # False when the Organization has no Agent Project, or EventStore says
  # that the Agent Project has no such Agent session or transcript.
  def found?
    return false unless project

    answer
    !@not_found
  end

  def to_h
    {
      event_store: event_store, agent_session: header, subagents: Array(answer&.dig("subagents")),
      cost_label: AgentSessionsView::COST_LABEL, resume_warning: resume_warning,
    }.merge(timeline_h)
  end

  # One page of the timeline: its steps, each with whom it is from, and the
  # cursor of the page after it, nil after the last.
  def timeline_h
    { steps: steps, next_cursor: answer&.dig("next_cursor").presence }
  end

  # The Agent session's totals, named as a row of the Agent sessions list.
  def header
    return @header if defined?(@header)

    session = answer&.dig("agent_session")
    @header = session && AgentSessionsView.named_rows([ session ]).first
  end

  def resume_warning
    RESUME_WARNING if (Array(header&.dig("flags")) & RESUME_FLAGS).any?
  end

  # 'Show full' on a cut value: the raw line of the Agent session's file
  # at path, version and offset, redacted as stored, as {"line", "cut"};
  # nil when the Agent Project has no line there. Raises
  # EventStore::Unavailable when EventStore cannot answer.
  def raw_line(path:, version:, offset:)
    return nil unless project

    EventStore.agent_session_line!(project_id: project.id, id: id, path: path, version: version, offset: offset,
      read_timeout: TIMEOUT_SECONDS)
  rescue EventStore::NotFound
    nil
  end

  def steps
    Array(answer&.dig("steps")).map { |step| step.merge("from" => from(step["role"])) }
  end

  private

  # Whom a step is from: its kind, and for a person message the person's
  # name when the page knows it.
  def from(role)
    kind = STEP_KINDS.fetch(role.to_s, role.to_s)
    return { kind: kind } unless kind == "person"

    own = header && header["run"].nil?
    { kind: kind, name: own ? header.dig("person", :name) : nil }
  end

  def answer
    event_store_answer("Agent session") do
      EventStore.agent_session!(project_id: project.id, id: id, path: path, cursor: cursor, search: search, limit: limit,
        order: order, read_timeout: TIMEOUT_SECONDS)
    rescue EventStore::NotFound
      @not_found = true
      nil
    end
  end
end
