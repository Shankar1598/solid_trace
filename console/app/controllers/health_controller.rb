# frozen_string_literal: true

class HealthController < ApplicationController
  skip_before_action :authenticate_user!, only: [:show]

  # GET /health
  def show
    checks = {
      database: check_database,
      event_store: check_event_store
    }

    status = checks.values.all? { |c| c[:status] == "ok" } ? "healthy" : "unhealthy"
    http_status = status == "healthy" ? :ok : :service_unavailable

    render json: { status: status, checks: checks }, status: http_status
  end

  private

  def check_database
    ActiveRecord::Base.connection.execute("SELECT 1")
    { status: "ok" }
  rescue StandardError => e
    { status: "error", message: e.message }
  end

  def check_event_store
    event_store_url = ENV.fetch("EVENT_STORE_URL", "http://localhost:4000")
    response = RestClient.get("#{event_store_url}/health", timeout: 5)
    result = JSON.parse(response.body)
    { status: result["status"] == "healthy" ? "ok" : "error" }
  rescue StandardError => e
    { status: "error", message: e.message }
  end
end
