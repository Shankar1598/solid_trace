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
