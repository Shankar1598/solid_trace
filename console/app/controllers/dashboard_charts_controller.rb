# frozen_string_literal: true

# The charts of a Dashboard, and #data, which runs a chart's saved Query
# through EventStore over the range the page shows. The Query is read from
# the chart, so it never travels in a URL, and runs on the chart's Project
# only while that Project is the Organization's.
class DashboardChartsController < ApplicationController
  include ChartRunning

  before_action :set_current_org
  before_action :set_dashboard
  before_action :set_chart, only: %i[update destroy duplicate data]
  before_action :set_time_range, only: %i[data preview]

  def create
    chart = @dashboard.charts.new(chart_params)
    chart.y = @dashboard.bottom unless chart_params.key?(:y)
    save(chart, notice: "Chart added.", status: :created)
  end

  def update
    @chart.assign_attributes(chart_params)
    save(@chart, notice: "Chart saved.")
  end

  def destroy
    @chart.destroy
    respond_to do |format|
      format.html { redirect_to dashboard_page, notice: "Chart deleted." }
      format.json { head :no_content }
    end
  end

  # A copy of the chart under every chart, its title marked as a copy.
  def duplicate
    copy = @chart.dup
    copy.assign_attributes(title: "#{@chart.title} (copy)".truncate(200), x: 0, y: @dashboard.bottom)
    save(copy, notice: "Chart duplicated.", status: :created)
  end

  # GET with from and to (ISO 8601): the chart's Query run over that range.
  def data
    run_chart(@current_org.projects.find(@chart.project_id), @chart.query)
  end

  # POST with project_id, query, from and to: the chart editor's preview of a
  # Query not yet saved, run as a chart's is, with its timeout and in its cap.
  def preview
    run_chart(@current_org.projects.find(params[:project_id]), params[:query].to_s)
  end

  private

  def set_dashboard
    @dashboard = @current_org.dashboards.find(params[:dashboard_id])
  end

  def set_chart
    @chart = @dashboard.charts.find(params[:id])
  end

  # The Project is looked up in the Organization: another Organization's
  # Project is no Project, so the chart is refused as having none.
  def chart_params
    @chart_params ||= begin
      permitted = params.require(:chart).permit(:project_id, :title, :query, :viz, :x, :y, :w, :h, options: %i[stacked unit decimals])
      permitted[:project] = @current_org.projects.find_by(id: permitted.delete(:project_id)) if permitted.key?(:project_id)
      permitted
    end
  end

  def save(chart, notice:, status: :ok)
    respond_to do |format|
      if chart.save
        format.html { redirect_to dashboard_page, notice: notice }
        format.json { render json: DashboardChartSerializer.new(chart).as_json, status: status }
      else
        format.html { redirect_to dashboard_page, inertia: { errors: chart.errors.to_hash } }
        format.json { render json: { errors: chart.errors.to_hash }, status: :unprocessable_content }
      end
    end
  end

  # The Dashboard's page, over the range it was showing.
  def dashboard_page
    dashboard_path(@dashboard, org_slug: @current_org.slug, **params.permit(:range, :from, :to).to_h.symbolize_keys)
  end
end
