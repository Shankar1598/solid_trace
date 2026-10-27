# frozen_string_literal: true

# A trace: every Span, Log and Event that shares its id across the
# Organization's Projects. Any member sees it; TraceView builds it, as it
# does for get_trace. A link from an Event or a Log gives its time, so that a
# trace its SDK didn't sample still shows its Logs.
class TracesController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def show
    trace = TraceView.new(organization: @current_org, trace_id: params[:trace_id], time: hint)
    render inertia: "Traces/Show", props: { trace: trace.to_h }
  end

  private

  # The time param, or nil when there's none that reads as a time.
  def hint
    Time.iso8601(params[:time].to_s) if params[:time].present?
  rescue ArgumentError
    nil
  end
end
