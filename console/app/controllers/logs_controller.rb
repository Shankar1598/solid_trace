# frozen_string_literal: true

# The Logs explorer: the page where a Project's Logs are searched and read.
# The page runs its Queries through #query, which proxies EventStore's query
# app, so the browser never talks to EventStore.
class LogsController < ApplicationController
  layout "dashboard"

  LOG_ID = /\A\h{8}-\h{4}-\h{4}-\h{4}-\h{12}\z/
  before_action :set_organization
  before_action :set_project, except: :start

  # The sidebar's Logs entry: the newest Project's explorer.
  def start
    project = @current_org.projects.order(created_at: :desc).first
    if project
      redirect_to project_logs_path(project, org_slug: @current_org.slug)
    else
      redirect_to projects_path(org_slug: @current_org.slug), notice: "Create a Project to see its Logs."
    end
  end

  def index
    render inertia: "Logs/Index", props: {
      project: project_props(@project),
      projects: @current_org.projects.order(:name).map { |p| project_props(p) },
    }
  end

  # GET with query, from and to (ISO 8601) and an optional cursor: one page
  # of Logs, newest first, as EventStore answers it.
  def query
    from, to = time_range
    return render json: { error: "from and to must be ISO 8601 times" }, status: :bad_request unless from && to

    render json: EventStore.query_logs(
      project_id: @project.id,
      query: params[:query].to_s,
      from: from,
      to: to,
      cursor: params[:cursor].presence
    )
  rescue EventStore::Rejected => e
    render json: e.body, status: :bad_request
  rescue EventStore::Unavailable => e
    eventstore_unavailable(e)
  end

  # GET with query: the Query's mistakes, with positions, and its chips when
  # the Builder can show it. The Builder reads chips only from here, so the
  # browser never parses Queries.
  def validate
    render json: EventStore.validate_logs_query(project_id: @project.id, query: params[:query].to_s)
  rescue EventStore::Rejected => e
    render json: e.body, status: :bad_request
  rescue EventStore::Unavailable => e
    eventstore_unavailable(e)
  end

  # GET one Log by id, for the inspector. time, the Log's own time, lets
  # EventStore look in one hour instead of every file of the Project.
  def show
    return head :not_found unless LOG_ID.match?(params[:id].to_s)

    log = EventStore.get_log(project_id: @project.id, log_id: params[:id], time: params[:time].presence)
    return head :not_found unless log

    render json: log
  rescue EventStore::Unavailable => e
    eventstore_unavailable(e)
  end

  private

  def eventstore_unavailable(error)
    Rails.logger.error(error.message)
    render json: { error: "EventStore is unavailable. Try again shortly." }, status: :bad_gateway
  end

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end

  def time_range
    [ params[:from], params[:to] ].map do |value|
      Time.iso8601(value.to_s)
    rescue ArgumentError
      nil
    end
  end

  def project_props(project)
    { id: project.id, name: project.name, slug: project.slug }
  end
end
