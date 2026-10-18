# frozen_string_literal: true

# For controllers that run a chart's Query for the browser: a Dashboard's
# charts and their editor's preview, and a Monitor's preview. Each run is one
# of the chart runs ChartRuns allows, and EventStore stops it after
# EventStoreProxy::RUN_TIMEOUT_SECONDS.
module ChartRunning
  extend ActiveSupport::Concern

  included do
    include EventStoreProxy

    rescue_from ChartRuns::Busy, with: :chart_runs_busy
    rescue_from ChartRuns::NotATable, with: :not_a_table
  end

  private

  # A Query run on a Project over @from to @to, answered as the explorer's
  # run is. A Query that answers Logs rather than a table can't be charted.
  def run_chart(project, query)
    render json: ChartRuns.run_chart(project, query, from: @from, to: @to)
  end

  def not_a_table(error)
    render json: { error: error.message }, status: :unprocessable_content
  end

  def chart_runs_busy
    render json: { error: "Console is busy running other charts. Refresh in a moment." }, status: :service_unavailable
  end
end
