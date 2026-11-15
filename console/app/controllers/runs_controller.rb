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

  # The Run page: one Run of the Agent Project from its run events. 404
  # when the Organization has no Agent Project or EventStore has no event
  # of the Run. After a stop, the state that the Dispatcher answered shows
  # until the stop event comes from EventStore.
  def show
    view = RunView.new(organization: @current_org, id: params[:id])
    raise ActiveRecord::RecordNotFound unless view.found?

    slug = @current_org.slug
    props = view.to_h do |kind, value|
      kind == :agent_session ? agent_session_path(org_slug: slug, id: value) : run_evidence_path(org_slug: slug, id: params[:id], path: value)
    end
    props[:list_path] = runs_path(org_slug: slug)
    props[:stop] = { can: Dispatcher.on? && view.stoppable?, path: stop_run_path(org_slug: slug, id: params[:id]),
                     sent_state: flash[:run_state].presence }
    render inertia: "Runs/Show", props: { run: props }
  end

  # One Evidence file that a report of the Run names, from its Agent
  # session: an image inline, each other file as a download, never sniffed.
  # 404 for a file that no report of the Run names.
  def evidence
    view = RunView.new(organization: @current_org, id: params[:id])
    raise ActiveRecord::RecordNotFound unless view.found?

    path = params[:path].to_s
    session_id = view.evidence_session(path)
    raise ActiveRecord::RecordNotFound unless session_id

    body, type = EventStore.agent_evidence!(project_id: view.project.id, session_id: session_id, path: path)
    image = RunView::IMAGE_TYPES.include?(File.extname(path).downcase)
    response.headers["X-Content-Type-Options"] = "nosniff"
    send_data body, type: image ? type : "application/octet-stream", filename: File.basename(path),
      disposition: image ? "inline" : "attachment"
  rescue EventStore::NotFound
    raise ActiveRecord::RecordNotFound
  rescue EventStore::Unavailable, EventStore::Rejected => e
    render plain: "EventStore can't read the Evidence file: #{e.message}", status: :service_unavailable
  end

  # The Stop button: a stop of the Run through the Dispatcher, with
  # Console's factory token for the signed-in user. 404 with no Dispatcher
  # URL.
  def stop
    raise ActiveRecord::RecordNotFound unless Dispatcher.on?

    answer = Dispatcher.new(user: current_user, organization: @current_org)
      .request(:post, "/api/v1/runs/#{ERB::Util.url_encode(params[:id])}/stop")
    state = answer.is_a?(Hash) ? answer["state"].to_s : ""
    flash[:run_state] = state if state.present?
    redirect_to run_path(org_slug: @current_org.slug, id: params[:id]), notice: "The Dispatcher stopped the Run."
  rescue Dispatcher::Refused, Dispatcher::Unavailable => e
    redirect_to run_path(org_slug: @current_org.slug, id: params[:id]), alert: "The Dispatcher did not stop the Run: #{e.message}"
  end

  private

  # The URL's range, or the default one when it names none that can be read.
  def range
    RunsView.range(range: params[:range], from: params[:from], to: params[:to])
  rescue McpTools::ToolError
    RunsView.range
  end
end
