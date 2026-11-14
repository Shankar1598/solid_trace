# frozen_string_literal: true

# Organization settings → Agent sessions → Capture setup (CaptureSetup):
# how an engineer turns on capture on a laptop or a dev VM. Each member of
# the Organization reads it; someone outside it gets 404 (set_current_org).
class CaptureSetupsController < ApplicationController
  layout "dashboard"

  before_action :set_current_org

  def show
    render inertia: "Settings/CaptureSetup", props: CaptureSetup.props(
      organization: @current_org,
      version: Rails.application.config.x.solidtrace_version,
      console_url: PublicUrls.console_url(request.base_url),
      ingest_url: PublicUrls.ingest_url(request.base_url)
    )
  end
end
