# frozen_string_literal: true

# The Logs explorer: the page where a Project's Logs are searched and read.
# The page runs its Queries through #query, which proxies EventStore's query
# app, so the browser never talks to EventStore. Its runs (#query, #histogram
# and #sql) stop at RUN_TIMEOUT_SECONDS, answering 504; they aren't counted
# in ChartRuns, which says why.
class LogsController < ApplicationController
  include EventStoreProxy

  layout "dashboard"

  LOG_ID = /\A\h{8}-\h{4}-\h{4}-\h{4}-\h{12}\z/

  before_action :set_organization
  before_action :set_project, except: :start
  before_action :set_time_range, only: %i[query histogram readable_sql sql]

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
      project: ProjectOptionSerializer.new(@project).as_json,
      projects: ProjectOptionSerializer.list(@current_org.projects.order(:name)),
      sql_queries_enabled: InstanceSetting.current.sql_queries_enabled?,
    }
  end

  # GET with query, from and to (ISO 8601) and an optional cursor: one page
  # of Logs, newest first, or a tabular Query's table, as EventStore answers
  # it.
  def query
    render json: EventStore.query_logs(
      project_id: @project.id,
      query: params[:query].to_s,
      from: @from,
      to: @to,
      cursor: params[:cursor].presence,
      timeout_seconds: RUN_TIMEOUT_SECONDS
    )
  end

  # GET with query, from and to (ISO 8601) and an optional bucket_seconds:
  # the Logs that pass the Query's filters counted per bucket and Level, for
  # the histogram and summary line, as EventStore answers them.
  def histogram
    bucket = params[:bucket_seconds].presence
    bucket_seconds = bucket && Integer(bucket, 10, exception: false)
    return render json: { error: "bucket_seconds must be a whole number of seconds" }, status: :bad_request if bucket && !bucket_seconds

    render json: EventStore.logs_histogram(
      project_id: @project.id,
      query: params[:query].to_s,
      from: @from,
      to: @to,
      bucket_seconds: bucket_seconds,
      timeout_seconds: RUN_TIMEOUT_SECONDS
    )
  end

  # GET with query: the Query's mistakes, with positions, and its chips when
  # the Builder can show it. The Builder reads chips only from here, so the
  # browser never parses Queries.
  def validate
    render json: EventStore.validate_logs_query(project_id: @project.id, query: params[:query].to_s)
  end

  # GET with query, from and to (ISO 8601): the Query as readable SQL against
  # the logs view, to seed SQL mode. Refused while SQL queries are off.
  def readable_sql
    render json: EventStore.readable_logs_sql(project_id: @project.id, query: params[:query].to_s, from: @from, to: @to)
  end

  # POST with sql, from and to (ISO 8601): runs the SQL as written against
  # the logs view of the Project and range, answering a table. Only a POST,
  # so a link can't run SQL as whoever opens it, and refused while SQL
  # queries are off.
  def sql
    render json: EventStore.run_logs_sql(
      project_id: @project.id, sql: params[:sql].to_s, from: @from, to: @to, timeout_seconds: RUN_TIMEOUT_SECONDS
    )
  end

  # GET one Log by id, for the inspector. time, the Log's own time, lets
  # EventStore look in one hour instead of every file of the Project.
  def show
    return head :not_found unless LOG_ID.match?(params[:id].to_s)

    log = EventStore.get_log(project_id: @project.id, log_id: params[:id], time: params[:time].presence)
    return head :not_found unless log

    render json: log
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end
end
