# frozen_string_literal: true

# An Organization's Dashboards. Any member can see, make, change and delete
# them. The pages post through Inertia and are redirected; the explorer's Add
# to dashboard posts JSON and gets JSON back.
class DashboardsController < ApplicationController
  include DashboardView

  layout "dashboard"

  before_action :set_current_org
  before_action :set_dashboard, only: %i[show update destroy layout]

  def index
    dashboards = @current_org.dashboards.includes(:created_by).order(:name)
    counts = DashboardChart.where(dashboard_id: dashboards.map(&:id)).group(:dashboard_id).count

    render inertia: "Dashboards/Index", props: {
      dashboards: dashboards.map { |d| DashboardSerializer.new(d, chart_count: counts.fetch(d.id, 0)).as_json },
    }
  end

  def show
    render inertia: "Dashboards/Show", props: {
      dashboard: DashboardSerializer.new(@dashboard, include_charts: true).as_json,
      projects: ProjectOptionSerializer.list(@current_org.projects.order(:name)),
    }
  end

  def create
    dashboard = @current_org.dashboards.new(dashboard_params.merge(created_by: current_user))

    respond_to do |format|
      if dashboard.save
        format.html { redirect_to dashboard_path(dashboard, org_slug: @current_org.slug), notice: "Dashboard created." }
        format.json { render json: DashboardSerializer.new(dashboard).as_json, status: :created }
      else
        format.html { redirect_to dashboards_path(org_slug: @current_org.slug), inertia: { errors: dashboard.errors.to_hash } }
        format.json { render json: { errors: dashboard.errors.to_hash }, status: :unprocessable_content }
      end
    end
  end

  def update
    respond_to do |format|
      if @dashboard.update(dashboard_params)
        format.html { redirect_to dashboard_page(@dashboard), notice: "Dashboard saved." }
        format.json { render json: DashboardSerializer.new(@dashboard).as_json }
      else
        format.html { redirect_to dashboard_page(@dashboard), inertia: { errors: @dashboard.errors.to_hash } }
        format.json { render json: { errors: @dashboard.errors.to_hash }, status: :unprocessable_content }
      end
    end
  end

  def destroy
    @dashboard.destroy
    redirect_to dashboards_path(org_slug: @current_org.slug), notice: "Dashboard deleted."
  end

  # PATCH with layout: [{id, x, y, w, h}], the place of each chart moved or
  # resized. All of them are saved or none.
  def layout
    places = params.permit(layout: %i[id x y w h]).fetch(:layout, [])
    charts = @dashboard.charts.index_by(&:id)
    unknown = places.map { |place| place[:id].to_i }.reject { |id| charts.key?(id) }
    return render json: { error: "Charts #{unknown.join(', ')} aren't on this Dashboard." }, status: :not_found if unknown.any?

    invalid = nil
    DashboardChart.transaction do
      places.each do |place|
        chart = charts.fetch(place[:id].to_i)
        next if chart.update(place.slice(:x, :y, :w, :h))

        invalid = chart
        raise ActiveRecord::Rollback
      end
    end
    return render json: { errors: invalid.errors.to_hash }, status: :unprocessable_content if invalid

    render json: DashboardSerializer.new(@dashboard.reload, include_charts: true).as_json
  end

  private

  def set_dashboard
    @dashboard = @current_org.dashboards.find(params[:id])
  end

  # filters, when given, is the whole list, which replaces the saved one.
  def dashboard_params
    params.require(:dashboard).permit(
      :name, :description, :default_range, :auto_refresh,
      filters: [ :name, :label, :kind, :multiple, { options: [ :from, { items: %i[label value] } ] } ],
    ).to_h
  end
end
