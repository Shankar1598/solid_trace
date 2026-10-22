# frozen_string_literal: true

# A Dashboard's filters, and #values, which lists the values a select of a
# field's values offers. The values load when its dropdown opens, so a
# Dashboard with filters opens as fast as one without.
class DashboardFiltersController < ApplicationController
  include ChartRunning

  before_action :set_current_org
  before_action :set_time_range, only: :values

  # GET with from and to (ISO 8601): the filter's field's 100 most common
  # values in its Project over that range, as {values: [{value, count}]},
  # most common first. Only a select of a field's values has any, and only
  # while its Project is the Organization's.
  def values
    dashboard = @current_org.dashboards.find(params[:dashboard_id])
    options = dashboard.filters.find { |filter| filter["name"] == params[:name] }&.dig("options")
    raise ActiveRecord::RecordNotFound unless options.is_a?(Hash) && options["from"] == "field"

    project = @current_org.projects.find(options["project_id"])
    render json: { values: ChartRuns.field_values(project, options["field"], from: @from, to: @to) }
  end
end
