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

  # Only the settings that apply at once change; anything else, Sign-up
  # included, is ignored.
  def update
    settings = InstanceSetting.current
    if settings.update_editable(params.fetch(:instance_settings, {}).permit(*InstanceSetting::EDITABLE))
      redirect_to instance_settings_path(org_slug: @current_org.slug), notice: "Instance settings saved"
    else
      redirect_to instance_settings_path(org_slug: @current_org.slug), inertia: { errors: settings.errors.to_hash }
    end
  end
end
