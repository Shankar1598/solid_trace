# frozen_string_literal: true

# For controllers that run Queries through EventStore's query app for the
# browser: EventStore's refusals and failures as JSON, and the from and to of
# a run's time range.
module EventStoreProxy
  extend ActiveSupport::Concern

  # How long a run for the browser may take before EventStore stops it: the
  # explorer's runs, a Dashboard's charts and a Monitor's preview.
  RUN_TIMEOUT_SECONDS = 30

  # How long a SQL query may take before EventStore stops it, from the
  # explorer or the MCP server. EventStore stops SQL by then whatever it is
  # asked: DuckDB gives a query all its threads, so a slow one holds up ingest.
  SQL_TIMEOUT_SECONDS = 10

  included do
    rescue_from EventStore::SQLQueriesOff, with: :sql_queries_off
    rescue_from EventStore::Rejected, with: :eventstore_rejected
    rescue_from EventStore::Unavailable, with: :eventstore_unavailable
    rescue_from EventStore::TimedOut, with: :run_timed_out
  end

  private

  def run_timed_out(error)
    message = error.body.is_a?(Hash) ? EventStore.error_text(error.body) : "the run was stopped at its timeout: try a shorter time range"
    render json: { error: message }, status: :gateway_timeout
  end

  def sql_queries_off(error)
    render json: { error: error.message }, status: :forbidden
  end

  def eventstore_rejected(error)
    render json: error.body, status: :bad_request
  end

  def eventstore_unavailable(error)
    Rails.logger.error(error.message)
    render json: { error: "EventStore is unavailable. Try again shortly." }, status: :bad_gateway
  end

  # The from and to (ISO 8601) of a request that reads a time range.
  def set_time_range
    @from, @to = [ params[:from], params[:to] ].map do |value|
      Time.iso8601(value.to_s)
    rescue ArgumentError
      nil
    end
    render json: { error: "from and to must be ISO 8601 times" }, status: :bad_request unless @from && @to
  end
end
