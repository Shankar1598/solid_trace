# frozen_string_literal: true

# An Organization's Monitors. Any member can see, make, change, turn off and
# on, run and delete them. Pages post through Inertia and are redirected;
# #preview runs a Query for the form's chart as a Dashboard's chart runs.
class MonitorsController < ApplicationController
  include ChartRunning

  layout "dashboard"

  before_action :set_current_org
  before_action :set_monitor, only: %i[show edit update destroy enable disable run]
  before_action :set_time_range, only: :preview

  def index
    monitors = @current_org.monitors.includes(:project, :states, :integrations).order(:name)

    render inertia: "Monitors/Index", props: {
      monitors: monitors.map { |monitor| MonitorSerializer.new(monitor).as_json },
    }
  end

  def show
    render inertia: "Monitors/Show", props: {
      monitor: MonitorSerializer.new(@monitor, include_details: true).as_json,
    }
  end

  # Opens with the explorer's Project, Query and range when it sends them.
  def new
    project = @current_org.projects.find_by(id: params[:project_id]) || @current_org.projects.order(:name).first
    range = params[:range_minutes].to_i.clamp(1, LogMonitor::MAX_RANGE_MINUTES) if params[:range_minutes].present?

    render inertia: "Monitors/New", props: form_props.merge(
      monitor: {
        id: nil, name: "", kind: "threshold", project_id: project&.id,
        query: params[:query].presence || (project && "['#{project.slug}']\n| summarize count()"),
        interval_minutes: 1, range_minutes: range || 5, compare: "value", baseline: nil, minimum: nil, operator: ">", threshold: nil,
        notify_by_group: false, alert_on_no_data: false, integration_ids: [],
      }
    )
  end

  def edit
    render inertia: "Monitors/Edit", props: form_props.merge(monitor: MonitorSerializer.new(@monitor).as_json)
  end

  def create
    monitor = @current_org.monitors.new

    if monitor.save_with(monitor_params, integration_ids: integration_ids_param)
      redirect_to monitor_page(monitor), notice: "Monitor created."
    else
      redirect_to new_monitor_path(org_slug: @current_org.slug), inertia: { errors: monitor.errors.to_hash }
    end
  end

  def update
    if @monitor.save_with(monitor_params, integration_ids: integration_ids_param)
      redirect_to monitor_page(@monitor), notice: "Monitor saved."
    else
      redirect_to edit_monitor_path(@monitor, org_slug: @current_org.slug), inertia: { errors: @monitor.errors.to_hash }
    end
  end

  def destroy
    @monitor.destroy
    redirect_to monitors_path(org_slug: @current_org.slug), notice: "Monitor deleted."
  end

  # Turning a Monitor on checks its Query again, so it can stay off.
  def enable
    if @monitor.save_with({ disabled_at: nil })
      redirect_to monitor_page(@monitor), notice: "Monitor turned on."
    else
      redirect_to monitor_page(@monitor), alert: "The Monitor stays off: #{@monitor.errors.full_messages.to_sentence}"
    end
  end

  def disable
    if @monitor.update(disabled_at: Time.current)
      redirect_to monitor_page(@monitor), notice: "Monitor turned off."
    else
      redirect_to monitor_page(@monitor), alert: @monitor.errors.full_messages.to_sentence
    end
  end

  # Run now: runs the Monitor at once, as its schedule would, while the
  # request waits, so the page shows what the run found. A scheduled run
  # going at the same moment holds the Monitor, and Run now says so.
  def run
    return redirect_to monitor_page(@monitor), alert: "Turn the Monitor on to run it." unless @monitor.enabled?

    run = MonitorEvaluation.run(@monitor, Time.current)
    if run.nil?
      redirect_to monitor_page(@monitor), alert: "The Monitor is running now: its run shows here when it ends."
    elsif run.status_error?
      redirect_to monitor_page(@monitor), alert: "The run failed: #{run.error}"
    else
      redirect_to monitor_page(@monitor), notice: "Ran the Monitor: #{run.found}."
    end
  end

  # POST with project_id, query, from and to: the form's chart and the groups
  # firing now, for a Query not yet saved, run as a chart's is.
  def preview
    run_chart(@current_org.projects.find(params[:project_id]), params[:query].to_s)
  end

  # POST with a monitor's unsaved settings, as the form would save them: the
  # ranges a run now would read and each group it would find, with its
  # number, its Baseline when comparing the change, and whether it would
  # fire, read and compared as a run does (MonitorEvaluation.preview). Both
  # reads are one chart run.
  def preview_groups
    monitor = @current_org.monitors.new(monitor_params.merge(kind: "threshold"))
    raise ActiveRecord::RecordNotFound, "no such Project" unless monitor.project

    found = ChartRuns.hold { MonitorEvaluation.preview(monitor, Time.current) }
    baseline_from, baseline_to = found.baseline_range&.map { |at| at.utc.iso8601 }
    render json: {
      from: found.from.utc.iso8601, to: found.to.utc.iso8601, baseline_from: baseline_from, baseline_to: baseline_to,
      groups: found.groups,
    }
  rescue MonitorEvaluation::Unusable => e
    render json: { error: e.message }, status: :unprocessable_content
  end

  private

  def set_monitor
    @monitor = @current_org.monitors.find(params[:id])
  end

  def form_props
    {
      projects: ProjectOptionSerializer.list(@current_org.projects.order(:name)),
      integrations: IntegrationOptionSerializer.list(@current_org.integrations.order(:name)),
    }
  end

  # The Project is looked up in the Organization: another Organization's
  # Project is no Project. LogMonitor#save_with looks up the Integrations.
  def monitor_params
    permitted = params.require(:monitor).permit(
      :project_id, :name, :kind, :query, :interval_minutes, :range_minutes, :compare, :baseline, :minimum, :operator, :threshold,
      :notify_by_group, :alert_on_no_data
    )
    permitted[:project] = @current_org.projects.find_by(id: permitted.delete(:project_id)) if permitted.key?(:project_id)
    permitted
  end

  # The Integration ids the form sent, or nil when it sent none.
  def integration_ids_param
    params.require(:monitor).permit(integration_ids: [])[:integration_ids]
  end

  def monitor_page(monitor)
    monitor_path(monitor, org_slug: @current_org.slug)
  end
end
