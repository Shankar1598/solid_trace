# frozen_string_literal: true

# The Agent sessions page (Organization navigation): the Agent sessions of
# the Organization's Agent Project over the range and the filters in the
# URL (range=<span> or from and to, person, repository, project, run),
# with their totals. AgentSessionsView builds it. Each member sees it;
# someone outside the Organization gets 404 (set_current_org). With Agent
# sessions off, the page tells how to turn them on.
class AgentSessionsController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def index
    view = AgentSessionsView.new(organization: @current_org, range: range, filters: AgentSessionsView.filters(params))
    props = view.to_h.merge(settings_path: agent_session_settings_path(org_slug: @current_org.slug))
    props[:can_turn_on] = admin? unless props[:on]
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
      parent_path: parent && agent_session_path(org_slug: slug, id: parent),
    )
    render inertia: "AgentSessions/Show", props: { agent_session: props }
  end

  # One page of the timeline as JSON: the main transcript's, or the
  # subagent transcript's at path, after cursor. 503 when EventStore cannot
  # answer.
  def timeline
    view = AgentSessionView.new(organization: @current_org, id: params[:id], path: params[:path], cursor: params[:cursor])
    raise ActiveRecord::RecordNotFound unless view.found?

    event_store = view.event_store
    return render json: { error: event_store[:error] }, status: :service_unavailable unless event_store[:available]

    render json: view.timeline_h
  end

  private

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
