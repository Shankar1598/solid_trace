# frozen_string_literal: true

# For controllers that run a chart's Query for the browser: a Dashboard's
# charts and their editor's preview, and a Monitor's preview. Each run is one
# of the chart runs ChartRuns allows, and EventStore stops it after
# RUN_TIMEOUT_SECONDS.
module ChartRunning
  extend ActiveSupport::Concern

  # How long a chart's run may take before EventStore stops it.
  RUN_TIMEOUT_SECONDS = 30

  included do
    include EventStoreProxy

    rescue_from EventStore::TimedOut, with: :run_timed_out
    rescue_from ChartRuns::Busy, with: :chart_runs_busy
  end

  private

  # A Query run on a Project over @from to @to, answered as the explorer's
  # run is. A Query that answers Logs rather than a table can't be charted.
  def run_chart(project, query)
    answer = ChartRuns.hold do
      EventStore.query_logs(project_id: project.id, query: query, from: @from, to: @to, timeout_seconds: RUN_TIMEOUT_SECONDS)
    end
    return render json: answer if answer.is_a?(Hash) && answer["table"].is_a?(Hash)

    render json: { error: "This Query answers Logs, not a table. End it with summarize, count or project to chart it." },
      status: :unprocessable_content
  end

  def run_timed_out(error)
    message = error.body["error"].presence || "the run was stopped after #{RUN_TIMEOUT_SECONDS} s, its timeout: try a shorter time range"
    render json: { error: message }, status: :gateway_timeout
  end

  def chart_runs_busy
    render json: { error: "Console is busy running other charts. Refresh in a moment." }, status: :service_unavailable
  end
end
