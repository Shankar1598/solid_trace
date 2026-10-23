# frozen_string_literal: true

# The Logs explorer: the page where a Project's Logs are searched and read.
# The page runs its Queries through #query, which proxies EventStore's query
# app, so the browser never talks to EventStore. Its runs (#query and
# #histogram at RUN_TIMEOUT_SECONDS, #sql at SQL_TIMEOUT_SECONDS) stop
# there, answering 504 with EventStore's message, and count against
# ChartRuns' cap, answering 503 past it; ChartRuns says why.
class LogsController < ApplicationController
  include EventStoreProxy

  rescue_from ChartRuns::Busy, with: :runs_busy

  layout "dashboard"

  before_action :set_current_org
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
      # Where the Project's Logs go, for an empty Project's snippets.
      ingest: { url: public_ingest_url, key: project_key&.public_key },
      # Loaded once the page shows, so EventStore never holds the page up.
      has_logs: InertiaRails.defer { logs_in_retention? },
    }
  end

  # POST: a sample Log sent to EventStore's ingest with the Project's key, as
  # a shipper would send it, so an empty Project's page can show one at once.
  # Answers the Log's time.
  def send_test
    key = project_key
    return render json: { error: "This Project has no key. Add one in its settings, then send a test Log." }, status: :unprocessable_content unless key

    log = test_log
    EventStore.ingest_logs(project_id: @project.id, key: key.public_key, logs: [ log ])
    render json: { time: log[:time] }, status: :created
  end

  # GET with query, from and to (ISO 8601), an optional cursor and totals:
  # one page of Logs, newest first, or a tabular Query's table, as EventStore
  # answers it. totals=1, as the explorer's run asks, adds a time chart's
  # totals over the range to its table.
  def query
    render json: ChartRuns.hold {
      EventStore.query_logs(
        project_id: @project.id,
        query: params[:query].to_s,
        from: @from,
        to: @to,
        cursor: params[:cursor].presence,
        totals: params[:totals] == "1",
        timeout_seconds: RUN_TIMEOUT_SECONDS
      )
    }
  end

  # GET with query, from and to (ISO 8601) and an optional bucket_seconds:
  # the Logs that pass the Query's filters counted per bucket and Level, for
  # the histogram and summary line, as EventStore answers them.
  def histogram
    bucket = params[:bucket_seconds].presence
    bucket_seconds = bucket && Integer(bucket, 10, exception: false)
    return render json: { error: "bucket_seconds must be a whole number of seconds" }, status: :bad_request if bucket && !bucket_seconds

    render json: ChartRuns.hold {
      EventStore.logs_histogram(
        project_id: @project.id,
        query: params[:query].to_s,
        from: @from,
        to: @to,
        bucket_seconds: bucket_seconds,
        timeout_seconds: RUN_TIMEOUT_SECONDS
      )
    }
  end

  # GET with query: the Query's mistakes, with positions, and its chips when
  # the Builder can show it. The Builder reads chips only from here, so the
  # browser never parses Queries.
  def validate
    render json: EventStore.validate_logs_query(project_id: @project.id, query: params[:query].to_s)
  end

  # GET with query, from and to (ISO 8601): the Query as readable SQL against
  # the logs table, to seed SQL mode. Refused while SQL queries are off.
  def readable_sql
    render json: EventStore.readable_logs_sql(project_id: @project.id, query: params[:query].to_s, from: @from, to: @to)
  end

  # POST with sql, from and to (ISO 8601): runs the SQL as written, as the
  # Project's SQL user, which sees the Project's rows of the range, answering
  # a table. Only a POST,
  # so a link can't run SQL as whoever opens it, and refused while SQL
  # queries are off.
  def sql
    render json: ChartRuns.hold {
      EventStore.run_logs_sql(
        project_id: @project.id, sql: params[:sql].to_s, from: @from, to: @to, timeout_seconds: SQL_TIMEOUT_SECONDS
      )
    }
  end

  # GET one Log by id, for the inspector. time, the Log's own time, lets
  # EventStore look in one hour instead of every file of the Project.
  def show
    log = EventStore.get_log(project_id: @project.id, log_id: params[:id], time: params[:time].presence)
    return head :not_found unless log

    render json: log
  end

  private

  def runs_busy
    render json: { error: "Console is busy running other Queries and charts. Run again in a moment." }, status: :service_unavailable
  end

  # The key snippets show and the test Log is sent with: the Project's first.
  def project_key
    @project.project_keys.order(:id).first
  end

  # Whether any Log is in the Project's Retention, up to the hour ahead
  # ingest accepts; nil when EventStore can't say.
  def logs_in_retention?
    days = @project.log_retention_days || InstanceSetting.current.default_log_retention_days
    now = Time.current
    answer = EventStore.query_logs(
      project_id: @project.id, query: "", from: now - days.days, to: now + 1.hour, limit: 1, timeout_seconds: RUN_TIMEOUT_SECONDS
    )
    # A page cut short by EventStore's scan budget may have older Logs.
    answer["logs"].present? || answer["next_cursor"].present?
  rescue EventStore::Unavailable, EventStore::Rejected => e
    Rails.logger.error(e.message)
    nil
  end

  def test_log
    {
      time: Time.current.utc.iso8601(3),
      level: "info",
      service: "solidtrace",
      message: "A test Log from SolidTrace: Logs sent with this Project's key show here.",
      sent_by: current_user.name,
      duration_ms: rand(5..250),
    }
  end

  def set_project
    @project = @current_org.projects.find_by!(slug: params[:project_slug])
  end
end
