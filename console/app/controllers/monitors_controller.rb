# frozen_string_literal: true

# An Organization's Monitors. Any member can see, make, change, turn off and
# on, run and delete them. Pages post through Inertia and are redirected;
# #preview runs a Query for the form's chart as a Dashboard's chart runs.
class MonitorsController < ApplicationController
  include ChartRunning

  layout "dashboard"

  before_action :set_organization
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
        interval_minutes: 1, range_minutes: range || 5, operator: ">", threshold: nil,
        notify_by_group: false, alert_on_no_data: false, integration_ids: [],
      }
    )
  end

  def edit
    render inertia: "Monitors/Edit", props: form_props.merge(monitor: MonitorSerializer.new(@monitor).as_json)
  end

  def create
    monitor = @current_org.monitors.new

    if save(monitor)
      redirect_to monitor_page(monitor), notice: "Monitor created."
    else
      redirect_to new_monitor_path(org_slug: @current_org.slug), inertia: { errors: monitor.errors.to_hash }
    end
  end

  def update
    if save(@monitor)
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
    @monitor.disabled_at = nil
    if checked_save(@monitor)
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
      redirect_to monitor_page(@monitor), notice: "Ran the Monitor: #{found(run)}."
    end
  end

  # POST with project_id, query, from and to: the form's chart and the groups
  # firing now, for a Query not yet saved, run as a chart's is.
  def preview
    run_chart(@current_org.projects.find(params[:project_id]), params[:query].to_s)
  end

  private

  def set_organization
    @current_org = current_user.organizations.find_by!(slug: params[:org_slug])
  end

  def set_monitor
    @monitor = @current_org.monitors.find(params[:id])
  end

  def form_props
    {
      projects: ProjectOptionSerializer.list(@current_org.projects.order(:name)),
      integrations: @current_org.integrations.order(:name).map do |integration|
        { id: integration.id, name: integration.name, provider: integration.provider, enabled: integration.active }
      end,
    }
  end

  # Saves the Monitor and, when the form sent them, its Integrations, all or
  # nothing. The Project and Integrations are looked up in the Organization:
  # another Organization's Project is no Project, and its Integrations are
  # refused.
  def save(monitor)
    integrations = integrations_param
    monitor.assign_attributes(monitor_params)
    if integrations == :foreign
      monitor.errors.add(:integrations, "must belong to the Monitor's Organization")
      return false
    end

    checked_save(monitor) { monitor.integrations = integrations unless integrations.nil? }
  end

  # Validating a Monitor may check its Query with EventStore, which can take
  # as long as a run, so it happens before the transaction opens: on SQLite a
  # write transaction holds the database's write lock, and every other writer
  # would wait on EventStore too.
  def checked_save(monitor)
    return false unless monitor.valid?

    LogMonitor.transaction do
      next false unless monitor.save(validate: false)

      yield if block_given?
      true
    end
  end

  def monitor_params
    permitted = params.require(:monitor).permit(
      :project_id, :name, :kind, :query, :interval_minutes, :range_minutes, :operator, :threshold,
      :notify_by_group, :alert_on_no_data
    )
    permitted[:project] = @current_org.projects.find_by(id: permitted.delete(:project_id)) if permitted.key?(:project_id)
    permitted
  end

  # The Organization's Integrations named, :foreign when any is not one of
  # them, or nil when the form sent none.
  def integrations_param
    permitted = params.require(:monitor).permit(integration_ids: [])
    return unless permitted.key?(:integration_ids)

    ids = permitted[:integration_ids].map(&:to_i).uniq
    found = @current_org.integrations.where(id: ids).to_a
    found.size == ids.size ? found : :foreign
  end

  def monitor_page(monitor)
    monitor_path(monitor, org_slug: @current_org.slug)
  end

  # What a run found, for its notice.
  def found(run)
    if @monitor.kind_match?
      "#{run.matched.to_i} #{'Log'.pluralize(run.matched.to_i)} matched"
    elsif run.firing.to_i.zero?
      "nothing firing"
    else
      "#{run.firing} #{'group'.pluralize(run.firing)} firing"
    end
  end
end
