# frozen_string_literal: true

# The Runs of an Organization as a member sees them on the Runs page: the
# last run event of each Run of its Agent Project with an event in a range,
# that the filters select, Active Runs first, then the latest event first
# (EventStore gives that order). Made here once, so that the page and the
# MCP server's list_runs cannot drift.
#
# The filters: project (the slug of the Project whose Factory settings the
# Run uses) and state (one of STATES). Both apply to the Run's last event. A
# state that is not one of STATES is not applied.
class RunsView
  include EventStoreView

  # How long Console waits for EventStore, in seconds.
  TIMEOUT_SECONDS = 10
  # The range when none is given: each Run that is not closed has an event
  # in it, because a Run closes after 30 days with no activity.
  DEFAULT_SPAN = "30d"
  # The states of a Run (GLOSSARY, Run), as the run events name them.
  STATES = %w[queued running waiting_on_person waiting_on_account_limit taken_over done failed stopped].freeze

  # The range a page's URL or a tool's arguments name: a span back from
  # now, 30d without one, or from and to. Raises McpTools::ToolError, saying
  # why, for one it cannot read.
  def self.range(range: nil, from: nil, to: nil)
    range = DEFAULT_SPAN if range.blank? && from.blank? && to.blank?
    McpTools::TimeRange.parse(range: range, from: from, to: to)
  end

  # The filters that params name, each nil when it names none or one that
  # cannot be a value.
  def self.filters(params)
    state = params[:state].to_s
    { project: params[:project].to_s.strip.presence, state: STATES.include?(state) ? state : nil }
  end

  attr_reader :organization, :range, :filters

  # range is a McpTools::TimeRange, as RunsView.range reads it; filters as
  # RunsView.filters reads them.
  def initialize(organization:, range:, filters: {})
    @organization = organization
    @range = range
    @filters = { project: nil, state: nil }.merge(filters.compact)
  end

  def project
    organization.agent_project
  end

  def to_h
    return { on: false } unless project

    {
      on: true, range: TransactionsView.range_h(range), filters: filters, event_store: event_store,
      rows: rows, more: answer&.dig("more") || false, projects: projects, states: STATES,
    }
  end

  # A row for each Run: its last event as EventStore reads it (id, time,
  # kind, state, reason, project, repository, folder, trigger, title,
  # actor, pr, executions, active, closed).
  def rows
    Array(answer&.dig("runs"))
  end

  # The choices of the Project filter: the Organization's Projects but the
  # Agent Project.
  def projects
    organization.projects.where(agent: false).order(:name).map { |p| { name: p.name, slug: p.slug } }
  end

  private

  def answer
    event_store_answer("Runs") do
      EventStore.runs!(project_id: project.id, from: range.from, to: range.to, project: filters[:project],
        state: filters[:state], read_timeout: TIMEOUT_SECONDS)
    end
  end
end
