# frozen_string_literal: true

# The Agent sessions page (Organization navigation): the Agent sessions of
# the Organization's Agent Project over the range and the filters in the
# URL (range=<span> or from and to, person, repository, project, run), in
# the order in the URL (sort and direction), with their totals. AgentSessionsView builds it. Each member sees it;
# someone outside the Organization gets 404 (set_current_org). With Agent
# sessions off, the page tells how to turn them on.
class AgentSessionsController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  # The range of a search across Agent sessions (story 29).
  SEARCH_RANGE = "7d"

  def index
    filters = AgentSessionsView.filters(params)
    # The Project and Run filters select the Agent sessions of Runs, so the
    # page has them only while the Factory is on; a link that names them
    # does not filter out of sight.
    filters = filters.merge(project: nil, run: nil) unless Dispatcher.on?
    view = AgentSessionsView.new(organization: @current_org, range: range, filters: filters, order: AgentSessionsView.order(params))
    props = view.to_h.merge(settings_path: agent_session_settings_path(org_slug: @current_org.slug))
    props[:can_turn_on] = admin? unless props[:on]
    props[:search] = search_across(@current_org.agent_project)
    render inertia: "AgentSessions/Index", props: { agent_sessions: props }
  end

  # The Agent session page: the header and the first page of the timeline
  # of one Agent session of the Agent Project. 404 when the Organization
  # has no Agent Project or EventStore has no such Agent session.
  def show
    view = AgentSessionView.new(organization: @current_org, id: params[:id])
    raise ActiveRecord::RecordNotFound unless view.found?

    slug = @current_org.slug
    parent = view.header&.dig("parent_session_id").presence
    props = view.to_h.merge(
      list_path: agent_sessions_path(org_slug: slug),
      timeline_path: agent_session_timeline_path(org_slug: slug, id: params[:id]),
      line_path: agent_session_line_path(org_slug: slug, id: params[:id]),
      parent_path: parent && agent_session_path(org_slug: slug, id: parent),
    )
    render inertia: "AgentSessions/Show", props: { agent_session: props }
  end

  # One page of the timeline as JSON: the main transcript's, or the
  # subagent transcript's at path, after cursor; with search, only the
  # steps that have that text, from all transcripts when no path is given.
  # 503 when EventStore cannot answer.
  def timeline
    view = AgentSessionView.new(organization: @current_org, id: params[:id], path: params[:path], cursor: params[:cursor],
      search: params[:search], order: params[:order])
    raise ActiveRecord::RecordNotFound unless view.found?

    event_store = view.event_store
    return render json: { error: event_store[:error] }, status: :service_unavailable unless event_store[:available]

    render json: view.timeline_h
  end

  # 'Show full' on a cut value, as JSON: the raw line of a step by its path,
  # version and offset, from the Organization's own Agent Project only.
  # 404 when it has no line there; 503 when EventStore cannot answer.
  def line
    version = Integer(params[:version].to_s, 10, exception: false)
    offset = Integer(params[:offset].to_s, 10, exception: false)
    raise ActiveRecord::RecordNotFound if params[:path].blank? || version.nil? || offset.nil? || version.negative? || offset.negative?

    raw = AgentSessionView.new(organization: @current_org, id: params[:id]).raw_line(path: params[:path], version: version, offset: offset)
    raise ActiveRecord::RecordNotFound unless raw

    render json: { line: raw["line"], cut: raw["cut"] == true }
  rescue EventStore::Unavailable => e
    Rails.logger.error("Agent session line: #{e.message}")
    render json: { error: "EventStore can't read the line: #{e.message}" }, status: :service_unavailable
  end

  private

  # Where search across Agent sessions goes: a Query on the Agent Project
  # in the Logs explorer, over SEARCH_RANGE, which the member can make
  # longer there. nil without an Agent Project.
  def search_across(project)
    project && { logs_path: project_logs_path(project, org_slug: @current_org.slug), project: project.slug, range: SEARCH_RANGE }
  end

  # The URL's range, or the default one when it names none that can be read.
  def range
    AgentSessionsView.range(range: params[:range], from: params[:from], to: params[:to])
  rescue McpTools::ToolError
    AgentSessionsView.range
  end

  def admin?
    @current_org.organization_users.find_by(user: current_user)&.admin? || false
  end
end
