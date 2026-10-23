# frozen_string_literal: true

# Settings → Instance: the install's settings, for instance admins. SQL
# queries, the default Retention and log-all-reads apply at once, so they
# change here. Sign-up and everything that needs a restart are shown read-only
# (InstanceSettingsAnswer builds what's shown).
class InstanceSettingsController < ApplicationController
  layout "dashboard"
  before_action :set_current_org
  before_action :require_instance_admin

  def show
    render inertia: "Settings/Instance", props: {
      settings: InstanceSettingsAnswer.new(console_url: request.base_url).as_json,
    }
  end

  def update
  end
end
