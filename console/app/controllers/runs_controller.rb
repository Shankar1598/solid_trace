# frozen_string_literal: true

# The Runs page (Organization navigation): the Runs of the Organization's
# Agent Project from their run events, over the range and the filters in
# the URL (range=<span> or from and to, project, state), Active Runs first.
# The default range is 30 days. RunsView builds it. Each member sees it;
# someone outside the Organization gets 404 (set_current_org).
class RunsController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def index
    view = RunsView.new(organization: @current_org, range: range, filters: RunsView.filters(params))
    props = view.to_h.merge(settings_path: agent_session_settings_path(org_slug: @current_org.slug))
    render inertia: "Runs/Index", props: { runs: props }
  end

  private

  # The URL's range, or the default one when it names none that can be read.
  def range
    RunsView.range(range: params[:range], from: params[:from], to: params[:to])
  rescue McpTools::ToolError
    RunsView.range
  end
end
