# frozen_string_literal: true

# A trace: every Span that shares its id across the Organization's Projects.
# Any member sees it; TraceView builds it, as it will for get_trace.
class TracesController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def show
    trace = TraceView.new(organization: @current_org, user: current_user, trace_id: params[:trace_id])
    render inertia: "Traces/Show", props: { trace: trace.to_h }
  end
end
