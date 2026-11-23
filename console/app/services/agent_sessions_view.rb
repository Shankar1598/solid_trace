# frozen_string_literal: true

# The Agent sessions of an Organization as a member sees them on the Agent
# sessions page: those of its Agent Project with activity in a range, that
# the filters select, in the order (latest activity first by default),
# with the totals of their tokens and estimated cost. Each row names its
# person, or its Run. Made here once, so that the page and the MCP
# server's list_agent_sessions cannot drift.
#
# The filters: person (a user id), repository (host/workspace/name),
# project (the slug of a Project of Runs: it selects the Agent sessions of
# Runs only, as an engineer's own Agent session has no Project), and run
# ("run" for the Agent sessions of Runs, "none" for those of no Run). A
# filter that is not one of these values is not applied.
#
# The order: sort (last, start, tokens or cost) and direction (desc or
# asc), as EventStore sorts the Agent sessions before its limit. With no
# order, or one that is not one of these values, EventStore gives the
# latest activity first.
class AgentSessionsView
  include EventStoreView

  # How long Console waits for EventStore, in seconds.
  TIMEOUT_SECONDS = 10
  # The range when none is given.
  DEFAULT_SPAN = "7d"
  RUN_FILTERS = %w[run none].freeze
  SORTS = %w[last start tokens cost].freeze
  DIRECTIONS = %w[desc asc].freeze
  # The cost is the tokens at list prices, or Claude Code's own estimate,
  # never an invoice (story 30).
  COST_LABEL = "estimate at list prices"

  # The range a page's URL or a tool's arguments name: a span back from
  # now, 7d without one, or from and to. Raises McpTools::ToolError, saying
  # why, for one it cannot read.
  def self.range(range: nil, from: nil, to: nil)
    range = DEFAULT_SPAN if range.blank? && from.blank? && to.blank?
    McpTools::TimeRange.parse(range: range, from: from, to: to)
  end

  # The filters that params name, each nil when it names none or one that
  # cannot be a value.
  def self.filters(params)
    person = params[:person].to_s
    run = params[:run].to_s
    {
      person: person.match?(/\A[1-9]\d{0,17}\z/) ? person.to_i : nil,
      repository: params[:repository].to_s.strip.presence,
      project: params[:project].to_s.strip.presence,
      run: RUN_FILTERS.include?(run) ? run : nil,
    }
  end

  # The order that params name: sort and direction, each nil when it names
  # none or one that is not a value.
  def self.order(params)
    sort = params[:sort].to_s
    direction = params[:direction].to_s
    { sort: SORTS.include?(sort) ? sort : nil, direction: DIRECTIONS.include?(direction) ? direction : nil }
  end

  attr_reader :organization, :range, :filters, :order, :limit

  # range is a McpTools::TimeRange, as AgentSessionsView.range reads it;
  # filters and order as AgentSessionsView.filters and .order read them.
  # limit is the most rows, nil for EventStore's own limit (500), which the
  # page uses.
  def initialize(organization:, range:, filters: {}, order: {}, limit: nil)
    @organization = organization
    @range = range
    @filters = { person: nil, repository: nil, project: nil, run: nil }.merge(filters.compact)
    @order = { sort: nil, direction: nil }.merge(order.compact)
    @limit = limit
  end

  def project
    organization.agent_project
  end

  def to_h
    return { on: false } unless project

    {
      on: true, range: TransactionsView.range_h(range), filters: filters, order: order, event_store: event_store,
      rows: rows, totals: totals, more: more, cost_label: COST_LABEL,
      people: people, projects: projects, repositories: repositories,
    }
  end

  # A row for each Agent session, with its person, or its Run (id and
  # Project slug), and the numbers EventStore answers.
  def rows
    self.class.named_rows(Array(answer&.dig("agent_sessions")))
  end

  # EventStore's rows of Agent sessions, each with its person ({id,
  # name}), or its Run ({id, project}), in place of user_id, run_id and
  # project, and its title ("" for none, also from an EventStore older
  # than Console). The Agent session page names its header in the same way.
  def self.named_rows(sessions)
    names = User.where(id: sessions.map { |s| s["user_id"] }.uniq).pluck(:id, :name, :email).to_h { |id, name, email| [ id, name.presence || email ] }
    sessions.map do |s|
      run = s["run_id"].presence && { id: s["run_id"], project: s["project"].presence }
      person = s["user_id"].to_i.positive? ? { id: s["user_id"], name: names[s["user_id"]] || "User #{s['user_id']}" } : nil
      s.except("user_id", "run_id", "project").merge("title" => s["title"].to_s, "person" => person, "run" => run)
    end
  end

  # The totals of the Agent sessions that the filters select: sessions,
  # tokens by kind, the cost (nil when none has one) and how many have no
  # cost.
  def totals
    zero = { "agent_sessions" => 0, "input_tokens" => 0, "output_tokens" => 0, "cache_read_tokens" => 0,
             "cache_write_tokens" => 0, "cost_usd" => nil, "no_cost" => 0 }
    zero.merge(answer&.dig("totals")&.slice(*zero.keys) || {})
  end

  # Whether the limit left some Agent sessions out of the rows.
  def more
    answer&.dig("more") || false
  end

  # The choices of the person filter: the Organization's members.
  def people
    organization.users.order(:name).map { |user| { id: user.id, name: user.name.presence || user.email } }
  end

  # The choices of the Project (Runs) filter: the Organization's Projects
  # (but the Agent Project) whose Runs have Agent sessions in the range,
  # and the filter's own. A Project with no Runs would only select none.
  def projects
    slugs = (Array(answer&.dig("projects")) + [ filters[:project] ].compact).uniq
    organization.projects.where(agent: false, slug: slugs).order(:name).map { |p| { name: p.name, slug: p.slug } }
  end

  # The choices of the repository filter: those of the range, and the
  # filter's own.
  def repositories
    (Array(answer&.dig("repositories")) + [ filters[:repository] ].compact).uniq.sort
  end

  private

  def answer
    event_store_answer("Agent sessions") do
      EventStore.agent_sessions!(project_id: project.id, from: range.from, to: range.to, user_id: filters[:person],
        repository: filters[:repository], project: filters[:project], run: filters[:run],
        sort: order[:sort], direction: order[:direction], limit: limit,
        read_timeout: TIMEOUT_SECONDS)
    end
  end
end
