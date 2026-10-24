# frozen_string_literal: true

# An Organization's Overview: what came in, what was kept and what it costs,
# over Today, 7 or 30 whole UTC days (?range=today|7d|30d). Any member sees
# it; Overview builds its sections, as it does for get_overview.
class OverviewController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def show
    overview = Overview.new(organization: @current_org, user: current_user, range: params[:range])
    render inertia: "Overview/Show", props: { overview: overview.to_h }
  end
end
